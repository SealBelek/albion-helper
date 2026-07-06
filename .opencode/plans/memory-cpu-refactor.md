# Memory & CPU Refactor + Architecture Decoupling Plan

## Problem

1. **Memory leak**: grows 5MB → 420MB in ~4.5 min (goroutine/timer exponential growth)
2. **High allocation rate**: ~20GB cumulative (lipgloss string building, DB query results)
3. **Tight coupling**: TUI imports `db` and `api` directly; business logic mixed into `prices_tab.go`; `api` writes to DB directly

---

## Architecture: 3-Layer Decoupling

```
internal/
├── models/        # Domain types (unchanged + Languages moved here)
├── db/            # Repository layer: SQL queries, schema, seeding
├── service/       # NEW: Business logic layer
│   ├── item.go    # ItemService interface + implementation
│   └── price.go   # PriceService interface + implementation
├── api/           # External API: HTTP-only, returns data, NO DB writes
├── nats/          # NATS subscriber: uses db for writes
└── tui/           # UI layer: depends on service interfaces only
```

**Dependency rule**: `tui` → `service` → `db`. `tui` NEVER imports `db` or `api` directly.

---

## Part A: Create Service Layer

### A1. Move `Languages` to `models`

**File**: `internal/models/models.go` — add `Languages` variable

```go
var Languages = []LanguageInfo{
    {Code: "EN-US", Name: "English", FTS: "european"},
    {Code: "DE-DE", Name: "Deutsch", FTS: "european"},
    // ... all 15 languages
}
```

**File**: `internal/db/queries.go` — remove `Languages` variable, update references to `models.Languages`

### A2. Create `internal/service/item.go`

```go
package service

import (
    "albion-helper/internal/db"
    "albion-helper/internal/models"
)

type ItemService interface {
    Search(lang, query string, limit int) ([]models.SearchResult, error)
    GetTracked(lang string) ([]models.TrackedItem, error)
    TrackItem(uniqueName string, enchantment, quality int, lang string) ([]models.TrackedItem, error)
    UntrackItem(uniqueName string, enchantment, quality int, lang string) ([]models.TrackedItem, error)
    GetSetting(key string) string
    SetSetting(key, value string) error
}

type itemService struct {
    db *sql.DB
}

func NewItemService(database *sql.DB) ItemService {
    return &itemService{db: database}
}

func (s *itemService) Search(lang, query string, limit int) ([]models.SearchResult, error) {
    return db.SearchItems(s.db, lang, query, limit)
}

func (s *itemService) GetTracked(lang string) ([]models.TrackedItem, error) {
    return db.GetTrackedItems(s.db, lang)
}

func (s *itemService) TrackItem(uniqueName string, enchantment, quality int, lang string) ([]models.TrackedItem, error) {
    if err := db.AddTrackedItem(s.db, uniqueName, enchantment, quality); err != nil {
        return nil, err
    }
    return db.GetTrackedItems(s.db, lang)
}

func (s *itemService) UntrackItem(uniqueName string, enchantment, quality int, lang string) ([]models.TrackedItem, error) {
    if err := db.RemoveTrackedItem(s.db, uniqueName, enchantment, quality); err != nil {
        return nil, err
    }
    return db.GetTrackedItems(s.db, lang)
}

func (s *itemService) GetSetting(key string) string {
    return db.GetSetting(s.db, key)
}

func (s *itemService) SetSetting(key, value string) error {
    return db.SetSetting(s.db, key, value)
}
```

### A3. Create `internal/service/price.go`

Move `groupPrices()` from `prices_tab.go` and the sync logic from `prices_tab.go`:

```go
package service

import (
    "math"
    "sort"

    "albion-helper/internal/api"
    "albion-helper/internal/db"
    "albion-helper/internal/models"
)

type PriceService interface {
    GetPriceGroups(lang string) ([]models.PriceItemGroup, error)
    SyncMissingPrices() error
    SyncHistory() error
}

type priceService struct {
    db        *sql.DB
    apiClient *api.Client
}

func NewPriceService(database *sql.DB, client *api.Client) PriceService {
    return &priceService{db: database, apiClient: client}
}

func (s *priceService) GetPriceGroups(lang string) ([]models.PriceItemGroup, error) {
    rows, err := db.GetPricesForTrackedItems(s.db, lang)
    if err != nil {
        return nil, err
    }
    return GroupPrices(rows), nil
}

func (s *priceService) SyncMissingPrices() error {
    items, err := db.MissingTrackedItems(s.db)
    if err != nil {
        return err
    }
    if len(items) > 0 {
        s.apiClient.FetchPrices(s.db, items)
    }
    return nil
}

func (s *priceService) SyncHistory() error {
    lang := models.Languages[0].Code
    tracked, err := db.GetTrackedItems(s.db, lang)
    if err != nil {
        return err
    }
    if len(tracked) > 0 {
        s.apiClient.FetchHistory(s.db, tracked)
    }
    return nil
}

func GroupPrices(rows []models.PriceRow) []models.PriceItemGroup {
    groupMap := make(map[string][]models.PriceRow, len(rows)/3)
    for _, r := range rows {
        groupMap[r.UniqueName] = append(groupMap[r.UniqueName], r)
    }

    groups := make([]models.PriceItemGroup, 0, len(groupMap))
    for key, cities := range groupMap {
        hasData := false
        filtered := cities[:0]
        for _, c := range cities {
            if c.City != "" && (c.BuyMax > 0 || c.SellMin > 0) {
                hasData = true
                filtered = append(filtered, c)
            }
        }

        if !hasData {
            groups = append(groups, models.PriceItemGroup{
                UniqueName: key,
                Name:       key,
                HasData:    false,
            })
            continue
        }

        sort.Slice(filtered, func(i, j int) bool {
            return filtered[i].Profit > filtered[j].Profit
        })

        bestCity := 0
        bestProfit := -math.MaxFloat64
        for i, c := range filtered {
            if c.Profit > bestProfit {
                bestProfit = c.Profit
                bestCity = i
            }
        }

        groups = append(groups, models.PriceItemGroup{
            UniqueName: key,
            Name:       key,
            Cities:     filtered,
            BestCity:   bestCity,
            HasData:    hasData,
        })
    }

    sort.Slice(groups, func(i, j int) bool {
        return groups[i].UniqueName < groups[j].UniqueName
    })

    return groups
}
```

**Note**: The `api.Client.FetchPrices` and `api.Client.FetchHistory` still take `*sql.DB` for now — we'll refactor the API layer in step A5 to separate HTTP from DB writes.

### A4. Refactor API client to return data only

This is a larger change. Currently `api.FetchPrices` and `api.FetchHistory` take `*sql.DB` and write directly to the database. Refactoring to separate concerns:

**Phase 1** (this PR): Keep `api.Client` methods taking `*sql.DB` but move the orchestration calls into the service layer. The service decides WHEN to call the API, the API decides HOW to fetch.

**Phase 2** (future): Extract the DB-write logic from `api/client.go` into `db/` package methods, making the API client purely HTTP.

For this refactoring, Phase 1 is sufficient. The service layer calls `api.FetchPrices(db, items)` — the DB dependency is on the service/api boundary, not the TUI.

### A5. Refactor TUI to use service interfaces

**File**: `internal/tui/items_tab.go`

Replace `*sql.DB` with `service.ItemService`:

```go
type ItemsTabModel struct {
    itemSvc   service.ItemService  // was: db *sql.DB
    // ... rest unchanged
}

func NewItemsTabModel(svc service.ItemService) ItemsTabModel {
    // ...
    langIdx := 0
    if saved := svc.GetSetting("lang_idx"); saved != "" {
        if v, err := strconv.Atoi(saved); err == nil && v >= 0 && v < len(models.Languages) {
            langIdx = v
        }
    }
    // ...
}
```

All cmd functions (`loadTracked`, `doSearch`, `trackItem`, `untrackItem`) change from calling `db.xxx(database, ...)` to calling `svc.xxx(...)`.

**File**: `internal/tui/prices_tab.go`

Replace `*sql.DB` and `api` imports with `service.PriceService`:

```go
type PricesTabModel struct {
    priceSvc service.PriceService  // was: db *sql.DB
    // ... rest unchanged, remove api import
}

func NewPricesTabModel(svc service.PriceService) PricesTabModel {
    return PricesTabModel{
        priceSvc: svc,
        langIdx:  0,
    }
}
```

Remove `groupPrices`, `formatPrice`, `formatProfit` — keep formatting functions in TUI (they're UI rendering), but move `groupPrices` to service.

Cmd functions change:
- `refreshPrices()` → calls `svc.GetPriceGroups(lang)` instead of `db.GetPricesForTrackedItems` + `groupPrices`
- `checkMissing()` → calls `svc.SyncMissingPrices()` and `svc.SyncHistory()` instead of `db.MissingTrackedItems` + `api.FetchPrices` etc.

**File**: `internal/tui/model.go`

Replace `*sql.DB` with service interfaces:

```go
type Model struct {
    itemSvc  service.ItemService
    priceSvc service.PriceService
    // ... rest unchanged (remove db field)
}

func NewModel(itemSvc service.ItemService, priceSvc service.PriceService) Model {
    it := NewItemsTabModel(itemSvc)
    pt := NewPricesTabModel(priceSvc)
    pt.SetLangIdx(it.langIdx)
    return Model{
        itemSvc:  itemSvc,
        priceSvc: priceSvc,
        active:   tabItems,
        itemsTab:  it,
        pricesTab: pt,
        viewGen:   1,
    }
}
```

### A6. Update `cmd/main.go` to wire services

```go
func main() {
    // ... flag parsing, DB open, seeding ...

    database, err := db.Open("db/items.db")
    // ... error handling, seeding ...

    apiClient := api.NewClient()
    itemSvc := service.NewItemService(database)
    priceSvc := service.NewPriceService(database, apiClient)

    subscriber := nats.NewSubscriber(database)  // nats still uses db directly
    go func() {
        if err := subscriber.Start(); err != nil {
            fmt.Fprintf(os.Stderr, "NATS subscriber error: %v\n", err)
        }
    }()
    defer subscriber.Stop()

    p := tea.NewProgram(tui.NewModel(itemSvc, priceSvc), tea.WithAltScreen())
    // ...
}
```

---

## Part B: Memory & CPU Optimization

### B1. Fix Timer/Goroutine Leak (CRITICAL)

**File**: `internal/tui/prices_tab.go`

Remove `m.tickRefresh()` from `pricesLoadedMsg` handler (line 74):
```go
case pricesLoadedMsg:
    m.groups = msg.groups
    if m.cursor >= len(m.groups) {
        m.cursor = len(m.groups) - 1
    }
    if m.cursor < 0 {
        m.cursor = 0
    }
    return m, nil  // was: return m, m.tickRefresh()
```

Similarly remove `m.refreshPrices()` from `apiFetchedMsg` (line 80) — the `tickMsg` chain already triggers refreshes:
```go
case apiFetchedMsg:
    m.lastAPICheck = time.Now()
    return m, nil  // was: return m, m.refreshPrices()
```

Also remove `m.refreshPrices()` from `historyFetchedMsg` (line 84):
```go
case historyFetchedMsg:
    m.lastHistory = time.Now()
    return m, nil  // was: return m, m.refreshPrices()
```

Increase tick intervals:
```go
func (m PricesTabModel) tickRefresh() tea.Cmd {
    return tea.Tick(60*time.Second, func(t time.Time) tea.Msg {  // was 30s
        return tickMsg{}
    })
}

func (m PricesTabModel) tickCheck() tea.Cmd {
    return tea.Tick(120*time.Second, func(t time.Time) tea.Msg {  // was 60s
        return apiCheckMsg{}
    })
}
```

### B2. Fix View Cache Effectiveness

**File**: `internal/tui/model.go`

Remove `m.viewGen++` from the top of `tea.KeyMsg` handler (line 108) — only bump on actual data changes.

Remove `m.viewGen++` from `tickMsg` handler (line 83) — the `pricesLoadedMsg` will bump `viewGen` when data actually changes.

### B3. Pre-allocate Slices

**File**: `internal/tui/prices_tab.go` — `View()` method:
```go
lines := make([]string, 0, m.height+2)  // was: var lines []string
```

**File**: `internal/tui/items_tab.go` — `renderResultsSection()`:
```go
lines := make([]string, 0, resultsVisibleLines+2)  // was: var lines []string
```

**File**: `internal/tui/items_tab.go` — `renderTrackedSection()`:
```go
lines := make([]string, 0, maxHeight+1)  // was: var lines []string
```

**File**: `internal/service/price.go` — already planned: use `make(map[string][]models.PriceRow, len(rows)/3)` and `make([]models.PriceItemGroup, 0, len(groupMap))`

**File**: `internal/db/prices.go` — `GetPricesForTrackedItems`:
```go
results := make([]models.PriceRow, 0, 64)  // was: var results []models.PriceRow
```

### B4. Prepared Statement for Price Query

**File**: `internal/db/prices.go`

Add a package-level prepared statement for the frequently-called price query. Since SQLite with `MaxOpenConns(1)` serialized access, the prepared statement can be reused:

```go
var priceStmt *sql.Stmt
var priceStmtMu sync.Mutex

func GetPricesForTrackedItems(database *sql.DB, lang string) ([]models.PriceRow, error) {
    priceStmtMu.Lock()
    if priceStmt == nil {
        stmt, err := database.Prepare(priceQuerySQL)
        if err != nil {
            priceStmtMu.Unlock()
            return nil, err
        }
        priceStmt = stmt
    }
    priceStmtMu.Unlock()
    
    rows, err := priceStmt.Query(lang)
    // ... rest unchanged
}
```

Extract the large SQL query to a constant `priceQuerySQL`.

### B5. Reduce NATS Channel Buffers

**File**: `internal/nats/subscriber.go`

```go
orderCh := make(chan *natsio.Msg, 64)   // was 256
histCh := make(chan *natsio.Msg, 64)     // was 256
```

### B6. DB Tuning

**File**: `internal/db/db.go`

```go
database.SetMaxOpenConns(2)    // was 1
database.SetMaxIdleConns(1)    // keep

// Change mmap_size
PRAGMA mmap_size=268435456    // was 0

// Add synchronous mode
PRAGMA synchronous=NORMAL
```

### B7. Nil Embedded Data After Seeding

**File**: `cmd/main.go`

```go
if err := db.SeedData(database, data.Items, data.World); err != nil {
    fmt.Fprintf(os.Stderr, "failed to seed database: %v\n", err)
    os.Exit(1)
}
data.Items = nil
data.World = nil
runtime.GC()  // force collection of 23MB
```

---

## File Change Summary

| File | Changes |
|------|---------|
| `internal/models/models.go` | Add `Languages` variable |
| `internal/db/queries.go` | Remove `Languages`, reference `models.Languages` |
| `internal/service/item.go` | **NEW**: ItemService interface + implementation |
| `internal/service/price.go` | **NEW**: PriceService interface + implementation, GroupPrices moved from tui |
| `internal/tui/model.go` | Replace `*sql.DB` with service interfaces, fix viewGen, fix NewModel signature |
| `internal/tui/items_tab.go` | Replace `*sql.DB` with `service.ItemService`, remove `db` import, use `models.Languages` |
| `internal/tui/prices_tab.go` | Replace `*sql.DB`+`api` with `service.PriceService`, remove `groupPrices`, `formatPrice`/`formatProfit` stay, fix timer leak, increase tick intervals, pre-allocate slices |
| `internal/db/prices.go` | Pre-allocate results slice, prepared statement |
| `internal/db/db.go` | MaxOpenConns=2, mmap_size, synchronous=NORMAL |
| `internal/nats/subscriber.go` | Reduce channel buffers 256→64 |
| `internal/api/client.go` | No change in this phase (service calls it with *sql.DB for now) |
| `cmd/main.go` | Wire services, nil embedded data |

---

## Execution Order

1. **Create `internal/service/`** — item.go and price.go with interfaces and implementations
2. **Move `Languages` to `models`** — update db/queries.go references
3. **Refactor TUI** — replace `*sql.DB` with service interfaces in items_tab, prices_tab, model
4. **Move `groupPrices`** from prices_tab.go to service/price.go
5. **Update main.go** — wire services
6. **Fix timer/goroutine leak** — remove duplicate tickRefresh calls
7. **Fix viewGen cache** — selective bumps only
8. **Pre-allocate slices** — all View() methods and price queries
9. **Prepared statements** — price query
10. **Channel buffers & DB tuning** — NATS, db.go
11. **Nil embedded data** — main.go
12. **Verify** — `make lint && make`