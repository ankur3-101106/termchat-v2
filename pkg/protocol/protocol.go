// Package protocol defines the wire protocol for the terminal chat application.
// All messages exchanged between clients and the relay server conform to the
// Packet structure. The relay server only inspects the Type and TargetID fields
// to route packets — it never decrypts or reads the Payload field.
package protocol

import (
	"encoding/json"
	"fmt"
	"time"
)

// MessageType identifies the intent of a Packet.
type MessageType string

const (
	// MsgHello is sent by the server to a newly connected client.
	// Payload contains the assigned short ID.
	MsgHello MessageType = "HELLO"

	// MsgUserList is broadcast by the server whenever the set of connected
	// clients changes. Payload contains a JSON array of short IDs.
	MsgUserList MessageType = "USER_LIST"

	// MsgConnectRequest is sent by the initiating client to request a
	// 1-on-1 chat session with another user.
	MsgConnectRequest MessageType = "CONNECT_REQUEST"

	// MsgConnectResponse is sent by the target client to accept or reject
	// a connection request.
	MsgConnectResponse MessageType = "CONNECT_RESPONSE"

	// MsgKeyExchange is used to exchange X25519 public keys after both
	// parties have agreed to chat. The relay server never processes the
	// embedded public key — it is opaque binary data from the server's POV.
	MsgKeyExchange MessageType = "KEY_EXCHANGE"

	// MsgChat carries an end-to-end encrypted chat message. The Payload
	// field contains a base64-encoded AES-256-GCM ciphertext. The relay
	// server forwards it verbatim without ever inspecting it.
	MsgChat MessageType = "CHAT"

	// MsgFileChunk carries an end-to-end encrypted file chunk payload.
	MsgFileChunk MessageType = "FILE_CHUNK"

	// MsgDisconnect is sent by a client to notify its peer that the session
	// has ended, or by the server to notify a client that its peer disconnected.
	MsgDisconnect MessageType = "DISCONNECT"

	// MsgError is sent by the server to notify a client of a protocol error.
	MsgError MessageType = "ERROR"

	// MsgPing is sent by the client to keep the WebSocket connection alive.
	MsgPing MessageType = "PING"

	// MsgPong is the server's response to a ping.
	MsgPong MessageType = "PONG"
)

// Packet is the universal wire-format envelope for all messages. The relay
// server routes packets based solely on TargetID — it never inspects Payload.
type Packet struct {
	// Type identifies the message intent. Required in all packets.
	Type MessageType `json:"type"`

	// SenderID is populated by the server when forwarding client messages.
	// Clients should not set this field; the server overwrites it.
	SenderID string `json:"sender_id,omitempty"`

	// TargetID is the short ID of the intended recipient. Required for
	// all client-to-client messages. Not used in server-to-client messages.
	TargetID string `json:"target_id,omitempty"`

	// Payload carries the message body, which varies by Type:
	//   - MsgHello:           HelloPayload
	//   - MsgUserList:        UserListPayload
	//   - MsgConnectRequest:  ConnectRequestPayload
	//   - MsgConnectResponse: ConnectResponsePayload
	//   - MsgKeyExchange:     KeyExchangePayload
	//   - MsgChat:            ChatPayload
	//   - MsgDisconnect:      DisconnectPayload
	//   - MsgError:           ErrorPayload
	Payload json.RawMessage `json:"payload,omitempty"`

	// Timestamp is set by the sender when the packet is created.
	Timestamp time.Time `json:"timestamp"`
}

// NewPacket creates a Packet with the current UTC timestamp, encoding the
// provided payload value as JSON. Returns an error if marshalling fails.
func NewPacket(msgType MessageType, targetID string, payload interface{}) (*Packet, error) {
	var raw json.RawMessage
	if payload != nil {
		var err error
		raw, err = json.Marshal(payload)
		if err != nil {
			return nil, fmt.Errorf("protocol: failed to marshal payload for %s: %w", msgType, err)
		}
	}
	return &Packet{
		Type:      msgType,
		TargetID:  targetID,
		Payload:   raw,
		Timestamp: time.Now().UTC(),
	}, nil
}

// Encode serialises the packet to JSON bytes ready for transmission.
func (p *Packet) Encode() ([]byte, error) {
	data, err := json.Marshal(p)
	if err != nil {
		return nil, fmt.Errorf("protocol: failed to encode packet: %w", err)
	}
	return data, nil
}

// DecodePacket deserialises a JSON byte slice into a Packet.
func DecodePacket(data []byte) (*Packet, error) {
	var p Packet
	if err := json.Unmarshal(data, &p); err != nil {
		return nil, fmt.Errorf("protocol: failed to decode packet: %w", err)
	}
	return &p, nil
}

// DecodePayload unmarshals the Packet's Payload into the provided target value.
func (p *Packet) DecodePayload(target interface{}) error {
	if p.Payload == nil {
		return fmt.Errorf("protocol: packet of type %s has no payload", p.Type)
	}
	if err := json.Unmarshal(p.Payload, target); err != nil {
		return fmt.Errorf("protocol: failed to decode payload for %s: %w", p.Type, err)
	}
	return nil
}

// ─────────────────────────────────────────────
// Payload types for each MessageType
// ─────────────────────────────────────────────

// HelloPayload is the payload for MsgHello packets.
type HelloPayload struct {
	// AssignedID is the 6-character short ID assigned by the relay server.
	AssignedID string `json:"assigned_id"`
	// ServerVersion communicates the relay server version to the client.
	ServerVersion string `json:"server_version"`
}

// UserListPayload is the payload for MsgUserList packets.
type UserListPayload struct {
	// Users is the list of short IDs currently connected to the relay.
	Users []string `json:"users"`
}

// ConnectRequestPayload is the payload for MsgConnectRequest packets.
type ConnectRequestPayload struct {
	// InitiatorID is the short ID of the user requesting a chat session.
	InitiatorID string `json:"initiator_id"`
	// Message is an optional human-readable greeting shown in the prompt.
	Message string `json:"message"`
}

// ConnectResponsePayload is the payload for MsgConnectResponse packets.
type ConnectResponsePayload struct {
	// Accepted indicates whether the target user accepted the request.
	Accepted bool `json:"accepted"`
	// ResponderID is the short ID of the user who responded.
	ResponderID string `json:"responder_id"`
	// Reason is an optional human-readable explanation for a rejection.
	Reason string `json:"reason,omitempty"`
}

// KeyExchangePayload is the payload for MsgKeyExchange packets.
// The relay server forwards this opaquely — it never sees the public key.
type KeyExchangePayload struct {
	// PublicKey is the sender's X25519 public key encoded as base64.
	PublicKey string `json:"public_key"`
}

// ChatPayload is the payload for MsgChat packets.
// The relay server forwards this opaquely — it only sees base64 ciphertext.
type ChatPayload struct {
	// Ciphertext is the AES-256-GCM encrypted message body as base64.
	// Format: base64(nonce || ciphertext || tag)
	Ciphertext string `json:"ciphertext"`
}

// FileChunkPayload is the payload for MsgFileChunk packets.
type FileChunkPayload struct {
	Filename   string `json:"filename"`
	ChunkIndex int    `json:"chunk_index"`
	TotalChunks int   `json:"total_chunks"`
	Ciphertext string `json:"ciphertext"`
}

// DisconnectPayload is the payload for MsgDisconnect packets.
type DisconnectPayload struct {
	// Reason describes why the session was terminated.
	Reason string `json:"reason,omitempty"`
}

// ErrorPayload is the payload for MsgError packets.
type ErrorPayload struct {
	// Code is a machine-readable error identifier.
	Code string `json:"code"`
	// Message is a human-readable error description.
	Message string `json:"message"`
}

// ─────────────────────────────────────────────
// Error code constants
// ─────────────────────────────────────────────

const (
	ErrCodeTargetNotFound  = "TARGET_NOT_FOUND"
	ErrCodeTargetBusy      = "TARGET_BUSY"
	ErrCodeSelfConnect     = "SELF_CONNECT"
	ErrCodeInvalidPacket   = "INVALID_PACKET"
	ErrCodeRateLimited     = "RATE_LIMITED"
)
