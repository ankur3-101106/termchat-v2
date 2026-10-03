// Package main is the entry point for the TermChat CLI client. It:
//  1. Parses command-line flags.
//  2. Generates an ephemeral X25519 key pair for this session.
//  3. Dials the relay server over WebSocket with automatic reconnect.
//  4. Runs a WebSocket read loop in a goroutine that converts server packets
//     into Bubbletea tea.Msg events.
//  5. Wraps the Bubbletea model's Update loop to intercept outgoing-packet
//     messages and push them through a write channel.
//  6. Manages the shared-secret lifecycle so the TUI model never touches
//     raw cryptographic key material directly.
package main

import (
	"crypto/rand"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"math/big"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/gorilla/websocket"
	"github.com/viveksec/termchat/pkg/crypto"
	"github.com/viveksec/termchat/pkg/protocol"
)

// ─────────────────────────────────────────────────────────────
// Session-level cryptographic state
// ─────────────────────────────────────────────────────────────

// sessionCrypto holds the ephemeral key pair and the derived shared secret
// for the current 1-on-1 session. It is protected by a mutex because it
// can be written from the Bubbletea update loop and read from the write pump.
type sessionCrypto struct {
	mu           sync.RWMutex
	keyPair      *crypto.KeyPair // our ephemeral X25519 key pair
	sharedSecret []byte          // 32-byte AES-256-GCM key derived via DH
}

// newSessionCrypto generates a fresh ephemeral key pair and returns a
// sessionCrypto ready for use. Exits on failure.
func newSessionCrypto() *sessionCrypto {
	kp, err := crypto.GenerateKeyPair()
	if err != nil {
		log.Fatalf("[client] failed to generate X25519 key pair: %v", err)
	}
	return &sessionCrypto{keyPair: kp}
}

// setSharedSecret stores the derived shared secret, replacing any previous value.
func (sc *sessionCrypto) setSharedSecret(secret []byte) {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	sc.sharedSecret = secret
}

// getSharedSecret returns the current shared secret (nil if not yet derived).
func (sc *sessionCrypto) getSharedSecret() []byte {
	sc.mu.RLock()
	defer sc.mu.RUnlock()
	return sc.sharedSecret
}

// clearSharedSecret zeroes and removes the shared secret on session end.
func (sc *sessionCrypto) clearSharedSecret() {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	for i := range sc.sharedSecret {
		sc.sharedSecret[i] = 0
	}
	sc.sharedSecret = nil
}

const (
	shortIDChars  = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"
	shortIDLength = 6
)

// generateShortID creates a cryptographically random, human-friendly short ID.
func generateShortID() string {
	id := make([]byte, shortIDLength)
	charCount := big.NewInt(int64(len(shortIDChars)))
	for i := range id {
		n, err := rand.Int(rand.Reader, charCount)
		if err != nil {
			log.Fatalf("[client] failed to generate random ID: %v", err)
		}
		id[i] = shortIDChars[n.Int64()]
	}
	return string(id)
}

// ─────────────────────────────────────────────────────────────
// WebSocket client
// ─────────────────────────────────────────────────────────────

// buildServerList returns relay URLs to try when connecting.
// Order: explicit flag value, TERMCHAT_SERVER env, then local fallback.
func buildServerList(primary string, customURL bool) []string {
	if customURL {
		return []string{primary}
	}

	seen := make(map[string]struct{})
	list := make([]string, 0, 3)
	add := func(url string) {
		if url == "" {
			return
		}
		if _, ok := seen[url]; ok {
			return
		}
		seen[url] = struct{}{}
		list = append(list, url)
	}

	add(primary)
	add(os.Getenv("TERMCHAT_SERVER"))
	add("ws://localhost:8080/ws")
	return list
}

// wsClient manages the WebSocket connection to the relay server.
type wsClient struct {
	serverURL  string
	serverList []string
	customURL  bool
	conn       *websocket.Conn
	connMu     sync.RWMutex
	program    *tea.Program
	sendCh     chan outgoingMsg
	sc         *sessionCrypto
	done       chan struct{}
	reconnect  bool
	clientID   string
}

// newWSClient creates a wsClient with multi-server fallback support.
func newWSClient(serverURL string, customURL bool, sc *sessionCrypto, clientID string) *wsClient {
	return &wsClient{
		serverURL:  serverURL,
		serverList: buildServerList(serverURL, customURL),
		customURL:  customURL,
		sendCh:     make(chan outgoingMsg, 256),
		sc:         sc,
		done:       make(chan struct{}),
		reconnect:  true,
		clientID:   clientID,
	}
}

// connect dials the relay server endpoints in sequence.
func (wc *wsClient) connect() error {
	var lastErr error
	for _, url := range wc.serverList {
		select {
		case <-wc.done:
			return fmt.Errorf("client closed")
		default:
		}

		dialURL := url
		if wc.clientID != "" {
			if strings.Contains(dialURL, "?") {
				dialURL += "&id=" + wc.clientID
			} else {
				dialURL += "?id=" + wc.clientID
			}
		}

		dialer := websocket.Dialer{
			HandshakeTimeout: 10 * time.Second,
		}
		header := http.Header{}
		header.Add("Bypass-Tunnel-Reminder", "true")
		conn, _, err := dialer.Dial(dialURL, header)
		if err == nil {
			wc.connMu.Lock()
			wc.conn = conn
			wc.serverURL = url
			wc.connMu.Unlock()
			return nil
		}
		lastErr = err
	}
	return lastErr
}

// writeRaw sends a raw byte slice to the relay server. It is goroutine-safe.
func (wc *wsClient) writeRaw(data []byte) error {
	wc.connMu.RLock()
	conn := wc.conn
	wc.connMu.RUnlock()
	if conn == nil {
		return fmt.Errorf("wsClient: not connected")
	}
	conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
	return conn.WriteMessage(websocket.TextMessage, data)
}

// close shuts down the WebSocket connection cleanly.
func (wc *wsClient) close() {
	wc.connMu.Lock()
	defer wc.connMu.Unlock()
	if wc.conn != nil {
		wc.conn.WriteMessage(websocket.CloseMessage,
			websocket.FormatCloseMessage(websocket.CloseNormalClosure, "client quit"))
		wc.conn.Close()
		wc.conn = nil
	}
}

// readLoop continuously reads packets from the WebSocket and posts them as
// Bubbletea tea.Msg events to the program. It handles reconnection internally.
func (wc *wsClient) readLoop() {
	const maxReconnectWait = 30 * time.Second
	attempt := 0

	for {
		select {
		case <-wc.done:
			return
		default:
		}

		if attempt > 0 {
			wait := time.Duration(attempt*2) * time.Second
			if wait > maxReconnectWait {
				wait = maxReconnectWait
			}
			wc.program.Send(wsReconnectingMsg{attempt: attempt})
			time.Sleep(wait)
		}

		if err := wc.connect(); err != nil {
			attempt++
			log.Printf("[client] connection failed (attempt %d): %v", attempt, err)
			if wc.program != nil {
				wc.program.Send(wsReconnectingMsg{attempt: attempt})
			}
			continue
		}

		attempt = 0
		wc.readMessages()

		select {
		case <-wc.done:
			return
		default:
			if !wc.reconnect {
				return
			}
			wc.program.Send(wsDisconnectedMsg{reason: "connection lost, reconnecting…"})
			attempt = 1
		}
	}
}

// readMessages reads packets from the open connection until it closes.
// A per-connection keepalive ticker sends an application-level MsgPing every
// 25 seconds to prevent cloud-proxy idle timeouts (which typically fire at ~60s).
func (wc *wsClient) readMessages() {
	wc.connMu.RLock()
	conn := wc.conn
	wc.connMu.RUnlock()

	// Client read deadline: 50s, well within the server's 45s pongWait +
	// 10s write-wait window, and short enough to detect dead connections fast.
	const clientReadDeadline = 50 * time.Second
	const keepAlivePeriod = 25 * time.Second

	conn.SetReadLimit(128 * 1024)
	conn.SetReadDeadline(time.Now().Add(clientReadDeadline))

	// Reset read deadline each time the server sends a WebSocket-level ping.
	conn.SetPingHandler(func(appData string) error {
		conn.SetReadDeadline(time.Now().Add(clientReadDeadline))
		return conn.WriteControl(websocket.PongMessage, []byte(appData), time.Now().Add(10*time.Second))
	})

	// keepAlive sends an application-level MsgPing every 25s so the underlying
	// TCP stream stays alive through Cloudflare and other idle-timeout proxies.
	stopKeepAlive := make(chan struct{})
	go func() {
		ticker := time.NewTicker(keepAlivePeriod)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				pkt, err := protocol.NewPacket(protocol.MsgPing, "", nil)
				if err != nil {
					continue
				}
				data, err := pkt.Encode()
				if err != nil {
					continue
				}
				if err := wc.writeRaw(data); err != nil {
					log.Printf("[client] keepalive ping failed: %v", err)
					return
				}
				log.Printf("[client] keepalive ping sent")
			case <-stopKeepAlive:
				return
			case <-wc.done:
				return
			}
		}
	}()
	defer close(stopKeepAlive)

	for {
		_, raw, err := conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err,
				websocket.CloseGoingAway, websocket.CloseNormalClosure) {
				log.Printf("[client] unexpected read error: %v", err)
			}
			return
		}

		// Any traffic resets the read deadline — the connection is alive.
		conn.SetReadDeadline(time.Now().Add(clientReadDeadline))

		pkt, err := protocol.DecodePacket(raw)
		if err != nil {
			log.Printf("[client] failed to decode packet: %v", err)
			continue
		}

		wc.handleIncomingPacket(pkt)
	}
}

// handleIncomingPacket translates a protocol.Packet into a Bubbletea tea.Msg
// and posts it to the UI program.
func (wc *wsClient) handleIncomingPacket(pkt *protocol.Packet) {
	switch pkt.Type {

	case protocol.MsgHello:
		var payload protocol.HelloPayload
		if err := pkt.DecodePayload(&payload); err != nil {
			log.Printf("[client] invalid HELLO payload: %v", err)
			return
		}
		// The assigned ID is guaranteed to match our requested clientID because
		// we pass it on the URL string. We don't overwrite wc.clientID anymore.
		wc.program.Send(wsConnectedMsg{assignedID: payload.AssignedID})

	case protocol.MsgUserList:
		var payload protocol.UserListPayload
		if err := pkt.DecodePayload(&payload); err != nil {
			log.Printf("[client] invalid USER_LIST payload: %v", err)
			return
		}
		wc.program.Send(wsUserListMsg{users: payload.Users})

	case protocol.MsgConnectRequest:
		var payload protocol.ConnectRequestPayload
		if err := pkt.DecodePayload(&payload); err != nil {
			log.Printf("[client] invalid CONNECT_REQUEST payload: %v", err)
			return
		}
		wc.program.Send(wsConnectRequestMsg{
			fromID:  pkt.SenderID,
			message: payload.Message,
		})

	case protocol.MsgConnectResponse:
		var payload protocol.ConnectResponsePayload
		if err := pkt.DecodePayload(&payload); err != nil {
			log.Printf("[client] invalid CONNECT_RESPONSE payload: %v", err)
			return
		}
		if payload.Accepted {
			wc.program.Send(wsConnectAcceptedMsg{peerID: pkt.SenderID})
		} else {
			wc.program.Send(wsConnectRejectedMsg{
				peerID: pkt.SenderID,
				reason: payload.Reason,
			})
		}

	case protocol.MsgKeyExchange:
		var payload protocol.KeyExchangePayload
		if err := pkt.DecodePayload(&payload); err != nil {
			log.Printf("[client] invalid KEY_EXCHANGE payload: %v", err)
			return
		}
		wc.program.Send(wsKeyExchangeMsg{publicKey: payload.PublicKey})

	case protocol.MsgChat:
		var payload protocol.ChatPayload
		if err := pkt.DecodePayload(&payload); err != nil {
			log.Printf("[client] invalid CHAT payload: %v", err)
			return
		}
		secret := wc.sc.getSharedSecret()
		if secret == nil {
			log.Println("[client] received CHAT packet but no shared secret — discarding")
			return
		}
		plaintext, err := crypto.Decrypt(secret, payload.Ciphertext)
		if err != nil {
			log.Printf("[client] decryption failed: %v", err)
			wc.program.Send(wsErrorMsg{
				code:    "DECRYPT_FAILED",
				message: "A message could not be decrypted (authentication failure).",
			})
			return
		}
		wc.program.Send(wsChatMsg{
			fromID: pkt.SenderID,
			text:   string(plaintext),
			ts:     pkt.Timestamp,
		})

	case protocol.MsgFileChunk:
		var payload protocol.FileChunkPayload
		if err := pkt.DecodePayload(&payload); err != nil {
			log.Printf("[client] invalid FILE_CHUNK payload: %v", err)
			return
		}
		secret := wc.sc.getSharedSecret()
		if secret == nil {
			return
		}
		chunkBytes, err := crypto.Decrypt(secret, payload.Ciphertext)
		if err != nil {
			log.Printf("[client] file chunk decryption failed: %v", err)
			return
		}
		wc.program.Send(wsFileChunkMsg{
			fromID:      pkt.SenderID,
			filename:    payload.Filename,
			chunkIndex:  payload.ChunkIndex,
			totalChunks: payload.TotalChunks,
			data:        chunkBytes,
		})

	case protocol.MsgDisconnect:
		var payload protocol.DisconnectPayload
		if err := pkt.DecodePayload(&payload); err == nil {
			wc.program.Send(wsPeerDisconnectedMsg{reason: payload.Reason})
		} else {
			wc.program.Send(wsPeerDisconnectedMsg{reason: "peer disconnected"})
		}
		wc.sc.clearSharedSecret()

	case protocol.MsgError:
		var payload protocol.ErrorPayload
		if err := pkt.DecodePayload(&payload); err != nil {
			log.Printf("[client] invalid ERROR payload: %v", err)
			return
		}
		wc.program.Send(wsErrorMsg{code: payload.Code, message: payload.Message})

	case protocol.MsgPong:
		// Handled at the transport level — no UI action needed.

	default:
		log.Printf("[client] unhandled packet type: %s", pkt.Type)
	}
}

// writePump drains the sendCh and writes each message to the WebSocket.
func (wc *wsClient) writePump() {
	for {
		select {
		case <-wc.done:
			return
		case msg, ok := <-wc.sendCh:
			if !ok {
				return
			}
			if err := wc.writeRaw(msg.data); err != nil {
				log.Printf("[client] write error: %v", err)
			}
		}
	}
}

// ─────────────────────────────────────────────────────────────
// Packet construction helpers
// ─────────────────────────────────────────────────────────────

func buildConnectRequestPacket(targetID string) ([]byte, error) {
	payload := protocol.ConnectRequestPayload{
		InitiatorID: "",
		Message:     "Would you like to chat?",
	}
	pkt, err := protocol.NewPacket(protocol.MsgConnectRequest, targetID, payload)
	if err != nil {
		return nil, err
	}
	return pkt.Encode()
}

func buildConnectResponsePacket(targetID string, accepted bool, reason string) ([]byte, error) {
	payload := protocol.ConnectResponsePayload{
		Accepted: accepted,
		Reason:   reason,
	}
	pkt, err := protocol.NewPacket(protocol.MsgConnectResponse, targetID, payload)
	if err != nil {
		return nil, err
	}
	return pkt.Encode()
}

func buildKeyExchangePacket(targetID string, pubKeyBase64 string) ([]byte, error) {
	payload := protocol.KeyExchangePayload{PublicKey: pubKeyBase64}
	pkt, err := protocol.NewPacket(protocol.MsgKeyExchange, targetID, payload)
	if err != nil {
		return nil, err
	}
	return pkt.Encode()
}

func buildChatPacket(targetID string, sharedSecret []byte, plaintext string) ([]byte, error) {
	ciphertext, err := crypto.Encrypt(sharedSecret, []byte(plaintext))
	if err != nil {
		return nil, fmt.Errorf("encrypt: %w", err)
	}
	payload := protocol.ChatPayload{Ciphertext: ciphertext}
	pkt, err := protocol.NewPacket(protocol.MsgChat, targetID, payload)
	if err != nil {
		return nil, err
	}
	return pkt.Encode()
}

func buildDisconnectPacket(targetID string) ([]byte, error) {
	payload := protocol.DisconnectPayload{Reason: "user ended session"}
	pkt, err := protocol.NewPacket(protocol.MsgDisconnect, targetID, payload)
	if err != nil {
		return nil, err
	}
	return pkt.Encode()
}

// ─────────────────────────────────────────────────────────────
// Wrapped Bubbletea update (intercepts outgoing-packet msgs)
// ─────────────────────────────────────────────────────────────

// wrappedProgram wraps the Bubbletea program's update function to intercept
// internal command messages that require access to cryptographic state or
// the WebSocket send channel — neither of which can be held in the pure model.
type wrappedProgram struct {
	inner tea.Model
	wc    *wsClient
	sc    *sessionCrypto
}

func (wp *wrappedProgram) Init() tea.Cmd {
	return wp.inner.Init()
}

func (wp *wrappedProgram) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch m := msg.(type) {

	// ── Outgoing connect request ───────────────────────────
	case sendPacketMsg:
		if m.msgType == "connect_request" {
			data, err := buildConnectRequestPacket(m.targetID)
			if err != nil {
				log.Printf("[client] failed to build connect request: %v", err)
				return wp, nil
			}
			select {
			case wp.wc.sendCh <- outgoingMsg{data: data}:
			default:
				log.Println("[client] send channel full — dropping connect request")
			}
		}
		return wp, nil

	// ── Outgoing connect response ──────────────────────────
	case sendConnectResponseMsg:
		data, err := buildConnectResponsePacket(m.targetID, m.accepted, m.reason)
		if err != nil {
			log.Printf("[client] failed to build connect response: %v", err)
			return wp, nil
		}
		select {
		case wp.wc.sendCh <- outgoingMsg{data: data}:
		default:
			log.Println("[client] send channel full — dropping connect response")
		}
		return wp, nil

	// ── Outgoing key exchange ──────────────────────────────
	case sendKeyExchangeMsg:
		pubKeyBase64 := wp.sc.keyPair.PublicKeyBase64()
		data, err := buildKeyExchangePacket(m.peerID, pubKeyBase64)
		if err != nil {
			log.Printf("[client] failed to build key exchange packet: %v", err)
			return wp, nil
		}
		select {
		case wp.wc.sendCh <- outgoingMsg{data: data}:
		default:
			log.Println("[client] send channel full — dropping key exchange")
		}
		return wp, nil

	// ── Peer's public key received — derive shared secret ──
	case peerPubKeyReceivedMsg:
		secret, err := crypto.DeriveSharedSecret(wp.sc.keyPair.PrivateKey, m.publicKey)
		if err != nil {
			log.Printf("[client] key derivation failed: %v", err)
			// Post an error to the TUI.
			newInner, cmd := wp.inner.Update(wsErrorMsg{
				code:    "KEY_EXCHANGE_FAILED",
				message: "Cryptographic key exchange failed: " + err.Error(),
			})
			wp.inner = newInner
			return wp, cmd
		}
		wp.sc.setSharedSecret(secret)
		safetyNum := crypto.CalculateSafetyNumber(wp.sc.keyPair.PublicKeyBase64(), m.publicKey)
		log.Printf("[client] shared secret derived successfully. Safety Code: %s", safetyNum)

		// Retrieve peerID from model state.
		innerModel, ok := wp.inner.(model)
		if !ok {
			return wp, nil
		}

		// Transition TUI to chat state.
		newInner, cmd := wp.inner.Update(sharedSecretDerivedMsg{
			peerID:        innerModel.peerID,
			sharedSecret:  secret,
			safetyNumber:  safetyNum,
			peerPublicKey: m.publicKey,
		})
		wp.inner = newInner
		return wp, cmd

	// ── Shared secret ready → enter chat state ─────────────
	case sharedSecretDerivedMsg:
		innerModel, ok := wp.inner.(model)
		if !ok {
			return wp, nil
		}
		innerModel.state = stateChat
		innerModel.peerID = m.peerID
		innerModel.safetyNumber = m.safetyNumber
		innerModel.appendSystem(
			fmt.Sprintf("🔒 Secure session established with %s (AES-256-GCM · Safety Code: %s).", m.peerID, m.safetyNumber))
		innerModel = innerModel.syncViewport()
		wp.inner = innerModel
		return wp, nil

	// ── Outgoing chat message ──────────────────────────────
	case sendChatMsg:
		secret := wp.sc.getSharedSecret()
		if secret == nil {
			log.Println("[client] cannot send: no shared secret")
			newInner, cmd := wp.inner.Update(wsErrorMsg{
				code:    "NO_SESSION",
				message: "Cannot send message: no active encrypted session.",
			})
			wp.inner = newInner
			return wp, cmd
		}
		data, err := buildChatPacket(m.peerID, secret, m.plaintext)
		if err != nil {
			log.Printf("[client] failed to encrypt message: %v", err)
			return wp, nil
		}
		select {
		case wp.wc.sendCh <- outgoingMsg{data: data}:
		default:
			log.Println("[client] send channel full — dropping chat message")
		}
		return wp, nil

	// ── Outgoing disconnect ────────────────────────────────
	case sendDisconnectMsg:
		if m.peerID != "" {
			data, err := buildDisconnectPacket(m.peerID)
			if err != nil {
				log.Printf("[client] failed to build disconnect packet: %v", err)
				return wp, nil
			}
			select {
			case wp.wc.sendCh <- outgoingMsg{data: data}:
			default:
			}
		}
		wp.sc.clearSharedSecret()
		return wp, nil

	// ── Outgoing encrypted file transfer ───────────────────
	case sendFileMsg:
		secret := wp.sc.getSharedSecret()
		if secret == nil {
			log.Println("[client] cannot send file: no shared secret")
			return wp, nil
		}
		targetPeerID := m.peerID
		targetFilePath := m.filePath
		go func() {
			data, err := os.ReadFile(targetFilePath)
			if err != nil {
				log.Printf("[client] failed to read file %s: %v", targetFilePath, err)
				return
			}
			chunkSize := 32 * 1024 // 32 KiB chunks
			totalChunks := (len(data) + chunkSize - 1) / chunkSize
			filename := filepath.Base(targetFilePath)

			for i := 0; i < totalChunks; i++ {
				start := i * chunkSize
				end := start + chunkSize
				if end > len(data) {
					end = len(data)
				}
				ciphertext, err := crypto.Encrypt(secret, data[start:end])
				if err != nil {
					log.Printf("[client] chunk %d encryption failed: %v", i, err)
					continue
				}
				payload := protocol.FileChunkPayload{
					Filename:    filename,
					ChunkIndex:  i,
					TotalChunks: totalChunks,
					Ciphertext:  ciphertext,
				}
				pkt, err := protocol.NewPacket(protocol.MsgFileChunk, targetPeerID, payload)
				if err != nil {
					continue
				}
				raw, err := pkt.Encode()
				if err != nil {
					continue
				}
				wp.wc.sendCh <- outgoingMsg{data: raw}
			}
		}()
		return wp, nil
	}

	// For all other messages, delegate to the inner model.
	newInner, cmd := wp.inner.Update(msg)
	wp.inner = newInner
	return wp, cmd
}

func (wp *wrappedProgram) View() string {
	return wp.inner.View()
}

// ─────────────────────────────────────────────────────────────
// Main
// ─────────────────────────────────────────────────────────────

func main() {
	defaultServer := os.Getenv("TERMCHAT_SERVER")
	if defaultServer == "" {
		defaultServer = "wss://termchat-relay.meetkhamar3501.workers.dev/ws"
	}
	serverURL := flag.String("server", defaultServer,
		"WebSocket URL of the TermChat relay server (or set TERMCHAT_SERVER)")
	logFile := flag.String("log", "",
		"Path to write debug logs (default: stderr)")
	flag.Parse()

	// Configure logging.
	if *logFile != "" {
		f, err := os.OpenFile(*logFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
		if err != nil {
			log.Fatalf("[client] cannot open log file: %v", err)
		}
		defer f.Close()
		log.SetOutput(f)
	} else {
		// Suppress log output so it doesn't corrupt the TUI.
		// Debug logs only appear if --log is specified.
		log.SetOutput(io.Discard)
	}

	customURL := false
	flag.Visit(func(f *flag.Flag) {
		if f.Name == "server" {
			customURL = true
		}
	})

	log.Printf("[client] starting TermChat client, connecting to %s (custom: %t)", *serverURL, customURL)

	// Generate ephemeral key pair for this session.
	sc := newSessionCrypto()
	log.Printf("[client] generated X25519 public key: %s", sc.keyPair.PublicKeyBase64())

	// Generate static session ID for the entire lifecycle of the app.
	clientID := generateShortID()
	log.Printf("[client] generated static session ID: %s", clientID)

	// Create the WebSocket client.
	wc := newWSClient(*serverURL, customURL, sc, clientID)

	// Create the initial Bubbletea model, wired to the WebSocket send channel.
	innerModel := initialModel(wc.sendCh, clientID)

	// Wrap the model so the update loop can handle crypto and send operations.
	wrapped := &wrappedProgram{
		inner: innerModel,
		wc:    wc,
		sc:    sc,
	}

	// Create the Bubbletea program. alt-screen keeps the TUI clean.
	p := tea.NewProgram(wrapped,
		tea.WithAltScreen(),
		tea.WithMouseCellMotion(),
	)

	// Give the WebSocket client a reference to the program so it can post msgs.
	wc.program = p

	// Start the WebSocket read loop (handles reconnection internally).
	go wc.readLoop()

	// Start the WebSocket write pump.
	go wc.writePump()

	// Run the Bubbletea event loop (blocks until quit).
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "TermChat exited with error: %v\n", err)
		os.Exit(1)
	}

	// Clean shutdown.
	close(wc.done)
	wc.close()
	log.Println("[client] shutdown complete")

	// Needed to silence the json import if not used directly by json.NewEncoder/Decoder.
	_ = json.Marshal
}
