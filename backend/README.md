# StreetMap backend foundation

This is an independent Go module for StreetMap's shared API. It does not change or connect the WXT extension. The backend provides health/readiness checks and a public read-only notes discovery endpoint over PostgreSQL/PostGIS. Authentication, individual note detail, mutations, voting, reporting, and moderation remain future work.

## Prerequisites

- Go 1.27.3 or newer. The module declares Go 1.27 and a Go 1.27.3 toolchain; Go's toolchain management downloads it when needed.
- Docker Engine with Docker Compose.
- PowerShell on Windows, or a POSIX shell.

The API uses Go's standard `net/http`, `log/slog`, and `database/sql`. `pgx/v5` is the PostgreSQL driver, and Goose applies versioned SQL migrations from an embedded filesystem. The selected pgx v5 release supports Go 1.25 and later; Goose v3.29 requires Go 1.26, which is why this module targets Go 1.27. [pgx support policy](https://github.com/jackc/pgx), [Goose releases](https://github.com/pressly/goose/releases).

## Local database

From this directory, make a local environment file and start PostGIS:

```powershell
Copy-Item .env.example .env
docker compose --env-file .env up -d
```

The Compose service uses `postgis/postgis:17-3.6`, maps PostgreSQL to `127.0.0.1:54329` by default, and persists data in a named Docker volume. The image provides PostGIS 3.6 for PostgreSQL 17. [PostGIS image versions](https://github.com/postgis/docker-postgis).

`.env.example` contains only local-development placeholders. Replace its local password before use. Compose reads `.env` for the database container; Go does not load dotenv files, so export `DATABASE_URL` in the shell where you run Go.

PowerShell:

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

## Database design

`migrations/00001_initial_schema.sql` enables PostGIS and creates:

- `users`: normalized unique email, display name, status, timestamps; no password or credential fields. User rows are retained and account status supports future deactivation.
- `locations`: typed as verified place, unverified place reference, broad area, or coordinate-only. Provider and provider place ID are separate fields and allowed only for verified places. URL-derived place labels and map-center coordinates do not imply venue identity. Coordinates use PostGIS `geography(Point, 4326)` (WGS 84); unverified place and broad-area records cannot carry a point, and coordinate-only records must carry one.
- `notes`: required author and location references, title/body/category/status, and timestamps. Foreign keys restrict deletion to preserve attribution and moderation history.
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

The seed uses fixed UUIDs, unique `.invalid` demo emails, and insert-only conflict handling. It verifies that any existing row with a seed ID still has the expected demo values and aborts rather than updating a conflicting row. One transaction and a PostgreSQL advisory lock make repeated or overlapping runs safe. All note titles include `[DEMO]`, and note bodies explicitly say they are fictional.

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

Migration and public-notes integration tests skip unless `STREETMAP_TEST_DATABASE_URL` is set. They only accept a dedicated database name ending in `_test`; migrations are applied there, and note fixtures are written inside a transaction that is rolled back. For the Compose database, create a dedicated test database first:

```sh
docker compose exec db createdb -U streetmap streetmap_test
```

Then set `STREETMAP_TEST_DATABASE_URL` to the same local connection URL with database name `streetmap_test` and run `go test ./...`. Do not point this variable at a database with valuable data.

## Current boundaries

There is no authentication, session handling, note creation, voting, reporting, moderation, website, or extension/backend integration. The public notes endpoint is an unauthenticated read-only API and is not deployed at a production URL. Do not expose write routes until authentication, pending review state, and moderation are implemented.
