# TermChat

TermChat is an open-source terminal chat client with end-to-end encrypted one-to-one sessions. Clients discover one another through a relay, then exchange ephemeral X25519 public keys and encrypt chat data locally with AES-256-GCM.

The relay assigns temporary six-character IDs and forwards protocol packets. It does not receive private keys, shared secrets, or plaintext chat messages.

## Current relay

The default client relay is the deployed Cloudflare Worker:

```text
wss://termchat-relay.meetkhamar3501.workers.dev/ws
```

Health check: <https://termchat-relay.meetkhamar3501.workers.dev/health>

## Quick start

### Download pre-built binaries

Download the latest release for your platform from [GitHub Releases](https://github.com/ankur3-101106/termchat-v2/releases/latest).

**Client binaries:** Linux (amd64/arm64), macOS (amd64/arm64), Windows (amd64/arm64), Android/Termux (arm64)

**Server binaries:** Linux (amd64/arm64), macOS (amd64/arm64), Windows (amd64/arm64)

```bash
# Linux/macOS/Termux
chmod +x termchat-linux-amd64
./termchat-linux-amd64

# Windows
termchat-windows-amd64.exe
```

The client uses the Cloudflare relay by default. To use a custom relay:
```bash
./termchat-linux-amd64 -server wss://your-relay.example.com/ws
```

### Build from source

Requirements: Go 1.23 or newer.

```bash
git clone https://github.com/ankur3-101106/termchat-v2.git
cd termchat-v2
go run ./cmd/client
```

Or build a local binary:

```bash
mkdir -p bin
go build -o bin/termchat ./cmd/client
./bin/termchat
```

### Cross-platform builds

```bash
./build-cross-platform.sh
```

This builds client and server binaries for Linux, macOS, Windows (amd64/arm64) and outputs to `bin/`.

## Using the client

1. Start the client on two machines.
2. Each client receives a temporary ID shown in the user list.
3. On one client, enter `/connect USER_ID`.
4. Accept the request on the other client.
5. Verify the safety number out of band with `/verify` or `Ctrl+V`.
6. Send messages after the encrypted session is established.

Commands:

| Command | Purpose |
| --- | --- |
| `/connect USER_ID` | Request a session with another online user |
| `/disconnect` or `/leave` | End the current session |
| `/sendfile PATH` | Send an encrypted file to the peer |
| `/verify` | Display the session safety number |
| `/whoami` | Display your temporary user ID |
| `/clear` | Clear chat history |
| `/panic` | Show the privacy screen |
| `/help` | Show built-in help |

Press `F1` for help and `Ctrl+C` to exit. Use `-log FILE` when troubleshooting connection problems:

```bash
./termchat-linux-amd64 -log /tmp/termchat.log
```

The server can also be selected with `TERMCHAT_SERVER`:

```bash
export TERMCHAT_SERVER=wss://example.com/ws
./termchat-linux-amd64
```

## Android / Termux

TermChat runs on Android via [Termux](https://termux.dev/) (install from F-Droid or GitHub):

```bash
# In Termux
pkg install curl
curl -LO https://github.com/ankur3-101106/termchat-v2/releases/download/v1.0.1/termchat-termux-arm64
chmod +x termchat-termux-arm64
./termchat-termux-arm64
```

The `linux/arm64` binary works natively in Termux's Linux environment.

## Run a local Go relay

The repository includes a self-hosted Go relay for local development or a server you operate yourself:

```bash
go run ./cmd/server -addr :8080
```

In another terminal:

```bash
go run ./cmd/client -server ws://localhost:8080/ws
```

The Go relay supports `PORT`, `PUBLIC_HOST`, and `PUBLIC_TLS` for deployment environments. For Docker-based deployments, see [Dockerfile](Dockerfile) and [docker-compose.yml](docker-compose.yml). The included [render.yaml](render.yaml) is an optional Render deployment configuration.

## Cloudflare Worker relay

The production relay is implemented with a Cloudflare Worker and Durable Object. Deployment and local Worker development instructions are in [cloudflare/README.md](cloudflare/README.md).

```bash
cd cloudflare
bun install
bunx wrangler deploy
```

Clients connect to the deployed Worker at its `/ws` path using `wss://`.

## Development

Run all Go tests:

```bash
go test ./...
```

Run the demo:

```bash
go run ./cmd/demo
```

The protocol and cryptography packages have focused unit tests. The relay tests cover connection IDs, user-list broadcasts, routing, and an encrypted session flow.

## Repository layout

```text
cmd/client/       Terminal client and WebSocket connection manager
cmd/server/       Go WebSocket relay
cmd/demo/         Protocol and encryption demonstration
pkg/crypto/       X25519, key derivation, AES-GCM, and safety numbers
pkg/protocol/     Shared JSON packet types
cloudflare/       Cloudflare Worker relay and Durable Object
scripts/test-relay/ Relay test utility
bin/              Cross-platform binaries (git-ignored, built locally)
```

## Security notes

- Chat payloads are encrypted before they are sent to the relay.
- Each client creates an ephemeral X25519 key pair for its process session.
- AES-GCM authenticates encrypted messages and file chunks.
- The safety number should be compared through a separate trusted channel.
- The relay still sees connection metadata such as temporary IDs, timing, and packet routing fields.

TermChat is provided under the [MIT License](LICENSE).