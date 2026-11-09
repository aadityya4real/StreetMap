# Notes, location context, and moderation model

**Status:** behavior aligned to the persisted model, public read API, and authenticated note-creation foundation. Migration `00002` adds a normalized location-label lookup index; `00003` adds external identity mapping; `00004` adds note categories and a per-user idempotency ledger without changing existing note records or ownership relationships.

## Existing persisted model

`backend/migrations/00001_initial_schema.sql` is authoritative for the current database:

| Table | Current facts and constraints |
| --- | --- |
| `users` | UUID; nullable email with a unique generated normalized-email index for non-null values; display name 1–80 chars; status `active`, `suspended`, `deleted`; no credentials. |
| `auth_identities` | UUID; required `user_id` FK to `users` with `ON DELETE RESTRICT`; required issuer and subject; unique `(issuer, subject)`; creation timestamp and index by user. Multiple identities may map to one internal StreetMap user. |
| `locations` | UUID; types `verified_place`, `unverified_place_reference`, `broad_area`, `coordinate_only`; label 1–200; optional PostGIS `geography(Point,4326)`; provider and provider-place ID must be paired and are allowed only for `verified_place`; coordinate-only requires a point, while unverified and broad types cannot carry coordinates. Provider/place ID is unique; coordinate GiST and `(location_type, lower(btrim(display_label)))` lookup indexes exist after migration 00002. |
| `notes` | UUID; required author and location FKs with `ON DELETE RESTRICT`; title 3–120; body 1–5000; categories `hidden_gem`, `shortcut`, `local_tip`, `parking`, `warning`, `road_condition`, `safety`, `other`; status defaults to `published` and allows only `published`, `hidden`, `removed`; created/updated timestamps and update trigger. |
| `note_creation_requests` | Migration `00004`; `(user_id, idempotency_key)` is unique, request hash is exactly 32 bytes, and each successful key points to one note. Foreign keys restrict deletion. The record binds retries to an internal user and normalized payload. |
| `votes` | One `helpful` row per `(note_id,user_id)` via primary key; FKs restrict deletion. |
| `reports` | Reason 1–1000; statuses `open`, `reviewing`, `resolved`, `dismissed`, `actioned`; unique active report per reporter/note while open or reviewing; note/status index. |

The Go API exposes anonymous `GET /api/v1/notes` and authenticated `POST /api/v1/notes`. POST uses the validated internal identity from the bearer-authentication middleware and immediately publishes. Note detail, edit, vote, report, and moderation routes are absent. The extension has an anonymous public-notes client but is not connected to POST. Its current category validator accepts the original five categories only; the frontend must add `road_condition`, `safety`, and `other` before it can render pages containing those contributions. Its public-URL parser distinguishes `broad-area`, `place-page-unverified`, `map-center`, `unverified-place-reference`, `maps-no-location`, and `maps-unavailable` plus errors. It reads the URL only; do not expand it into DOM scraping or undocumented Maps state.

## Location evidence and query policy

Represent evidence strength explicitly in storage and every response. A UI/parser label is evidence about what URL text was observed, not proof that Google currently selects that business.

| Evidence | Existing extension signal | Persist/query meaning | Public wording |
| --- | --- | --- | --- |
| Verified place | Not available today | `verified_place` only after a supported, compliant verification/resolution process. Use `location_id` for exact notes. | Exact place context only after backend has a verified record. |
| Place route label | `place-page-unverified` with decoded URL segment | Unverified reference. Match only to the same explicit reference when intentionally persisted; do not text-join to verified venues. Never invent a provider ID or venue point. | “Place page detected” with the label; say it is URL-derived if context is discussed. |
| Search/area query | `broad-area` and query label | Broad-area context only; current extension may also include a viewport center but it remains a separate nearby anchor. | “Broad area/search context”; never selected venue. |
| Map viewport center | `map-center` or optional `mapCenter` on place/search context | A nearby-search anchor only. Coordinates following `@lat,lng` are not selected-business coordinates and should not be persisted as venue coordinates. | “Map center” with coordinates, clearly separate from place label. |
| User-confirmed coordinate | No current signal | Can map to `coordinate_only` with a point only after explicit user selection and consent. | Coordinate context, not a named business. |
| Place ID in URL query | `unverified-place-reference` | Unverified external reference; it is not validated against provider data by current parser. Never treat as an internal ID or verified identity. | Specific place reference available, unverified. |

The API accepts one label selector per request and may pair a broad or unverified label with the observed viewport center as a nearby-search anchor. Label matches remain in `context_matches`; map-center discoveries remain in `nearby`. The coordinates are never persisted as or interpreted as the labeled place's coordinates. A label without a center remains a label-only request, and a center without a label remains a `map_center` request. Never infer a selected point from route display text or a viewport center. If there is no reliable context, do not call discovery with fabricated values.

### Exact and nearby results

- `exact_location` means the note references the same verified StreetMap location UUID and the client requested that UUID. `exact_coordinate` is allowed only when a user-confirmed coordinate request exactly equals a persisted `coordinate_only` point; it is not a proximity match. A label equality or viewport-center proximity is never exact venue identity.
- Broad and unverified URL-label matches are returned in a distinct `context_matches` collection, never the `exact` collection. Matching uses same location type and exact label equality after trim and case folding. This cannot disambiguate different venues with an identical unverified label; the match remains explicitly `unverified_reference_label` and must not be described as verified identity.
- Return exact results first. Nearby results are a distinct array with `match.type = nearby`, source location label, and computed distance in meters. Never blend them into the selected place’s note list without a visible nearby designation.
- Nearby radius is at most 5,000 meters, enforced server-side. The current API uses PostGIS `geography`, `ST_DWithin` for index-assisted filtering, and `ST_Distance` for ordering; the reported distance is rounded to the nearest 10 meters to avoid implying survey precision. A URL viewport center can only anchor nearby candidates.
- Nearby notes supplement sparse exact results only for a verified point/location, when fewer than the initial proposed threshold of 3 published exact notes exist. A label context can also request separate nearby discoveries when its URL provides an independent map-center anchor; this does not verify the label or assign the center to it. Broad areas without an observed center must not trigger an invented centroid query.
- Candidate nearby notes must themselves have persisted points. Ignore missing points; return zero nearby results if query origin or candidates lack coordinates. The current API uses a three-second query context, deterministic keyset pagination, and a maximum of 20 supplemental nearby notes for sparse exact-location results. Map-center requests return nearby results only and are page-bounded by `limit`.
- Future ranking may use a quality score with vote volume and recency smoothing; the current public API uses deterministic newest-first ordering within exact/context groups and distance-first ordering for nearby results. Do not let raw vote count alone determine rank. Exclude self-votes in transaction and in any score. Distance is a separate relevance signal; do not label proximity as relevance to a business.

## Note creation and lifecycle

The current product decision is immediate publication. An authenticated POST transaction writes `status='published'`; no `pending` or `rejected` state is introduced. Public GET can therefore return a successfully created note immediately. This milestone adds eight allowed categories and does not change the status constraint or public ranking/grouping.

Every creation request requires an `Idempotency-Key` UUID. The database binds `(authenticated internal user ID, key)` to the SHA-256 hash of the normalized request and resulting note. A same-key/same-payload retry returns the original note; same-key/different-payload returns conflict. Idempotency records remain with the note to preserve retry safety. A database-serialized per-user rate limit defaults to 3 creations per hour and is configurable; completed idempotent retries do not consume another slot.

The writer rechecks active account status while holding a row lock in the creation transaction. It obtains author identity only from the validated request principal, never the JSON body. Verified locations must already exist as `verified_place`; unverified and broad labels persist as their separate location kinds with no coordinates; coordinate-only locations require bounded coordinates and the explicit `user_confirmed: true` request field. Map-center coordinates are never treated as selected-place coordinates.

No editing or moderation transitions are implemented. Existing statuses remain `published`, `hidden`, and `removed`. Any future moderation must have a separate approved product design and authorization model. A report remains a review signal, not automatic proof or an automatic removal threshold. Do not accept client-specified votes, status, author, moderation outcome, verification flags, or vote counts.

Votes remain one helpful vote per user per note, enforced by the composite PK. Repeated `PUT` is idempotent; removing a vote affects only the caller. Prevent authors voting on their own notes in an atomic transaction. Votes on hidden/removed notes should be rejected or hidden and excluded from public counts per implementation policy.

## Remaining schema and behavior gaps

1. **Website sessions:** Supabase identity mapping and Go JWT authentication are implemented. The future website BFF may need server-managed session/revocation records; none are added here. Email remains nullable and is not an identity key.
2. **Place reference identity:** `locations_verified_provider` requires provider and provider_place_id iff type is verified, so an unverified external place reference cannot currently persist its external provider ID. Add a separate source/reference table or fields with type-appropriate constraints; do not weaken verification semantics accidentally.
3. **Moderation lifecycle:** immediate publication is the current product decision. If moderation tools or delayed publication are later requested, design their states, audit events, authorization, and user messaging separately; this milestone intentionally adds no pending/rejected states.
4. **Account removal and retention:** `users.email` is nullable and remains unique when non-null; notes/votes/reports and auth identity FKs use `RESTRICT`. Physical user deletion conflicts with preserving attribution. Decide whether to deactivate and pseudonymize, retain a tombstone, or anonymize via a schema change while satisfying privacy/retention rules. Revoke identity grants/sessions regardless.
5. **Edits and public counts:** there is no edit history or moderation actor. The creation idempotency ledger is transactional; votes and reports have no new endpoints. Never make user-provided counters authoritative.
6. **Spatial model:** broad areas currently have only a display label; no polygon or administrative-area identifier exists. Do not fabricate a point/centroid. If polygon search is needed, define its source/licensing and geometry model separately.
7. **Integrity policy:** add checks/transactions for self-vote, eligible vote/report note status, and any title/body edit review policy. Database constraints should support invariants that must survive concurrent requests.

## Decisions still needed

- Whether author edits return published notes to pending, or whether only materially changed edits require review.
- Moderation roles, staffing, appeal/restore process, retention for reports/audit history, and abuse escalation.
- How a provider identity is verified and licensed before a location becomes `verified_place`.
- Whether StreetMap will support broad administrative regions, and which source and geometry provenance are acceptable.
