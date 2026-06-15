# AGENTS.md

## Build & run

```bash
make          # go build -o albion-helper ./cmd/
make run      # build + run binary
make lint     # go vet ./...
make clean    # rm binary
```

- Go module alias is `albion-helper` (matches root module name).
- Entry point is `cmd/main.go`.

## Database

- SQLite via `modernc.org/sqlite` (pure Go, no CGo). DB file: `db/items.db`.
- Before first run, populate the DB with static data:
  ```bash
  make db-init    # runs scripts/init-db.py, prices-db.py, world-db.py
  make db-reset   # rm db/items.db && make db-init
  ```
- Python scripts need `python3` and read `data/items.json` (~450k lines) and `data/world.json`.
- At app startup, `db.Open()` runs an inline migration (`internal/db/db.go:migrate()`) that creates/alters tables, indexes, and the `prices` view. This runs every startup.
- FTS5 search uses two virtual tables: `items_fts_european` (unicode61 tokenizer) for Latin/Cyrillic languages, `items_fts_cjk` (trigram) for CJK.

## Architecture

- **Single Go module** (no monorepo, no workspaces). All business code under `internal/`.
- **TUI framework**: Bubble Tea + Bubbles + Lipgloss (charmbracelet). Tabs: Items (search/track) and Prices (profit grid). Model in `internal/tui/model.go`.
- **Live data**: NATS subscriber (`internal/nats/subscriber.go`) for `marketorders.deduped` + `markethistories.deduped`. Public server at `nats.albion-online-data.com:34222`.
- **REST fallback**: `internal/api/client.go` fetches from `europe.albion-online-data.com` for tracked items not in the NATS stream.
- **Cleanup**: goroutine deletes market orders older than 24h every 5 minutes. Prices auto-refresh every 3s in the UI.

## Testing

- No tests exist, no test target in Makefile. `go vet ./...` is the only validation.

## Conventions

- Uses Bubble Tea "The Elm Architecture" pattern: `Init()`, `Update(msg)`, `View()` on each component.
- `internal/models/models.go` defines all shared structs (MarketOrder, SearchResult, TrackedItem, PriceRow, etc.).
- DB queries are raw SQL in `internal/db/queries.go` and `internal/db/prices.go`.
