package service

import (
	"database/sql"

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