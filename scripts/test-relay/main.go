// Quick integration test against a running relay (Go server or Cloudflare worker).
// Usage: go run ./scripts/test-relay ws://127.0.0.1:8787/ws
package main

import (
	"fmt"
	"os"
	"time"

	"github.com/gorilla/websocket"
	"github.com/viveksec/termchat/pkg/crypto"
	"github.com/viveksec/termchat/pkg/protocol"
)

func main() {
	url := "ws://127.0.0.1:8787/ws"
	if len(os.Args) > 1 {
		url = os.Args[1]
	}

	fmt.Printf("testing relay at %s\n", url)

	connA, idA, err := connect(url)
	if err != nil {
		fmt.Fprintf(os.Stderr, "client A failed: %v\n", err)
		os.Exit(1)
	}
	defer connA.Close()
	fmt.Printf("client A connected: %s\n", idA)

	connB, idB, err := connect(url)
	if err != nil {
		fmt.Fprintf(os.Stderr, "client B failed: %v\n", err)
		os.Exit(1)
	}
	defer connB.Close()
	fmt.Printf("client B connected: %s\n", idB)

	drain(connA)

	// Connect request A -> B
	req, _ := protocol.NewPacket(protocol.MsgConnectRequest, idB,
		protocol.ConnectRequestPayload{InitiatorID: idA, Message: "hello"})
	send(connA, req)

	pkt := read(connB)
	if pkt.Type != protocol.MsgConnectRequest || pkt.SenderID != idA {
		fail("expected CONNECT_REQUEST from A on B")
	}

	// B accepts
	resp, _ := protocol.NewPacket(protocol.MsgConnectResponse, idA,
		protocol.ConnectResponsePayload{Accepted: true, ResponderID: idB})
	send(connB, resp)

	pkt = read(connA)
	if pkt.Type != protocol.MsgConnectResponse {
		fail("expected CONNECT_RESPONSE on A")
	}

	// Key exchange + encrypted chat
	kpA, _ := crypto.GenerateKeyPair()
	kpB, _ := crypto.GenerateKeyPair()

	kexA, _ := protocol.NewPacket(protocol.MsgKeyExchange, idB,
		protocol.KeyExchangePayload{PublicKey: kpA.PublicKeyBase64()})
	send(connA, kexA)

	kexB, _ := protocol.NewPacket(protocol.MsgKeyExchange, idA,
		protocol.KeyExchangePayload{PublicKey: kpB.PublicKeyBase64()})
	send(connB, kexB)

	pkt = read(connA)
	var kexPayloadA protocol.KeyExchangePayload
	pkt.DecodePayload(&kexPayloadA)

	pkt = read(connB)
	var kexPayloadB protocol.KeyExchangePayload
	pkt.DecodePayload(&kexPayloadB)

	secretA, _ := crypto.DeriveSharedSecret(kpA.PrivateKey, kexPayloadA.PublicKey)
	secretB, _ := crypto.DeriveSharedSecret(kpB.PrivateKey, kexPayloadB.PublicKey)

	plaintext := "remote relay works"
	ciphertext, _ := crypto.Encrypt(secretA, []byte(plaintext))
	chat, _ := protocol.NewPacket(protocol.MsgChat, idB, protocol.ChatPayload{Ciphertext: ciphertext})
	send(connA, chat)

	pkt = read(connB)
	var chatPayload protocol.ChatPayload
	pkt.DecodePayload(&chatPayload)
	decrypted, err := crypto.Decrypt(secretB, chatPayload.Ciphertext)
	if err != nil {
		fail("decrypt failed: " + err.Error())
	}
	if string(decrypted) != plaintext {
		fail(fmt.Sprintf("expected %q got %q", plaintext, string(decrypted)))
	}

	fmt.Println("PASS: full encrypted session over relay")
}

func connect(url string) (*websocket.Conn, string, error) {
	conn, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		return nil, "", err
	}
	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	_, raw, err := conn.ReadMessage()
	if err != nil {
		conn.Close()
		return nil, "", err
	}
	pkt, err := protocol.DecodePacket(raw)
	if err != nil {
		conn.Close()
		return nil, "", err
	}
	if pkt.Type != protocol.MsgHello {
		conn.Close()
		return nil, "", fmt.Errorf("expected HELLO, got %s", pkt.Type)
	}
	var hello protocol.HelloPayload
	if err := pkt.DecodePayload(&hello); err != nil {
		conn.Close()
		return nil, "", err
	}
	drain(conn)
	conn.SetReadDeadline(time.Time{})
	return conn, hello.AssignedID, nil
}

func drain(conn *websocket.Conn) {
	conn.SetReadDeadline(time.Now().Add(1 * time.Second))
	conn.ReadMessage()
	conn.SetReadDeadline(time.Time{})
}

func send(conn *websocket.Conn, pkt *protocol.Packet) {
	data, _ := pkt.Encode()
	conn.WriteMessage(websocket.TextMessage, data)
}

func read(conn *websocket.Conn) *protocol.Packet {
	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	defer conn.SetReadDeadline(time.Time{})
	_, raw, err := conn.ReadMessage()
	if err != nil {
		fail("read failed: " + err.Error())
	}
	pkt, err := protocol.DecodePacket(raw)
	if err != nil {
		fail("decode failed: " + err.Error())
	}
	return pkt
}

func fail(msg string) {
	fmt.Fprintf(os.Stderr, "FAIL: %s\n", msg)
	os.Exit(1)
}
