package api

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"albion-helper/internal/db"
	"albion-helper/internal/models"
)

const (
	baseURL    = "https://europe.albion-online-data.com"
	maxURLSize = 4000
)

type priceResponse struct {
	ItemID       string `json:"item_id"`
	City         string `json:"city"`
	Quality      int    `json:"quality"`
	SellPriceMin int    `json:"sell_price_min"`
	SellPriceMax int    `json:"sell_price_max"`
	BuyPriceMin  int    `json:"buy_price_min"`
	BuyPriceMax  int    `json:"buy_price_max"`
}

type historyDataPoint struct {
	ItemCount int    `json:"item_count"`
	AvgPrice  int    `json:"avg_price"`
	Timestamp string `json:"timestamp"`
}

type historyResponse struct {
	Location string             `json:"location"`
	ItemID   string             `json:"item_id"`
	Quality  int                `json:"quality"`
	Data     []historyDataPoint `json:"data"`
}

type Client struct {
	http *http.Client
}

func NewClient(proxyURL string) *Client {
	transport := &http.Transport{}
	if proxyURL != "" {
		if u, err := url.Parse(proxyURL); err == nil {
			transport.Proxy = http.ProxyURL(u)
		}
	}
	return &Client{
		http: &http.Client{
			Timeout:   30 * time.Second,
			Transport: transport,
		},
	}
}

func (c *Client) FetchPrices(database *sql.DB, tracked []models.TrackedItem) {
	batches := buildBatches(tracked)
	qualities := buildQualityList(tracked)

	for _, batch := range batches {
		url := fmt.Sprintf("%s/api/v2/stats/prices/%s.json?qualities=%s",
			baseURL, strings.Join(batch, ","), qualities)

		resp, err := c.http.Get(url)
		if err != nil {
			log.Printf("API prices fetch error: %v", err)
			continue
		}

		var prices []priceResponse
		if err := json.NewDecoder(resp.Body).Decode(&prices); err != nil {
			resp.Body.Close()
			log.Printf("API prices decode error: %v", err)
			continue
		}
		resp.Body.Close()

		insertPriceSnapshot(database, prices)
		time.Sleep(500 * time.Millisecond)
	}
}

func (c *Client) EnrichBatch(database *sql.DB, names []string) error {
	url := fmt.Sprintf("%s/api/v2/stats/prices/%s.json?qualities=0,1,2,3,4",
		baseURL, strings.Join(names, ","))

	resp, err := c.http.Get(url)
	if err != nil {
		return fmt.Errorf("enrich fetch: %w", err)
	}
	defer resp.Body.Close()

	var prices []priceResponse
	if err := json.NewDecoder(resp.Body).Decode(&prices); err != nil {
		return fmt.Errorf("enrich decode: %w", err)
	}

	insertPriceSnapshot(database, prices)
	return nil
}

func (c *Client) EnrichHistory(database *sql.DB, names []string) error {
	url := fmt.Sprintf("%s/api/v2/stats/history/%s.json?qualities=0,1,2,3,4&time-scale=24",
		baseURL, strings.Join(names, ","))

	resp, err := c.http.Get(url)
	if err != nil {
		return fmt.Errorf("enrich history fetch: %w", err)
	}
	defer resp.Body.Close()

	var histories []historyResponse
	if err := json.NewDecoder(resp.Body).Decode(&histories); err != nil {
		return fmt.Errorf("enrich history decode: %w", err)
	}

	insertHistorySnapshot(database, histories)
	return nil
}

func (c *Client) FetchHistory(database *sql.DB, tracked []models.TrackedItem) {
	batches := buildBatches(tracked)
	qualities := buildQualityList(tracked)

	for _, batch := range batches {
		url := fmt.Sprintf("%s/api/v2/stats/history/%s.json?qualities=%s&time-scale=24",
			baseURL, strings.Join(batch, ","), qualities)

		resp, err := c.http.Get(url)
		if err != nil {
			log.Printf("API history fetch error: %v", err)
			continue
		}

		var histories []historyResponse
		if err := json.NewDecoder(resp.Body).Decode(&histories); err != nil {
			resp.Body.Close()
			log.Printf("API history decode error: %v", err)
			continue
		}
		resp.Body.Close()

		insertHistorySnapshot(database, histories)
		time.Sleep(500 * time.Millisecond)
	}
}

func buildBatches(tracked []models.TrackedItem) [][]string {
	var names []string
	for _, t := range tracked {
		name := t.UniqueName
		if t.Enchantment > 0 {
			name += fmt.Sprintf("@%d", t.Enchantment)
		}
		names = append(names, name)
	}

	var batches [][]string
	var current []string
	currentLen := 0
	for _, n := range names {
		itemLen := len(n) + 1
		if currentLen+itemLen > maxURLSize && len(current) > 0 {
			batches = append(batches, current)
			current = nil
			currentLen = 0
		}
		current = append(current, n)
		currentLen += itemLen
	}
	if len(current) > 0 {
		batches = append(batches, current)
	}
	return batches
}

func buildQualityList(tracked []models.TrackedItem) string {
	qs := make(map[int]bool)
	for _, t := range tracked {
		qs[t.Quality] = true
	}
	var list []int
	for q := range qs {
		list = append(list, q)
	}
	sort.Ints(list)
	strs := make([]string, len(list))
	for i, q := range list {
		strs[i] = fmt.Sprintf("%d", q)
	}
	return strings.Join(strs, ",")
}

func insertPriceSnapshot(database *sql.DB, prices []priceResponse) {
	tx, err := database.Begin()
	if err != nil {
		log.Printf("insertPriceSnapshot: begin tx: %v", err)
		return
	}
	defer tx.Rollback()

	for _, p := range prices {
		if p.SellPriceMin == 0 && p.SellPriceMax == 0 && p.BuyPriceMin == 0 && p.BuyPriceMax == 0 {
			continue
		}

		p.City = db.MarketNameForLocation(database, p.City)

		tx.Exec("DELETE FROM marketorders WHERE source='api' AND item_id=? AND city=? AND quality_level=?",
			p.ItemID, p.City, p.Quality)

		insertSyntheticTx(tx, p.ItemID, p.City, p.Quality, p.SellPriceMin, "offer")
		if p.SellPriceMax != p.SellPriceMin && p.SellPriceMax > 0 {
			insertSyntheticTx(tx, p.ItemID, p.City, p.Quality, p.SellPriceMax, "offer")
		}
		if p.BuyPriceMin > 0 {
			insertSyntheticTx(tx, p.ItemID, p.City, p.Quality, p.BuyPriceMin, "request")
			if p.BuyPriceMax != p.BuyPriceMin && p.BuyPriceMax > 0 {
				insertSyntheticTx(tx, p.ItemID, p.City, p.Quality, p.BuyPriceMax, "request")
			}
		}
	}

	if err := tx.Commit(); err != nil {
		log.Printf("insertPriceSnapshot: commit: %v", err)
	}
}

func insertSyntheticTx(tx *sql.Tx, itemID, city string, quality, price int, auctionType string) {
	tx.Exec(`
		INSERT INTO marketorders (item_id, city, quality_level, price, amount, auction_type, source)
		VALUES (?, ?, ?, ?, 1, ?, 'api')
	`, itemID, city, quality, price, auctionType)
}

func insertHistorySnapshot(database *sql.DB, histories []historyResponse) {
	locNames := make(map[string]string, len(histories))
	for _, h := range histories {
		if len(h.Data) == 0 {
			continue
		}
		if _, ok := locNames[h.Location]; !ok {
			locNames[h.Location] = db.MarketNameForLocation(database, h.Location)
		}
	}

	tx, err := database.Begin()
	if err != nil {
		log.Printf("insertHistorySnapshot: begin tx: %v", err)
		return
	}
	defer tx.Rollback()

	for _, h := range histories {
		if len(h.Data) == 0 {
			continue
		}

		locName := locNames[h.Location]

		sort.Slice(h.Data, func(i, j int) bool {
			return h.Data[i].Timestamp > h.Data[j].Timestamp
		})

		avg1d, cnt1d := computeWeightedAverage(h.Data, 1)
		avg7d, cnt7d := computeWeightedAverage(h.Data, 7)
		avg28d, cnt28d := computeWeightedAverage(h.Data, 28)

		ts := parseTimestamp(h.Data[0].Timestamp)

		tx.Exec("DELETE FROM markethistories WHERE item_id=? AND location_id=? AND quality_level=? AND timescale=?",
			h.ItemID, locName, h.Quality, 0)
		tx.Exec("DELETE FROM markethistories WHERE item_id=? AND location_id=? AND quality_level=? AND timescale=?",
			h.ItemID, locName, h.Quality, 1)
		tx.Exec("DELETE FROM markethistories WHERE item_id=? AND location_id=? AND quality_level=? AND timescale=?",
			h.ItemID, locName, h.Quality, 2)

		if cnt1d > 0 {
			tx.Exec(`INSERT INTO markethistories (item_id, location_id, quality_level, timescale, item_amount, silver_amount, timestamp) VALUES (?, ?, ?, 0, ?, ?, ?)`,
				h.ItemID, locName, h.Quality, cnt1d, avg1d, ts)
		} else if cnt7d > 0 {
			tx.Exec(`INSERT INTO markethistories (item_id, location_id, quality_level, timescale, item_amount, silver_amount, timestamp) VALUES (?, ?, ?, 0, ?, ?, ?)`,
				h.ItemID, locName, h.Quality, cnt7d/7, avg7d, ts)
		}
		if cnt7d > 0 {
			tx.Exec(`INSERT INTO markethistories (item_id, location_id, quality_level, timescale, item_amount, silver_amount, timestamp) VALUES (?, ?, ?, 1, ?, ?, ?)`,
				h.ItemID, locName, h.Quality, cnt7d, avg7d, ts)
		}
		if cnt28d > 0 {
			tx.Exec(`INSERT INTO markethistories (item_id, location_id, quality_level, timescale, item_amount, silver_amount, timestamp) VALUES (?, ?, ?, 2, ?, ?, ?)`,
				h.ItemID, locName, h.Quality, cnt28d, avg28d, ts)
		}
	}

	if err := tx.Commit(); err != nil {
		log.Printf("insertHistorySnapshot: commit: %v", err)
	}
}

func parseTimestamp(s string) int64 {
	t, err := time.Parse("2006-01-02T15:04:05", s)
	if err != nil {
		return 0
	}
	return t.Unix()
}

func computeWeightedAverage(data []historyDataPoint, days int) (int, int64) {
	cutoff := time.Now().AddDate(0, 0, -days)
	var totalSilver, totalItems int64
	for _, d := range data {
		ts, err := time.Parse("2006-01-02T15:04:05", d.Timestamp)
		if err != nil {
			continue
		}
		if ts.Before(cutoff) {
			break
		}
		totalSilver += int64(d.AvgPrice) * int64(d.ItemCount)
		totalItems += int64(d.ItemCount)
	}
	if totalItems == 0 {
		return 0, 0
	}
	return int(totalSilver / totalItems), totalItems
}