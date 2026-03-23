# DonkeyGo

Shared Go packages for iOS app backends. Interface-based DB, stdlib `http.ServeMux` compatible, zero database driver dependency.

## After modifying or adding packages

When you add, remove, or modify any public type, interface, function, or package:

1. **Update `COMPONENTS.md`** — Add/update the entry following the existing format (## package, ### DB Interface, ### Types, ### Functions)
2. **Re-index MCP** — Run `cd mcp && node indexer.mjs` to rebuild the SQLite FTS5 search index
3. **Update `openapi/routes.go`** — If new HTTP endpoints were added, add route comments there

All three steps are required. The MCP index is how LLMs discover packages — if you skip step 2, the new package won't be findable.

## Project structure

- Top-level directories are Go packages (auth, sync, push, chat, middleware, etc.)
- `postgres/` — PostgreSQL implementations of all DB interfaces
- `openapi/` — Route documentation
- `admin/` — Pre-built admin panel (HTMX + html/template)
- `starter/` — Deployment templates (Dockerfile, docker-compose, Caddyfile, CI/CD workflows)
- `mcp/` — MCP server + SQLite FTS5 index for AI-assisted discovery
- `COMPONENTS.md` — Package API catalog (source of truth for MCP index)

## Conventions

- Every package defines a DB interface (e.g. `AuthDB`, `SyncDB`) — no direct database dependency
- Handlers take `http.ResponseWriter, *http.Request` — compatible with stdlib ServeMux
- Use `httputil.WriteJSON` / `httputil.WriteError` for responses
- Use `httputil.DecodeJSON` for request parsing
