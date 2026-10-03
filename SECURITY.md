# Security Policy

Security is a core design principle of **TermChat**. We take all security vulnerabilities seriously and appreciate the efforts of the security research community to disclose vulnerabilities responsibly.

---

## Supported Versions

Only the latest release receive security patches and updates.

| Version | Supported          |
| :---    | :---               |
| `1.x`   | :white_check_mark: |
| `< 1.0` | :x:                |

---

## Security Architecture & Threat Model

TermChat is designed with a **zero-knowledge relay architecture**:

1. **End-to-End Encryption (E2EE)**:
   - Client sessions perform an ephemeral cryptographic key exchange.
   - All chat messages, files, and room payloads are encrypted with **AES-GCM-256** using derived session keys before leaving the client device.
2. **Blind Relay**:
   - The relay server (whether running locally, in Docker, or on Cloudflare Workers) acts strictly as a packet forwarder.
   - The relay routes packets based on `room_id` and ephemeral `target_id`. It never possesses private keys and cannot decrypt or inspect message contents.
3. **No Persistent Chat Storage**:
   - Neither the Go relay server nor the Cloudflare Worker persists chat message content to disk or databases. Messages exist transiently in memory solely for packet routing.
4. **Known Limitations**:
   - The relay server observes connection metadata, such as peer IP addresses, packet sizes, and timestamps.
   - Traffic analysis and metadata timing attacks are out of scope for the current relay design.

---

## Reporting a Vulnerability

If you discover a security vulnerability or suspect a security flaw in TermChat, **please do not disclose it publicly** through public GitHub issues, discussions, or pull requests.

### Disclosure Process

1. **Submit Privately**:
   - Send an email to [ankurdcs101106@gmail.com](mailto:ankurdcs101106@gmail.com) with the subject line:  
     `[SECURITY] TermChat Vulnerability Report - <Brief Description>`
   - Alternatively, submit a **GitHub Security Advisory** through the repository's *Security* tab if available.

2. **Information to Include**:
   - Clear description of the vulnerability.
   - Affected components (`cmd/client`, `cmd/server`, `pkg/crypto`, `pkg/protocol`, or `cloudflare/`).
   - Step-by-step instructions or Proof of Concept (PoC) to reproduce the vulnerability.
   - Any potential impact or exploitation scenarios.
   - Suggested remediation or patch (if you have one).

---

## Response Process & SLA

- **Initial Acknowledgment**: You will receive an acknowledgment within **48 hours** of receipt.
- **Assessment & Status**: We will evaluate the report, verify the impact, and provide status updates within **5 business days**.
- **Fix & Disclosure**: Once a fix is verified, a patch will be released along with appropriate security release notes crediting the reporter (unless anonymity is requested).

---

## Responsible Disclosure Guidelines

We ask that you:
- Allow reasonable time for remediation before any public disclosure.
- Do not exploit the vulnerability beyond what is strictly necessary to demonstrate proof-of-concept.
- Do not compromise the privacy of real users or degrade the performance of live relay services.
