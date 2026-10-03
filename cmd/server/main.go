// Package main implements the zero-knowledge WebSocket relay server for the
// terminal chat application. The server assigns each connecting client a unique
// 6-character short ID, maintains an in-memory registry of active connections,
// broadcasts user-list updates, and routes encrypted Packet envelopes between
// clients without ever inspecting message payloads.
package main

import (
	"crypto/rand"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"math/big"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/gorilla/websocket"
	"github.com/ankur3-101106/termchat-v2/pkg/protocol"
)

const (
	serverVersion = "1.0.1"

	// WebSocket tunables.
	// pongWait is intentionally short (45s) so we detect dead connections well
	// before cloud proxies (e.g. Cloudflare) silently drop the TCP stream at ~60s.
	// pingPeriod is 30s — well inside the proxy timeout — giving the client
	// ample time to respond before we declare the connection stale.
	writeWait      = 10 * time.Second
	pongWait       = 45 * time.Second
	pingPeriod     = 30 * time.Second
	maxMessageSize = 64 * 1024 // 64 KiB — more than enough for encrypted messages

	// Short ID character set: unambiguous alphanumeric characters only.
	shortIDChars  = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"
	shortIDLength = 6
)

// upgrader upgrades HTTP connections to WebSocket. It permits all origins
// in this implementation; in production, restrict to your known client origins.
var upgrader = websocket.Upgrader{
	ReadBufferSize:  4096,
	WriteBufferSize: 4096,
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

// client represents a single connected WebSocket client.
type client struct {
	id      string
	conn    *websocket.Conn
	send    chan []byte
	server  *relayServer
	mu      sync.Mutex
	peerID  string // non-empty when this client is in an active session
}

// relayServer maintains the global client registry and orchestrates routing.
type relayServer struct {
	clients   map[string]*client
	mu        sync.RWMutex
	register  chan *client
	unregister chan *client
	broadcast chan []byte
}

// newRelayServer initialises a relay server with empty state.
func newRelayServer() *relayServer {
	return &relayServer{
		clients:    make(map[string]*client),
		register:   make(chan *client, 32),
		unregister: make(chan *client, 32),
		broadcast:  make(chan []byte, 128),
	}
}

// run is the central event loop for the relay server. It must be called in
// a dedicated goroutine and runs until the context is cancelled.
func (s *relayServer) run() {
	for {
		select {
		case c := <-s.register:
			s.mu.Lock()
			if old, ok := s.clients[c.id]; ok {
				c.peerID = old.peerID
				old.conn.Close()
			}
			s.clients[c.id] = c
			s.mu.Unlock()
			log.Printf("[relay] client connected: %s (total: %d)", c.id, s.clientCount())
			s.broadcastUserList()

		case c := <-s.unregister:
			s.mu.Lock()
			existing, ok := s.clients[c.id]
			if ok && existing == c {
				delete(s.clients, c.id)
				close(c.send)
				peerID := c.peerID
				s.mu.Unlock()

				log.Printf("[relay] client disconnected: %s (total: %d)", c.id, s.clientCount())

				// Notify the peer that their session partner has disconnected.
				if peerID != "" {
					s.notifyPeerDisconnected(peerID, c.id)
				}

				s.broadcastUserList()
			} else if ok && existing != c {
				// A newer connection has already replaced this one (session resumption).
				// Don't process disconnect for the stale connection.
				log.Printf("[relay] ignoring stale disconnect for client %s (newer connection active)", c.id)
				s.mu.Unlock()
			} else {
				s.mu.Unlock()
			}
		}
	}
}

// clientCount returns the number of currently registered clients.
func (s *relayServer) clientCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.clients)
}

// broadcastUserList sends the current list of connected user IDs to all clients.
func (s *relayServer) broadcastUserList() {
	s.mu.RLock()
	ids := make([]string, 0, len(s.clients))
	for id := range s.clients {
		ids = append(ids, id)
	}
	s.mu.RUnlock()

	payload := protocol.UserListPayload{Users: ids}
	pkt, err := protocol.NewPacket(protocol.MsgUserList, "", payload)
	if err != nil {
		log.Printf("[relay] failed to create user list packet: %v", err)
		return
	}
	data, err := pkt.Encode()
	if err != nil {
		log.Printf("[relay] failed to encode user list packet: %v", err)
		return
	}

	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, c := range s.clients {
		select {
		case c.send <- data:
		default:
			log.Printf("[relay] send buffer full for client %s, dropping user-list update", c.id)
		}
	}
}

// notifyPeerDisconnected sends a MsgDisconnect packet to the given client on
// behalf of the disconnected peer.
func (s *relayServer) notifyPeerDisconnected(recipientID, disconnectedID string) {
	s.mu.RLock()
	recipient, ok := s.clients[recipientID]
	s.mu.RUnlock()
	if !ok {
		return
	}

	// Clear peer association.
	recipient.mu.Lock()
	recipient.peerID = ""
	recipient.mu.Unlock()

	payload := protocol.DisconnectPayload{
		Reason: fmt.Sprintf("peer %s disconnected", disconnectedID),
	}
	pkt, err := protocol.NewPacket(protocol.MsgDisconnect, "", payload)
	if err != nil {
		return
	}
	pkt.SenderID = disconnectedID
	data, err := pkt.Encode()
	if err != nil {
		return
	}

	select {
	case recipient.send <- data:
	default:
		log.Printf("[relay] send buffer full for client %s, dropping disconnect notification", recipientID)
	}
}

// routePacket dispatches an incoming packet from a sender to the appropriate
// handler. The relay only reads Type and TargetID — never Payload.
func (s *relayServer) routePacket(sender *client, raw []byte) {
	pkt, err := protocol.DecodePacket(raw)
	if err != nil {
		s.sendError(sender, protocol.ErrCodeInvalidPacket, "malformed packet: "+err.Error())
		return
	}

	// Stamp the sender ID so the recipient knows who sent it.
	pkt.SenderID = sender.id

	switch pkt.Type {
	case protocol.MsgPing:
		s.handlePing(sender)

	case protocol.MsgConnectRequest:
		s.handleConnectRequest(sender, pkt)

	case protocol.MsgConnectResponse:
		s.handleConnectResponse(sender, pkt)

	case protocol.MsgKeyExchange, protocol.MsgChat, protocol.MsgFileChunk, protocol.MsgDisconnect:
		// These packets are forwarded verbatim to the target.
		// The relay never inspects their payloads.
		s.forwardToTarget(sender, pkt)

	default:
		s.sendError(sender, protocol.ErrCodeInvalidPacket,
			fmt.Sprintf("unknown message type: %s", pkt.Type))
	}
}

// handlePing responds to a ping with a pong.
func (s *relayServer) handlePing(c *client) {
	pkt, err := protocol.NewPacket(protocol.MsgPong, "", nil)
	if err != nil {
		return
	}
	data, err := pkt.Encode()
	if err != nil {
		return
	}
	select {
	case c.send <- data:
	default:
	}
}

// handleConnectRequest validates and forwards a connection request from the
// initiating client to the target client.
func (s *relayServer) handleConnectRequest(sender *client, pkt *protocol.Packet) {
	if pkt.TargetID == "" {
		s.sendError(sender, protocol.ErrCodeInvalidPacket, "connect request missing target_id")
		return
	}
	if pkt.TargetID == sender.id {
		s.sendError(sender, protocol.ErrCodeSelfConnect, "cannot connect to yourself")
		return
	}

	s.mu.RLock()
	target, ok := s.clients[pkt.TargetID]
	s.mu.RUnlock()

	if !ok {
		s.sendError(sender, protocol.ErrCodeTargetNotFound,
			fmt.Sprintf("user %s is not connected", pkt.TargetID))
		return
	}

	target.mu.Lock()
	busy := target.peerID != ""
	target.mu.Unlock()

	if busy {
		s.sendError(sender, protocol.ErrCodeTargetBusy,
			fmt.Sprintf("user %s is already in a session", pkt.TargetID))
		return
	}

	// Re-encode with the server-stamped SenderID.
	data, err := pkt.Encode()
	if err != nil {
		return
	}
	select {
	case target.send <- data:
	default:
		log.Printf("[relay] send buffer full for client %s", target.id)
	}
}

// handleConnectResponse forwards an acceptance or rejection response and,
// on acceptance, records the peer association on both sides.
func (s *relayServer) handleConnectResponse(sender *client, pkt *protocol.Packet) {
	if pkt.TargetID == "" {
		s.sendError(sender, protocol.ErrCodeInvalidPacket, "connect response missing target_id")
		return
	}

	var resp protocol.ConnectResponsePayload
	if err := pkt.DecodePayload(&resp); err != nil {
		s.sendError(sender, protocol.ErrCodeInvalidPacket, "invalid connect response payload")
		return
	}

	s.mu.RLock()
	target, ok := s.clients[pkt.TargetID]
	s.mu.RUnlock()

	if !ok {
		// Initiator disconnected before receiving the response — ignore.
		return
	}

	if resp.Accepted {
		// Record the peer association so disconnect notifications can be sent.
		sender.mu.Lock()
		sender.peerID = pkt.TargetID
		sender.mu.Unlock()
		target.mu.Lock()
		target.peerID = sender.id
		target.mu.Unlock()
	}

	data, err := pkt.Encode()
	if err != nil {
		return
	}
	select {
	case target.send <- data:
	default:
		log.Printf("[relay] send buffer full for client %s", target.id)
	}
}

// forwardToTarget routes a packet to the target specified in pkt.TargetID.
// Used for MsgKeyExchange, MsgChat, and MsgDisconnect — all zero-knowledge.
func (s *relayServer) forwardToTarget(sender *client, pkt *protocol.Packet) {
	if pkt.TargetID == "" {
		s.sendError(sender, protocol.ErrCodeInvalidPacket, "packet missing target_id")
		return
	}

	s.mu.RLock()
	target, ok := s.clients[pkt.TargetID]
	s.mu.RUnlock()

	if !ok {
		s.sendError(sender, protocol.ErrCodeTargetNotFound,
			fmt.Sprintf("user %s is not connected", pkt.TargetID))
		return
	}

	// Handle disconnect: clear peer associations.
	if pkt.Type == protocol.MsgDisconnect {
		sender.mu.Lock()
		sender.peerID = ""
		sender.mu.Unlock()
		target.mu.Lock()
		target.peerID = ""
		target.mu.Unlock()
	}

	data, err := pkt.Encode()
	if err != nil {
		return
	}
	select {
	case target.send <- data:
	default:
		log.Printf("[relay] send buffer full for client %s, dropping %s packet", target.id, pkt.Type)
	}
}

// sendError delivers an error packet to the specified client.
func (s *relayServer) sendError(c *client, code, message string) {
	payload := protocol.ErrorPayload{Code: code, Message: message}
	pkt, err := protocol.NewPacket(protocol.MsgError, "", payload)
	if err != nil {
		return
	}
	data, err := pkt.Encode()
	if err != nil {
		return
	}
	select {
	case c.send <- data:
	default:
	}
	log.Printf("[relay] error to client %s: [%s] %s", c.id, code, message)
}

// generateShortID creates a cryptographically random, human-friendly short ID
// of length shortIDLength using the characters in shortIDChars.
func generateShortID() (string, error) {
	id := make([]byte, shortIDLength)
	charCount := big.NewInt(int64(len(shortIDChars)))
	for i := range id {
		n, err := rand.Int(rand.Reader, charCount)
		if err != nil {
			return "", err
		}
		id[i] = shortIDChars[n.Int64()]
	}
	return string(id), nil
}

// generateUniqueID generates a short ID that does not collide with any
// currently registered client ID.
func (s *relayServer) generateUniqueID() (string, error) {
	const maxAttempts = 20
	for i := 0; i < maxAttempts; i++ {
		id, err := generateShortID()
		if err != nil {
			return "", err
		}
		s.mu.RLock()
		_, exists := s.clients[id]
		s.mu.RUnlock()
		if !exists {
			return id, nil
		}
	}
	return "", fmt.Errorf("relay: failed to generate unique ID after %d attempts", maxAttempts)
}

// writePump pumps messages from the send channel to the WebSocket connection.
// It also issues periodic pings to detect stale connections.
func (c *client) writePump() {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		c.conn.Close()
	}()

	for {
		select {
		case message, ok := <-c.send:
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				// Channel was closed — send a WebSocket close frame.
				c.conn.WriteMessage(websocket.CloseMessage,
					websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
				return
			}
			if err := c.conn.WriteMessage(websocket.TextMessage, message); err != nil {
				log.Printf("[relay] write error for client %s: %v", c.id, err)
				return
			}

		case <-ticker.C:
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				log.Printf("[relay] ping error for client %s: %v", c.id, err)
				return
			}
		}
	}
}

// readPump pumps messages from the WebSocket connection to the relay server.
// It runs until the connection is closed.
func (c *client) readPump() {
	defer func() {
		c.server.unregister <- c
		c.conn.Close()
	}()

	c.conn.SetReadLimit(maxMessageSize)
	c.conn.SetReadDeadline(time.Now().Add(pongWait))
	c.conn.SetPongHandler(func(string) error {
		c.conn.SetReadDeadline(time.Now().Add(pongWait))
		return nil
	})

	for {
		_, message, err := c.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err,
				websocket.CloseGoingAway, websocket.CloseNormalClosure) {
				log.Printf("[relay] unexpected close from client %s: %v", c.id, err)
			}
			return
		}
		c.server.routePacket(c, message)
	}
}

func isValidShortID(id string) bool {
	if len(id) != shortIDLength {
		return false
	}
	for _, char := range id {
		if !strings.ContainsRune(shortIDChars, char) {
			return false
		}
	}
	return true
}

// serveWS upgrades an HTTP request to a WebSocket connection and registers
// the new client with the relay server.
//
// Session resumption: the client may pass its previously-assigned ID via
// either the "id" or "client_id" URL query parameter. If the ID is valid and
// already registered, the new connection is bound to that existing session
// (preserving any in-progress peer association) instead of minting a fresh ID.
func serveWS(s *relayServer, w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("[relay] WebSocket upgrade failed: %v", err)
		return
	}

	// Accept the client-provided ID from either query param name.
	requestedID := r.URL.Query().Get("id")
	if requestedID == "" {
		requestedID = r.URL.Query().Get("client_id")
	}

	var id string
	var isResume bool
	if requestedID != "" && isValidShortID(requestedID) {
		id = requestedID
		s.mu.RLock()
		_, isResume = s.clients[id]
		s.mu.RUnlock()
	} else {
		id, err = s.generateUniqueID()
		if err != nil {
			log.Printf("[relay] failed to generate client ID: %v", err)
			conn.WriteMessage(websocket.CloseMessage,
				websocket.FormatCloseMessage(websocket.CloseInternalServerErr, "ID allocation failed"))
			conn.Close()
			return
		}
	}

	if isResume {
		log.Printf("[relay] session resumed for client %s", id)
	} else {
		log.Printf("[relay] new session created for client %s", id)
	}

	c := &client{
		id:     id,
		conn:   conn,
		send:   make(chan []byte, 256),
		server: s,
	}

	// Send the HELLO packet before registering so the client knows its ID
	// before the user-list broadcast fires.
	helloPayload := protocol.HelloPayload{
		AssignedID:    id,
		ServerVersion: serverVersion,
	}
	pkt, err := protocol.NewPacket(protocol.MsgHello, "", helloPayload)
	if err == nil {
		if data, err := pkt.Encode(); err == nil {
			conn.SetWriteDeadline(time.Now().Add(writeWait))
			conn.WriteMessage(websocket.TextMessage, data)
		}
	}

	s.register <- c

	// Start the I/O goroutines.
	go c.writePump()
	go c.readPump()
}

// healthHandler returns a simple JSON health check response.
func healthHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{
		"status":  "ok",
		"version": serverVersion,
	})
}

func defaultListenAddr() string {
	if port := os.Getenv("PORT"); port != "" {
		if port[0] == ':' {
			return port
		}
		return ":" + port
	}
	return ":8080"
}

func main() {
	addr := flag.String("addr", defaultListenAddr(), "TCP address for the relay server to listen on")
	flag.Parse()

	srv := newRelayServer()
	go srv.run()

	mux := http.NewServeMux()
	mux.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		serveWS(srv, w, r)
	})
	mux.HandleFunc("/health", healthHandler)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprintln(w, "TermChat relay server is running.")
	})

	httpServer := &http.Server{
		Addr:         *addr,
		Handler:      mux,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Resolve and display the actual listening address.
	listener, err := net.Listen("tcp", *addr)
	if err != nil {
		log.Fatalf("[relay] failed to bind to %s: %v", *addr, err)
	}

	log.Printf("[relay] TermChat relay server v%s listening on %s", serverVersion, listener.Addr())
	log.Printf("[relay] WebSocket endpoint: ws://%s/ws", listener.Addr())
	if publicHost := os.Getenv("PUBLIC_HOST"); publicHost != "" {
		scheme := "ws"
		if os.Getenv("PUBLIC_TLS") == "true" {
			scheme = "wss"
		}
		log.Printf("[relay] Public WebSocket endpoint: %s://%s/ws", scheme, publicHost)
	}

	// Graceful shutdown on SIGINT or SIGTERM.
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		if err := httpServer.Serve(listener); err != nil && err != http.ErrServerClosed {
			log.Fatalf("[relay] server error: %v", err)
		}
	}()

	<-stop
	log.Println("[relay] received shutdown signal, closing connections...")
	httpServer.Close()
	log.Println("[relay] server stopped")
}
