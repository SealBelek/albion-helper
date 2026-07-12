package enricher

import (
	"database/sql"
	"log"
	"strconv"
	"time"

	"albion-helper/internal/api"
	"albion-helper/internal/db"
)

const (
	initialDelay  = 10 * time.Second
	batchTick     = 1 * time.Minute
	retryBackoff  = 30 * time.Second
	maxRetries    = 3
	minBatchSize  = 1
	maxBatchSize  = 30
	startBatchSize = 30
)

var enchants = []string{"", "@1", "@2", "@3"}

type Enricher struct {
	db        *sql.DB
	client    *api.Client
	baseNames []string
	pos       int
	batchSize int
}

func New(database *sql.DB, client *api.Client) *Enricher {
	return &Enricher{
		db:        database,
		client:    client,
		batchSize: startBatchSize,
	}
}

func (e *Enricher) Start() {
	go func() {
		time.Sleep(initialDelay)

		for {
			if err := e.ensureLoaded(); err != nil {
				log.Printf("enricher: load items: %v", err)
				time.Sleep(5 * time.Minute)
				continue
			}
			e.restorePosition()
			e.run()
		}
	}()
}

func (e *Enricher) ensureLoaded() error {
	if len(e.baseNames) > 0 {
		return nil
	}
	return e.loadBaseNames()
}

func (e *Enricher) loadBaseNames() error {
	rows, err := e.db.Query(`
		SELECT DISTINCT
			CASE WHEN UniqueName LIKE '%@%'
				THEN substr(UniqueName, 1, instr(UniqueName, '@')-1)
				ELSE UniqueName
			END
		FROM items
		WHERE (UniqueName LIKE 'T4%' OR UniqueName LIKE 'T5%' OR UniqueName LIKE 'T6%' OR UniqueName LIKE 'T7%' OR UniqueName LIKE 'T8%')
			AND UniqueName NOT LIKE 'TREASURE%'
	`)
	if err != nil {
		return err
	}
	defer rows.Close()

	e.baseNames = e.baseNames[:0]
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			continue
		}
		e.baseNames = append(e.baseNames, n)
	}
	return rows.Err()
}

func (e *Enricher) totalItems() int {
	return len(e.baseNames) * len(enchants)
}

func (e *Enricher) restorePosition() {
	e.pos = e.loadIntSetting("enricher_pos", 0)
	e.batchSize = e.loadIntSetting("enricher_batch", startBatchSize)
	log.Printf("enricher: restored pos=%d batch=%d", e.pos, e.batchSize)
}

func (e *Enricher) savePosition() {
	_ = db.SetSetting(e.db, "enricher_pos", strconv.Itoa(e.pos))
	_ = db.SetSetting(e.db, "enricher_batch", strconv.Itoa(e.batchSize))
}

func (e *Enricher) loadIntSetting(key string, def int) int {
	v := db.GetSetting(e.db, key)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return n
}

func (e *Enricher) buildBatch(from, count int) []string {
	total := e.totalItems()
	if total == 0 || from >= total {
		return nil
	}
	if from+count > total {
		count = total - from
	}

	batch := make([]string, 0, count)
	for i := 0; i < count; i++ {
		idx := (from + i) % total
		base := e.baseNames[idx/len(enchants)]
		enc := enchants[idx%len(enchants)]
		batch = append(batch, base+enc)
	}
	return batch
}

func (e *Enricher) run() {
	total := e.totalItems()

	for {
		time.Sleep(batchTick)

		batch := e.buildBatch(e.pos, e.batchSize)
		if len(batch) == 0 {
			e.pos = 0
			batch = e.buildBatch(e.pos, e.batchSize)
		}

		n := len(batch)
		log.Printf("enricher: %d items (pos %d/%d, batch %d)",
			n, e.pos, total, e.batchSize)

		if err := e.client.EnrichBatch(e.db, batch); err == nil {
			e.pos = (e.pos + n) % total
			if e.batchSize < maxBatchSize {
				e.batchSize = min(e.batchSize*2, maxBatchSize)
			}
			e.savePosition()
			e.fetchHistory(batch)
			continue
		} else {
			log.Printf("enricher: error: %v", err)
		}

		sz := e.batchSize
		advanced := false
		backoff := retryBackoff
		prevSz := sz

		for attempt := 0; attempt < maxRetries; attempt++ {
			sz = sz / 2
			if sz < minBatchSize {
				sz = minBatchSize
			}
			if sz >= n || sz == prevSz {
				break
			}
			prevSz = sz

			time.Sleep(backoff)
			backoff += retryBackoff

			retry := e.buildBatch(e.pos, sz)
			log.Printf("enricher: retry %d/%d: %d items (batch %d)",
				attempt+1, maxRetries, len(retry), sz)

			if err := e.client.EnrichBatch(e.db, retry); err == nil {
				e.pos = (e.pos + len(retry)) % total
				e.batchSize = sz
				advanced = true
				e.savePosition()
				e.fetchHistory(retry)
				break
			} else {
				log.Printf("enricher: retry %d failed: %v", attempt+1, err)
			}
		}

		if !advanced {
			e.pos = (e.pos + 1) % total
			e.batchSize = minBatchSize
		}
	}
}

func (e *Enricher) fetchHistory(batch []string) {
	if err := e.client.EnrichHistory(e.db, batch); err != nil {
		log.Printf("enricher: history error: %v", err)
	}
}
