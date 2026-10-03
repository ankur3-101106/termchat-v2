# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

TermChat is an open-source terminal chat client with end-to-end encrypted one-to-one sessions. Clients discover one another through a relay, exchange ephemeral X25519 public keys, and encrypt chat data locally with AES-256-GCM. The relay assigns temporary six-character IDs and forwards protocol packets — it never receives private keys, shared secrets, or plaintext chat messages.

**Key technologies:**
- Go 1.23+ for client, server, and demo
- Bubble Tea (charmbracelet) for TUI
- Gorilla WebSocket for WebSocket connections
- X25519 (via golang.org/x/crypto) for ephemeral key exchange
- AES-256-GCM for authenticated encryption
- Cloudflare Workers + Durable Objects for production relay

## Commands

### Development
```bash
# Run client (connects to default Cloudflare relay)
go run ./cmd/client

# Run client with custom relay
go run ./cmd/client -server wss://your-relay.example.com/ws

# Run local Go relay server
go run ./cmd/server -addr :8080

# Run demo (spins up local relay + two simulated clients)
go run ./cmd/demo

# Run all Go tests
go test ./...

# Run specific package tests
go test ./pkg/crypto/...
go test ./pkg/protocol/...
go test ./cmd/server/...
```

### Building
```bash
# Build client binary
mkdir -p bin
go build -o bin/termchat ./cmd/client

# Build server binary
go build -o bin/relay-server ./cmd/server

# Cross-platform builds (Linux, macOS, Windows, amd64/arm64)
./build-cross-platform.sh

# Build Docker image
docker build -t termchat-relay .
```

### Cloudflare Worker Relay
```bash
cd cloudflare
npm install
npm run dev        # Local development (port 8787)
npx wrangler deploy  # Deploy to Cloudflare
```

### Testing against a running relay
```bash
# Against local Go relay
go run ./scripts/test-relay ws://localhost:8080/ws

# Against Cloudflare worker dev server
go run ./scripts/test-relay ws://127.0.0.1:8787/ws
```

## Architecture

### Repository Layout
```
cmd/
  client/     Terminal client + WebSocket connection manager (main.go, ui.go)
  server/     Go WebSocket relay server (main.go, main_test.go)
  demo/       Protocol/encryption demonstration with two simulated clients
pkg/
  crypto/     X25519 key generation, DH key derivation, AES-256-GCM, safety numbers
  protocol/   Shared JSON packet types (zero-knowledge wire format)
cloudflare/   Cloudflare Worker relay + Durable Object (TypeScript)
scripts/
  test-relay/ Integration test utility against a running relay
bin/          Cross-platform client and relay binaries (git-ignored in build)
```

### Core Components

**pkg/crypto** — Cryptographic primitives:
- `GenerateKeyPair()` — Creates ephemeral X25519 key pair per session (forward secrecy)
- `DeriveSharedSecret(privKey, peerPubKeyBase64)` — X25519 DH + SHA-256 → 32-byte AES key
- `Encrypt(key, plaintext)` — AES-256-GCM with random 12-byte nonce, returns base64
- `Decrypt(key, ciphertextBase64)` — Verifies auth tag, returns plaintext or error
- `CalculateSafetyNumber(pubA, pubB)` — 6-digit SAS (XXX-XXX) for MITM verification

**pkg/protocol** — Wire protocol (zero-knowledge for relay):
- Packet: `{type, sender_id, target_id, payload, timestamp}`
- Message types: HELLO, USER_LIST, CONNECT_REQUEST, CONNECT_RESPONSE, KEY_EXCHANGE, CHAT, FILE_CHUNK, DISCONNECT, ERROR, PING, PONG
- Payload structs for each type; relay only reads `type` and `target_id`

**cmd/server** — Go relay server (`main.go`):
- Single-process, in-memory client registry with `sync.RWMutex`
- WebSocket upgrade at `/ws`, health at `/health`
- Assigns 6-char short IDs, broadcasts USER_LIST on connect/disconnect
- Routes packets by `TargetID`; never inspects Payload
- Tracks `peerID` per client for disconnect notifications
- Session resumption via `?id=` or `?client_id=` query param

**cmd/client** — Terminal client (`main.go` + `ui.go`):
- Ephemeral X25519 key pair generated at startup
- WebSocket connection manager with auto-reconnect + application-level keepalive (25s ping)
- Bubble Tea TUI with state machine: connecting → idle → awaiting_response → handshake → chat
- `wrappedProgram` intercepts outgoing commands needing crypto/WS access
- Encrypted file transfer in 32 KiB chunks
- Safety number verification (Ctrl+V), Stealth Panic Mode (Ctrl+P), help (F1)

**cloudflare/** — Production relay (Durable Object):
- Same protocol as Go server, implemented in TypeScript
- Single global `RelayServer` Durable Object with SQLite persistence
- WebSocket attachments store client ID + peerID
- Wrangler config: `wrangler.toml` with Durable Object binding + migration

### Security Model

1. **Forward Secrecy**: New X25519 key pair per client process; session keys never reused
2. **Zero-Knowledge Relay**: Relay routes by `target_id` only; never sees plaintext or keys
3. **Authenticated Encryption**: AES-256-GCM with per-message random nonce + auth tag
4. **Safety Numbers**: 6-digit SAS derived from sorted public keys; verify out-of-band for MITM protection
5. **Key Confirmation**: Both parties derive identical shared secret via X25519 DH

### Client Commands (in TUI)
| Command | Purpose |
|---------|---------|
| `/connect USER_ID` | Request encrypted session with another user |
| `/disconnect` or `/leave` | End current session |
| `/sendfile PATH` | Send encrypted file (32 KiB chunks) |
| `/verify` or `Ctrl+V` | Show Safety Number for MITM verification |
| `/whoami` | Display your temporary user ID |
| `/clear` | Clear chat history |
| `/panic` or `Ctrl+P` | Toggle stealth terminal disguise |
| `/help` or `F1` | Show help overlay |

### Key Implementation Details

**Client session lifecycle:**
1. Generate ephemeral X25519 key pair + static 6-char session ID
2. Connect to relay (try explicit `-server`, `TERMCHAT_SERVER`, then `localhost:8080`)
3. Receive HELLO with assigned ID (matches requested ID if provided)
4. USER_LIST broadcasts show online users
5. `/connect TARGET` → CONNECT_REQUEST → target accepts → CONNECT_RESPONSE (accepted)
6. Both send KEY_EXCHANGE with their public keys
7. Each derives shared secret via `DeriveSharedSecret`; compute Safety Number
8. Encrypted CHAT packets (AES-256-GCM) routed through relay

**Reconnection logic (client):**
- Exponential backoff (max 30s) with `wsReconnectingMsg` updates to TUI
- Application-level keepalive (MsgPing every 25s) prevents Cloudflare 60s idle timeout
- Client read deadline 50s (within server's 45s pongWait + 10s writeWait)
- Session ID persisted via `?id=` query param for seamless resumption

**Relay server timeouts (Go):**
- `writeWait = 10s`, `pongWait = 45s`, `pingPeriod = 30s`
- `maxMessageSize = 64 KiB`

## Testing

- `pkg/crypto/crypto_test.go`: Key generation, DH exchange, AES-GCM encrypt/decrypt, tampering detection, safety number symmetry
- `pkg/protocol/protocol_test.go`: Packet encode/decode round-trip
- `cmd/server/main_test.go`: In-process relay + WebSocket client tests covering HELLO, USER_LIST, connect request/response, full encrypted session, error codes (self-connect, target-not-found, target-busy), crypto primitives
- `cmd/demo/main.go`: End-to-end demonstration with logged wire-format packets showing relay's zero-knowledge view
- `scripts/test-relay/main.go`: Standalone integration test against any running relay

Run all tests: `go test ./...`

## Environment Variables

| Variable | Purpose |
|----------|---------|
| `TERMCHAT_SERVER` | Default relay WebSocket URL for client |
| `PORT` | Relay server listen port (default :8080) |
| `PUBLIC_HOST` | Public hostname for relay (used in logs) |
| `PUBLIC_TLS` | Set to "true" if public endpoint uses TLS (wss://) |

## Deployment

- **Go relay**: Docker (multi-stage, scratch base), docker-compose, Render (render.yaml)
- **Cloudflare Worker**: `cd cloudflare && npx wrangler deploy` (requires Cloudflare account)
- **Binaries**: `./build-cross-platform.sh` produces 6 client + 6 server binaries in `bin/`

## Important Patterns

- **No direct crypto in TUI**: `wrappedProgram` in `main.go` intercepts `tea.Cmd` messages needing key material; TUI (`ui.go`) never touches raw keys
- **Zero-knowledge relay**: `routePacket` only switches on `pkt.Type` and `pkt.TargetID`; payloads forwarded opaquely
- **Channel-based WebSocket I/O**: `sendCh` (buffered 256) for writes, `readLoop` goroutine for reads; `writePump` drains send channel
- **Bubble Tea architecture**: `model.Update` handles UI events + WebSocket messages (as `tea.Msg`); `model.View` renders split panels
- **Safety number calculation**: Lexicographically sort both base64 public keys, SHA-256, extract two 10-bit values → "XXX-XXX"

## Files to Watch When Making Changes

- Protocol changes: `pkg/protocol/protocol.go` + `cloudflare/src/protocol.ts` (must stay in sync)
- Crypto changes: `pkg/crypto/crypto.go` + tests in `pkg/crypto/crypto_test.go`
- Client TUI: `cmd/client/ui.go` (state machine, rendering, keybindings)
- Client networking: `cmd/client/main.go` (WS connection, crypto lifecycle, reconnection)
- Server routing: `cmd/server/main.go` (relay logic, client registry, timeouts)
- Cloudflare Worker: `cloudflare/src/worker.ts` (mirrors Go relay logic)