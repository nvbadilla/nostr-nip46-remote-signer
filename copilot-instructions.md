
Project: minimal NIP-46 remote signer technical exercise.

Stack:

- Go backend.
- Vanilla HTML/CSS/JS only.
- Use `fiatjaf.com/nostr` for Nostr primitives, relay communication, NIP-44 and cryptography.
- Do not use a ready-made NIP-46 signer implementation.

Goal:
Allow a user to import or generate a Nostr secret key, run a NIP-46 remote signer, generate a `bunker://` URI and authenticate end-to-end with Stacker News.

Scope:

- One identity.
- One active client/session.
- Secret key kept in memory only and never logged.
- Separate ephemeral remote-signer key from user identity key.
- Configurable relay.
- One-time random bunker secret.
- Support `connect`, `get_public_key`, `sign_event`, `ping`, `switch_relays`, `logout`.
- Reject unsupported methods.
- Validate requests, signatures, p-tags and authorized session.
- No database, accounts, Docker, frontend frameworks or unrelated features.

Keep implementation small, idiomatic and testable.
Prefer stdlib and minimal dependencies.
Do not invent requirements or expand scope.

Before modifying code, briefly state assumptions and files affected.
Afterwards report commands run, tests and remaining tradeoffs.
