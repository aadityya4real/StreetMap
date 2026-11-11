# StreetMap backend foundation

This is an independent Go module for StreetMap's shared API. It does not connect the WXT extension yet. The backend provides health/readiness checks, anonymous public note discovery, and an authenticated immediate-publication note-creation foundation over PostgreSQL/PostGIS. Note detail, editing, voting, reporting, and moderation remain future work.

## Prerequisites

- Go 1.27.3 or newer. The module declares Go 1.27 and a Go 1.27.3 toolchain; Go's toolchain management downloads it when needed.
- Docker Engine with Docker Compose.
- PowerShell on Windows, or a POSIX shell.

The API uses Go's standard `net/http`, `log/slog`, and `database/sql`. `pgx/v5` is the PostgreSQL driver, Goose applies versioned SQL migrations from an embedded filesystem, and `golang-jwt/jwt/v5` verifies Supabase access tokens for future protected handlers. The selected pgx v5 release supports Go 1.25 and later; Goose v3.29 requires Go 1.26, which is why this module targets Go 1.27. [pgx support policy](https://github.com/jackc/pgx), [Goose releases](https://github.com/pressly/goose/releases).

## Local database

From this directory, make a local environment file and start PostGIS:

```powershell
Copy-Item .env.example .env
docker compose --env-file .env up -d
```

The Compose service uses `postgis/postgis:17-3.6`, maps PostgreSQL to `127.0.0.1:54329` by default, and persists data in a named Docker volume. The image provides PostGIS 3.6 for PostgreSQL 17. [PostGIS image versions](https://github.com/postgis/docker-postgis).

`.env.example` contains only local-development placeholders. Replace its local password before use. Compose reads `.env` for the database container. When `STREETMAP_ENV` is unset or set to `development`, the API also reads `.env` from the backend working directory as a local convenience. Values already present in the process environment take precedence. A missing `.env` is allowed; required configuration validation still applies.

For production, set `STREETMAP_ENV=production` and provide configuration through the process environment or deployment secret manager. The API does not load `.env` in production or other non-development modes.

PowerShell (optional when using the local `.env`):

```powershell
$env:DATABASE_URL = 'postgres://streetmap:replace-with-a-local-only-password@127.0.0.1:54329/streetmap?sslmode=disable'
```

POSIX shell:

```sh
export DATABASE_URL='postgres://streetmap:replace-with-a-local-only-password@127.0.0.1:54329/streetmap?sslmode=disable'
```

## Migrations and API

Migrations are explicit; the API never modifies the schema on startup. After PostgreSQL is healthy and `DATABASE_URL` is set, run:

```sh
go run ./cmd/migrate up
go run ./cmd/api
```

The server defaults to `127.0.0.1:8080`. It can start while PostgreSQL is unavailable; readiness reports the dependency failure until the database is reachable. `GET /api/v1/health` reports that the HTTP process is running. `GET /api/v1/ready` pings PostgreSQL with a timeout and returns `503` with a safe JSON error when unavailable. Unknown paths and unsupported methods use the same JSON error envelope. The server uses JSON structured logs, bounded HTTP/database settings, and graceful shutdown.

Configuration is read from environment variables:

| Variable | Default | Purpose |
| --- | --- | --- |
| `DATABASE_URL` | Required | PostgreSQL URL; never logged or returned |
| `HTTP_ADDR` | `127.0.0.1:8080` | HTTP listen address |
| `LOG_LEVEL` | `info` | `debug`, `info`, `warn`, or `error` |
| `DB_MAX_OPEN_CONNS` | `10` | Maximum pooled PostgreSQL connections |
| `DB_MAX_IDLE_CONNS` | `5` | Maximum idle connections; cannot exceed max open |
| `DB_CONN_MAX_LIFETIME` | `30m` | Maximum lifetime per pooled connection |
| `DB_CONN_MAX_IDLE_TIME` | `5m` | Maximum idle time per pooled connection |
| `HTTP_READ_HEADER_TIMEOUT` | `5s` | HTTP header read limit |
| `HTTP_READ_TIMEOUT` | `10s` | HTTP request read limit |
| `HTTP_WRITE_TIMEOUT` | `15s` | HTTP response write limit |
| `HTTP_IDLE_TIMEOUT` | `60s` | HTTP keep-alive idle limit |
| `READINESS_TIMEOUT` | `2s` | Database readiness ping limit |
| `SHUTDOWN_TIMEOUT` | `10s` | Graceful shutdown limit |
| `NOTE_CREATION_MAX_PER_WINDOW` | `3` | Maximum new notes per authenticated account in the configured window (maximum 100) |
| `NOTE_CREATION_WINDOW` | `1h` | Note-creation rate-limit window, from `1m` through `24h` |
| `SUPABASE_PROJECT_URL` | unset | Optional Supabase project HTTPS origin; enables backend token validation when all required auth settings are present |
| `SUPABASE_ISSUER` | unset | Exact project issuer, expected to be the project origin followed by `/auth/v1` |
| `SUPABASE_AUDIENCE` | unset | One exact audience value confirmed from the eventual project access tokens; no production value is assumed |
| `SUPABASE_JWT_ALGORITHM` | unset | One allowed asymmetric algorithm: `RS256`, `ES256`, or `EdDSA`, verified against the configured project |
| `SUPABASE_JWKS_CACHE_DURATION` | `5m` | Optional cache lifetime, greater than zero and no more than `10m` |
| `AUTH_CLOCK_SKEW` | `30s` | Optional JWT clock leeway, from `0s` through `2m` |

Leave all Supabase settings unset until a real StreetMap project supplies verified issuer, audience, and signing algorithm values. With no auth configuration, the existing public API starts normally. Partial or invalid auth configuration prevents startup. `Handler.Authenticated` is the reusable bearer-authentication wrapper for future protected routes; no current route is wrapped, so health, readiness, and public notes remain anonymous. A missing authenticator makes a future protected handler return `503`.

When enabled, the backend fetches keys only from the configured project's fixed `/auth/v1/.well-known/jwks.json` path, restricts algorithms to the configured asymmetric algorithm, validates signature, issuer, exact audience, expiration and not-before, and refreshes a bounded in-memory cache on unknown key IDs. Authentication resolves the validated `(iss, sub)` pair transactionally through `auth_identities`; it never maps by email, stores provider credentials, or links existing/demo users automatically. The account status is read from StreetMap on every protected request. `role=authenticated` is only an accepted Supabase access-token shape check and does not confer StreetMap privileges.

## Database design

The migrations enable PostGIS and create these tables. Migration `00004_note_creation.sql` expands note categories and adds the idempotency ledger; it does not change existing notes or the status constraint.

- `users`: nullable email with a unique normalized-email index for non-null values, display name, status, timestamps; no password or credential fields. User rows are retained and account status supports future deactivation.
- `auth_identities` (migration `00003`): external issuer and subject mapped to an internal `users.id`, unique by `(issuer, subject)`. It supports multiple provider identities per StreetMap user and deliberately contains no provider-specific columns or email mapping. Existing users, including seeded demo users, are not linked automatically.
- `locations`: typed as verified place, unverified place reference, broad area, or coordinate-only. Provider and provider place ID are separate fields and allowed only for verified places. URL-derived place labels and map-center coordinates do not imply venue identity. Coordinates use PostGIS `geography(Point, 4326)` (WGS 84); unverified place and broad-area records cannot carry a point, and coordinate-only records must carry one.
- `notes`: required author and location references, title/body/category/status, and timestamps. Foreign keys restrict deletion to preserve attribution and moderation history.
- `note_creation_requests`: unique per authenticated internal user and idempotency UUID; stores a SHA-256 hash of normalized request data and the successfully created note ID. The database record is retained for the lifetime of the note so retries remain safe. Migration rollback refuses if a note uses `road_condition`, `safety`, or `other` and removes the ledger only when rollback is safe.
- `votes`: one row per `(note_id, user_id)` using a primary-key uniqueness constraint, so a user can have at most one active vote on a note.
- `reports`: reporter, note, reason, status, and creation time. A partial unique index prevents duplicate open/reviewing reports by one user for one note while allowing a new report after resolution or dismissal.

Partial indexes support published notes by exact location, author, and category; a GiST index supports spatial filtering, and migration `00002` adds a btree lookup for normalized location labels used by broad-area and unverified-label queries. Vote and report lookup indexes remain in place. Nearby discovery uses PostGIS geography with `ST_DWithin` and `ST_Distance`, includes the originating location and approximate distance, and returns exact results separately from nearby results. No Maps data or coordinates are imported by this milestone.

## Public notes discovery

`GET /api/v1/notes` accepts one of these context selectors:

- `location_id=<uuid>`: exact notes for an existing `verified_place` StreetMap location; if fewer than three published notes exist and the place has coordinates, nearby notes within 5,000 m may supplement the response.
- `context_type=broad_area&label=<label>`: same-type broad-area records with the same trimmed, case-insensitive label.
- `context_type=place_page_unverified&label=<label>`: same-type unverified URL-label records only. Text equality is not identity verification; these matches are returned in `context_matches`, never as exact venue matches.
- `context_type=map_center&latitude=<lat>&longitude=<lon>&radius_m=<1..5000>`: nearby notes only; map-center coordinates never produce exact-place results.
- `context_type=coordinate&latitude=<lat>&longitude=<lon>`: exact equality against persisted user-confirmed `coordinate_only` locations; no proximity fallback.

`limit` defaults to 20 and must be 1–50. Pagination uses validated context-bound keyset cursors. Requests with invalid or repeated parameters return `400 validation_error`; unknown verified location contexts return a generic `404`; database failures return a safe `500`. Public projections contain no emails, account state, or report details. See [`docs/architecture/api-contract.md`](../docs/architecture/api-contract.md) for the response contract and matching constraints.

## Authenticated note creation foundation

`POST /api/v1/notes` uses the existing Supabase JWT verification and identity-resolution middleware. The author is exclusively the internal user ID in the authenticated request principal. Public `GET /api/v1/notes` remains anonymous. Successful new notes are explicitly stored as `published` and appear immediately through the existing public discovery query; no pending/rejected moderation states are introduced.

Each request requires an `Idempotency-Key` UUID. A retry with the same authenticated user, key, and normalized payload returns the original note; reusing the key with a different payload returns `409`. The write transaction locks the user's row, checks account status and idempotency, enforces the configured per-user rate limit, stores the location and note, and writes the ledger atomically. Exact verified locations must already exist in StreetMap. Unverified URL labels, broad labels, and user-confirmed coordinates remain separate persisted location types. Coordinate submissions require `user_confirmed: true`; a Google Maps viewport center is not implicitly accepted as a selected location. Strict JSON decoding rejects extra fields and all client-supplied author, status, vote, or verification fields.

## Local development seed data

The optional `cmd/seed` command writes fictional UI/API fixtures to a local development database only. It is never called by the API. The command requires `STREETMAP_ENV=development`, a loopback `DATABASE_URL` for the database named `streetmap`, and the explicit `--confirm-local-seed` flag. It refuses remote hosts, other development database names, production environments, and database schemas older than migration 2. It prints only the validated environment, database name, and host; it does not print the connection URL.

PowerShell, from `backend/` (the command reads the existing local `.env` without echoing its contents):

```powershell
$env:STREETMAP_ENV = 'development'
$databaseLine = Get-Content .env | Where-Object { $_ -match '^DATABASE_URL=' } | Select-Object -First 1
if (-not $databaseLine) { throw 'DATABASE_URL is missing from backend/.env' }
$env:DATABASE_URL = $databaseLine -replace '^DATABASE_URL=', ''
go run ./cmd/seed --confirm-local-seed
```

POSIX shell, from `backend/`:

```sh
export STREETMAP_ENV=development
export DATABASE_URL="$(sed -n 's/^DATABASE_URL=//p' .env | head -n 1)"
go run ./cmd/seed --confirm-local-seed
```

The seed uses fixed UUIDs, unique `.invalid` demo emails, and insert-only conflict handling. It verifies that any existing row with a seed ID still has the expected demo values and aborts rather than updating a conflicting row. One transaction and a PostgreSQL advisory lock make repeated or overlapping runs safe. All note titles include `[DEMO]`, and note bodies explicitly say they are fictional. Seed users remain unlinked from external authentication identities; email is not used to attach identities.

The v1 dataset contains 60 published fictional notes: 51 at a verified demo location so the extension's 50-note page can exercise “Load more,” two sparse exact notes plus a separate nearby note to exercise exact/nearby grouping, nearby verified locations at different distances (including one outside 5 km), an unverified place reference, a broad area, a coordinate-only pin, and sample helpful votes. The coordinates around `0,0` and `1,1` are synthetic test coordinates, not real business locations. The provider IDs use the `streetmap_demo` namespace and do not represent Google Place IDs.

To inspect only the seed rows, use the stable IDs and `[DEMO]` marker in `internal/devseed/seed.go`. To remove them, run the same command with `--remove`:

```powershell
go run ./cmd/seed --confirm-local-seed --remove
```

Removal validates the fixed seed IDs and their expected contents before deleting only those IDs. It removes the seed's own votes first. If any non-seed note, vote, or report references a seed user, location, or note, removal stops and rolls back so user-created data and moderation history remain intact. It does not delete Docker volumes, other rows, or reset the database.

## Tests

Run unit tests without Docker or a database:

```sh
go test ./...
```

Migration, identity-resolution, and public-notes integration tests skip unless `STREETMAP_TEST_DATABASE_URL` is set. They require a loopback database URL with a database name ending in `_test`; the public-notes suite requires the exact name `streetmap_test`. Identity and migration checks use isolated temporary schemas and never reset the database. Public-note fixtures are written in a transaction that is rolled back. For the Compose database, create a dedicated test database first:

```sh
docker compose exec db createdb -U streetmap streetmap_test
```

Then set `STREETMAP_TEST_DATABASE_URL` to the same local connection URL with database name `streetmap_test` and run `go test ./...`. Do not point this variable at a database with valuable data.

## Current boundaries

The WXT extension now sends authenticated note creation requests to `POST /api/v1/notes` through its typed API client, while public note reads remain anonymous. The backend independently validates the Supabase token and derives the author from its authenticated principal. The first authenticated request provisions only an unlinked local user for its validated `(issuer, subject)`. Email is nullable and remains unique when non-null, but provider email is not synchronized. Newly created notes are immediately public and the extension reloads the current public feed after creation. The composer supports unverified place labels, broad-area labels, and explicitly confirmed map-center coordinates. Verified-place selection remains unavailable until the extension has an approved way to obtain an existing StreetMap location ID; Google Maps references are not such IDs. This documentation does not establish whether production provider settings or real project tokens have been verified. Voting, reporting, editing, and moderation remain absent. If authentication config is absent, public reads still work and protected writes fail closed with `503 authentication_unavailable`.
