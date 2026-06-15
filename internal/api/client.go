package api

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
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
	Location string            `json:"location"`
	ItemID   string            `json:"item_id"`
	Quality  int               `json:"quality"`
	Data     []historyDataPoint `json:"data"`
}

var client = &http.Client{Timeout: 10 * time.Second}

func FetchPrices(database *sql.DB, tracked []models.TrackedItem) {
	batches := buildBatches(tracked)
	qualities := buildQualityList(tracked)

	for _, batch := range batches {
		url := fmt.Sprintf("%s/api/v2/stats/prices/%s.json?qualities=%s",
			baseURL, strings.Join(batch, ","), qualities)

		resp, err := client.Get(url)
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

func FetchHistory(database *sql.DB, tracked []models.TrackedItem) {
	batches := buildBatches(tracked)
	qualities := buildQualityList(tracked)

	for _, batch := range batches {
		url := fmt.Sprintf("%s/api/v2/stats/history/%s.json?qualities=%s&time-scale=24",
			baseURL, strings.Join(batch, ","), qualities)

		resp, err := client.Get(url)
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
	for _, p := range prices {
		if p.SellPriceMin == 0 && p.SellPriceMax == 0 && p.BuyPriceMin == 0 && p.BuyPriceMax == 0 {
			continue
		}

		database.Exec("DELETE FROM marketorders WHERE source='api' AND item_id=? AND city=? AND quality_level=?",
			p.ItemID, p.City, p.Quality)

		insertSynthetic(database, p.ItemID, p.City, p.Quality, p.SellPriceMin, "request")
		if p.SellPriceMax != p.SellPriceMin && p.SellPriceMax > 0 {
			insertSynthetic(database, p.ItemID, p.City, p.Quality, p.SellPriceMax, "request")
		}
		if p.BuyPriceMin > 0 {
			insertSynthetic(database, p.ItemID, p.City, p.Quality, p.BuyPriceMin, "offer")
			if p.BuyPriceMax != p.BuyPriceMin && p.BuyPriceMax > 0 {
				insertSynthetic(database, p.ItemID, p.City, p.Quality, p.BuyPriceMax, "offer")
			}
		}
	}
}

func insertSynthetic(database *sql.DB, itemID, city string, quality, price int, auctionType string) {
	database.Exec(`
		INSERT INTO marketorders (item_id, city, quality_level, price, amount, auction_type, source)
		VALUES (?, ?, ?, ?, 1, ?, 'api')
	`, itemID, city, quality, price, auctionType)
}

func insertHistorySnapshot(database *sql.DB, histories []historyResponse) {
	for _, h := range histories {
		if len(h.Data) == 0 {
			continue
		}

		locName := db.MarketNameForLocation(database, h.Location)

		sort.Slice(h.Data, func(i, j int) bool {
			return h.Data[i].Timestamp > h.Data[j].Timestamp
		})

		avg1d := computeWeightedAverage(h.Data, 1)
		avg7d := computeWeightedAverage(h.Data, 7)
		avg28d := computeWeightedAverage(h.Data, 28)

		ts := parseTimestamp(h.Data[0].Timestamp)

		database.Exec("DELETE FROM markethistories WHERE item_id=? AND location_id=? AND quality_level=? AND timescale=?",
			h.ItemID, locName, h.Quality, 0)
		database.Exec("DELETE FROM markethistories WHERE item_id=? AND location_id=? AND quality_level=? AND timescale=?",
			h.ItemID, locName, h.Quality, 1)
		database.Exec("DELETE FROM markethistories WHERE item_id=? AND location_id=? AND quality_level=? AND timescale=?",
			h.ItemID, locName, h.Quality, 2)

		if avg1d > 0 {
			database.Exec(`INSERT INTO markethistories (item_id, location_id, quality_level, timescale, item_amount, silver_amount, timestamp) VALUES (?, ?, ?, 0, 1, ?, ?)`,
				h.ItemID, locName, h.Quality, avg1d, ts)
		}
		if avg7d > 0 {
			database.Exec(`INSERT INTO markethistories (item_id, location_id, quality_level, timescale, item_amount, silver_amount, timestamp) VALUES (?, ?, ?, 1, 1, ?, ?)`,
				h.ItemID, locName, h.Quality, avg7d, ts)
		}
		if avg28d > 0 {
			database.Exec(`INSERT INTO markethistories (item_id, location_id, quality_level, timescale, item_amount, silver_amount, timestamp) VALUES (?, ?, ?, 2, 1, ?, ?)`,
				h.ItemID, locName, h.Quality, avg28d, ts)
		}
	}
}

func parseTimestamp(s string) int64 {
	t, err := time.Parse("2006-01-02T15:04:05", s)
	if err != nil {
		return 0
	}
	return t.Unix()
}

func computeWeightedAverage(data []historyDataPoint, days int) int {
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
		return 0
	}
	return int(totalSilver / totalItems)
}
