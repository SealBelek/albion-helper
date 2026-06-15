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
