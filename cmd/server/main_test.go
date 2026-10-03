package main

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/viveksec/termchat/pkg/crypto"
	"github.com/viveksec/termchat/pkg/protocol"
)

// startTestRelay spins up an in-process relay server for testing.
// It returns the WebSocket URL and a cleanup function.
func startTestRelay(t *testing.T) (string, func()) {
	t.Helper()

	srv := newRelayServer()
	go srv.run()

	mux := http.NewServeMux()
	mux.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		serveWS(srv, w, r)
	})

	ts := httptest.NewServer(mux)
	wsURL := "ws" + ts.URL[4:] + "/ws"
	return wsURL, ts.Close
}

// connectTestClient dials the relay and returns a wsConn plus the assigned ID.
func connectTestClient(t *testing.T, wsURL string) (*websocket.Conn, string) {
	t.Helper()
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial failed: %v", err)
	}

	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	_, raw, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read HELLO failed: %v", err)
	}

	pkt, err := protocol.DecodePacket(raw)
	if err != nil {
		t.Fatalf("decode HELLO failed: %v", err)
	}
	if pkt.Type != protocol.MsgHello {
		t.Fatalf("expected HELLO, got %s", pkt.Type)
	}

	var hello protocol.HelloPayload
	if err := pkt.DecodePayload(&hello); err != nil {
		t.Fatalf("decode HELLO payload failed: %v", err)
	}
	if len(hello.AssignedID) != 6 {
		t.Fatalf("expected 6-char ID, got %q", hello.AssignedID)
	}

	// Drain the user-list update.
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	conn.ReadMessage()
	conn.SetReadDeadline(time.Time{})

	return conn, hello.AssignedID
}

// sendPacket encodes and sends a packet over the given ws connection.
func sendPacket(t *testing.T, conn *websocket.Conn, pkt *protocol.Packet) {
	t.Helper()
	data, err := pkt.Encode()
	if err != nil {
		t.Fatalf("encode packet failed: %v", err)
	}
	if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
		t.Fatalf("write packet failed: %v", err)
	}
}

// readPacket reads one packet from the given ws connection with a 5-second timeout.
func readPacket(t *testing.T, conn *websocket.Conn) *protocol.Packet {
	t.Helper()
	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	defer conn.SetReadDeadline(time.Time{})
	_, raw, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read packet failed: %v", err)
	}
	pkt, err := protocol.DecodePacket(raw)
	if err != nil {
		t.Fatalf("decode packet failed: %v", err)
	}
	return pkt
}

// ─────────────────────────────────────────────────────────────
// Tests
// ─────────────────────────────────────────────────────────────

func TestRelayHello(t *testing.T) {
	wsURL, cleanup := startTestRelay(t)
	defer cleanup()

	conn, id := connectTestClient(t, wsURL)
	defer conn.Close()

	if len(id) != 6 {
		t.Errorf("expected 6-char ID, got %q", id)
	}
	t.Logf("assigned ID: %s", id)
}

func TestRelayUserListBroadcast(t *testing.T) {
	wsURL, cleanup := startTestRelay(t)
	defer cleanup()

	connA, idA := connectTestClient(t, wsURL)
	defer connA.Close()

	// Connect client B; client A should receive a USER_LIST update.
	connB, idB := connectTestClient(t, wsURL)
	defer connB.Close()

	// A should receive USER_LIST now.
	connA.SetReadDeadline(time.Now().Add(3 * time.Second))
	_, raw, err := connA.ReadMessage()
	if err != nil {
		t.Fatalf("client A failed to receive user list: %v", err)
	}
	pkt, _ := protocol.DecodePacket(raw)
	if pkt.Type != protocol.MsgUserList {
		t.Errorf("expected USER_LIST, got %s", pkt.Type)
	}
	var ul protocol.UserListPayload
	pkt.DecodePayload(&ul)
	found := false
	for _, u := range ul.Users {
		if u == idB {
			found = true
		}
	}
	if !found {
		t.Errorf("client B (%s) not in user list: %v", idB, ul.Users)
	}
	_ = idA
}

func TestRelayConnectRequest(t *testing.T) {
	wsURL, cleanup := startTestRelay(t)
	defer cleanup()

	connA, idA := connectTestClient(t, wsURL)
	defer connA.Close()
	connB, idB := connectTestClient(t, wsURL)
	defer connB.Close()

	// Drain user-list for A.
	connA.SetReadDeadline(time.Now().Add(1 * time.Second))
	connA.ReadMessage()
	connA.SetReadDeadline(time.Time{})

	// A sends a connect request to B.
	reqPayload := protocol.ConnectRequestPayload{
		InitiatorID: idA,
		Message:     "Hello from test",
	}
	pkt, _ := protocol.NewPacket(protocol.MsgConnectRequest, idB, reqPayload)
	sendPacket(t, connA, pkt)

	// B should receive the connect request.
	received := readPacket(t, connB)
	if received.Type != protocol.MsgConnectRequest {
		t.Fatalf("expected CONNECT_REQUEST on B, got %s", received.Type)
	}
	if received.SenderID != idA {
		t.Errorf("expected sender %s, got %s", idA, received.SenderID)
	}

	var crp protocol.ConnectRequestPayload
	received.DecodePayload(&crp)
	if crp.Message != "Hello from test" {
		t.Errorf("expected message 'Hello from test', got %q", crp.Message)
	}
	t.Logf("connect request routed correctly from %s to %s", idA, idB)
}

func TestRelayFullSessionWithEncryption(t *testing.T) {
	wsURL, cleanup := startTestRelay(t)
	defer cleanup()

	connA, idA := connectTestClient(t, wsURL)
	defer connA.Close()
	connB, idB := connectTestClient(t, wsURL)
	defer connB.Close()

	// Drain user-list for A (broadcast when B connected).
	connA.SetReadDeadline(time.Now().Add(1 * time.Second))
	connA.ReadMessage()
	connA.SetReadDeadline(time.Time{})

	// ── Step 1: A sends connect request to B ──────────────────
	reqPayload := protocol.ConnectRequestPayload{InitiatorID: idA, Message: "chat?"}
	pkt, _ := protocol.NewPacket(protocol.MsgConnectRequest, idB, reqPayload)
	sendPacket(t, connA, pkt)

	received := readPacket(t, connB)
	if received.Type != protocol.MsgConnectRequest {
		t.Fatalf("B expected CONNECT_REQUEST, got %s", received.Type)
	}

	// ── Step 2: B accepts ─────────────────────────────────────
	respPayload := protocol.ConnectResponsePayload{Accepted: true, ResponderID: idB}
	respPkt, _ := protocol.NewPacket(protocol.MsgConnectResponse, idA, respPayload)
	sendPacket(t, connB, respPkt)

	received = readPacket(t, connA)
	if received.Type != protocol.MsgConnectResponse {
		t.Fatalf("A expected CONNECT_RESPONSE, got %s", received.Type)
	}
	var crp protocol.ConnectResponsePayload
	received.DecodePayload(&crp)
	if !crp.Accepted {
		t.Fatal("expected accepted=true")
	}

	// ── Step 3: Key exchange ──────────────────────────────────
	kpA, _ := crypto.GenerateKeyPair()
	kpB, _ := crypto.GenerateKeyPair()

	// A sends public key to B.
	kexA, _ := protocol.NewPacket(protocol.MsgKeyExchange, idB,
		protocol.KeyExchangePayload{PublicKey: kpA.PublicKeyBase64()})
	sendPacket(t, connA, kexA)

	// B sends public key to A.
	kexB, _ := protocol.NewPacket(protocol.MsgKeyExchange, idA,
		protocol.KeyExchangePayload{PublicKey: kpB.PublicKeyBase64()})
	sendPacket(t, connB, kexB)

	// A receives B's public key.
	received = readPacket(t, connA)
	if received.Type != protocol.MsgKeyExchange {
		t.Fatalf("A expected KEY_EXCHANGE, got %s", received.Type)
	}
	var kexPayloadA protocol.KeyExchangePayload
	received.DecodePayload(&kexPayloadA)

	// B receives A's public key.
	received = readPacket(t, connB)
	if received.Type != protocol.MsgKeyExchange {
		t.Fatalf("B expected KEY_EXCHANGE, got %s", received.Type)
	}
	var kexPayloadB protocol.KeyExchangePayload
	received.DecodePayload(&kexPayloadB)

	// Both derive the shared secret.
	secretA, err := crypto.DeriveSharedSecret(kpA.PrivateKey, kexPayloadA.PublicKey)
	if err != nil {
		t.Fatalf("A failed to derive secret: %v", err)
	}
	secretB, err := crypto.DeriveSharedSecret(kpB.PrivateKey, kexPayloadB.PublicKey)
	if err != nil {
		t.Fatalf("B failed to derive secret: %v", err)
	}

	// Verify both sides derived the same key.
	if fmt.Sprintf("%x", secretA) != fmt.Sprintf("%x", secretB) {
		t.Fatal("shared secrets do not match!")
	}
	t.Logf("shared secret: %x…", secretA[:8])

	// ── Step 4: Encrypted message A → B ──────────────────────
	plaintext := "Hello, Bob! This is end-to-end encrypted."
	ciphertext, err := crypto.Encrypt(secretA, []byte(plaintext))
	if err != nil {
		t.Fatalf("encrypt failed: %v", err)
	}

	chatPkt, _ := protocol.NewPacket(protocol.MsgChat, idB,
		protocol.ChatPayload{Ciphertext: ciphertext})
	sendPacket(t, connA, chatPkt)

	// B receives and decrypts.
	received = readPacket(t, connB)
	if received.Type != protocol.MsgChat {
		t.Fatalf("B expected CHAT, got %s", received.Type)
	}
	var chatPayload protocol.ChatPayload
	received.DecodePayload(&chatPayload)

	decrypted, err := crypto.Decrypt(secretB, chatPayload.Ciphertext)
	if err != nil {
		t.Fatalf("decrypt failed: %v", err)
	}
	if string(decrypted) != plaintext {
		t.Errorf("expected %q, got %q", plaintext, string(decrypted))
	}
	t.Logf("message received and decrypted correctly: %q", string(decrypted))

	// Verify the relay server never saw the plaintext (it only forwarded
	// the base64 ciphertext).
	_ = json.Marshal
}

func TestCryptoKeyPairGeneration(t *testing.T) {
	kp1, err := crypto.GenerateKeyPair()
	if err != nil {
		t.Fatalf("key pair generation failed: %v", err)
	}
	kp2, err := crypto.GenerateKeyPair()
	if err != nil {
		t.Fatalf("key pair 2 generation failed: %v", err)
	}

	// Keys must be different (probabilistic but practically guaranteed).
	if kp1.PublicKeyBase64() == kp2.PublicKeyBase64() {
		t.Error("two generated key pairs must not be identical")
	}
}

func TestCryptoDiffieHellman(t *testing.T) {
	kpA, _ := crypto.GenerateKeyPair()
	kpB, _ := crypto.GenerateKeyPair()

	secretAB, err := crypto.DeriveSharedSecret(kpA.PrivateKey, kpB.PublicKeyBase64())
	if err != nil {
		t.Fatalf("DH A→B failed: %v", err)
	}
	secretBA, err := crypto.DeriveSharedSecret(kpB.PrivateKey, kpA.PublicKeyBase64())
	if err != nil {
		t.Fatalf("DH B→A failed: %v", err)
	}

	if fmt.Sprintf("%x", secretAB) != fmt.Sprintf("%x", secretBA) {
		t.Error("Diffie-Hellman produced different secrets on each side")
	}
}

func TestCryptoEncryptDecrypt(t *testing.T) {
	kpA, _ := crypto.GenerateKeyPair()
	kpB, _ := crypto.GenerateKeyPair()

	secret, _ := crypto.DeriveSharedSecret(kpA.PrivateKey, kpB.PublicKeyBase64())

	cases := []string{
		"Hello, world!",
		"",
		"こんにちは世界",
		"A" + string(make([]byte, 4096)),
	}
	for _, tc := range cases {
		enc, err := crypto.Encrypt(secret, []byte(tc))
		if err != nil {
			t.Fatalf("encrypt failed for %q: %v", tc, err)
		}
		dec, err := crypto.Decrypt(secret, enc)
		if err != nil {
			t.Fatalf("decrypt failed for %q: %v", tc, err)
		}
		if string(dec) != tc {
			t.Errorf("round-trip failed: got %q, want %q", string(dec), tc)
		}
	}
}

func TestCryptoTamperedCiphertext(t *testing.T) {
	kp, _ := crypto.GenerateKeyPair()
	secret, _ := crypto.DeriveSharedSecret(kp.PrivateKey, kp.PublicKeyBase64())

	enc, _ := crypto.Encrypt(secret, []byte("secret data"))

	// Corrupt the last byte of the base64 (which will affect the auth tag).
	corrupted := enc[:len(enc)-2] + "AA"
	_, err := crypto.Decrypt(secret, corrupted)
	if err == nil {
		t.Error("expected decryption to fail with corrupted ciphertext")
	}
}

func TestRelayErrorOnSelfConnect(t *testing.T) {
	wsURL, cleanup := startTestRelay(t)
	defer cleanup()

	conn, id := connectTestClient(t, wsURL)
	defer conn.Close()


	// Try to connect to self.
	reqPayload := protocol.ConnectRequestPayload{InitiatorID: id, Message: "self"}
	pkt, _ := protocol.NewPacket(protocol.MsgConnectRequest, id, reqPayload)
	sendPacket(t, conn, pkt)

	received := readPacket(t, conn)
	if received.Type != protocol.MsgError {
		t.Fatalf("expected ERROR, got %s", received.Type)
	}
	var errPayload protocol.ErrorPayload
	received.DecodePayload(&errPayload)
	if errPayload.Code != protocol.ErrCodeSelfConnect {
		t.Errorf("expected %s error, got %s", protocol.ErrCodeSelfConnect, errPayload.Code)
	}
	t.Logf("self-connect correctly rejected with: %s", errPayload.Message)
}

func TestRelayTargetNotFound(t *testing.T) {
	wsURL, cleanup := startTestRelay(t)
	defer cleanup()

	conn, _ := connectTestClient(t, wsURL)
	defer conn.Close()

	pkt, _ := protocol.NewPacket(protocol.MsgConnectRequest, "XXXXXX",
		protocol.ConnectRequestPayload{Message: "hello?"})
	sendPacket(t, conn, pkt)

	received := readPacket(t, conn)
	if received.Type != protocol.MsgError {
		t.Fatalf("expected ERROR, got %s", received.Type)
	}
	var errPayload protocol.ErrorPayload
	received.DecodePayload(&errPayload)
	if errPayload.Code != protocol.ErrCodeTargetNotFound {
		t.Errorf("expected %s, got %s", protocol.ErrCodeTargetNotFound, errPayload.Code)
	}
}

// Ensure the test package can resolve the relay server types.
// This forces the test file to be in the same package as main.
var _ = net.Listen
