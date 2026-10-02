# E2E Demo (Stacker News + NIP-46)

## Reproducible Steps
1. Run the server:
   - `go run ./cmd/server`
2. Open:
   - `http://127.0.0.1:8080`
3. Generate a new key or import an existing key (`nsec` or 64-char hex).
4. Start the signer (default relay is prefilled; relay remains user-configurable).
5. Copy the generated `bunker://` token.
6. Open Stacker News Nostr login:
   - `https://stacker.news/signup?callbackUrl=https%3A%2F%2Fstacker.news%2F&type=nostr`
7. Paste the token in the Nostr login flow.
8. Complete login/approval.

## Expected Signer States
- `waiting`
- `connected`
- `signed`

Expected progression during a successful flow:
- `waiting -> connected -> signed`
