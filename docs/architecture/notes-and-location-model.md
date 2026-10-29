# Notes, location context, and moderation model

**Status:** behavior aligned to the existing persisted model and the public read implementation. Migration `00002` adds a normalized location-label lookup index; no other schema shape changes are made here.

## Existing persisted model

`backend/migrations/00001_initial_schema.sql` is authoritative for the current database:

| Table | Current facts and constraints |
| --- | --- |
| `users` | UUID; unique generated normalized email; display name 1–80 chars; status `active`, `suspended`, `deleted`; no credentials or external identity mapping. |
| `locations` | UUID; types `verified_place`, `unverified_place_reference`, `broad_area`, `coordinate_only`; label 1–200; optional PostGIS `geography(Point,4326)`; provider and provider-place ID must be paired and are allowed only for `verified_place`; coordinate-only requires a point, while unverified and broad types cannot carry coordinates. Provider/place ID is unique; coordinate GiST and `(location_type, lower(btrim(display_label)))` lookup indexes exist after migration 00002. |
| `notes` | UUID; required author and location FKs with `ON DELETE RESTRICT`; title 3–120; body 1–5000; categories `hidden_gem`, `shortcut`, `local_tip`, `parking`, `warning`; status defaults to `published` and allows only `published`, `hidden`, `removed`; created/updated timestamps and update trigger. |
| `votes` | One `helpful` row per `(note_id,user_id)` via primary key; FKs restrict deletion. |
| `reports` | Reason 1–1000; statuses `open`, `reviewing`, `resolved`, `dismissed`, `actioned`; unique active report per reporter/note while open or reviewing; note/status index. |

The Go API now exposes public `GET /api/v1/notes` discovery; note detail, auth, vote, report, mutation, and moderation routes are still absent. The extension does not call this API and uses separate demo notes. Its public-URL parser distinguishes `broad-area`, `place-page-unverified`, `map-center`, `unverified-place-reference`, `maps-no-location`, and `maps-unavailable` plus errors. It reads the URL only; do not expand it into DOM scraping or undocumented Maps state.

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

The current API accepts one context selector per request. For `place-page-unverified`, send the extracted label with `context_type=place_page_unverified`; for broad search, send broad query text with `context_type=broad_area`; and for viewport coordinates, send only `context_type=map_center` with latitude/longitude. These are separate requests because a label plus a viewport anchor is not currently represented as one combined context. Never infer a selected point from route display text or a viewport center. If there is no reliable context, do not call discovery with fabricated values.

### Exact and nearby results

- `exact_location` means the note references the same verified StreetMap location UUID and the client requested that UUID. `exact_coordinate` is allowed only when a user-confirmed coordinate request exactly equals a persisted `coordinate_only` point; it is not a proximity match. A label equality or viewport-center proximity is never exact venue identity.
- Broad and unverified URL-label matches are returned in a distinct `context_matches` collection, never the `exact` collection. Matching uses same location type and exact label equality after trim and case folding. This cannot disambiguate different venues with an identical unverified label; the match remains explicitly `unverified_reference_label` and must not be described as verified identity.
- Return exact results first. Nearby results are a distinct array with `match.type = nearby`, source location label, and computed distance in meters. Never blend them into the selected place’s note list without a visible nearby designation.
- Nearby radius is at most 5,000 meters, enforced server-side. The current API uses PostGIS `geography`, `ST_DWithin` for index-assisted filtering, and `ST_Distance` for ordering; the reported distance is rounded to the nearest 10 meters to avoid implying survey precision. A URL viewport center can only anchor nearby candidates.
- Nearby notes supplement sparse exact results only for a verified point/location, when fewer than the initial proposed threshold of 3 published exact notes exist. A place label and broad area have no reliable point in the present schema and do not qualify. A broad context may return broad-area notes labeled broad; it must not trigger an invented centroid query.
- Candidate nearby notes must themselves have persisted points. Ignore missing points; return zero nearby results if query origin or candidates lack coordinates. The current API uses a three-second query context, deterministic keyset pagination, and a maximum of 20 supplemental nearby notes for sparse exact-location results. Map-center requests return nearby results only and are page-bounded by `limit`.
- Future ranking may use a quality score with vote volume and recency smoothing; the current public API uses deterministic newest-first ordering within exact/context groups and distance-first ordering for nearby results. Do not let raw vote count alone determine rank. Exclude self-votes in transaction and in any score. Distance is a separate relevance signal; do not label proximity as relevance to a business.

## Note lifecycle and moderation

The current database allows immediate `published` status and lacks pending/rejected states and an audit log. Recommended public submission policy is review-before-publication:

```text
pending -> published
pending -> rejected
published -> hidden -> published (moderator restore)
published -> hidden -> removed (moderator removal)
published -> pending (meaningful author edit; policy decision)
published -> removed (author soft-delete)
```

Use server-side authorization for every transition. Authors may edit or soft-remove only their own eligible notes. Moderators require a separately defined role/capability and every moderation action records actor, note, previous/new state, reason, and timestamp. Public queries expose only `published`; return a generic 404 for non-public notes. A report is a review signal, not automatic proof or an automatic removal threshold. Allow one active report per reporter/note, matching the existing partial unique index; resolve/dismiss/action reports with an auditable moderator event. Never expose reporter identity or report body publicly.

Fields and validation should stay aligned with the SQL limits and categories listed above. Store only needed public author `display_name`; never expose email. Add a server-enforced idempotency key for note creation to reduce duplicate retry submissions, with bounded retention and key/payload binding. Detect likely spam/duplicates with rate limits, normalized similarity/location review queues, and moderation; do not make a fuzzy deduper silently merge content. Do not accept client-specified votes, status, author, moderation outcome, verification flags, or vote counts.

Votes remain one helpful vote per user per note, enforced by the composite PK. Repeated `PUT` is idempotent; removing a vote affects only the caller. Prevent authors voting on their own notes in an atomic transaction. Votes on hidden/removed notes should be rejected or hidden and excluded from public counts per implementation policy.

## Schema gaps to resolve in a later migration

1. **Authentication:** add `auth_identities(issuer, subject, user_id, ...)`, unique on issuer+subject. Add session/grant records only if the chosen website BFF/provider architecture requires local revocation. Current users has no auth fields; email cannot safely be the identity key.
2. **Place reference identity:** `locations_verified_provider` requires provider and provider_place_id iff type is verified, so an unverified external place reference cannot currently persist its external provider ID. Add a separate source/reference table or fields with type-appropriate constraints; do not weaken verification semantics accidentally.
3. **Review lifecycle:** add `pending`/`rejected` (and possibly explicit `deleted_at`) with a safe default before enabling note submission. Existing partial indexes only include published notes. Add moderation events/audit records before moderators act.
4. **Account removal and retention:** `users.email` is non-null and unique, and notes/votes/reports FKs use `RESTRICT`; physical user deletion conflicts with preserving attribution. Decide whether to deactivate and pseudonymize, retain a tombstone, or anonymize via a schema change while satisfying privacy/retention rules. Revoke identity grants/sessions regardless.
5. **Edits and public counts:** there is no edit history, moderation actor, vote aggregate, or transactional idempotency ledger. Add only what implementation requirements need; never make user-provided counters authoritative.
6. **Spatial model:** broad areas currently have only a display label; no polygon or administrative-area identifier exists. Do not fabricate a point/centroid. If polygon search is needed, define its source/licensing and geometry model separately.
7. **Integrity policy:** add checks/transactions for self-vote, eligible vote/report note status, and any title/body edit review policy. Database constraints should support invariants that must survive concurrent requests.

## Decisions still needed

- Whether author edits return published notes to pending, or whether only materially changed edits require review.
- Moderation roles, staffing, appeal/restore process, retention for reports/audit history, and abuse escalation.
- How a provider identity is verified and licensed before a location becomes `verified_place`.
- Whether StreetMap will support broad administrative regions, and which source and geometry provenance are acceptable.
