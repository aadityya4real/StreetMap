# StreetMap API contract (proposal)

**Status:** the public note-discovery endpoint is implemented for exact verified locations, explicit coordinate contexts, broad-area labels, unverified place labels, and bounded map-center nearby searches. Note detail and all mutation endpoints remain proposals.
**Current implementation:** `GET /api/v1/health`, `GET /api/v1/ready`, and `GET /api/v1/notes`, with JSON envelopes and validation errors.

## Contract conventions

- Base path is `/api/v1`; production traffic is HTTPS only. IDs are UUID strings. Timestamps are RFC 3339 UTC strings. JSON uses `snake_case`.
- Success bodies use `{ "data": ... }`. Errors use `{ "error": { "code": "...", "message": "...", "fields": { "field": "reason" } }`; `fields` is optional and messages never include SQL, credentials, stack traces, or provider internals. This preserves the envelope already used by the Go HTTP API.
- JSON request bodies are UTF-8, capped at 16 KiB for note and report writes, decoded once, and reject unknown fields. Responses set JSON content type and `X-Content-Type-Options: nosniff`.
- Public reads return only published notes and public author display names. They never include email, report details, hidden/removed/pending content, private moderation notes, or internal account status.
- Mutations require a valid StreetMap user identity. The backend resolves author identity from authentication, never a caller-supplied `author_user_id`.
- `limit` defaults to 20 and values outside 1–50 are rejected. `cursor` is a base64url continuation token whose format, phase, sort keys, and context hash are validated by the server and bound to the request context/order. It is pagination state, not an authorization or integrity credential. Stable ordering uses the primary sort key plus note UUID as a tie-breaker. Do not use offset pagination for a changing feed.
- Validation failures are `400 validation_error` with field keys; malformed JSON is `400 invalid_json`; unauthenticated is `401 unauthenticated`; disallowed operation is `403 forbidden`; absent or non-public resource is `404 not_found`; uniqueness/state conflict is `409 conflict`; oversized body is `413 request_too_large`; throttling is `429 rate_limited` with `Retry-After`; unexpected errors are safe `500 internal_error`.

## Endpoint inventory

| Method and path | Access | Purpose |
| --- | --- | --- |
| `GET /api/v1/health` | Public | Process liveness; current behavior retained. |
| `GET /api/v1/ready` | Public | Dependency readiness; current behavior retained. |
| `GET /api/v1/notes` | Public | Exact-context, broad-area, or bounded nearby note discovery. |
| `GET /api/v1/notes/{note_id}` | Public (proposed) | One published note; not implemented in this milestone. |
| `POST /api/v1/notes` | Authenticated | Submit a note for review. |
| `PATCH /api/v1/notes/{note_id}` | Authenticated owner | Edit an eligible own note. |
| `DELETE /api/v1/notes/{note_id}` | Authenticated owner | Soft-remove own note. |
| `PUT /api/v1/notes/{note_id}/vote` | Authenticated | Add or idempotently ensure own helpful vote. |
| `DELETE /api/v1/notes/{note_id}/vote` | Authenticated | Remove own helpful vote. |
| `POST /api/v1/notes/{note_id}/reports` | Authenticated | Report a note. |
| `GET /api/v1/me` | Authenticated | Current public profile and account state. |

## Discover notes

`GET /api/v1/notes` accepts exactly one context selector. The server validates all inputs and applies the 5 km ceiling itself; client-provided limits are never authoritative. Results are split into `exact`, `context_matches`, and `nearby` arrays so label-based results cannot be mistaken for verified exact-place results. The `context_matches` array contains same-type label matches for broad and unverified label requests; labels are trimmed and compared case-insensitively for exact equality, never fuzzy-matched.

### Known StreetMap location

`GET /api/v1/notes?location_id=7c48a51e-3d31-4a34-b503-88747a555a74&limit=20`

Use `location_id` only when the client already has a StreetMap location identity. `verified_place` locations can return `exact_location` results. A place label from a Maps URL is not sufficient to select or create that verified identity.

### Broad search/area

`GET /api/v1/notes?context_type=broad_area&label=Greater%20Noida%2C%20Uttar%20Pradesh`

Only `broad_area` notes may be returned in `context_matches` for a broad-area request. A readable query does not become a selected venue. Broad areas currently have labels, not polygons or points; never use an invented centroid to make a proximity query. Matching is exact after trim and case folding. This is label-context matching, not proof that two broad labels denote the same administrative boundary.

### URL-derived place page

`GET /api/v1/notes?context_type=place_page_unverified&label=GP%20Hotel%20%26%20Banquet`

This label is an unverified URL reference, not verified identity. The API may return only persisted `unverified_place_reference` notes with the same normalized label in `context_matches`. It never joins to a verified venue or computes proximity from the label. Exact label equality does not distinguish two different venues that happen to share a label; the response match type remains `unverified_reference_label`, not `exact_location`. Current schema cannot store provider/provider-place ID on this type because of `locations_verified_provider`; a stable reference identity requires a future schema decision.

### Map-center anchor

`GET /api/v1/notes?context_type=map_center&latitude=26.8178068&longitude=80.9071453&radius_m=5000`

The extension’s `@lat,lng` URL segment denotes the viewport center. Such coordinates are only a nearby-search anchor. They are never the selected venue coordinates and never yield exact results. Validate finite latitude in `[-90,90]`, longitude in `[-180,180]`; reject malformed or out-of-range values. `radius_m` defaults to 5000 and is accepted only in the range 1–5000. The current request selects `map_center` alone; it does not combine a URL label with a nearby anchor for broad or unverified-label matching.

### Coordinate-only selection

`GET /api/v1/notes?context_type=coordinate&latitude=...&longitude=...`

Reserve this for a user-confirmed coordinate selection explicitly captured by a supported UI. The current extension has no such signal, so it must not issue this request from a map-center URL. Coordinate-only exact matches require a persisted `coordinate_only` location, not approximate spatial proximity.

### Sparse exact results and response

For `location_id` only, return exact published notes first. If fewer than 3 are available, append up to 20 nearby published notes within 5,000 m when the verified location has a usable point. Three and twenty are conservative provisional implementation defaults, kept server-side constants rather than client policy. `map_center` requests return nearby results only; broad and unverified-label requests do not acquire nearby results. The query uses PostGIS geography distance (`ST_DWithin`/`ST_Distance`) and includes the originating location. Exact/context results order newest first with note UUID descending as a tie-breaker; nearby results order by distance ascending, then newest and note UUID descending. Each response page contains at most `limit` total notes across its result arrays.

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

`GET /api/v1/notes/{note_id}` returns `{ "data": { "note": ... } }` for a published note. A hidden, removed, pending, rejected, or unknown note is indistinguishable to a public caller (`404`).

## Mutations

### Create

`POST /api/v1/notes`

```json
{
  "location": { "type": "location_id", "id": "7c48a51e-3d31-4a34-b503-88747a555a74" },
  "title": "The quiet courtyard entrance",
  "body": "Use the lane beside the bakery; it is open during business hours.",
  "category": "local_tip"
}
```

Allow `location` as a discriminated union only: existing StreetMap `location_id`, a bounded label reference, or an explicit user-confirmed coordinate. Do not accept `author_user_id`, arbitrary status, vote count, provider verification claims, or a client-specified moderation state. `title` 3–120 chars; `body` 1–5000; categories are `hidden_gem`, `shortcut`, `local_tip`, `parking`, `warning`; trim for validation and store normalized user input consistently. Return `201` plus the note ID and `pending` state after the schema is migrated to support review-before-publication. Current schema defaults new notes directly to `published`; do not expose note creation until the pending-state migration and moderation path are ready.

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
