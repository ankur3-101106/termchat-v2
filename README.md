<div align="center">

# 💬 TermChat

**Secure, end-to-end encrypted terminal chat with zero-knowledge relays.**

[![Latest Release](https://img.shields.io/github/v/release/ankur3-101106/termchat-v2?style=flat-square&logo=github&color=0969da)](https://github.com/ankur3-101106/termchat-v2/releases/latest)
[![Go Version](https://img.shields.io/badge/Go-1.23%2B-00ADD8?style=flat-square&logo=go&logoColor=white)](https://go.dev)
[![License: MIT](https://img.shields.io/badge/License-MIT-2ea44f.svg?style=flat-square)](LICENSE)
[![Encryption](https://img.shields.io/badge/Encryption-X25519%20%7C%20AES--256--GCM-8a2be2?style=flat-square&logo=matrix)](SECURITY.md)
[![Relay](https://img.shields.io/badge/Relay-Cloudflare%20Edge-F38020?style=flat-square&logo=cloudflare&logoColor=white)](https://termchat-relay.meetkhamar3501.workers.dev/health)
[![PRs Welcome](https://img.shields.io/badge/PRs-welcome-brightgreen.svg?style=flat-square)](CONTRIBUTING.md)

<p align="center">
  <a href="#quick-start">Quick Start</a> •
  <a href="#current-relay">Relay</a> •
  <a href="#using-the-client">Usage</a> •
  <a href="#android--termux">Android</a> •
  <a href="#self-hosting">Self-Hosting</a> •
  <a href="#security-notes">Security</a> •
  <a href="CONTRIBUTING.md">Contributing</a>
</p>

</div>

---

TermChat is an open-source terminal chat client with end-to-end encrypted one-to-one sessions. Clients discover one another through a relay, exchange ephemeral **X25519** public keys, and encrypt chat data locally with **AES-256-GCM**.

The relay assigns temporary six-character IDs and forwards protocol packets. It never receives private keys, shared secrets, or plaintext chat messages.

---

## 📡 Current Relay

The default client relay is deployed on a Cloudflare Worker with Durable Objects:

```text
wss://termchat-relay.meetkhamar3501.workers.dev/ws
```

- **Health Check**: <https://termchat-relay.meetkhamar3501.workers.dev/health>

---

## 🚀 Quick Start

### Automated Install (Linux, macOS, Android/Termux)

Run the automated installer to detect your system architecture, download the latest binary, and configure your PATH:

```bash
curl -fsSL https://raw.githubusercontent.com/ankur3-101106/termchat-v2/master/install.sh | bash
```

Or run locally from this repository:

```bash
./install.sh
```

---

### Download Pre-built Binaries

Download the latest release for your platform from [GitHub Releases](https://github.com/ankur3-101106/termchat-v2/releases/latest).

```bash
# Linux / macOS / Termux
chmod +x termchat-linux-amd64
./termchat-linux-amd64

# Windows
termchat-windows-amd64.exe
```

The client connects to the Cloudflare relay by default. To specify a custom relay:
```bash
./termchat-linux-amd64 -server wss://your-relay.example.com/ws
```

---

### Build From Source

**Prerequisite:** Go 1.23 or newer.

```bash
# Clone the repository
git clone https://github.com/ankur3-101106/termchat-v2.git
cd termchat-v2

# Run directly
go run ./cmd/client

# Or compile locally
mkdir -p bin
go build -o bin/termchat ./cmd/client
./bin/termchat
```

### Cross-Platform Builds

```bash
./build-cross-platform.sh
```

Builds binaries for Linux, macOS, and Windows across `amd64` and `arm64`, and saves them to `bin/`.

---

## 💻 Using the Client

1. Start the client on two different terminals or machines.
2. Each client receives a temporary six-character ID.
3. On one client, request connection: `/connect <USER_ID>`.
4. Accept the incoming request on the second client.
5. Verify the safety number out-of-band using `/verify` or `Ctrl+V`.
6. Start chatting securely with end-to-end encryption.

### Client Commands

| Command | Shortcut | Purpose |
| :--- | :--- | :--- |
| `/connect <USER_ID>` | — | Request a session with an online user |
| `/disconnect`, `/leave` | — | Terminate the current session |
| `/sendfile <PATH>` | — | Send an encrypted file to the connected peer |
| `/verify` | `Ctrl+V` | Display the cryptographic session safety number |
| `/whoami` | — | Display your assigned temporary ID |
| `/clear` | — | Clear current chat history |
| `/panic` | — | Immediately toggle privacy screen |
| `/help` | `F1` | Show built-in command assistance |

Press `Ctrl+C` to quit. For troubleshooting:
```bash
./termchat-linux-amd64 -log /tmp/termchat.log
```

Or set the default server environment variable:
```bash
export TERMCHAT_SERVER=wss://your-relay.example.com/ws
./termchat-linux-amd64
```

---

## 📱 Android / Termux

TermChat runs seamlessly on Android via [Termux](https://termux.dev/):

```bash
# Inside Termux
pkg update && pkg install curl
curl -LO https://github.com/ankur3-101106/termchat-v2/releases/download/v1.0.1/termchat-termux-arm64
chmod +x termchat-termux-arm64
./termchat-termux-arm64
```

---

## 🛠️ Self-Hosting

### Local Go Relay
Run a self-hosted Go relay server:
```bash
go run ./cmd/server -addr :8080
```

In another terminal, connect using:
```bash
go run ./cmd/client -server ws://localhost:8080/ws
```

Environment variables supported: `PORT`, `PUBLIC_HOST`, and `PUBLIC_TLS`. Container deployment files are available at [Dockerfile](Dockerfile) and [docker-compose.yml](docker-compose.yml), with optional Render configuration in [render.yaml](render.yaml).

### Cloudflare Worker Relay
The production relay runs on Cloudflare Workers using Durable Objects. See [cloudflare/README.md](cloudflare/README.md) for full deployment instructions:

```bash
cd cloudflare
bun install
bunx wrangler deploy
```

---

## 🧪 Development & Testing

Run all Go unit and integration tests:
```bash
go test ./...
```

Run the interactive encryption demo:
```bash
go run ./cmd/demo
```

---

## 📁 Repository Layout

```text
├── cmd/
│   ├── client/          # Terminal UI client (Bubbletea, Lip Gloss)
│   ├── server/          # Go WebSocket relay server
│   └── demo/            # Interactive encryption protocol demonstration
├── pkg/
│   ├── crypto/          # X25519 ECDH, HKDF key derivation, AES-256-GCM, safety numbers
│   └── protocol/        # Shared packet models and JSON serialization
├── cloudflare/          # Serverless relay worker (Cloudflare Workers + Durable Objects)
├── scripts/test-relay/  # Automated relay WebSocket integration test
└── build-cross-platform.sh # Multi-platform release build script
```

---

## 🔒 Security Notes

- **End-to-End Encryption**: Chat payloads and file transfers are encrypted locally with AES-256-GCM before transmission.
- **Ephemeral Sessions**: Each client session generates a fresh, ephemeral X25519 key pair that is discarded on disconnect.
- **Zero-Knowledge Relay**: Relays route packets using temporary IDs and never hold private keys or plaintext data.
- **Out-of-Band Verification**: Compare safety numbers (`/verify`) via an external channel to guard against MITM attacks.
- Review our full [Security Policy](SECURITY.md) for vulnerability reporting and architecture details.

---

## 📄 License

TermChat is open source released under the [MIT License](LICENSE).
