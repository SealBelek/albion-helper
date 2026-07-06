package service

import (
	"database/sql"
	"math"
	"sort"
	"time"

	"albion-helper/internal/api"
	"albion-helper/internal/db"
	"albion-helper/internal/models"
)

type PriceService interface {
	GetPriceGroups(lang string) ([]models.PriceItemGroup, error)
	SyncMissingPrices() error
	SyncHistory() error
	NeedsHistorySync(lastSync time.Time) bool
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

func (s *priceService) NeedsHistorySync(lastSync time.Time) bool {
	return time.Since(lastSync) > 5*time.Minute
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