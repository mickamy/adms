# adms

_Pronounced "adams"._

PostgREST-style HTTP API for PostgreSQL and MySQL, plus an optional bundled admin UI — all in one binary.

[![CI](https://github.com/mickamy/adms/actions/workflows/ci.yml/badge.svg)](https://github.com/mickamy/adms/actions/workflows/ci.yml)
[![Go Report Card](https://goreportcard.com/badge/github.com/mickamy/adms)](https://goreportcard.com/report/github.com/mickamy/adms)
[![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)
[![GitHub Sponsors](https://img.shields.io/github/sponsors/mickamy?label=sponsor&logo=github)](https://github.com/sponsors/mickamy)

> Status: early development. See the [Roadmap](#roadmap) for what works today. This README is the spec we are building
> toward.

## TL;DR

Point `adms` at a database and you get two ways in: an HTTP API the frontend can call directly, and an optional
browser-based admin UI hosted from the same binary. No service layer, no codegen, no schema duplicated in two places.

### As an HTTP API

```sh
adms serve --driver=postgres --dsn="postgres://postgres@localhost/myapp?sslmode=disable"
```

```sh
# list active users, newest first, with their three latest posts embedded
curl 'http://localhost:7777/users?status=eq.active&order=created_at.desc&limit=10&select=id,name,posts(id,title)'
```

```json
[
  {
    "id": 42,
    "name": "alice",
    "posts": [
      {
        "id": 1001,
        "title": "Hello"
      },
      {
        "id": 998,
        "title": "Notes on B-trees"
      }
    ]
  }
]
```

### As a browser UI

```sh
adms serve --ui --driver=postgres --dsn="postgres://postgres@localhost/myapp?sslmode=disable"
# → open http://localhost:7778/
```

You land on a dark-mode admin console with schema-grouped tables in the sidebar, sortable / filterable / pageable row
views in the main pane, FK-aware embedded rows, inline editing, typed insert forms, and a built-in schema viewer. No
`node_modules`, no separate deploy — the UI is embedded in the binary.

The same idea drives both surfaces: reads, writes, joins, ordering, paging, counting — all defined by your database
schema.

## Why

Two friction points in every admin tool you have ever built:

1. **The backend is generic, but you keep writing it.** List endpoints with filters, sorting, paging, related rows,
   CRUD — the shape is already in the database schema, but every project hand-writes it again.
2. **The frontend is generic too, eventually.** Once the API exists, the dashboard becomes "tables with filters and
   forms" yet again. Spinning up a React app, picking a component library, and wiring it up is a separate, parallel
   project.

`adms` collapses both into a single binary. It introspects your database on startup and exposes
a [PostgREST](https://postgrest.org/)-style HTTP API automatically. Add `--ui` and the same binary serves a complete
admin frontend — no extra deploy, no separate codebase.

The closest neighbor is PostgREST itself: excellent, but PostgreSQL only and API only. `adms` aims for **PostgreSQL +
MySQL** and **API + (optional) UI**, with no extra dependencies to install beyond the binary.

## Install

Tagged releases are not out yet. Once `v0.1.0` ships:

```sh
# Homebrew (tap)
brew install mickamy/tap/adms

# go install
go install github.com/mickamy/adms@latest
```

While unreleased, build from source:

```sh
git clone https://github.com/mickamy/adms
cd adms
make build
./bin/adms --version
```

## Quickstart

### PostgreSQL

```sh
adms serve \
  --driver=postgres \
  --dsn="postgres://postgres@localhost:5432/myapp?sslmode=disable"
```

### MySQL

```sh
adms serve \
  --driver=mysql \
  --dsn="user:pass@tcp(localhost:3306)/myapp?parseTime=true"
```

On boot, `adms` introspects the target database, builds an in-memory schema model, and starts listening on `:7777` (
override with `--listen`). Every introspected table becomes a resource at `/<table_name>`. If `--ui` is set, a second
listener on `:7778` (override with `--ui-listen`) also serves the bundled admin UI from the same process.

Verify it works:

```sh
curl http://localhost:7777/                  # schema dump (JSON)
curl http://localhost:7777/healthz           # → "ok"
curl http://localhost:7777/<some_table>      # first 100 rows as JSON
```

## The HTTP API

`GET /<table>` returns rows. Everything else — filtering, projection, ordering, paging, embedding — is driven by URL
query parameters. Writes use `POST` / `PATCH` / `DELETE` with JSON bodies. The shape mirrors PostgREST so existing
clients and mental models transfer.

### Reading data

#### Filters

A filter has the form `?<column>=<op>.<value>`. Multiple filters are AND-combined.

| Operator     | Example                | SQL equivalent                                             |
|--------------|------------------------|------------------------------------------------------------|
| `eq`         | `status=eq.active`     | `status = 'active'`                                        |
| `gt` / `gte` | `age=gte.18`           | `age >= 18`                                                |
| `ilike`      | `name=ilike.AL*`       | `name ILIKE 'AL%'` (MySQL: case-insensitive via `LOWER()`) |
| `in`         | `id=in.(1,2,3)`        | `id IN (1, 2, 3)`                                          |
| `is`         | `deleted_at=is.null`   | `deleted_at IS NULL`                                       |
| `like`       | `name=like.al*`        | `name LIKE 'al%'`                                          |
| `lt` / `lte` | `score=lt.100`         | `score < 100`                                              |
| `neq`        | `status=neq.banned`    | `status <> 'banned'`                                       |
| `not`        | `status=not.eq.banned` | `NOT (status = 'banned')`                                  |

Wildcards in `like` / `ilike` use `*` (translated to `%`); `_` remains a single-character wildcard.

```sh
curl 'http://localhost:7777/users?status=eq.active&age=gte.18&deleted_at=is.null'
```

#### Projection (`select`)

By default, every column is returned. Use `select` to pick columns:

```sh
curl 'http://localhost:7777/users?select=id,name,email'
```

Use `*` to mean "all columns of this row":

```sh
curl 'http://localhost:7777/users?select=*,created_at'
```

#### Embedding related rows

`adms` reads foreign keys from the schema and lets you embed related rows by table name in parentheses:

```sh
# user → posts (one-to-many via posts.user_id → users.id)
curl 'http://localhost:7777/users?id=eq.1&select=id,name,posts(id,title,created_at)'
```

```sh
# post → author (many-to-one), with an alias
curl 'http://localhost:7777/posts?select=*,author:users(id,name)'
```

Embeds nest:

```sh
curl 'http://localhost:7777/users?select=*,posts(id,title,comments(id,body))'
```

Embedded relations resolve to JSON arrays for one-to-many, and JSON objects for many-to-one, derived from the FK
direction.

#### Ordering and paging

```sh
curl 'http://localhost:7777/users?order=created_at.desc,id.asc&limit=20&offset=40'
```

`limit` is capped (default 1000) and defaults to 100 when omitted. Configure both with `--default-limit` and
`--max-limit`.

#### Counting rows

To get a total count alongside the page, send `Prefer: count=exact`:

```sh
curl -i -H 'Prefer: count=exact' 'http://localhost:7777/users?limit=20'
```

```
HTTP/1.1 200 OK
Content-Range: 0-19/1342
Content-Type: application/json
```

### Writing data

All write methods accept JSON bodies (`Content-Type: application/json` assumed).

#### Insert

```sh
curl -X POST http://localhost:7777/users \
  -H 'Content-Type: application/json' \
  -d '{"name": "carol", "status": "active"}'
```

```
HTTP/1.1 201 Created
Location: /users?id=eq.42
```

#### Bulk insert

```sh
curl -X POST http://localhost:7777/users \
  -H 'Content-Type: application/json' \
  -d '[{"name": "dave"}, {"name": "eve"}]'
```

#### Update

`PATCH` requires at least one filter — `adms` rejects an unfiltered `PATCH` with `400 Bad Request` to prevent accidental
table-wide updates.

```sh
curl -X PATCH 'http://localhost:7777/users?id=eq.1' \
  -H 'Content-Type: application/json' \
  -d '{"status": "inactive"}'
```

#### Delete

Same rule as `PATCH`: a filter is mandatory.

```sh
curl -X DELETE 'http://localhost:7777/users?id=eq.1'
```

#### `Prefer` header

| Value                                 | Effect                                      |
|---------------------------------------|---------------------------------------------|
| `count=exact`                         | `Content-Range` header with total row count |
| `return=minimal` (default for writes) | Empty body, `Location` header for inserts   |
| `return=representation`               | Body contains the affected rows             |

```sh
curl -X POST http://localhost:7777/users \
  -H 'Content-Type: application/json' \
  -H 'Prefer: return=representation' \
  -d '{"name": "frank"}'
```

```json
{
  "id": 43,
  "name": "frank",
  "status": null,
  "created_at": "2026-05-21T08:12:00Z"
}
```

### Errors

Errors follow a PostgREST-shaped JSON envelope with adms-specific codes (prefixed `ADMS_`):

```json
{
  "code": "ADMS_UNKNOWN_COLUMN",
  "message": "column \"foo\" does not exist in table \"users\"",
  "details": null,
  "hint": "available columns: id, name, status, created_at"
}
```

| HTTP | code                    | When                                   |
|------|-------------------------|----------------------------------------|
| 400  | `ADMS_INVALID_FILTER`   | Bad operator or value format           |
| 400  | `ADMS_UNFILTERED_WRITE` | `PATCH` / `DELETE` without any filter  |
| 400  | `ADMS_UNKNOWN_COLUMN`   | Column name not in schema              |
| 403  | `ADMS_READ_ONLY`        | Write attempted while `--read-only`    |
| 404  | `ADMS_UNKNOWN_TABLE`    | Table name not in (allowed) schema     |
| 409  | `ADMS_CONFLICT`         | DB-level unique / FK violation         |
| 422  | `ADMS_INVALID_BODY`     | JSON body fails column-type validation |
| 500  | `ADMS_INTERNAL`         | Anything unexpected                    |

### Schema endpoint

`GET /` returns the introspected schema as JSON. A frontend (yours or the bundled admin UI) uses this to render forms,
infer column types, and discover relations without bundling a schema of its own.

```sh
curl http://localhost:7777/
```

```json
{
  "tables": [
    {
      "schema": "public",
      "name": "users",
      "primary_key": [
        "id"
      ],
      "columns": [
        {
          "name": "id",
          "type": "bigint",
          "nullable": false,
          "default": "nextval(...)"
        },
        {
          "name": "name",
          "type": "text",
          "nullable": false
        },
        {
          "name": "status",
          "type": "text",
          "nullable": true
        },
        {
          "name": "created_at",
          "type": "timestamptz",
          "nullable": false,
          "default": "now()"
        }
      ],
      "foreign_keys": [],
      "referenced_by": [
        {
          "table": "posts",
          "columns": [
            "user_id"
          ],
          "references": [
            "id"
          ]
        }
      ]
    }
  ]
}
```

## The admin UI

Enabled with `--ui`. Off by default, so API-only deployments stay lean. When on, the UI is served on a **separate
listener** (`--ui-listen`, default `:7778`) by the same process. The API at `:7777` stays untouched, with table names
occupying the full URL root. The UI calls the same HTTP API documented above, with CORS auto-configured between the two
listeners — it is not a parallel implementation, it is the first-class client of it.

The UI is a single-binary affair: HTML, CSS, and JavaScript are embedded into the `adms` executable via `embed.FS`. No
`node_modules`, no separate frontend deploy. It is rendered server-side with Go's `html/template`, made interactive
with [HTMX](https://htmx.org/), and styled with Tailwind CSS.

### What you get

- **Sidebar** — schema-grouped table list with incremental search
- **Table view** — row list with column resize, PostgREST-style filter builder, column-header sort, paging; the URL
  stays in sync so links are shareable
- **Row detail** — foreign keys are clickable: one-to-many appears as a collapsible nested table, many-to-one as a link
  to the parent row
- **Edit** — inline cell editing or modal edit, both wired to `PATCH /:table`
- **Insert** — typed input forms with FK pickers, a JSON editor for `json` columns, and dropdowns for enums
- **Delete** — confirm dialog → `DELETE /:table`
- **Schema viewer** — tables / columns / PKs / FKs / indexes, with a small ER diagram
- **Export** — stream the current filtered result as CSV or JSON

### Design

- **Dark mode** by default, with a light toggle that follows `prefers-color-scheme` and persists per browser
- **Responsive** down to tablet widths (>= 768px)
- **Accessible** — focus rings, ARIA labels, semantic HTML, full keyboard navigation
- **Keyboard shortcuts** — `Cmd/Ctrl+K` for the table palette, `↑↓` to walk rows, `Enter` to open, `Esc` to dismiss
- **Loading / empty / error states** drawn explicitly on every screen — optional does not mean half-finished

### Access

The UI calls the same HTTP API you would. Cross-origin calls between the two listeners are handled automatically —
`adms` adds the UI's origin to the API's allowed origins, so you do not need to list it in `--cors-origins`. When
`--auth-token-env=...` is set, the UI carries the token on every request.
When `--read-only` is set, the UI hides edit / insert / delete affordances. The UI does not introduce its own login
flow — keep it behind your network or gateway.

## Security

`adms` is designed to sit behind your authn layer (reverse proxy, API gateway, etc.), but it ships several built-in
safety nets so an accidental misconfiguration is not catastrophic.

### Identifier allowlist

Table and column names from query parameters are checked against the introspected schema before they are interpolated
into SQL. Unknown identifiers return `400 Bad Request`, never reach the database, and never appear in error messages
echoed back to the client unsanitized.

### Read-only mode

```sh
adms serve --read-only ...
```

Returns `403 Forbidden` for `POST`, `PATCH`, and `DELETE`. The admin UI hides write affordances in this mode. Useful for
staging dashboards, demos, or anywhere writes must be impossible by construction.

### Schema and table allowlist

Restrict which schemas (or tables) are exposed:

```sh
adms serve --allowed-schemas=public,reporting ...
adms serve --allowed-tables=users,posts,comments ...
```

Anything outside the allowlist is invisible — at `GET /`, at the per-table endpoints, and in the UI sidebar.

### Bearer token (planned, Phase 6)

```sh
adms serve --auth-token-env=ADMS_TOKEN ...
```

When set, requests must include `Authorization: Bearer <token>`. The admin UI carries the token automatically. This is
intentionally simple — for OIDC / JWT, terminate auth at your gateway.

### CORS

```sh
adms serve --cors-origins="https://admin.example.com,https://staff.example.com" ...
```

Defaults to no CORS headers, so the API is only reachable from same-origin contexts unless you opt in. When `--ui` is
enabled, the bundled admin UI's origin is automatically added to the allowed origins — you do not need to list it here.

### Mandatory filters on writes

`PATCH` and `DELETE` without a `where` clause return `400` — there is no "update every row" path, in the API or the UI.

## CLI

```
adms <command> [flags]

Commands:
  serve   Run the HTTP API server (and optional admin UI)
  check   Verify DB connectivity and schema introspection without starting the server

Flags:
  --version, -v   Print version
  --help, -h      Show help
```

### `adms serve`

| Flag                | Default        | Description                               |
|---------------------|----------------|-------------------------------------------|
| `--allowed-schemas` | driver default | Comma-separated schemas to introspect     |
| `--allowed-tables`  | _(all)_        | Comma-separated table allowlist           |
| `--auth-token-env`  | _(none)_       | Env var holding a bearer token to require |
| `--cors-origins`    | _(none)_       | Comma-separated allowed origins for CORS  |
| `--default-limit`   | `100`          | LIMIT applied when client omits it        |
| `--driver`          | _(required)_   | `postgres` or `mysql`                     |
| `--dsn`             | _(required)_   | Database connection string                |
| `--listen`          | `:7777`        | Listen address                            |
| `--log-level`       | `info`         | `debug` / `info` / `warn` / `error`       |
| `--max-limit`       | `1000`         | Cap on client-supplied LIMIT              |
| `--read-only`       | `false`        | Reject all write methods with `403`       |
| `--ui`              | `false`        | Mount the bundled admin UI                |
| `--ui-listen`       | `:7778`        | Listen address for the admin UI           |

### `adms check`

Verifies DSN connectivity and schema introspection without starting the server. Exits non-zero with a diagnostic on
stderr if anything fails — handy as a CI preflight step before deploying.

```sh
adms check --driver=postgres --dsn="postgres://..."
# → "ok: connected, introspected 12 tables in schema(s) public"
# → exit 0 on success, non-zero on failure
```

## Configuration (environment variables)

Every CLI flag has a matching environment variable. Flags win when both are set.

| Env                    | Flag                |
|------------------------|---------------------|
| `ADMS_ALLOWED_SCHEMAS` | `--allowed-schemas` |
| `ADMS_ALLOWED_TABLES`  | `--allowed-tables`  |
| `ADMS_CORS_ORIGINS`    | `--cors-origins`    |
| `ADMS_DEFAULT_LIMIT`   | `--default-limit`   |
| `ADMS_DRIVER`          | `--driver`          |
| `ADMS_DSN`             | `--dsn`             |
| `ADMS_LISTEN`          | `--listen`          |
| `ADMS_LOG_LEVEL`       | `--log-level`       |
| `ADMS_MAX_LIMIT`       | `--max-limit`       |
| `ADMS_READ_ONLY`       | `--read-only`       |
| `ADMS_UI`              | `--ui`              |
| `ADMS_UI_LISTEN`       | `--ui-listen`       |

For the bearer token, set `--auth-token-env=<ENV_NAME>` so `adms` reads the actual secret from the environment variable
named `<ENV_NAME>`. The token value itself is never read from a CLI flag, to avoid leaking it into shell history.

## Roadmap

- [x] Phase 0 — CLI scaffolding (`serve` / `check` subcommands), goreleaser metadata
- [ ] Phase 1 — Schema introspection (PostgreSQL + MySQL); working `adms check`
- [ ] Phase 2 — HTTP server, `GET /` schema endpoint, `GET /healthz`, graceful shutdown
- [ ] Phase 3 — Read API: filter, projection, ordering, paging
- [ ] Phase 4 — Read API: relation embedding (FK-aware JSON aggregation)
- [ ] Phase 5 — Write API: `POST` / `PATCH` / `DELETE`, `Prefer` header, `Content-Range`
- [ ] Phase 6 — CORS, logging, panic recovery, `--read-only`, allowlists, bearer token, README polish
- [ ] Phase 7 — Bundled admin UI (opt-in via `--ui`): served on a separate listener (`--ui-listen`, default `:7778`),
  HTML/CSS/JS embedded, SSR with HTMX + Tailwind, CORS auto-configured, dark mode, keyboard shortcuts, accessibility

## Why not PostgREST?

Use PostgREST if you are PostgreSQL-only and want a battle-tested project with a large community — it is genuinely
excellent, and `adms` borrows heavily from its URL conventions.

Reach for `adms` when:

- you are on MySQL, or operating a fleet with both PostgreSQL and MySQL, and want one server to manage,
- you want a UI shipped in the same binary as the API, not a separate frontend project,
- you prefer a single self-contained Go binary with no Haskell runtime to deploy,
- you want a tighter scope focused on admin dashboards — opinionated defaults, identifier allowlists, mandatory filters
  on writes — rather than a general-purpose data API.

## Acknowledgements

The URL conventions, the embedding syntax, and the `Prefer`-header semantics in this project are taken almost verbatim
from [PostgREST](https://postgrest.org/). The admin UI is rendered with [HTMX](https://htmx.org/) and styled
with [Tailwind CSS](https://tailwindcss.com/). Standing on giants' shoulders — thank you.

## Sponsor

If `adms` saves you time, consider supporting ongoing development
via [GitHub Sponsors](https://github.com/sponsors/mickamy). Sponsorships pay for the maintenance time that keeps
Postgres / MySQL parity, security fixes, and roadmap items moving.

## License

[MIT](LICENSE) © 2026 Tetsuro Mikami
