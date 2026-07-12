package service

import (
	"database/sql"

	"albion-helper/internal/db"
	"albion-helper/internal/models"
)

const mmPageSize = 10

type MarketMakerService interface {
	GetOpportunities(city, lang string, page int) ([]models.Opportunity, int, error)
	GetOpenPositions(lang string) ([]models.Position, error)
	MarkBuy(itemID string, enchantment, quality int, city string, buyPrice, qty int) error
	MarkSell(positionID int64) error
	GetCities() ([]string, error)
	PageSize() int
}

type marketMakerService struct {
	db *sql.DB
}

func NewMarketMakerService(database *sql.DB) MarketMakerService {
	return &marketMakerService{db: database}
}

func (s *marketMakerService) GetOpportunities(city, lang string, page int) ([]models.Opportunity, int, error) {
	if page < 0 {
		page = 0
	}
	return db.GetOpportunities(s.db, city, lang, page, mmPageSize)
}

func (s *marketMakerService) GetOpenPositions(lang string) ([]models.Position, error) {
	return db.GetOpenPositions(s.db, lang)
}

func (s *marketMakerService) MarkBuy(itemID string, enchantment, quality int, city string, buyPrice, qty int) error {
	return db.InsertPosition(s.db, itemID, enchantment, quality, city, buyPrice, qty)
}

func (s *marketMakerService) MarkSell(positionID int64) error {
	return db.ClosePosition(s.db, positionID)
}

func (s *marketMakerService) GetCities() ([]string, error) {
	return db.GetCities(s.db)
}

func (s *marketMakerService) PageSize() int { return mmPageSize }