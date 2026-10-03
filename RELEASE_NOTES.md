# TermChat v1.0.1 Release Notes

## What's New

### Session Resumption Support
- **Go Relay Server**: Fixed race condition in session resumption. When a client reconnects with the same ID (via `?id=` or `?client_id=` query param), the server now properly handles stale disconnects from the old connection without killing the active session.
- **Cloudflare Worker**: Added session resumption support matching the Go relay behavior. Clients can now resume their session after network interruptions by passing their ID in the WebSocket URL.

### Bug Fixes
- Fixed race condition where rapid reconnects could cause the relay to incorrectly notify peers of disconnection
- Server now detects when a newer connection has replaced a stale one and ignores the stale disconnect

### Cryptography & Protocol
- X25519 ephemeral key exchange with perfect forward secrecy
- AES-256-GCM authenticated encryption
- 6-digit Safety Numbers (SAS) for MITM verification
- Zero-knowledge relay - server never sees plaintext or keys

## Downloads

### Client Binaries

| Platform | Binary | Size | Download |
|----------|--------|------|----------|
| Linux x86_64 | `termchat-linux-amd64` | 7.3 MB | [Download](https://github.com/ankur3-101106/termchat-v2/releases/download/v1.0.1/termchat-linux-amd64) |
| Linux ARM64 | `termchat-linux-arm64` | 6.7 MB | [Download](https://github.com/ankur3-101106/termchat-v2/releases/download/v1.0.1/termchat-linux-arm64) |
| Linux ARM64 (Termux/Android) | `termchat-termux-arm64` | 6.7 MB | [Download](https://github.com/ankur3-101106/termchat-v2/releases/download/v1.0.1/termchat-termux-arm64) |
| macOS Intel | `termchat-macos-amd64` | 7.4 MB | [Download](https://github.com/ankur3-101106/termchat-v2/releases/download/v1.0.1/termchat-macos-amd64) |
| macOS Apple Silicon | `termchat-macos-arm64` | 6.8 MB | [Download](https://github.com/ankur3-101106/termchat-v2/releases/download/v1.0.1/termchat-macos-arm64) |
| Windows x86_64 | `termchat-windows-amd64.exe` | 7.4 MB | [Download](https://github.com/ankur3-101106/termchat-v2/releases/download/v1.0.1/termchat-windows-amd64.exe) |
| Windows ARM64 | `termchat-windows-arm64.exe` | 6.7 MB | [Download](https://github.com/ankur3-101106/termchat-v2/releases/download/v1.0.1/termchat-windows-arm64.exe) |

### Relay Server Binaries

| Platform | Binary | Size | Download |
|----------|--------|------|----------|
| Linux x86_64 | `termchat-server-linux-amd64` | 6.7 MB | [Download](https://github.com/ankur3-101106/termchat-v2/releases/download/v1.0.1/termchat-server-linux-amd64) |
| Linux ARM64 | `termchat-server-linux-arm64` | 6.2 MB | [Download](https://github.com/ankur3-101106/termchat-v2/releases/download/v1.0.1/termchat-server-linux-arm64) |
| macOS Intel | `termchat-server-macos-amd64` | 6.8 MB | [Download](https://github.com/ankur3-101106/termchat-v2/releases/download/v1.0.1/termchat-server-macos-amd64) |
| macOS Apple Silicon | `termchat-server-macos-arm64` | 6.3 MB | [Download](https://github.com/ankur3-101106/termchat-v2/releases/download/v1.0.1/termchat-server-macos-arm64) |
| Windows x86_64 | `termchat-server-windows-amd64.exe` | 6.9 MB | [Download](https://github.com/ankur3-101106/termchat-v2/releases/download/v1.0.1/termchat-server-windows-amd64.exe) |
| Windows ARM64 | `termchat-server-windows-arm64.exe` | 6.2 MB | [Download](https://github.com/ankur3-101106/termchat-v2/releases/download/v1.0.1/termchat-server-windows-arm64.exe) |

### Convenience Symlinks
- `termchat` → `termchat-linux-amd64` (Linux default)
- `termchat-linux` → `termchat-linux-amd64` (Linux default)
- `termchat.exe` → `termchat-windows-amd64.exe` (Windows default)

## Quick Start

```bash
# Linux/macOS
chmod +x termchat-linux-amd64
./termchat-linux-amd64

# Windows
termchat-windows-amd64.exe

# Android (Termux)
chmod +x termchat-termux-arm64
./termchat-termux-arm64

# Custom relay server
./termchat-linux-amd64 -server wss://your-relay.example.com/ws
```

## Running a Local Relay

```bash
# Go relay server
./termchat-server-linux-amd64 -addr :8080

# Or with Docker
docker build -t termchat-relay .
docker run -p 8080:8080 termchat-relay
```

## Cloudflare Worker Deployment

```bash
cd cloudflare
bun install
bunx wrangler deploy
```

## Security

- All messages are end-to-end encrypted with AES-256-GCM
- Keys are exchanged via ephemeral X25519 Diffie-Hellman
- The relay server never has access to private keys or plaintext
- Verify Safety Numbers out-of-band for 100% MITM protection

## Changelog

- **v1.0.1** (2026-10-03): Session resumption fixes for both Go and Cloudflare relays
- **v1.0.0** (initial): Initial release with E2E encrypted chat, file transfer, safety numbers