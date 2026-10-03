# Contributing to TermChat

Thank you for your interest in contributing to **TermChat**! We welcome contributions of all kinds, whether you are fixing bugs, proposing new features, improving documentation, or adding tests.

Please review this document to ensure a smooth and productive workflow.

---

## Code of Conduct

By participating in this project, you agree to abide by the [Code of Conduct](CODE_OF_CONDUCT.md). Please report any unacceptable behavior to [ankurdcs101106@gmail.com](mailto:ankurdcs101106@gmail.com).

---

## Ways to Contribute

- **Reporting Bugs**: Open an issue detailing the bug, operating system, Go version, and step-by-step reproduction steps.
- **Suggesting Enhancements**: Propose ideas or UI/UX improvements through GitHub Issues before starting major work.
- **Improving Documentation**: Fix typos, clarify guides, or add architectural diagrams.
- **Writing Code**: Pick up an existing issue or implement an approved feature.

---

## Development Setup

### Prerequisites

- **Go**: 1.23 or higher ([golang.org](https://go.dev/dl/))
- **Git**: For version control
- **Node.js** (Optional): Only required if developing or deploying the Cloudflare Worker relay (`cloudflare/`)

### Clone and Run Locally

1. **Fork and clone the repository**:
   ```bash
   git clone https://github.com/ankur3-101106/termchat-v2.git
   cd termchat-v2
   ```

2. **Download Go dependencies**:
   ```bash
   go mod download
   ```

3. **Start the local relay server**:
   ```bash
   go run ./cmd/server -port 8080
   ```

4. **Connect two clients in separate terminal windows**:
   ```bash
   # Window 1 (Host a room)
   go run ./cmd/client -server ws://localhost:8080/ws

   # Window 2 (Join the room code printed in Window 1)
   go run ./cmd/client -server ws://localhost:8080/ws
   ```

5. **Run the interactive demo runner** (optional):
   ```bash
   go run ./cmd/demo
   ```

---

## Testing & Quality Assurance

Before submitting any code changes, ensure all tests and linters pass cleanly:

1. **Run Unit & Integration Tests**:
   ```bash
   go test -v ./...
   ```

2. **Run Static Analysis (vet)**:
   ```bash
   go vet ./...
   ```

3. **Format Code**:
   ```bash
   gofmt -s -w .
   ```

4. **Test Cross-Platform Compilation** (if touching builds or dependencies):
   ```bash
   ./build-cross-platform.sh
   ```

---

## Commit Guidelines

We enforce the **Conventional Commits** specification (`<type>(<scope>): <summary>`).

Please consult the project's [Commit & Release Guidelines](commit.md) for full details on:
- Allowed commit types (`feat`, `fix`, `docs`, `refactor`, `perf`, `test`, `build`, `ci`, `style`, `chore`).
- Component scopes (`client`, `server`, `protocol`, `crypto`, `worker`, `relay`, etc.).
- Formatting rules, breaking changes syntax, and release procedures.

### Example Commit:
```text
feat(client): add copy shortcut for 6-character room codes
```

---

## Pull Request Workflow

1. **Create a topic branch**:
   ```bash
   git checkout -b feat/my-new-feature
   # or
   git checkout -b fix/issue-description
   ```

2. **Keep changes focused**: Make atomic commits that address a single logical change.

3. **Ensure tests pass**: Add unit tests in `pkg/protocol`, `pkg/crypto`, or `cmd/server` if introducing new logic.

4. **Push your branch**:
   ```bash
   git push origin feat/my-new-feature
   ```

5. **Open a Pull Request**:
   - Provide a concise title following Conventional Commits.
   - Describe the motivation, changes, and testing performed.
   - Reference related issues (e.g., `Closes #42`).

---

## Project Structure Overview

```text
├── cmd/
│   ├── client/          # Terminal UI client (Bubbletea, Lip Gloss)
│   ├── server/          # Go WebSocket relay server
│   └── demo/            # Local interactive demo runner
├── pkg/
│   ├── crypto/          # Diffie-Hellman key exchange & AES-GCM-256 encryption
│   └── protocol/        # Wire protocol packet definitions & JSON serialization
├── cloudflare/          # Serverless relay worker (Cloudflare Workers + Durable Objects)
├── scripts/
│   └── test-relay/      # WebSocket integration test script
└── build-cross-platform.sh # Multi-platform build script (Linux, macOS, Windows)
```

---

## Need Help?

If you have questions or encounter issues, feel free to open a discussion or reach out to the project maintainers.
