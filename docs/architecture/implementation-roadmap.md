# StreetMap implementation roadmap

This order follows the current dependency graph: the backend is Go `net/http` with health/readiness and public note discovery, the schema has user/location/note/vote/report tables and an external identity mapping foundation, and Stage 11A.5 adds backend JWT/identity middleware primitives. Client sign-in and contribution authorization remain future work. Each milestone should be separately reviewable and tested.

## Stage 4 / Stage 5A — Contract and provider decision (complete as documentation)

- Finalize the public API envelope, endpoint and validation contract.
- Select a managed identity provider and confirm website BFF versus same-origin server handling.
- Resolve location identity, nearby-result provenance, note review and account deletion policies.
- Deliverable: API/location/authentication proposals. Supabase Auth was selected in Stage 11A.2; no runtime authentication or tenant setup is implied.

## Superseded Stage 5B preflight

This former provider preflight is no longer active. Supabase Auth is the approved provider, and Stage 11A.4 supplies the identity table and nullable email schema foundation. Provider setup remains a separately authorized future step.

## Stage 6 — Authentication and account security (future; Supabase design)

- Implement the future website BFF flow with Supabase Auth: bind state/nonce, exchange and validate the authorization result server-side, resolve the validated `(iss,sub)` mapping, and issue an opaque `HttpOnly; Secure; SameSite` session with CSRF/Origin checks and explicit logout/revocation behavior.
- Implement extension Google login as a public client using `chrome.identity.launchWebAuthFlow` from a user-initiated extension page; validate the exact callback, state, PKCE S256 exchange, and access-token audience. Keep session material in extension-owned storage and away from content scripts and Maps pages.
- Integrate the Stage 11A.5 middleware into future protected routes only after real-project token claims and configuration have been verified. Keep public note GET endpoints anonymous; protect mutations and enforce local-user ownership/status server-side.
- Add rate limits and generic authentication/recovery errors when those flows are introduced. Never include provider client secrets or service-role keys in WXT/Vite output, the manifest, extension storage, or repository.
- Add tests for wrong issuer/audience/signature/expiry, replayed state/code, account linking, suspended/deleted accounts, token revocation, CSRF and callback allowlists.
- Do not ship note mutations until every path uses backend-derived identity and authorization.

## Stage 7 — Public read API (in progress)

- Implemented `GET /api/v1/notes` for verified location IDs, exact coordinate-only matches, strict-label broad/unverified context matches, and bounded map-center nearby search. Public projection includes only note fields, author display name, helpful count, and match provenance.
- Implemented stable keyset pagination, a 50-result page limit, published-only filtering, exact-before-nearby grouping, 5,000 m server ceiling, 20-note sparse-result nearby cap, and a normalized label index migration.
- Remaining: implement `GET /api/v1/notes/{id}` if it remains useful to product clients; check query plans using representative production-shaped data and continue contract review before client integration.
- Integration tests run against a dedicated `_test` PostGIS database. The extension remains on demo data and no API URL is configured in the client.

## Stage 8 — Note submission and ownership

- Implement authenticated create/edit/soft-delete; only allow server-selected fields and enforce title/body/category limits.
- Create submissions in pending state; define whether edits re-enter review. Add idempotency, duplicate/spam controls, and audit events.
- Test owner/non-owner access, invalid status transitions, SQL constraints, concurrent retries and public filtering.

## Stage 9 — Voting, reporting, and moderation operations

- Add idempotent helpful vote put/delete with unique database enforcement and self-vote prevention.
- Add report creation with active duplicate handling; keep report details private.
- Implement moderator capability, auditable transitions, queue access control, and reviewed restore/removal/appeal policy.
- Add abuse throttles and tests for concurrent vote/report requests, non-public notes, and audit completeness.

## Stage 10 — Extension integration

- First investigate a compliant and testable way to resolve selected Maps locations. Document limitations and applicable content/terms before integration; do not use undocumented Maps internals, DOM scraping, or guessed URL identity.
- Add the narrowest required StreetMap API host permission only when needed; keep API calls in extension context, tokens out of content scripts, and provenance visible in UI.
- Replace demo notes only after public-read behavior is verified; preserve no-context and loading/error states, and never imply demo data is contextual.
- Test current Chrome and packaged MV3 build, permission review, network failure, token expiration, malformed responses and no-location contexts.

## Stage 11 — Website and account experience

- Build the site and BFF/account pages using the same local StreetMap identity. Keep tokens server-side and make session/CSRF behavior explicit.
- Add public note pages only for published content and avoid leaking emails, report information, moderation metadata, or removed content.
- Add account deletion/export flows consistent with the retention decision and verify provider grant/session revocation.

## Stage 12 — Production hardening and launch gate

- Choose deployment, TLS, secret management, backup/restore, migrations, observability, error budget and incident response.
- Add structured audit logs without secrets/PII, rate limits, quotas, abuse monitoring, moderation staffing, data retention, privacy disclosures and security headers.
- Perform dependency and vulnerability review, API authorization review, threat-model review, load/query-plan validation, backup restore drill, and Web Store permission review.
- Launch only when database migrations, auth flows, abuse operations, privacy/retention, and rollback are supportable by the team.

## Immediate next milestone

Stage 11A.6 — configure an explicitly approved Supabase test project and verify its issuer, API audience, asymmetric signing algorithm, JWKS response, and access-token claim shape. Only after those values are confirmed should the backend auth settings be enabled. Then implement the Chrome extension's Google OAuth PKCE flow and persistent session policy. Stage 11A.5's reusable backend foundation is implemented but has not been exercised with real provider tokens. Do not expose note writes until client authentication, pending-state moderation, and authorization are implemented and verified.
