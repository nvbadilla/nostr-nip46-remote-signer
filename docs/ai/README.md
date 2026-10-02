# AI-Assisted Development Notes

This project was built iteratively with GitHub Copilot support in the following sequence:

1. Scope definition
- Confirmed minimal NIP-46 signer boundaries and non-goals.

2. Implementation
- Implemented Go backend + vanilla frontend flow for identity import/generation, signer lifecycle, bunker URL generation, and relay-based request handling.

3. Tests
- Added/expanded unit tests for key management, API behavior, relay runner, request validation, and response handling.

4. Security/behavior audit
- Reviewed constraints: memory-only key handling, one-time bunker secret consumption, p-tag/signature/session validation, and single active client model.

5. E2E debugging
- Traced signer/relay/connect issues using temporary diagnostics and targeted regressions.

6. Interoperability fix
- Added narrow legacy compatibility for inbound NIP-04 connect while keeping NIP-44 default and preserving strict validation/security checks.

7. Final validation
- Removed temporary debugging traces, retained concise operational logs, and re-ran formatting/tests.

## Instruction Reference
- Assignment requested reference: `.github/copilot-instructions.md`.
- Current repository instruction file used in this workspace: `copilot-instructions.md`.

## Conversation Logs Organization
Existing Copilot logs were copied as-is (no rewriting) under:
- `docs/ai/logs/20aac6fe-138a-4f2b-a967-4ae2a80761fd.jsonl`
- `docs/ai/logs/main.jsonl`
- `docs/ai/logs/models.json`
