# StreetMap backend foundation

This is an independent Go module for StreetMap's future shared API. It does not change or connect the WXT extension. The milestone provides the HTTP/runtime foundation, a PostgreSQL/PostGIS schema, and health/readiness checks; account authentication and note, vote, and report endpoints remain future work.

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

Partial indexes support published notes by exact location, author, and category; a GiST index supports spatial filtering, with btree indexes for vote and report lookups. A future nearby query should use `ST_DWithin(location.coordinates, selected_point, 5000)` and `ST_Distance` on geography values, return the originating location and distance, and order exact location matches ahead of nearby matches. Ranking and nearby discovery endpoints are not implemented here. No Maps data or coordinates are imported by this milestone.

## Tests

Run unit tests without Docker or a database:

```sh
go test ./...
```

The migration integration test skips unless `STREETMAP_TEST_DATABASE_URL` is set. It only accepts a dedicated database name ending in `_test`, applies the checked-in migrations, and verifies the tables and PostGIS extension. For the Compose database, create a disposable test database first:

```sh
docker compose exec db createdb -U streetmap streetmap_test
```

Then set `STREETMAP_TEST_DATABASE_URL` to the same local connection URL with database name `streetmap_test` and run `go test ./...`. Do not point this variable at a database with valuable data.

## Current boundaries

No authentication, sessions, note creation, voting, moderation, nearby discovery, website, or extension/backend integration is implemented. There is no production API URL or deployment claim. The next milestone should define the public note-read API and identity/authentication model before either client connects.
