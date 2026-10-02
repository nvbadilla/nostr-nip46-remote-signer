# Nostr Remote Signer (Minimal NIP-46)

## Purpose
This project is a minimal Go + vanilla JS NIP-46 remote signer for a single user/session flow.

It lets you:
- import a Nostr private key (`nsec` or 64-char hex), or generate one
- derive and show public identity (`pubkey`, `npub`)
- start a signer connected to one configurable relay
- generate and copy a `bunker://` URL
- complete a Stacker News Nostr login flow

## Architecture
- Backend HTTP server (`cmd/server`) serves static UI and JSON API.
- API state/controller (`internal/server/api.go`) manages in-memory identity, signer lifecycle, and UI state.
- Key handling (`internal/keymgr`) validates and parses `nsec`/hex private keys.
- Signer logic (`internal/signer/service.go`) implements request/session logic for NIP-46 methods.
- Relay loop (`internal/signer/relay_runner.go`) subscribes to relevant events (kind `24133`, correct `p` tag), handles requests, publishes encrypted responses, and shuts down cleanly via context cancel.
- Frontend (`web/`) is vanilla HTML/CSS/JS and uses polling to render signer status.

## Prerequisites
- Go `1.25+` (module target is `go 1.25`)
- Network access to at least one Nostr relay
- A modern browser

## Run Commands
Install dependencies and run tests:

```bash
go test ./...
```

Run the app:

```bash
go run ./cmd/server
```

Optional environment variables:

```bash
PORT=8080
STATIC_DIR=web
NOSTR_RELAY_URL=wss://relay.nip46.com
```

Then open:

```text
http://localhost:8080
```

Note: default bind is loopback (`127.0.0.1`).

## Stacker News End-to-End Demo Steps
1. Open the app in your browser.
2. Import a private key (`nsec` or hex) or click Generate New Key.
3. Confirm public identity appears (`pubkey`, `npub`).
4. Set relay URL if needed.
5. Click Start Signer.
6. Copy the generated `bunker://` URL.
7. Open Stacker News Nostr login (`https://stacker.news/signup?callbackUrl=https%3A%2F%2Fstacker.news%2F&type=nostr`) from the built-in link.
8. Choose Nostr login flow and paste the `bunker://` URL.
9. Approve/connect from the client side; signer status should move from `waiting` to `connected` and later `signed` when signing occurs.

## Supported NIP-46 Methods
Implemented request methods:
- `connect`
- `get_public_key`
- `sign_event`
- `ping`
- `switch_relays`
- `logout`

Unsupported methods return an explicit error and preserve request ID in responses.

## Content Encryption Transports
- `NIP-44` is the default and current NIP-46 transport encryption for request/response content.
- `NIP-04` is supported only as **legacy interoperability** with older NIP-46 clients/signers. It is never initiated by this signer; it is detected automatically on inbound requests via the legacy `"?iv="` content marker.
- The response to any request is always encrypted using the same scheme (`NIP-44` or `NIP-04`) the request used. All other validation (signature, `p` tag, session, bunker secret) is unchanged regardless of transport.

## Verified E2E Findings
- Default relay is `wss://relay.nip46.com`.
- Current NIP-46 with NIP-44 transport remains supported.
- During Stacker News login interoperability testing, Stacker News was observed sending legacy NIP-04 payloads for kind `24133`.
- The observed Stacker News `connect` request used an empty signer pubkey parameter (`params[0] == ""`).
- Legacy compatibility is narrowly scoped to that `connect` shape and still requires valid event signature, valid `p` tag target, and exact match on the active one-time bunker secret.
- Private keys remain memory-only.
- Runtime model remains one identity and one active client/session.
- This project intentionally does not include a database, wallet, browser extension, or production-grade key storage.

## Assumptions
- One in-memory identity at a time.
- One active client session at a time.
- One relay runner active at a time.
- No persistence across process restarts.
- Client and signer can reach the configured relay.

## Scope Decisions
This implementation intentionally excludes:
- persistent key/session storage
- multi-user account/session management
- universal signer behavior across arbitrary methods/protocols

Reasoning:
- Key persistence was excluded to keep secrets memory-only and reduce local secret-at-rest risk.
- Multi-user support was excluded to keep the technical exercise small, avoid auth/database concerns, and match single-session scope.
- A universal signer was excluded to keep strict method allowlisting and predictable behavior for the targeted NIP-46 + Stacker News flow.

## Security Decisions
- Imported/generated private keys are kept in memory only.
- Imported key input is not returned by API responses.
- UI clears key input immediately after submit.
- Separate transport key is used for signer transport vs user identity key.
- `bunker://` secret is random one-time, consumed on successful `connect`, and rotated on `logout`.
- `bunker://` URI is only returned by signer start response; it is not exposed by GET/status APIs.
- Incoming requests validate:
  - event kind (`24133`)
  - event ID
  - event signature
  - `p` tag target
  - connected client/session authorization

## Tradeoffs
- UI status uses polling (simple and robust, not real-time push).
- `switch_relays` is implemented as a no-params query that returns the configured relay list (or `null`), without live reconnect behavior.
- Errors are surfaced to UI state, but observability/logging is intentionally minimal.

## Testing
- Core validation command: `go test ./...`
- This verifies key parsing, API behavior, NIP-46 request handling, relay-runner behavior, and NIP-44/NIP-04 interoperability paths used by the current implementation.

## Known Limitations
- No key/session persistence (state is lost on restart).
- Single identity, single active client/session model.
- No built-in auth/TLS layer for API endpoints (intended for local/dev workflow).
- Relay failover strategy is minimal.

## AI-Assisted Development Disclosure
This repository was developed with AI assistance (GitHub Copilot / GPT-based coding support) for code generation, test generation, and iterative implementation guidance. All resulting code paths were manually executed and verified with local `go test` runs in this workspace.
