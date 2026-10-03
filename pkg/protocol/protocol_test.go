package protocol_test

import (
	"testing"

	"github.com/viveksec/termchat/pkg/protocol"
)

func TestPacketEncodeDecode(t *testing.T) {
	hello := protocol.HelloPayload{
		AssignedID:    "AB1234",
		ServerVersion: "1.0.0",
	}

	pkt, err := protocol.NewPacket(protocol.MsgHello, "", hello)
	if err != nil {
		t.Fatalf("failed to create packet: %v", err)
	}

	encoded, err := pkt.Encode()
	if err != nil {
		t.Fatalf("failed to encode packet: %v", err)
	}

	decoded, err := protocol.DecodePacket(encoded)
	if err != nil {
		t.Fatalf("failed to decode packet: %v", err)
	}

	if decoded.Type != protocol.MsgHello {
		t.Errorf("expected type %s, got %s", protocol.MsgHello, decoded.Type)
	}

	var decodedHello protocol.HelloPayload
	if err := decoded.DecodePayload(&decodedHello); err != nil {
		t.Fatalf("failed to decode payload: %v", err)
	}

	if decodedHello.AssignedID != "AB1234" {
		t.Errorf("expected ID AB1234, got %s", decodedHello.AssignedID)
	}
}

func TestChatPacketPayload(t *testing.T) {
	chat := protocol.ChatPayload{
		Ciphertext: "dGVzdF9jaXBoZXJ0ZXh0",
	}

	pkt, err := protocol.NewPacket(protocol.MsgChat, "TARGET1", chat)
	if err != nil {
		t.Fatalf("failed to create chat packet: %v", err)
	}

	if pkt.TargetID != "TARGET1" {
		t.Errorf("expected target TARGET1, got %s", pkt.TargetID)
	}

	encoded, _ := pkt.Encode()
	decoded, _ := protocol.DecodePacket(encoded)

	var decodedChat protocol.ChatPayload
	if err := decoded.DecodePayload(&decodedChat); err != nil {
		t.Fatalf("failed to decode chat payload: %v", err)
	}

	if decodedChat.Ciphertext != "dGVzdF9jaXBoZXJ0ZXh0" {
		t.Errorf("ciphertext mismatch: %s", decodedChat.Ciphertext)
	}
}
