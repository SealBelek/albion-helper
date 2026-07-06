package models

type SearchResult struct {
	UniqueName string
	Name       string
	Tracked    bool
}

type TrackedItem struct {
	UniqueName  string
	Enchantment int
	Quality     int
	AddedAt     string
	Name        string
}

type LanguageInfo struct {
	Code string
	Name string
	FTS  string
}

var Languages = []LanguageInfo{
	{Code: "EN-US", Name: "English", FTS: "european"},
	{Code: "DE-DE", Name: "Deutsch", FTS: "european"},
	{Code: "FR-FR", Name: "Français", FTS: "european"},
	{Code: "RU-RU", Name: "Русский", FTS: "european"},
	{Code: "PL-PL", Name: "Polski", FTS: "european"},
	{Code: "ES-ES", Name: "Español", FTS: "european"},
	{Code: "PT-BR", Name: "Português", FTS: "european"},
	{Code: "IT-IT", Name: "Italiano", FTS: "european"},
	{Code: "ID-ID", Name: "Indonesia", FTS: "european"},
	{Code: "TR-TR", Name: "Türkçe", FTS: "european"},
	{Code: "AR-SA", Name: "العربية", FTS: "european"},
	{Code: "ZH-CN", Name: "简体中文", FTS: "cjk"},
	{Code: "ZH-TW", Name: "繁體中文", FTS: "cjk"},
	{Code: "KO-KR", Name: "한국어", FTS: "cjk"},
	{Code: "JA-JP", Name: "日本語", FTS: "cjk"},
}

type MarketOrder struct {
	ID               int    `json:"Id"`
	ItemID           string `json:"ItemTypeId"`
	GroupTypeId      string `json:"ItemGroupTypeId"`
	LocationID       int    `json:"LocationId"`
	QualityLevel     int    `json:"QualityLevel"`
	EnchantmentLevel int    `json:"EnchantmentLevel"`
	Price            int    `json:"UnitPriceSilver"`
	Amount           int    `json:"Amount"`
	AuctionType      string `json:"AuctionType"`
	Expires          string `json:"Expires"`
}

type MarketUpload struct {
	Orders []*MarketOrder `json:"Orders"`
}

type PriceRow struct {
	UniqueName string
	Name       string
	City       string
	BuyMax     int
	SellMin    int
	Profit     float64
	Avg24h     int
	Avg7d      int
	Avg4w      int
}

type PriceItemGroup struct {
	UniqueName string
	Name       string
	Cities     []PriceRow
	BestCity   int
	HasData    bool
}

type MarketHistory struct {
	ItemAmount   int64  `json:"ItemAmount"`
	SilverAmount uint64 `json:"SilverAmount"`
	Timestamp    uint64 `json:"Timestamp"`
}

type MarketHistoriesUpload struct {
	AlbionId        int              `json:"AlbionId"`
	AlbionIdString  string           `json:"AlbionIdString"`
	LocationId      int              `json:"LocationId"`
	QualityLevel    uint8            `json:"QualityLevel"`
	Timescale       uint8            `json:"Timescale"`
	Histories       []*MarketHistory `json:"MarketHistories"`
}
