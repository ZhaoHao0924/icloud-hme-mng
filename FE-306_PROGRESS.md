# FE-306 Progress Handoff

Updated: 2026-08-02 (Asia/Shanghai)

## Completed

- Alias automation was implemented and pushed in `7c0ba87`.
- iCloud trust-challenge handling was added in `cfc615e`:
  - `HTTP 421` trust challenges are treated as a session-recovery condition.
  - Upstream trust/session tokens are not returned to the UI.
- China-region password login was fixed in `a544717`:
  - `accountLogin` now uses the account's regional setup endpoint.
  - The regional country code and returned WebAuth cookies are handled correctly.
  - A password login is only persisted after HME session validation succeeds.
- Inbox stale alias filtering was fixed in `a8c58e9`:
  - A URL alias filter that no longer belongs to the account is cleared automatically.
  - Inbox loading waits for a selected alias to be validated before sending an alias-filtered request.

## Current Local Acceptance Environment

- UI: `http://127.0.0.1:5184/`
- UI proxy target: `http://127.0.0.1:18084`
- Backend version: `manual-smoke-region-login-fix`
- The temporary port `18085` has been stopped.

## Verified Today

- The newly logged-in account has a WebAuth session and passed backend session validation.
- Read-only alias request: HTTP 200, currently zero aliases.
- Read-only inbox request: HTTP 200, Web API response parsed successfully.
- Frontend verification passed:
  - `npm test`: 22 files, 139 tests.
  - `npm run typecheck`
  - `npm run lint`
  - `npm run build`
- Backend verification passed after the session fixes:
  - `go test ./...`
  - `go vet ./...`

## Next Session

1. Have the user hard-refresh `http://127.0.0.1:5184/` and open Inbox again.
2. Confirm the stale `alias` URL parameter is removed and the default inbox view renders.
3. If the user requests further production acceptance, use read-only checks first. Do not create, deactivate, reactivate, or delete aliases without explicit approval because the configured account is real.
4. Check the GitHub Actions result for commits `cfc615e`, `a544717`, and `a8c58e9` if release readiness is needed.

