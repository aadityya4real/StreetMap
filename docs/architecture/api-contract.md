# StreetMap API contract

**Status:** public note discovery and authenticated note creation are implemented. Note detail, editing, voting, reporting, and moderation routes remain proposals.
**Current implementation:** `GET /api/v1/health`, `GET /api/v1/ready`, anonymous `GET /api/v1/notes`, and authenticated `POST /api/v1/notes`.

## Contract conventions

- Base path is `/api/v1`; production traffic is HTTPS only. IDs are UUID strings. Timestamps are RFC 3339 UTC strings. JSON uses `snake_case`.
- Success bodies use `{ "data": ... }`. Errors use `{ "error": { "code": "...", "message": "...", "fields": { "field": "reason" } }`; `fields` is optional and messages never include SQL, credentials, stack traces, or provider internals. This preserves the envelope already used by the Go HTTP API.
- Note creation JSON request bodies are capped at 32 KiB, decoded once, and reject unknown fields. Responses set JSON content type and `X-Content-Type-Options: nosniff`.
- Public reads return only published notes and public author display names. They never include email, report details, hidden/removed content, private moderation notes, or internal account status.
- Mutations require a valid StreetMap user identity. The backend resolves author identity from authentication, never a caller-supplied `author_user_id`.
- `limit` defaults to 20 and values outside 1–50 are rejected. `cursor` is a base64url continuation token whose format, phase, sort keys, and context hash are validated by the server and bound to the request context/order. It is pagination state, not an authorization or integrity credential. Stable ordering uses the primary sort key plus note UUID as a tie-breaker. Do not use offset pagination for a changing feed.
- Validation failures are `400 validation_error` with field keys; malformed JSON is `400 invalid_json`; unauthenticated is `401 unauthorized`; suspended/deleted account is `403 account_unavailable`; absent resource is `404 not_found`; idempotency conflict is `409 idempotency_conflict`; unsupported media type is `415 unsupported_media_type`; oversized body is `413 request_too_large`; throttling is `429 note_creation_rate_limited` with `Retry-After`; unexpected errors are safe `500 internal_error`.

## Endpoint inventory

| Method and path | Access | Purpose |
| --- | --- | --- |
| `GET /api/v1/health` | Public | Process liveness; current behavior retained. |
| `GET /api/v1/ready` | Public | Dependency readiness; current behavior retained. |
| `GET /api/v1/notes` | Public | Exact-context, broad-area, or bounded nearby note discovery. |
| `GET /api/v1/notes/{note_id}` | Public (proposed) | One published note; not implemented in this milestone. |
| `POST /api/v1/notes` | Authenticated | Publish a new note immediately. |
| `PATCH /api/v1/notes/{note_id}` | Authenticated owner | Edit an eligible own note. |
| `DELETE /api/v1/notes/{note_id}` | Authenticated owner | Soft-remove own note. |
| `PUT /api/v1/notes/{note_id}/vote` | Authenticated | Add or idempotently ensure own helpful vote. |
| `DELETE /api/v1/notes/{note_id}/vote` | Authenticated | Remove own helpful vote. |
| `POST /api/v1/notes/{note_id}/reports` | Authenticated | Report a note. |
| `GET /api/v1/me` | Authenticated | Current public profile and account state. |

## Discover notes

`GET /api/v1/notes` accepts exactly one context selector. A broad or unverified label selector may include both latitude and longitude to add a separate map-center nearby phase. The server validates all inputs and applies the 5 km ceiling itself; client-provided limits are never authoritative. Results are split into `exact`, `context_matches`, and `nearby` arrays so label-based results cannot be mistaken for verified exact-place results. The `context_matches` array contains same-type label matches for broad and unverified label requests; labels are trimmed and compared case-insensitively for exact equality, never fuzzy-matched.

### Known StreetMap location

`GET /api/v1/notes?location_id=7c48a51e-3d31-4a34-b503-88747a555a74&limit=20`

Use `location_id` only when the client already has a StreetMap location identity. `verified_place` locations can return `exact_location` results. A place label from a Maps URL is not sufficient to select or create that verified identity.

### Broad search/area

`GET /api/v1/notes?context_type=broad_area&label=Greater%20Noida%2C%20Uttar%20Pradesh`

Only `broad_area` notes may be returned in `context_matches` for a broad-area request. A readable query does not become a selected venue. Broad areas currently have labels, not polygons or points; never use an invented centroid to make a proximity query. Matching is exact after trim and case folding. This is label-context matching, not proof that two broad labels denote the same administrative boundary.

### URL-derived place page

`GET /api/v1/notes?context_type=place_page_unverified&label=GP%20Hotel%20%26%20Banquet`

This label is an unverified URL reference, not verified identity. The API may return only persisted `unverified_place_reference` notes with the same normalized label in `context_matches`. It never joins to a verified venue or computes proximity from the label. Exact label equality does not distinguish two different venues that happen to share a label; the response match type remains `unverified_reference_label`, not `exact_location`. Current schema cannot store provider/provider-place ID on this type because of `locations_verified_provider`; a stable reference identity requires a future schema decision.

When the Maps URL also provides a valid viewport center, the extension may request both signals while retaining the place-page label selector:

`GET /api/v1/notes?context_type=place_page_unverified&label=GP%20Hotel%20%26%20Banquet&latitude=26.8178068&longitude=80.9071453&radius_m=5000`

The API returns matching unverified-label notes in `context_matches` and map-center discoveries in `nearby`. The center is never treated as the venue's coordinates, and nearby notes are not attributed to the venue. Broad-area requests use the same optional coordinate/radius parameters. Latitude and longitude must be supplied together; `radius_m` requires both and defaults to 5000 when omitted. The radius remains restricted to 1–5000 metres.

### Map-center anchor

`GET /api/v1/notes?context_type=map_center&latitude=26.8178068&longitude=80.9071453&radius_m=5000`

The extension’s `@lat,lng` URL segment denotes the viewport center. Such coordinates are only a nearby-search anchor. They are never the selected venue coordinates and never yield exact results. Validate finite latitude in `[-90,90]`, longitude in `[-180,180]`; reject malformed or out-of-range values. `radius_m` defaults to 5000 and is accepted only in the range 1–5000. The anchor may be sent alone as `map_center`, or paired with a broad/unverified label selector; the paired form returns label matches and nearby results in their separate groups.

### Coordinate-only selection

`GET /api/v1/notes?context_type=coordinate&latitude=...&longitude=...`

Reserve this for a user-confirmed coordinate selection explicitly captured by a supported UI. The current extension has no such signal, so it must not issue this request from a map-center URL. Coordinate-only exact matches require a persisted `coordinate_only` location, not approximate spatial proximity.

### Sparse exact results and response

For `location_id` only, return exact published notes first. If fewer than 3 are available, append up to 20 nearby published notes within 5,000 m when the verified location has a usable point. Three and twenty are conservative provisional implementation defaults, kept server-side constants rather than client policy. `map_center` requests return nearby results only. Paired broad/unverified-label requests paginate through label matches first, then nearby results; they never promote the label or map center to an exact match. The cursor is bound to the selector, normalized label, coordinates, and radius. A note that qualifies for both groups is retained in `context_matches` and excluded from `nearby`. The query uses PostGIS geography distance (`ST_DWithin`/`ST_Distance`) and includes the originating location. Exact/context results order newest first with note UUID descending as a tie-breaker; nearby results order by distance ascending, then newest and note UUID descending. Each response page contains at most `limit` total notes across its result arrays.

Example:

```json
{
  "data": {
    "context": { "type": "map_center", "label": "GP Hotel & Banquet" },
    "exact": [],
    "context_matches": [],
    "nearby": [
      {
        "id": "8bc2b4ce-4277-4139-825d-c559694c8524",
        "title": "Tea stall behind the bus stop",
        "body": "The family-run stall is open early.",
        "category": "hidden_gem",
        "author": { "display_name": "Mira" },
        "created_at": "2026-10-20T08:30:00Z",
        "helpful_vote_count": 12,
        "match": {
          "type": "nearby",
          "location_label": "Bus stop near ...",
          "distance_m": 640
        }
      }
    ],
    "page": { "next_cursor": null, "has_more": false }
  }
}
```

`context.type` and `match.type` are server-defined provenance. `exact` is used only for a verified StreetMap `location_id` or an exact persisted coordinate-only point; label-based results use `context_matches` with `broad_area` or `unverified_reference_label`. Nearby results are in their own collection, have a distance and source label, and must never be rendered as notes about the selected place. The public response exposes only note content, author display name, timestamp, helpful-vote count, and match provenance; it does not expose email, account status, or reports. A nearby query excludes non-published notes and is bounded to a three-second database context and a page limit of 50. When no matches exist, return empty arrays and the requested context; do not guess a location.

`GET /api/v1/notes/{note_id}` returns `{ "data": { "note": ... } }` for a published note. A hidden, removed, or unknown note is indistinguishable to a public caller (`404`). This route is not implemented yet.

## Mutations

### Create

`POST /api/v1/notes`

Requires one valid Supabase bearer access token and one `Idempotency-Key` UUID header. The backend resolves the internal user from the validated `(issuer, subject)` identity; no client-supplied author ID is accepted. Each account is limited by `NOTE_CREATION_MAX_PER_WINDOW` within `NOTE_CREATION_WINDOW` (defaults: 3 notes per 1h). Replaying the same key and normalized payload returns the original note with `200`; using the same key with a different payload returns `409 idempotency_conflict`. New notes return `201` and are immediately `published`.

```json
{
  "location": { "type": "verified", "location_id": "7c48a51e-3d31-4a34-b503-88747a555a74" },
  "title": "The quiet courtyard entrance",
  "body": "Use the lane beside the bakery; it is open during business hours.",
  "category": "local_tip"
}
```

`location` is a strict discriminated union:

```json
{ "type": "unverified", "label": "GP Hotel & Banquet" }
{ "type": "broad", "label": "Greater Noida" }
{ "type": "coordinates", "label": "Near the west gate", "latitude": 28.5, "longitude": 77.2, "user_confirmed": true }
```

`verified` accepts only an existing `verified_place` StreetMap `location_id`; labels, coordinates, or provider assertions cannot promote an unverified reference. `unverified` and `broad` require a label and persist as their corresponding location kinds without coordinates. `coordinates` requires finite latitude in `[-90,90]`, longitude in `[-180,180]`, and explicit `user_confirmed: true`; it persists as `coordinate_only`. The coordinate confirmation is a request-contract assertion that the UI must collect from an explicit user action. A Google Maps viewport center is not sufficient and is not accepted as an implicit venue coordinate. If a coordinate label is omitted, the server stores `User-confirmed coordinates`.

The server trims title, body, and labels, validates Unicode character lengths, and accepts categories `hidden_gem`, `shortcut`, `local_tip`, `parking`, `warning`, `road_condition`, `safety`, and `other`. Title length is 3–120 characters; body is 1–5000; labels are 1–200. The body may contain line breaks and tabs but not other control characters. The JSON request is limited to 32 KiB. Unknown fields are rejected, including `author_user_id`, `user_id`, `status`, `vote_count`, and verification flags.

Example response:

```json
{
  "data": {
    "note": {
      "id": "8bc2b4ce-4277-4139-825d-c559694c8524",
      "title": "The quiet courtyard entrance",
      "body": "Use the lane beside the bakery; it is open during business hours.",
      "category": "local_tip",
      "author": { "display_name": "Mira" },
      "created_at": "2026-11-10T08:30:00Z",
      "helpful_vote_count": 0,
      "match": { "type": "exact_location", "location_label": "Verified cafe" }
    }
  }
}
```

Migration `00004_note_creation.sql` adds categories without changing existing notes and creates `note_creation_requests`, keyed by internal user ID and idempotency UUID. It stores a SHA-256 hash of the normalized payload and the successful note ID. The record is kept for the lifetime of the note so retries remain safe; a future retention policy must preserve the no-duplicate guarantee. The migration rollback refuses while any notes use one of the three added categories; rolling back also removes idempotency records, so it should be used only as a reviewed schema rollback.

The write transaction locks the active internal user row, checks idempotency before rate limiting, creates or resolves the location, explicitly inserts a `published` note, and records the idempotency result atomically. The lock serializes concurrent writes from the same account, protecting both duplicate retries and the per-account rate limit. No frontend composer, editing, vote, report, or moderation endpoint is included in this implementation.

### Edit and remove

`PATCH /api/v1/notes/{id}` accepts only `title`, `body`, and `category`; validate the same limits. The author may edit own pending notes; edits to published content should create an audit/review event and return it to pending (policy choice to confirm before implementation). Non-owner and non-editable states return `404` or `409` according to a consistent anti-enumeration policy. `DELETE` is a soft removal: transition own note to `removed`, retain author/location references, and return `204`; do not physically delete rows because foreign keys and attribution history use `RESTRICT`.

### Helpful votes

`PUT /api/v1/notes/{id}/vote` has no required body and creates the caller’s single `helpful` vote; repeat `PUT` is idempotent and returns `200` with `{ "helpful_vote_count": 13, "viewer_voted": true }`. `DELETE` removes only that caller’s vote and returns `200` with the resulting count/state. Database uniqueness on `(note_id,user_id)` is authoritative. Authors may not vote on their own notes; enforce in the transaction. Public counts exclude invalid/self votes if the policy is adopted and are not moderation status.

### Report

`POST /api/v1/notes/{id}/reports` body `{ "reason": "..." }`; validate 1–1000 characters. Return `201` for a new active report. A second active report by the same reporter for the same note returns `409 duplicate_active_report`; current partial unique index backs this rule. Do not return reports or report reasons through public note APIs.

### Current account

`GET /api/v1/me` returns `{ "data": { "user": { "id": "...", "display_name": "Mira", "account_status": "active" } } }`. Do not return email unless a separate authenticated account-management endpoint has a clear need. Suspended/deleted accounts cannot mutate; response is `403 account_unavailable` with a generic message.

## Health and readiness

Preserve current behavior and envelope:

- `GET /api/v1/health` is liveness-only: `200 {"data":{"status":"ok"}}`; it must not depend on Postgres.
- `GET /api/v1/ready` pings Postgres with the configured timeout and returns `200 {"data":{"status":"ready","dependencies":{"database":"available"}}}` or `503 {"error":{"code":"dependency_unavailable","message":"A required service is temporarily unavailable."}}`.
- Other methods return `405 method_not_allowed` with `Allow: GET`; unknown routes return `404 not_found`.

## References

- Current implementation: [`backend/internal/httpapi`](../../backend/internal/httpapi) and [`backend/migrations/00001_initial_schema.sql`](../../backend/migrations/00001_initial_schema.sql).
- [RFC 9110 HTTP Semantics](https://www.rfc-editor.org/rfc/rfc9110.html).
