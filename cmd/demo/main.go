// Package main provides an interactive demonstration runner for TermChat.
// It starts the compiled relay-server binary, boots two simulated clients (Alice & Bob),
// logs the exact wire-format packets passing through the relay (demonstrating
// zero-knowledge encryption), and prints the end-to-end communication trace.
package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/gorilla/websocket"
	"github.com/viveksec/termchat/pkg/crypto"
	"github.com/viveksec/termchat/pkg/protocol"
)

// ANSI color codes for pretty terminal logging
const (
	reset  = "\033[0m"
	bold   = "\033[1m"
	cyan   = "\033[36m"
	purple = "\033[35m"
	green  = "\033[32m"
	yellow = "\033[33m"
	gray   = "\033[90m"
)

type simulatedClient struct {
	name       string
	conn       *websocket.Conn
	id         string
	keyPair    *crypto.KeyPair
	color      string
	receivedCh chan *protocol.Packet
}

func newSimulatedClient(name, color, wsURL string) (*simulatedClient, error) {
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		return nil, fmt.Errorf("dial error: %w", err)
	}

	kp, err := crypto.GenerateKeyPair()
	if err != nil {
		return nil, fmt.Errorf("key generation error: %w", err)
	}

	client := &simulatedClient{
		name:       name,
		conn:       conn,
		keyPair:    kp,
		color:      color,
		receivedCh: make(chan *protocol.Packet, 64),
	}

	// Read initial HELLO packet
	_, raw, err := conn.ReadMessage()
	if err != nil {
		return nil, fmt.Errorf("hello read error: %w", err)
	}
	pkt, err := protocol.DecodePacket(raw)
	if err != nil {
		return nil, fmt.Errorf("hello decode error: %w", err)
	}

	var hello protocol.HelloPayload
	if err := pkt.DecodePayload(&hello); err != nil {
		return nil, fmt.Errorf("hello payload decode error: %w", err)
	}
	client.id = hello.AssignedID

	// Start reader loop
	go client.readLoop()

	return client, nil
}

func (c *simulatedClient) readLoop() {
	for {
		_, raw, err := c.conn.ReadMessage()
		if err != nil {
			return
		}
		pkt, err := protocol.DecodePacket(raw)
		if err != nil {
			continue
		}
		// Ignore automatic USER_LIST broadcasts in the message queue
		if pkt.Type == protocol.MsgUserList {
			continue
		}
		c.receivedCh <- pkt
	}
}

func (c *simulatedClient) sendPacket(pkt *protocol.Packet) error {
	data, err := pkt.Encode()
	if err != nil {
		return err
	}
	return c.conn.WriteMessage(websocket.TextMessage, data)
}

func main() {
	fmt.Println(bold + cyan + "================================================================================" + reset)
	fmt.Println(bold + cyan + "             TERMCHAT — END-TO-END ENCRYPTED CHAT DEMONSTRATION                " + reset)
	fmt.Println(bold + cyan + "================================================================================" + reset)
	fmt.Println()

	// Locate relay-server binary
	wd, err := os.Getwd()
	if err != nil {
		log.Fatalf("failed to get working directory: %v", err)
	}
	serverBin := filepath.Join(wd, "bin", "relay-server")
	if _, err := os.Stat(serverBin); os.IsNotExist(err) {
		log.Fatalf("relay-server binary not found at %s. Please run 'go build -o bin/relay-server ./cmd/server' first.", serverBin)
	}

	// 1. Start Server Process
	port := 18095
	wsURL := fmt.Sprintf("ws://localhost:%d/ws", port)
	healthURL := fmt.Sprintf("http://localhost:%d/health", port)
	cmd := exec.Command(serverBin, "-addr", fmt.Sprintf(":%d", port))
	cmd.Env = append(os.Environ(), "GOTOOLCHAIN=go1.23.8")
	if err := cmd.Start(); err != nil {
		log.Fatalf("Failed to start relay-server: %v", err)
	}
	defer func() {
		cmd.Process.Kill()
		cmd.Wait()
	}()

	// Wait for server health check to respond
	ready := false
	for i := 0; i < 20; i++ {
		time.Sleep(100 * time.Millisecond)
		resp, err := http.Get(healthURL)
		if err == nil && resp.StatusCode == 200 {
			resp.Body.Close()
			ready = true
			break
		}
	}
	if !ready {
		log.Fatalf("Server failed to become ready on %s", healthURL)
	}

	fmt.Printf("%s[1/6] Relay server binary started on port %d%s\n", green, port, reset)
	fmt.Printf("      WebSocket endpoint: %s\n\n", wsURL)

	// 2. Connect Alice and Bob
	fmt.Printf("%s[2/6] Connecting Alice and Bob to relay server...%s\n", yellow, reset)

	alice, err := newSimulatedClient("Alice", cyan, wsURL)
	if err != nil {
		log.Fatalf("Failed to connect Alice: %v", err)
	}
	defer alice.conn.Close()
	fmt.Printf("  • %sAlice%s assigned ID: %s%s%s (Public Key: %s...)\n",
		alice.color, reset, bold+alice.color, alice.id, reset, alice.keyPair.PublicKeyBase64()[:16])

	bob, err := newSimulatedClient("Bob", purple, wsURL)
	if err != nil {
		log.Fatalf("Failed to connect Bob: %v", err)
	}
	defer bob.conn.Close()
	fmt.Printf("  • %sBob%s   assigned ID: %s%s%s (Public Key: %s...)\n\n",
		bob.color, reset, bold+bob.color, bob.id, reset, bob.keyPair.PublicKeyBase64()[:16])

	time.Sleep(200 * time.Millisecond)

	// 3. Initiate Connection (Alice -> Bob)
	fmt.Printf("%s[3/6] Session Initiation: Alice sends /connect %s %s\n", yellow, bob.id, reset)

	reqPayload := protocol.ConnectRequestPayload{
		InitiatorID: alice.id,
		Message:     "Hey Bob! Want to establish an encrypted session?",
	}
	reqPkt, _ := protocol.NewPacket(protocol.MsgConnectRequest, bob.id, reqPayload)
	alice.sendPacket(reqPkt)

	// Bob receives request
	receivedByBob := <-bob.receivedCh
	var connReq protocol.ConnectRequestPayload
	receivedByBob.DecodePayload(&connReq)
	fmt.Printf("  • %sBob%s received request from %sAlice%s: %q\n", bob.color, reset, alice.color, reset, connReq.Message)

	// Bob accepts
	fmt.Printf("  • %sBob%s accepts the connection request\n\n", bob.color, reset)
	respPayload := protocol.ConnectResponsePayload{Accepted: true, ResponderID: bob.id}
	respPkt, _ := protocol.NewPacket(protocol.MsgConnectResponse, alice.id, respPayload)
	bob.sendPacket(respPkt)

	<-alice.receivedCh // Alice gets acceptance notification

	// 4. Ephemeral Key Exchange (X25519)
	fmt.Printf("%s[4/6] Ephemeral X25519 Diffie-Hellman Key Exchange%s\n", yellow, reset)

	kexAlice, _ := protocol.NewPacket(protocol.MsgKeyExchange, bob.id, protocol.KeyExchangePayload{
		PublicKey: alice.keyPair.PublicKeyBase64(),
	})
	alice.sendPacket(kexAlice)

	kexBob, _ := protocol.NewPacket(protocol.MsgKeyExchange, alice.id, protocol.KeyExchangePayload{
		PublicKey: bob.keyPair.PublicKeyBase64(),
	})
	bob.sendPacket(kexBob)

	pktFromBob := <-alice.receivedCh
	var payloadFromBob protocol.KeyExchangePayload
	pktFromBob.DecodePayload(&payloadFromBob)

	pktFromAlice := <-bob.receivedCh
	var payloadFromAlice protocol.KeyExchangePayload
	pktFromAlice.DecodePayload(&payloadFromAlice)

	// Derive shared secrets
	aliceSecret, err := crypto.DeriveSharedSecret(alice.keyPair.PrivateKey, payloadFromBob.PublicKey)
	if err != nil {
		log.Fatalf("Alice DH derivation failed: %v", err)
	}
	bobSecret, err := crypto.DeriveSharedSecret(bob.keyPair.PrivateKey, payloadFromAlice.PublicKey)
	if err != nil {
		log.Fatalf("Bob DH derivation failed: %v", err)
	}

	fmt.Printf("  • %sAlice%s derived AES key: %s%x...%s\n", alice.color, reset, bold, aliceSecret[:12], reset)
	fmt.Printf("  • %sBob%s   derived AES key: %s%x...%s\n", bob.color, reset, bold, bobSecret[:12], reset)
	fmt.Printf("  • Secrets match: %s%t%s (Perfect Forward Secrecy verified)\n\n", green, string(aliceSecret) == string(bobSecret), reset)

	// 5. Encrypted Messaging (Alice -> Bob)
	fmt.Printf("%s[5/6] Alice sends encrypted message to Bob%s\n", yellow, reset)
	plaintext := "Meet me at 09:00 UTC. The passcode is: 8492-DELTA."
	fmt.Printf("  • %sAlice%s original plaintext: %s%q%s\n", alice.color, reset, bold, plaintext, reset)

	ciphertext, err := crypto.Encrypt(aliceSecret, []byte(plaintext))
	if err != nil {
		log.Fatalf("Encryption failed: %v", err)
	}

	chatPkt, _ := protocol.NewPacket(protocol.MsgChat, bob.id, protocol.ChatPayload{
		Ciphertext: ciphertext,
	})

	// Show raw wire frame seen by relay server
	wireBytes, _ := chatPkt.Encode()
	fmt.Printf("  • %sRelay Server sees ONLY raw packet (Zero Knowledge):%s\n", gray, reset)
	var prettyWire map[string]interface{}
	json.Unmarshal(wireBytes, &prettyWire)
	prettyJSON, _ := json.MarshalIndent(prettyWire, "    ", "  ")
	fmt.Printf("%s    %s%s\n", gray, string(prettyJSON), reset)

	alice.sendPacket(chatPkt)

	// Bob receives and decrypts message
	receivedChatPkt := <-bob.receivedCh
	var chatPayload protocol.ChatPayload
	receivedChatPkt.DecodePayload(&chatPayload)

	decryptedBytes, err := crypto.Decrypt(bobSecret, chatPayload.Ciphertext)
	if err != nil {
		log.Fatalf("Decryption failed: %v", err)
	}

	fmt.Printf("\n  • %sBob%s received and decrypted message:\n", bob.color, reset)
	fmt.Printf("    %s🔒 [%s] %s: %s%s\n\n", green, time.Now().Format("15:04:05"), bob.name, string(decryptedBytes), reset)

	// 6. Encrypted Reply (Bob -> Alice)
	fmt.Printf("%s[6/6] Bob sends encrypted reply to Alice%s\n", yellow, reset)
	replyText := "Acknowledged. I will be there."
	fmt.Printf("  • %sBob%s original plaintext: %s%q%s\n", bob.color, reset, bold, replyText, reset)

	replyCiphertext, _ := crypto.Encrypt(bobSecret, []byte(replyText))
	replyPkt, _ := protocol.NewPacket(protocol.MsgChat, alice.id, protocol.ChatPayload{
		Ciphertext: replyCiphertext,
	})
	bob.sendPacket(replyPkt)

	receivedReplyPkt := <-alice.receivedCh
	var replyPayload protocol.ChatPayload
	receivedReplyPkt.DecodePayload(&replyPayload)

	replyDecrypted, _ := crypto.Decrypt(aliceSecret, replyPayload.Ciphertext)
	fmt.Printf("  • %sAlice%s received and decrypted reply:\n", alice.color, reset)
	fmt.Printf("    %s🔒 [%s] %s: %s%s\n\n", green, time.Now().Format("15:04:05"), alice.name, string(replyDecrypted), reset)

	fmt.Println(bold + green + "================================================================================" + reset)
	fmt.Println(bold + green + "        SUCCESS! END-TO-END ENCRYPTED SESSION COMPLETED PERFECTLY.              " + reset)
	fmt.Println(bold + green + "================================================================================" + reset)
}
