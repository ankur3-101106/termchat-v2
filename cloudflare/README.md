# Cloudflare relay

This directory contains the TermChat relay Worker. It uses one Durable Object instance as the global room and implements the same JSON packet protocol as the Go relay in `cmd/server`.

## Production endpoint

The current deployed Worker is:

```text
https://termchat-relay.meetkhamar3501.workers.dev
```

Clients use its WebSocket endpoint:

```text
wss://termchat-relay.meetkhamar3501.workers.dev/ws
```

Health check:

```bash
curl https://termchat-relay.meetkhamar3501.workers.dev/health
```

## Deploy

Requirements: Node.js and a Cloudflare account with Wrangler access.

```bash
cd cloudflare
npm install
npx wrangler login
npx wrangler deploy
```

Wrangler prints the deployed URL. Use that hostname with `wss://` and the `/ws` path when configuring clients:

```bash
go run ../cmd/client -server wss://YOUR_WORKER_HOST/ws
```

The Worker configuration is in [wrangler.toml](wrangler.toml). It defines the `RELAY` Durable Object binding and the `RelayServer` SQLite-backed Durable Object migration.

## Local development

Start the Worker:

```bash
cd cloudflare
npm install
npm run dev
```

Wrangler normally serves it at `http://127.0.0.1:8787`. In separate terminals, run two clients:

```bash
go run ./cmd/client -server ws://127.0.0.1:8787/ws
```

Use `/whoami` to see each temporary ID, then `/connect USER_ID` to begin a session.

## Endpoints

| Path | Method | Purpose |
| --- | --- | --- |
| `/` | GET | Plain-text service status |
| `/health` | GET | JSON health and version response |
| `/ws` | WebSocket upgrade | Relay connection |

Other paths return `404`. Non-WebSocket requests to `/ws` return `426 Expected WebSocket`.

## Architecture

- The Worker handles HTTP routing and forwards `/ws` requests to the Durable Object.
- The Durable Object assigns six-character client IDs and stores active WebSocket attachments.
- The relay routes connection requests, key exchanges, chat packets, and file chunks by target ID.
- The relay does not derive encryption keys or decrypt chat payloads.

For the client commands, local Go relay, tests, and security model, see the main [README](../README.md).
