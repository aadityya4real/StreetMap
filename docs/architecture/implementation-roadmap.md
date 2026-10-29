# StreetMap implementation roadmap

This order follows the current dependency graph: the backend is Go `net/http` with health/readiness and public note discovery, the schema has user/location/note/vote/report tables but no auth identity or moderation audit, and the extension remains on demo data. Each milestone should be separately reviewable and tested.

## Stage 4 / Stage 5A — Contract and provider decision (complete as documentation)

- Finalize the public API envelope, endpoint and validation contract.
- Select a managed identity provider and confirm website BFF versus same-origin server handling.
- Resolve location identity, nearby-result provenance, note review and account deletion policies.
- Deliverable: API/location/authentication proposals and a recommendation for Auth0; no runtime behavior change or tenant setup is included.

## Next: Stage 5B — Auth0 preflight and schema readiness

- Decide and approve tenant/environment cost first. Current Auth0 Free pricing shows up to 25,000 external active users but only one tenant; Essentials currently includes three tenants and starts at $35/month with a listed 500-MAU tier. Reconfirm the live plan and expected MAU cost. Free email workflow customization is not included, and Auth0's built-in email provider is limited to testing (10 messages/minute, excess discarded, no reliability guarantee); select and budget an external production email provider.
- Configure separate development and production tenants if the approved plan supports them; otherwise obtain explicit approval for the risk of a shared tenant before using it. In each tenant, create a Regular Web Application client for the Go website BFF, a Native public client for the extension, and a custom API audience. Enable the same Auth0 database connection on both clients. Keep separate client IDs; never create or use an extension secret.
- Select the website's same-origin deployment topology and exact callback URLs. Proposed production callback/logout are `https://streetmap.in/auth/callback` and `https://streetmap.in/`; proposed local values use the current backend default `http://127.0.0.1:8080/auth/callback` and `http://127.0.0.1:8080/` pending confirmation that the BFF will serve that origin. Allowed Web Origins can remain unset for a server-side BFF; add only exact origins if a browser SDK/silent flow is introduced.
- Establish the Chrome Web Store extension ID and retain its public manifest key in the build configuration so local unpacked builds use the same ID; keep private signing material out of the repository. Register exactly `chrome.identity.getRedirectURL('streetmap-auth')` as the Native client's callback. If provider logout redirects back to the extension, also register the exact `getRedirectURL('streetmap-logout')` as Allowed Logout URL. Do not add wildcard callbacks or broad origins.
- Complete and record these smoke tests in a real MV3 build and disposable/test tenant before Stage 6: (1) user-triggered `launchWebAuthFlow` navigates and returns to the exact Chromium callback; reject altered callback/state and replayed code; (2) PKCE S256 verifier/challenge exchange succeeds without a client secret; (3) request the configured custom API audience and confirm the resulting access token is a JWT with exactly that audience (not an ID token or opaque `/userinfo` token); (4) the Go API accepts valid issuer/audience/signature/expiry/scope and rejects invalid issuer, audience, signature, expiry, algorithm and token type; (5) website BFF creates a server-managed session after validated code/ID token and clears/revokes it on logout; verify cookie attributes and confirm no token/session identifier is accessible to website JavaScript; (6) signup verification, resend/recovery, password reset, expired/reused links and generic failure responses work through the selected email provider; (7) determine whether Auth0-side CORS configuration is needed for the extension's token POST and whether StreetMap API CORS is needed, adding only exact origins if the test proves it. Auth0 `/authorize` must be invoked as browser navigation, not fetch/XHR. Chrome host permission for Auth0 token fetch belongs only in the later auth implementation and only for the tenant origin.
- Document the approved issuer, API identifier/audience, client IDs, callbacks, logout URLs, web origins, and server environment-variable names. Keep secrets in a deployment secret manager and ignored local env files only. No Auth0 tenant exists and these configuration/smoke checks have not yet been run.
- Add migrations for unique `(issuer, subject)` identity mapping and only the server-side web-session/revocation data needed by the same-origin BFF.
- Add pending/rejected note lifecycle, moderation event/audit storage, and a retention/anonymization path before exposing note submission.
- Define and migrate a safe way to store unverified external place references without making them verified places.
- Add unique constraints and transaction rules for provider identity, idempotency, state changes, and account lifecycle.
- Verify forward migration, constraints, rollback strategy, and integration tests on a dedicated disposable PostGIS test database. Never reset a valuable/local shared volume to test migrations.

## Stage 6 — Authentication and account security

- Implement the Auth0 website Regular Web Application authorization-code flow in the Go BFF: state/nonce, server-side code exchange, ID-token verification, `(iss,sub)` upsert, opaque `HttpOnly; Secure; SameSite` session, CSRF/Origin checks, logout and session revocation.
- Implement the extension as a Native public client using `launchWebAuthFlow` from a dedicated extension sign-in page opened by user gesture; validate redirect origin/path, state, PKCE S256 and API audience. Store short-lived access token only in `chrome.storage.session`; initially issue/store no extension refresh token.
- Add Go API middleware using a maintained Auth0 JWT validation package with cached JWKS, pinned issuer/audience/RS256, expiry and scope checks. Keep public note GET endpoints anonymous; protect mutation routes and authorize local user ownership/status on the server.
- Add rate limits and generic sign-in/reset errors. Do not include Auth0 client secrets in WXT/Vite output, manifest, extension storage, or repository.
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

Complete Stage 7 by deciding whether public clients need `GET /api/v1/notes/{id}` and reviewing the query plan/index behavior with representative data. Keep the extension disconnected until a separate integration milestone is approved. Stage 5B Auth0 tenant/email decisions and smoke tests remain a gate before Stage 6 authentication; do not expose note writes until authentication, pending-state moderation, and authorization are implemented and verified.
