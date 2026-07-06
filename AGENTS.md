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

- SQLite via `modernc.org/sqlite` (pure Go, no CGo). DB file is `db/items.db`.
- The `db/` directory is gitignored; the database is created automatically on first run.
- At startup, `db.Open()` runs an inline migration (`internal/db/db.go:migrate()`) that creates/alters tables, indexes, and the `prices` view. This runs every startup.
- Static item data (`data/items.json`, `data/world.json`) is embedded into the binary via `//go:embed` in `data/data.go`. If the items table is empty on startup, `db.SeedData()` auto-populates items, localizations, FTS indexes, and markets.
- FTS5 search uses two virtual tables: `items_fts_european` (unicode61 tokenizer, for Latin/Cyrillic) and `items_fts_cjk` (trigram, for CJK). Only created if the SQLite build supports trigram.
- `db-reset` just deletes the DB file — no scripts needed. It re-seeds on next `make run`.

## Architecture

- **Single Go module**, all business code under `internal/`.
- **TUI framework**: Bubble Tea + Bubbles + Lipgloss. Tabs: Items (search/track) and Prices (profit grid). Model in `internal/tui/model.go`.
- **Live data**: NATS subscriber (`internal/nats/subscriber.go`) for `marketorders.deduped` + `markethistories.deduped`. Public server at `nats.albion-online-data.com:34222`. Orders batched in memory and flushed every 100ms or at 50 entries.
- **REST fallback**: `internal/api/client.go` fetches from `europe.albion-online-data.com` for tracked items not in the NATS stream.
- **Cleanup**: goroutine deletes market orders older than 24h in batches of 1000 every 5 minutes. Prices auto-refresh every 3s in the UI.
- **Service layer**: `internal/service/` defines interfaces (`ItemService`, `PriceService`) wired in `cmd/main.go`. Services delegate to `internal/db/` for queries.
- **Settings**: key/value store in `settings` table, accessed via `ItemService.GetSetting`/`SetSetting`.

## Testing

- No tests exist. `go vet ./...` is the only validation.

## Conventions

- Uses Bubble Tea "The Elm Architecture" pattern: `Init()`, `Update(msg)`, `View()` on each component.
- `internal/models/models.go` defines all shared structs (MarketOrder, SearchResult, TrackedItem, PriceRow, etc.).
- DB queries are raw SQL in `internal/db/queries.go` and `internal/db/prices.go`.
- New services should follow the interface pattern used by `ItemService`/`PriceService`.

## Debug flags

- `-profile` starts a pprof HTTP server on `localhost:6060`.
- `-debug` writes heap profiles to `profiles/` every 30s (keeps last 10).
