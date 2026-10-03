// Package main provides the Bubbletea TUI model for the terminal chat client.
// The model drives a split-panel interface: an online-users list, a scrollable
// chat history viewport with timestamps, a connection-status bar, and a
// command/message input field.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// ─────────────────────────────────────────────────────────────
// Colour palette & style definitions
// ─────────────────────────────────────────────────────────────

var (
	// Primary accent: electric cyan.
	accentColor = lipgloss.Color("#00D9FF")
	// Secondary accent: soft violet.
	secondaryColor = lipgloss.Color("#A855F7")
	// Success: emerald green.
	successColor = lipgloss.Color("#10B981")
	// Warning: amber.
	warningColor = lipgloss.Color("#F59E0B")
	// Error: rose.
	errorColor = lipgloss.Color("#F43F5E")
	// Muted: slate-400.
	mutedColor = lipgloss.Color("#94A3B8")
	// Background: near-black.
	bgColor = lipgloss.Color("#0F172A")
	// Panel background: dark slate.
	panelBg = lipgloss.Color("#1E293B")
	// Border: slate-600.
	borderColor = lipgloss.Color("#475569")
	// Text primary.
	textColor = lipgloss.Color("#E2E8F0")
	// Highlight background.
	highlightBg = lipgloss.Color("#1E40AF")
)

// Base container styles.
var (
	appStyle = lipgloss.NewStyle().
			Background(bgColor)

	// Users panel on the left.
	usersPanelStyle = lipgloss.NewStyle().
			Background(panelBg).
			Border(lipgloss.RoundedBorder()).
			BorderForeground(borderColor).
			Padding(0, 1)

	usersPanelTitleStyle = lipgloss.NewStyle().
				Foreground(accentColor).
				Bold(true).
				MarginBottom(1)

	userItemStyle = lipgloss.NewStyle().
			Foreground(textColor).
			PaddingLeft(1)

	userItemActiveStyle = lipgloss.NewStyle().
				Foreground(successColor).
				Bold(true).
				PaddingLeft(1)

	userItemSelfStyle = lipgloss.NewStyle().
				Foreground(accentColor).
				Bold(true).
				PaddingLeft(1)

	userItemPeerStyle = lipgloss.NewStyle().
				Foreground(secondaryColor).
				Bold(true).
				PaddingLeft(1)

	// Chat panel on the right.
	chatPanelStyle = lipgloss.NewStyle().
			Background(panelBg).
			Border(lipgloss.RoundedBorder()).
			BorderForeground(accentColor)

	chatPanelIdleStyle = lipgloss.NewStyle().
				Background(panelBg).
				Border(lipgloss.RoundedBorder()).
				BorderForeground(borderColor)

	// Chat message bubbles.
	sentBubbleStyle = lipgloss.NewStyle().
			Background(highlightBg).
			Foreground(lipgloss.Color("#DBEAFE")).
			Padding(0, 1).
			MarginLeft(4)

	receivedBubbleStyle = lipgloss.NewStyle().
				Background(lipgloss.Color("#1F2937")).
				Foreground(textColor).
				Padding(0, 1)

	// Timestamps.
	timestampStyle = lipgloss.NewStyle().
			Foreground(mutedColor).
			Italic(true)

	senderNameStyle = lipgloss.NewStyle().
			Foreground(secondaryColor).
			Bold(true)

	selfNameStyle = lipgloss.NewStyle().
			Foreground(accentColor).
			Bold(true)

	// Status bar at the bottom.
	statusBarStyle = lipgloss.NewStyle().
			Background(lipgloss.Color("#0C4A6E")).
			Foreground(textColor).
			Padding(0, 1)

	statusConnectedStyle = lipgloss.NewStyle().
				Foreground(successColor).
				Bold(true)

	statusDisconnectedStyle = lipgloss.NewStyle().
				Foreground(errorColor).
				Bold(true)

	statusChatStyle = lipgloss.NewStyle().
			Foreground(secondaryColor).
			Bold(true)

	// Input line.
	inputPromptStyle = lipgloss.NewStyle().
				Foreground(accentColor).
				Bold(true)

	inputInactivePromptStyle = lipgloss.NewStyle().
					Foreground(mutedColor)

	// Incoming request dialog.
	dialogStyle = lipgloss.NewStyle().
			Border(lipgloss.DoubleBorder()).
			BorderForeground(warningColor).
			Background(lipgloss.Color("#1C1917")).
			Foreground(textColor).
			Padding(1, 2).
			Width(50)

	// Help overlay.
	helpStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(accentColor).
			Background(lipgloss.Color("#0F172A")).
			Foreground(textColor).
			Padding(1, 2).
			Width(58)

	// System/info messages inside the chat view.
	sysMessageStyle = lipgloss.NewStyle().
			Foreground(mutedColor).
			Italic(true)

	errorMessageStyle = lipgloss.NewStyle().
				Foreground(errorColor).
				Bold(true)

	successMessageStyle = lipgloss.NewStyle().
				Foreground(successColor)
)

// ─────────────────────────────────────────────────────────────
// TUI state machine
// ─────────────────────────────────────────────────────────────

// uiState represents the high-level mode the TUI is currently in.
type uiState int

const (
	// stateConnecting — waiting for the WebSocket connection and HELLO packet.
	stateConnecting uiState = iota
	// stateIdle — connected, showing the user list, waiting for a command.
	stateIdle
	// stateAwaitingResponse — sent a /connect request, waiting for peer to respond.
	stateAwaitingResponse
	// statePendingIncoming — received an incoming connect request, showing accept/decline.
	// statePendingIncoming
	// stateHandshake — both parties accepted; key exchange in progress.
	stateHandshake
	// stateChat — in an active encrypted chat session.
	stateChat
)

// ─────────────────────────────────────────────────────────────
// Chat message record
// ─────────────────────────────────────────────────────────────

// messageKind categorises the visual style of a chat entry.
type messageKind int

const (
	msgKindSent     messageKind = iota // message sent by the local user
	msgKindReceived                    // message received from the peer
	msgKindSystem                      // informational system message
	msgKindError                       // error notification
	msgKindSuccess                     // success notification
)

// chatMessage represents a single entry in the chat history.
type chatMessage struct {
	kind      messageKind
	senderID  string
	text      string
	timestamp time.Time
}

// ─────────────────────────────────────────────────────────────
// Tea messages (events posted from the WebSocket goroutine)
// ─────────────────────────────────────────────────────────────

// wsConnectedMsg is posted when the WebSocket connection is established and
// the server has sent the HELLO packet.
type wsConnectedMsg struct{ assignedID string }

// wsDisconnectedMsg is posted when the WebSocket connection drops.
type wsDisconnectedMsg struct{ reason string }

// wsUserListMsg carries an updated list of online user IDs.
type wsUserListMsg struct{ users []string }

// wsConnectRequestMsg is posted when a peer sends a /connect request.
type wsConnectRequestMsg struct {
	fromID  string
	message string
}

// wsConnectAcceptedMsg is posted when the peer accepted our connect request.
type wsConnectAcceptedMsg struct{ peerID string }

// wsConnectRejectedMsg is posted when the peer declined our connect request.
type wsConnectRejectedMsg struct {
	peerID string
	reason string
}

// wsKeyExchangeMsg carries the peer's X25519 public key.
type wsKeyExchangeMsg struct{ publicKey string }

// wsChatMsg carries a decrypted chat message from the peer.
type wsChatMsg struct {
	fromID string
	text   string
	ts     time.Time
}

// wsPeerDisconnectedMsg is posted when the current chat peer disconnects.
type wsPeerDisconnectedMsg struct{ reason string }

// wsErrorMsg carries a server-reported error.
type wsErrorMsg struct {
	code    string
	message string
}

// wsReconnectingMsg is posted when the client is attempting to reconnect.
type wsReconnectingMsg struct{ attempt int }

// ─────────────────────────────────────────────────────────────
// Bubbletea model
// ─────────────────────────────────────────────────────────────

// model is the central Bubbletea application state.
type model struct {
	// Terminal dimensions.
	width  int
	height int

	// Client state.
	state       uiState
	myID        string
	onlineUsers []string

	// Active session state.
	peerID   string
	messages []chatMessage

	// Incoming request state.
	incomingFrom    string
	incomingMessage string

	// Input field.
	input     textinput.Model
	inputMode string // "command" or "message"

	// Chat viewport.
	viewport      viewport.Model
	viewportReady bool

	// Error / notification display.
	notification     string
	notificationKind messageKind
	notificationAt   time.Time

	// Help overlay toggle.
	showHelp bool

	// Channel for sending data to the WebSocket write pump.
	// Populated by the bootstrapper (main.go) before the Program starts.
	sendCh chan<- outgoingMsg

	// Key-exchange state: peer's public key arrives before or after we send ours.
	pendingPeerPublicKey string

	// Advanced Security & Stealth Features
	safetyNumber    string
	panicMode       bool
	showSafetyModal bool
}

// outgoingMsg is sent from the TUI to the WebSocket write pump.
type outgoingMsg struct {
	data []byte
}

// initialModel creates a zeroed model with an initialised text input.
func initialModel(sendCh chan<- outgoingMsg, clientID string) model {
	ti := textinput.New()
	ti.Placeholder = "Type /help for commands, or a message…"
	ti.Focus()
	ti.CharLimit = 1024
	ti.Width = 60
	ti.PromptStyle = inputPromptStyle
	ti.Prompt = "❯ "

	return model{
		state:    stateConnecting,
		input:    ti,
		sendCh:   sendCh,
		messages: []chatMessage{},
		myID:     clientID,
	}
}

// ─────────────────────────────────────────────────────────────
// Init
// ─────────────────────────────────────────────────────────────

func (m model) Init() tea.Cmd {
	return textinput.Blink
}

// ─────────────────────────────────────────────────────────────
// Update
// ─────────────────────────────────────────────────────────────

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {

	// ── Terminal resize ──────────────────────────────────────
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m = m.recalcLayout()
		return m, nil

	// ── WebSocket events ─────────────────────────────────────
	case wsConnectedMsg:
		m.myID = msg.assignedID
		m.state = stateIdle
		m.setNotification("Connected to relay. Your ID: "+msg.assignedID, msgKindSuccess)
		m.appendSystem("🟢 Connected to relay server! Your assigned User ID: " + msg.assignedID)
		m = m.syncViewport()

	case wsDisconnectedMsg:
		m.state = stateConnecting
		m.peerID = ""
		m.setNotification("Disconnected: "+msg.reason, msgKindError)

	case wsReconnectingMsg:
		m.setNotification(fmt.Sprintf("Reconnecting… (attempt %d)", msg.attempt), msgKindSystem)

	case wsUserListMsg:
		m.onlineUsers = msg.users

	case wsConnectRequestMsg:
		if m.state == stateIdle {
			// m.state = statePendingIncoming
			m.incomingFrom = msg.fromID
			// m.incomingMessage = msg.message
			m.appendSystem(fmt.Sprintf("⚡ Incoming connection request from %s. Type /connect %s to accept.", msg.fromID, msg.fromID))
			m = m.syncViewport()
		} else if m.state == stateAwaitingResponse && m.peerID == msg.fromID {
			cmds = append(cmds, m.sendConnectResponse(msg.fromID, true, ""))
			m.state = stateHandshake
			m.appendSystem("Mutual connection request received. Auto-establishing encrypted session…")
			cmds = append(cmds, m.sendKeyExchange())
			if m.pendingPeerPublicKey != "" {
				pk := m.pendingPeerPublicKey
				m.pendingPeerPublicKey = ""
				cmds = append(cmds, func() tea.Msg {
					return peerPubKeyReceivedMsg{publicKey: pk}
				})
			}
			m = m.syncViewport()
		} else {
			// Already in a session — auto-reject.
			cmds = append(cmds, m.sendConnectResponse(msg.fromID, false, "user is busy"))
		}

	case wsConnectAcceptedMsg:
		if m.state == stateAwaitingResponse {
			m.state = stateHandshake
			m.peerID = msg.peerID
			m.appendSystem("Peer accepted. Performing key exchange…")
			cmds = append(cmds, m.sendKeyExchange())
			if m.pendingPeerPublicKey != "" {
				pk := m.pendingPeerPublicKey
				m.pendingPeerPublicKey = ""
				cmds = append(cmds, func() tea.Msg {
					return peerPubKeyReceivedMsg{publicKey: pk}
				})
			}
		}

	case wsConnectRejectedMsg:
		m.state = stateIdle
		m.peerID = ""
		reason := msg.reason
		if reason == "" {
			reason = "no reason given"
		}
		m.setNotification(fmt.Sprintf("User %s declined the request: %s", msg.peerID, reason), msgKindError)

	case wsKeyExchangeMsg:
		m = m.handleKeyExchange(msg.publicKey, &cmds)

	case sharedSecretDerivedMsg:
		if m.state == stateHandshake {
			m.state = stateChat
			m.peerID = msg.peerID
			m.appendSystem(fmt.Sprintf("Secure session established. Safety Number: %s", msg.safetyNumber))
			m = m.syncViewport()
		}

	case wsChatMsg:
		m.appendReceived(msg.fromID, msg.text, msg.ts)
		m = m.syncViewport()

	case wsFileChunkMsg:
		os.MkdirAll("downloads", 0755)
		outPath := filepath.Join("downloads", filepath.Base(msg.filename))
		flags := os.O_CREATE | os.O_WRONLY | os.O_APPEND
		if msg.chunkIndex == 0 {
			flags = os.O_CREATE | os.O_WRONLY | os.O_TRUNC
		}
		f, err := os.OpenFile(outPath, flags, 0644)
		if err == nil {
			f.Write(msg.data)
			f.Close()
		}
		pct := int(float64(msg.chunkIndex+1) / float64(msg.totalChunks) * 100)
		if msg.chunkIndex+1 == msg.totalChunks {
			m.appendSystem(fmt.Sprintf("📥 Received & decrypted file: downloads/%s (100%%)", msg.filename))
			m = m.syncViewport()
		} else {
			m.setNotification(fmt.Sprintf("Receiving %s: %d%% (%d/%d)", msg.filename, pct, msg.chunkIndex+1, msg.totalChunks), msgKindSystem)
		}

	case wsPeerDisconnectedMsg:
		if m.state == stateChat || m.state == stateHandshake {
			m.state = stateIdle
			m.appendSystem("Peer disconnected: " + msg.reason)
		}
		m.peerID = ""
		m.safetyNumber = ""
		m.showSafetyModal = false
		m = m.syncViewport()

	case wsErrorMsg:
		m.setNotification(fmt.Sprintf("[%s] %s", msg.code, msg.message), msgKindError)
		if m.state == stateAwaitingResponse {
			m.state = stateIdle
			m.peerID = ""
		}

	// ── Keyboard input ───────────────────────────────────────
	case tea.KeyMsg:
		switch msg.Type {

		case tea.KeyCtrlC:
			return m, tea.Quit

		case tea.KeyCtrlP:
			m.panicMode = !m.panicMode

		case tea.KeyCtrlV:
			if m.state == stateChat {
				m.showSafetyModal = !m.showSafetyModal
			} else {
				m.setNotification("Safety numbers are only available in an active session.", msgKindSystem)
			}

		case tea.KeyEsc:
			if m.panicMode {
				m.panicMode = false
			} else if m.showSafetyModal {
				m.showSafetyModal = false
			} else if m.showHelp {
				m.showHelp = false
				// } else if m.state == statePendingIncoming {
				// 	// Decline on Esc.
				// 	cmds = append(cmds, m.sendConnectResponse(m.incomingFrom, false, "declined"))
				// 	m.state = stateIdle
				// 	m.incomingFrom = ""
			}

		case tea.KeyEnter:
			cmds = append(cmds, m.handleEnter()...)

		case tea.KeyCtrlD:
			if m.state == stateChat {
				cmds = append(cmds, m.sendDisconnect())
				m.state = stateIdle
				m.appendSystem("You left the session.")
				m.peerID = ""
				m = m.syncViewport()
			}

		case tea.KeyF1:
			m.showHelp = !m.showHelp

		case tea.KeyPgUp:
			m.viewport.HalfViewUp()
		case tea.KeyPgDown:
			m.viewport.HalfViewDown()

		default:
			// In pending-incoming state, 'y' and 'n' are shortcuts.
			// if m.state == statePendingIncoming {
			// 	switch strings.ToLower(msg.String()) {
			// 	case "y":
			// 		cmds = append(cmds, m.sendConnectResponse(m.incomingFrom, true, ""))
			// 		m.state = stateHandshake
			// 		m.peerID = m.incomingFrom
			// 		m.incomingFrom = ""
			// 		m.appendSystem("Accepted connection. Performing key exchange…")
			// 		cmds = append(cmds, m.sendKeyExchange())
			// 		m = m.syncViewport()
			// 		return m, tea.Batch(cmds...)
			// 	case "n":
			// 		cmds = append(cmds, m.sendConnectResponse(m.incomingFrom, false, "declined"))
			// 		m.state = stateIdle
			// 		m.incomingFrom = ""
			// 		return m, tea.Batch(cmds...)
			// 	}
			// }

			var inputCmd tea.Cmd
			m.input, inputCmd = m.input.Update(msg)
			cmds = append(cmds, inputCmd)
		}
	}

	// Always keep the viewport updated.
	if m.viewportReady {
		var vpCmd tea.Cmd
		m.viewport, vpCmd = m.viewport.Update(msg)
		cmds = append(cmds, vpCmd)
	}

	return m, tea.Batch(cmds...)
}

// handleEnter processes the Enter key based on the current TUI state.
func (m *model) handleEnter() []tea.Cmd {
	var cmds []tea.Cmd
	raw := strings.TrimSpace(m.input.Value())

	switch m.state {
	// case statePendingIncoming:
	// 	if strings.HasPrefix(strings.ToLower(raw), "/connect ") {
	// 		parts := strings.Fields(raw)
	// 		if len(parts) == 2 && strings.ToUpper(parts[1]) == m.incomingFrom {
	// 			raw = "y"
	// 		} else {
	// 			cmds = append(cmds, m.sendConnectResponse(m.incomingFrom, false, "connecting to someone else"))
	// 			m.state = stateIdle
	// 			m.incomingFrom = ""
	// 			cmds = append(cmds, m.processCommand(raw)...)
	// 			m.input.SetValue("")
	// 			return cmds
	// 		}
	// 	}
	//
	// 	switch strings.ToLower(raw) {
	// 	case "y", "yes":
	// 		cmds = append(cmds, m.sendConnectResponse(m.incomingFrom, true, ""))
	// 		m.state = stateHandshake
	// 		m.peerID = m.incomingFrom
	// 		m.incomingFrom = ""
	// 		m.appendSystem("Accepted connection. Performing key exchange…")
	// 		cmds = append(cmds, m.sendKeyExchange())
	// 		if m.pendingPeerPublicKey != "" {
	// 			pk := m.pendingPeerPublicKey
	// 			m.pendingPeerPublicKey = ""
	// 			cmds = append(cmds, func() tea.Msg {
	// 				return peerPubKeyReceivedMsg{publicKey: pk}
	// 			})
	// 		}
	// 		*m = m.syncViewport()
	// 	case "n", "no":
	// 		cmds = append(cmds, m.sendConnectResponse(m.incomingFrom, false, "declined"))
	// 		m.state = stateIdle
	// 		m.incomingFrom = ""
	// 	}
	// 	m.input.SetValue("")
	// 	return cmds

	case stateConnecting, stateIdle, stateAwaitingResponse:
		if raw == "" {
			return nil
		}
		cmds = append(cmds, m.processCommand(raw)...)
		m.input.SetValue("")
		return cmds

	case stateChat:
		if raw == "" {
			return nil
		}
		if strings.HasPrefix(raw, "/") {
			cmds = append(cmds, m.processCommand(raw)...)
		} else {
			cmds = append(cmds, m.sendChatMessage(raw)...)
		}
		m.input.SetValue("")
		return cmds
	}

	return nil
}

// processCommand parses and dispatches a slash command.
func (m *model) processCommand(input string) []tea.Cmd {
	var cmds []tea.Cmd
	parts := strings.Fields(input)
	if len(parts) == 0 {
		return nil
	}

	cmd := strings.ToLower(parts[0])
	args := parts[1:]

	switch cmd {
	case "/help":
		m.showHelp = !m.showHelp

	case "/connect":
		if len(args) == 0 {
			m.setNotification("Usage: /connect <USER_ID>", msgKindError)
			return nil
		}
		if m.state != stateIdle {
			m.setNotification("You are already in a session or awaiting a response.", msgKindError)
			return nil
		}
		targetID := strings.ToUpper(args[0])
		if targetID == m.myID {
			m.setNotification("You cannot connect to yourself.", msgKindError)
			return nil
		}
		m.state = stateAwaitingResponse
		m.peerID = targetID
		m.setNotification(fmt.Sprintf("Sent connection request to %s…", targetID), msgKindSystem)
		cmds = append(cmds, m.sendConnectRequest(targetID))

	case "/disconnect", "/leave":
		if m.state == stateChat || m.state == stateAwaitingResponse || m.state == stateHandshake {
			cmds = append(cmds, m.sendDisconnect())
			m.appendSystem("You left the session.")
			m.state = stateIdle
			m.peerID = ""
			m.safetyNumber = ""
			m.showSafetyModal = false
			*m = m.syncViewport()
		} else {
			m.setNotification("You are not in an active session.", msgKindSystem)
		}

	case "/clear":
		m.messages = []chatMessage{}
		*m = m.syncViewport()

	case "/whoami":
		if m.myID != "" {
			m.setNotification("Your ID: "+m.myID, msgKindSuccess)
		} else {
			m.setNotification("Connecting to relay server... User ID not assigned yet.", msgKindSystem)
		}

	case "/sendfile":
		if m.state != stateChat {
			m.setNotification("File transfer requires an active encrypted session.", msgKindError)
			return nil
		}
		if len(args) == 0 {
			m.setNotification("Usage: /sendfile <FILE_PATH>", msgKindError)
			return nil
		}
		filePath := strings.Join(args, " ")
		if _, err := os.Stat(filePath); os.IsNotExist(err) {
			m.setNotification("File not found: "+filePath, msgKindError)
			return nil
		}
		m.appendSystem(fmt.Sprintf("📤 Initiated encrypted transfer of %s...", filepath.Base(filePath)))
		*m = m.syncViewport()
		cmds = append(cmds, m.sendFile(filePath))

	case "/verify":
		if m.state == stateChat {
			m.showSafetyModal = !m.showSafetyModal
		} else {
			m.setNotification("Safety numbers are only available in an active session.", msgKindSystem)
		}

	case "/panic":
		m.panicMode = !m.panicMode

	default:
		m.setNotification(fmt.Sprintf("Unknown command: %s. Type /help for help.", cmd), msgKindError)
	}

	return cmds
}

// ─────────────────────────────────────────────────────────────
// View
// ─────────────────────────────────────────────────────────────

func (m model) View() string {
	if m.width == 0 {
		return "Initialising…"
	}

	// Stealth Panic Mode takes over everything if active.
	if m.panicMode {
		return m.renderStealthScreen()
	}

	// Help overlay takes over the whole screen.
	if m.showHelp {
		return m.renderHelp()
	}

	// Safety Number verification modal.
	if m.showSafetyModal {
		return m.renderSafetyModal()
	}

	// Incoming request dialog takes priority.
	// if m.state == statePendingIncoming {
	// 	return m.renderIncomingRequest()
	// }

	leftWidth := 20
	rightWidth := m.width - leftWidth - 4 // account for borders and gap
	if rightWidth < 30 {
		rightWidth = 30
	}

	left := m.renderUsersPanel(leftWidth)
	right := m.renderChatPanel(rightWidth)

	body := lipgloss.JoinHorizontal(lipgloss.Top, left, " ", right)
	statusBar := m.renderStatusBar(m.width)
	inputBar := m.renderInputBar(m.width)

	return lipgloss.JoinVertical(lipgloss.Left,
		body,
		statusBar,
		inputBar,
	)
}

// renderUsersPanel renders the left panel listing online users.
func (m model) renderUsersPanel(width int) string {
	title := usersPanelTitleStyle.Render("● Online Users")
	var lines []string
	lines = append(lines, title)

	if len(m.onlineUsers) == 0 {
		lines = append(lines, sysMessageStyle.Render("  (none)"))
	} else {
		for _, uid := range m.onlineUsers {
			var rendered string
			prefix := "  "
			if uid == m.myID {
				rendered = userItemSelfStyle.Render(prefix + uid + " (you)")
			} else if uid == m.peerID && m.state == stateChat {
				rendered = userItemPeerStyle.Render(prefix + uid + " ◀ chat")
			} else {
				rendered = userItemStyle.Render(prefix + uid)
			}
			lines = append(lines, rendered)
		}
	}

	content := strings.Join(lines, "\n")
	panelHeight := m.height - 4 // reserve space for status + input bars
	return usersPanelStyle.Width(width).Height(panelHeight).Render(content)
}

// renderChatPanel renders the main chat area or idle prompt.
func (m model) renderChatPanel(width int) string {
	panelHeight := m.height - 4

	var header string
	switch m.state {
	case stateConnecting:
		header = sysMessageStyle.Render("Connecting to relay server…")
	case stateIdle:
		header = sysMessageStyle.Render("No active session. Use /connect <USER_ID> to start chatting.")
	case stateAwaitingResponse:
		header = lipgloss.NewStyle().Foreground(warningColor).
			Render(fmt.Sprintf("Waiting for %s to respond…", m.peerID))
	case stateHandshake:
		header = lipgloss.NewStyle().Foreground(warningColor).
			Render(fmt.Sprintf("Exchanging encryption keys with %s…", m.peerID))
	case stateChat:
		header = lipgloss.NewStyle().Foreground(successColor).Bold(true).
			Render(fmt.Sprintf("🔒 Encrypted session with %s", m.peerID))
	}

	var body string
	if m.viewportReady && (m.state == stateChat || len(m.messages) > 0) {
		body = m.viewport.View()
	} else if m.state == stateIdle || m.state == stateConnecting {
		body = m.renderWelcomeBanner()
	}

	content := lipgloss.JoinVertical(lipgloss.Left,
		header,
		lipgloss.NewStyle().Foreground(borderColor).Render(strings.Repeat("─", width-4)),
		body,
	)

	style := chatPanelIdleStyle
	if m.state == stateChat {
		style = chatPanelStyle
	}

	return style.Width(width).Height(panelHeight).Render(content)
}

// renderWelcomeBanner renders an ASCII art welcome message shown in the idle state.
func (m model) renderWelcomeBanner() string {
	banner := lipgloss.NewStyle().
		Foreground(accentColor).
		Bold(true).
		Render(`
  ████████╗███████╗██████╗ ███╗   ███╗ ██████╗██╗  ██╗ █████╗ ████████╗
  ╚══██╔══╝██╔════╝██╔══██╗████╗ ████║██╔════╝██║  ██║██╔══██╗╚══██╔══╝
     ██║   █████╗  ██████╔╝██╔████╔██║██║     ███████║███████║   ██║   
     ██║   ██╔══╝  ██╔══██╗██║╚██╔╝██║██║     ██╔══██║██╔══██║   ██║   
     ██║   ███████╗██║  ██║██║ ╚═╝ ██║╚██████╗██║  ██║██║  ██║   ██║   
     ╚═╝   ╚══════╝╚═╝  ╚═╝╚═╝     ╚═╝ ╚═════╝╚═╝  ╚═╝╚═╝  ╚═╝   ╚═╝  `)

	sub := lipgloss.NewStyle().
		Foreground(mutedColor).
		Italic(true).
		Render("  End-to-End Encrypted Terminal Chat  ·  X25519 + AES-256-GCM\n")

	var hintStr string
	if m.myID != "" {
		hintStr = fmt.Sprintf("  YOUR USER ID: %s  ·  /connect <ID> to start  ·  Press F1 for help", m.myID)
	} else {
		hintStr = "  Press F1 for help  ·  /connect <ID> to start  ·  Ctrl+C to quit"
	}

	hint := lipgloss.NewStyle().
		Foreground(accentColor).
		Bold(true).
		Render(hintStr)

	return lipgloss.JoinVertical(lipgloss.Left, banner, sub, hint)
}

// renderStatusBar renders the bottom status strip.
func (m model) renderStatusBar(width int) string {
	var left, center, right string

	switch m.state {
	case stateConnecting:
		left = statusDisconnectedStyle.Render("⬤ CONNECTING")
	case stateIdle:
		left = statusConnectedStyle.Render("⬤ ONLINE")
	case stateAwaitingResponse:
		left = lipgloss.NewStyle().Foreground(warningColor).Bold(true).Render("⬤ AWAITING")
	case stateHandshake:
		left = lipgloss.NewStyle().Foreground(warningColor).Bold(true).Render("⬤ HANDSHAKE")
	case stateChat:
		left = statusChatStyle.Render("⬤ ENCRYPTED CHAT")
		// case statePendingIncoming:
		// 	left = lipgloss.NewStyle().Foreground(warningColor).Bold(true).Render("⬤ INCOMING REQUEST")
	}

	if m.myID != "" {
		center = lipgloss.NewStyle().Foreground(accentColor).Render("ID: " + m.myID)
	}

	onlineCount := len(m.onlineUsers)
	right = lipgloss.NewStyle().Foreground(mutedColor).
		Render(fmt.Sprintf("%d online  F1:help  Ctrl+C:quit", onlineCount))

	// Calculate spacing.
	leftLen := lipgloss.Width(left)
	centerLen := lipgloss.Width(center)
	rightLen := lipgloss.Width(right)
	totalContent := leftLen + centerLen + rightLen
	spaces := width - totalContent - 4
	if spaces < 1 {
		spaces = 1
	}
	leftGap := spaces / 2
	rightGap := spaces - leftGap

	bar := left + strings.Repeat(" ", leftGap) + center + strings.Repeat(" ", rightGap) + right
	return statusBarStyle.Width(width).Render(bar)
}

// renderInputBar renders the message/command input line.
func (m model) renderInputBar(width int) string {
	var prompt string
	if m.state == stateChat {
		prompt = inputPromptStyle.Render("❯ ")
	} else {
		prompt = inputInactivePromptStyle.Render("❯ ")
	}

	inputView := m.input.View()

	// Notification shown above the input if fresh (within 5 seconds).
	var notifLine string
	if m.notification != "" && time.Since(m.notificationAt) < 5*time.Second {
		switch m.notificationKind {
		case msgKindError:
			notifLine = errorMessageStyle.Render("  ⚠ " + m.notification)
		case msgKindSuccess:
			notifLine = successMessageStyle.Render("  ✓ " + m.notification)
		default:
			notifLine = sysMessageStyle.Render("  ℹ " + m.notification)
		}
		notifLine += "\n"
	}

	_ = prompt // prompt is embedded in the textinput component
	return notifLine + inputView
}

// renderIncomingRequest renders the accept/decline dialog for an incoming
// connection request.
// func (m model) renderIncomingRequest() string {
// 	msg := m.incomingMessage
// 	if msg == "" {
// 		msg = "(no message)"
// 	}
// 	content := lipgloss.JoinVertical(lipgloss.Left,
// 		lipgloss.NewStyle().Foreground(warningColor).Bold(true).
// 			Render("📨 Incoming Chat Request"),
// 		"",
// 		lipgloss.NewStyle().Foreground(accentColor).
// 			Render(fmt.Sprintf("From: %s", m.incomingFrom)),
// 		lipgloss.NewStyle().Foreground(mutedColor).
// 			Render(fmt.Sprintf("Message: %s", msg)),
// 		"",
// 		lipgloss.NewStyle().Foreground(textColor).
// 			Render("Type 'y' + Enter to Accept, 'n' + Enter to Decline"),
// 		lipgloss.NewStyle().Foreground(mutedColor).Italic(true).
// 			Render("(or press Y / N directly)"),
// 		"",
// 		m.input.View(),
// 	)
// 	dialog := dialogStyle.Render(content)
// 	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, dialog)
// }

// renderHelp renders the full-screen help overlay.
func (m model) renderHelp() string {
	content := lipgloss.JoinVertical(lipgloss.Left,
		lipgloss.NewStyle().Foreground(accentColor).Bold(true).Render("TermChat Help"),
		lipgloss.NewStyle().Foreground(borderColor).Render(strings.Repeat("─", 54)),
		"",
		lipgloss.NewStyle().Foreground(secondaryColor).Bold(true).Render("Commands:"),
		lipgloss.NewStyle().Foreground(textColor).
			Render("  /connect <ID>    Connect to a user by their short ID"),
		lipgloss.NewStyle().Foreground(textColor).
			Render("  /disconnect      End the current chat session"),
		lipgloss.NewStyle().Foreground(textColor).
			Render("  /clear           Clear chat history"),
		lipgloss.NewStyle().Foreground(textColor).
			Render("  /whoami          Display your assigned short ID"),
		lipgloss.NewStyle().Foreground(textColor).
			Render("  /verify          Show SAS Safety Number verification"),
		lipgloss.NewStyle().Foreground(textColor).
			Render("  /panic           Toggle Stealth Panic Mode screen"),
		lipgloss.NewStyle().Foreground(textColor).
			Render("  /help            Show this help screen"),
		"",
		lipgloss.NewStyle().Foreground(secondaryColor).Bold(true).Render("Keybindings:"),
		lipgloss.NewStyle().Foreground(textColor).
			Render("  Enter            Send message or confirm action"),
		lipgloss.NewStyle().Foreground(textColor).
			Render("  Ctrl+V           Verify Safety Number (SAS)"),
		lipgloss.NewStyle().Foreground(textColor).
			Render("  Ctrl+P           Toggle Stealth Panic Mode"),
		lipgloss.NewStyle().Foreground(textColor).
			Render("  Ctrl+D           Leave current session"),
		lipgloss.NewStyle().Foreground(textColor).
			Render("  Ctrl+C           Quit TermChat"),
		lipgloss.NewStyle().Foreground(textColor).
			Render("  F1 / Esc         Toggle help screen / close modal"),
		lipgloss.NewStyle().Foreground(textColor).
			Render("  PgUp / PgDn      Scroll chat history"),
		"",
		lipgloss.NewStyle().Foreground(secondaryColor).Bold(true).Render("Security:"),
		lipgloss.NewStyle().Foreground(mutedColor).Italic(true).
			Render("  All messages are encrypted with AES-256-GCM."),
		lipgloss.NewStyle().Foreground(mutedColor).Italic(true).
			Render("  Keys are exchanged via ephemeral X25519 DH."),
		lipgloss.NewStyle().Foreground(mutedColor).Italic(true).
			Render("  The relay server never sees your plaintext."),
		"",
		lipgloss.NewStyle().Foreground(mutedColor).Italic(true).
			Render("Press Esc or F1 to close"),
	)
	overlay := helpStyle.Render(content)
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, overlay)
}

// renderStealthScreen renders a realistic system terminal output to disguise the application.
func (m model) renderStealthScreen() string {
	content := fmt.Sprintf(`Last login: Thu Aug 13 13:48:10 2026 from 127.0.0.1
user@macbook-air:~$ uptime
 13:48:10 up 4 days,  3:12,  2 users,  load averages: 1.45 1.62 1.58
user@macbook-air:~$ systemctl status network.service
● network.service - LSB: Bring up/down networking
   Loaded: loaded (/etc/init.d/network; generated)
   Active: active (running) since Mon 2026-08-10 10:00:00 UTC; 4 days ago
user@macbook-air:~$ %s`, m.input.View())
	return content
}

// renderSafetyModal renders the SAS Safety Number verification dialog.
func (m model) renderSafetyModal() string {
	safety := m.safetyNumber
	if safety == "" {
		safety = "N/A (No Session)"
	}
	content := lipgloss.JoinVertical(lipgloss.Left,
		lipgloss.NewStyle().Foreground(accentColor).Bold(true).
			Render("🛡️  Short Authentication String (SAS) Verification"),
		"",
		lipgloss.NewStyle().Foreground(textColor).
			Render(fmt.Sprintf("Active Session Peer: %s", m.peerID)),
		"",
		lipgloss.NewStyle().Foreground(mutedColor).
			Render("Compare this 6-digit Safety Number out-of-band"),
		lipgloss.NewStyle().Foreground(mutedColor).
			Render("(e.g., over voice call or in person) with your peer:"),
		"",
		lipgloss.NewStyle().
			Foreground(successColor).
			Background(panelBg).
			Bold(true).
			Padding(1, 4).
			Render(fmt.Sprintf("  🔒 [  %s  ]  ", safety)),
		"",
		lipgloss.NewStyle().Foreground(mutedColor).Italic(true).
			Render("If numbers match on both sides, 100% MITM protection is guaranteed."),
		"",
		lipgloss.NewStyle().Foreground(secondaryColor).
			Render("Press Esc or Ctrl+V to close"),
	)
	dialog := helpStyle.Render(content)
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, dialog)
}

// ─────────────────────────────────────────────────────────────
// Viewport management
// ─────────────────────────────────────────────────────────────

// recalcLayout recomputes viewport dimensions when the terminal resizes.
func (m model) recalcLayout() model {
	leftWidth := 20
	rightWidth := m.width - leftWidth - 4
	if rightWidth < 30 {
		rightWidth = 30
	}

	vpWidth := rightWidth - 4 // inside panel borders + padding
	vpHeight := m.height - 7  // header, divider, status bar, input bar

	if vpHeight < 3 {
		vpHeight = 3
	}

	if !m.viewportReady {
		m.viewport = viewport.New(vpWidth, vpHeight)
		m.viewportReady = true
	} else {
		m.viewport.Width = vpWidth
		m.viewport.Height = vpHeight
	}

	m = m.syncViewport()
	return m
}

// syncViewport re-renders all chat messages into the viewport content and
// scrolls to the bottom.
func (m model) syncViewport() model {
	if !m.viewportReady {
		return m
	}

	var sb strings.Builder
	for _, msg := range m.messages {
		sb.WriteString(m.renderChatMessage(msg))
		sb.WriteString("\n")
	}
	m.viewport.SetContent(sb.String())
	m.viewport.GotoBottom()
	return m
}

// renderChatMessage formats a single chat message entry.
func (m model) renderChatMessage(msg chatMessage) string {
	ts := timestampStyle.Render(msg.timestamp.Local().Format("15:04:05"))

	switch msg.kind {
	case msgKindSent:
		name := selfNameStyle.Render("You")
		bubble := sentBubbleStyle.Render(msg.text)
		return fmt.Sprintf("%s %s\n%s", ts, name, bubble)

	case msgKindReceived:
		name := senderNameStyle.Render(msg.senderID)
		bubble := receivedBubbleStyle.Render(msg.text)
		return fmt.Sprintf("%s %s\n%s", ts, name, bubble)

	case msgKindSystem:
		return sysMessageStyle.Render(fmt.Sprintf("%s  ℹ %s", ts, msg.text))

	case msgKindError:
		return errorMessageStyle.Render(fmt.Sprintf("%s  ⚠ %s", ts, msg.text))

	case msgKindSuccess:
		return successMessageStyle.Render(fmt.Sprintf("%s  ✓ %s", ts, msg.text))

	default:
		return msg.text
	}
}

// ─────────────────────────────────────────────────────────────
// Message append helpers
// ─────────────────────────────────────────────────────────────

func (m *model) appendSystem(text string) {
	m.messages = append(m.messages, chatMessage{
		kind: msgKindSystem, text: text, timestamp: time.Now(),
	})
}

func (m *model) appendReceived(fromID, text string, ts time.Time) {
	m.messages = append(m.messages, chatMessage{
		kind: msgKindReceived, senderID: fromID, text: text, timestamp: ts,
	})
}

func (m *model) appendSent(text string) {
	m.messages = append(m.messages, chatMessage{
		kind: msgKindSent, senderID: m.myID, text: text, timestamp: time.Now(),
	})
}

func (m *model) setNotification(msg string, kind messageKind) {
	m.notification = msg
	m.notificationKind = kind
	m.notificationAt = time.Now()
}

// ─────────────────────────────────────────────────────────────
// Key exchange handling
// ─────────────────────────────────────────────────────────────

// handleKeyExchange is called when the peer's public key arrives.
// The shared secret and state transition are managed by the event loop in
// main.go via the keyExchangeReadyCmd command channel.
func (m model) handleKeyExchange(peerPubKey string, cmds *[]tea.Cmd) model {
	if m.state == stateHandshake {
		*cmds = append(*cmds, func() tea.Msg {
			return peerPubKeyReceivedMsg{publicKey: peerPubKey}
		})
		// } else if m.state == stateAwaitingResponse || m.state == statePendingIncoming {
	} else if m.state == stateAwaitingResponse {
		m.pendingPeerPublicKey = peerPubKey
	} else {
		m.appendSystem("Received unexpected key-exchange packet — ignoring.")
	}
	return m
}

// peerPubKeyReceivedMsg is a tea.Msg that carries the peer's public key.
// It is handled in main.go's wrappedUpdate which has access to the key material.
type peerPubKeyReceivedMsg struct{ publicKey string }

// sharedSecretDerivedMsg is posted by main.go's wrappedUpdate after
// successfully deriving the shared secret from the peer's public key.
type sharedSecretDerivedMsg struct {
	peerID        string
	sharedSecret  []byte
	safetyNumber  string
	peerPublicKey string
}

// ─────────────────────────────────────────────────────────────
// Commands: outgoing WebSocket messages
// ─────────────────────────────────────────────────────────────

// sendConnectRequest returns a tea.Cmd that enqueues a connect-request packet.
func (m *model) sendConnectRequest(targetID string) tea.Cmd {
	return func() tea.Msg {
		return sendPacketMsg{targetID: targetID, msgType: "connect_request"}
	}
}

// sendConnectResponse returns a tea.Cmd that enqueues a connect-response packet.
func (m *model) sendConnectResponse(targetID string, accepted bool, reason string) tea.Cmd {
	return func() tea.Msg {
		return sendConnectResponseMsg{targetID: targetID, accepted: accepted, reason: reason}
	}
}

// sendKeyExchange returns a tea.Cmd that triggers the public-key transmission.
// The actual encoding happens in main.go's wrappedUpdate.
func (m *model) sendKeyExchange() tea.Cmd {
	peerID := m.peerID
	return func() tea.Msg {
		return sendKeyExchangeMsg{peerID: peerID}
	}
}

// sendDisconnect enqueues a disconnect packet to the current peer.
func (m *model) sendDisconnect() tea.Cmd {
	peerID := m.peerID
	return func() tea.Msg {
		return sendDisconnectMsg{peerID: peerID}
	}
}

// sendChatMessage encrypts and enqueues a chat message.
func (m *model) sendChatMessage(text string) []tea.Cmd {
	m.appendSent(text)
	*m = m.syncViewport()
	peerID := m.peerID
	return []tea.Cmd{func() tea.Msg {
		return sendChatMsg{peerID: peerID, plaintext: text}
	}}
}

// sendFile enqueues a file for encrypted transfer.
func (m *model) sendFile(filePath string) tea.Cmd {
	peerID := m.peerID
	return func() tea.Msg {
		return sendFileMsg{peerID: peerID, filePath: filePath}
	}
}

// wsFileChunkMsg is posted when an encrypted file chunk arrives.
type wsFileChunkMsg struct {
	fromID      string
	filename    string
	chunkIndex  int
	totalChunks int
	data        []byte
}

// ─────────────────────────────────────────────────────────────
// Internal command message types (handled in main.go wrappedUpdate)
// ─────────────────────────────────────────────────────────────

type sendPacketMsg struct {
	targetID string
	msgType  string
}

type sendConnectResponseMsg struct {
	targetID string
	accepted bool
	reason   string
}

type sendKeyExchangeMsg struct{ peerID string }
type sendDisconnectMsg struct{ peerID string }
type sendChatMsg struct {
	peerID    string
	plaintext string
}
type sendFileMsg struct {
	peerID   string
	filePath string
}
