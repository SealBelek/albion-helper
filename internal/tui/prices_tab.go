package tui

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"database/sql"

	"albion-helper/internal/api"
	"albion-helper/internal/db"
	"albion-helper/internal/models"
)

var (
	styleProfitBest   = lipgloss.NewStyle().Foreground(lipgloss.Color("46")).Bold(true)
	styleProfitNormal = lipgloss.NewStyle().Foreground(lipgloss.Color("250"))
	stylePriceGold    = lipgloss.NewStyle().Foreground(lipgloss.Color("220"))
	stylePriceSilver  = lipgloss.NewStyle().Foreground(lipgloss.Color("250"))
	styleNoData       = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	styleHeaderCol    = lipgloss.NewStyle().Foreground(lipgloss.Color("62")).Bold(true)
)

type pricesLoadedMsg struct {
	groups []models.PriceItemGroup
}

type PricesTabModel struct {
	db          *sql.DB
	width       int
	height      int
	groups      []models.PriceItemGroup
	cursor      int
	langIdx     int
	lastAPICheck   time.Time
	lastHistory    time.Time
}

func NewPricesTabModel(database *sql.DB) PricesTabModel {
	return PricesTabModel{
		db:      database,
		langIdx: 0,
	}
}

func (m *PricesTabModel) SetLangIdx(idx int) {
	m.langIdx = idx
}

func (m PricesTabModel) Init() tea.Cmd {
	return tea.Batch(
		m.refreshPrices(),
		m.tickRefresh(),
		m.tickCheck(),
		m.tickHistory(),
		m.checkMissing(),
	)
}

func (m PricesTabModel) Update(msg tea.Msg) (PricesTabModel, tea.Cmd) {
	switch msg := msg.(type) {
	case pricesLoadedMsg:
		m.groups = msg.groups
		if m.cursor >= len(m.groups) {
			m.cursor = len(m.groups) - 1
		}
		if m.cursor < 0 {
			m.cursor = 0
		}
		return m, m.tickRefresh()

	case apiCheckMsg:
		return m, tea.Batch(m.checkMissing(), m.tickCheck())

	case apiFetchedMsg:
		m.lastAPICheck = time.Now()
		return m, m.refreshPrices()

	case historyFetchedMsg:
		m.lastHistory = time.Now()
		return m, m.refreshPrices()

	case historyCheckMsg:
		m.lastHistory = time.Time{}
		return m, tea.Batch(m.checkMissing(), m.tickHistory())

	case tickMsg:
		return m, tea.Batch(m.refreshPrices(), m.tickRefresh())

	case tea.KeyMsg:
		switch msg.String() {
		case "up":
			if m.cursor > 0 {
				m.cursor--
			}
		case "down":
			if m.cursor < len(m.groups)-1 {
				m.cursor++
			}
		}
	}

	return m, nil
}

type tickMsg struct{}
type apiCheckMsg struct{}
type apiFetchedMsg struct{}
type historyCheckMsg struct{}
type historyFetchedMsg struct{}

func (m PricesTabModel) tickRefresh() tea.Cmd {
	return tea.Tick(3*time.Second, func(t time.Time) tea.Msg {
		return tickMsg{}
	})
}

func (m PricesTabModel) tickCheck() tea.Cmd {
	return tea.Tick(60*time.Second, func(t time.Time) tea.Msg {
		return apiCheckMsg{}
	})
}

func (m PricesTabModel) tickHistory() tea.Cmd {
	return tea.Tick(30*time.Minute, func(t time.Time) tea.Msg {
		return historyCheckMsg{}
	})
}

func (m PricesTabModel) checkMissing() tea.Cmd {
	database := m.db
	lastHist := m.lastHistory
	return func() tea.Msg {
		items, err := db.MissingTrackedItems(database)
		if err != nil {
			return nil
		}
		if len(items) > 0 {
			api.FetchPrices(database, items)
		}
		if time.Since(lastHist) > 5*time.Minute {
			lang := db.Languages[0].Code
			tracked, err := db.GetTrackedItems(database, lang)
			if err == nil && len(tracked) > 0 {
				api.FetchHistory(database, tracked)
				return historyFetchedMsg{}
			}
		}
		return apiFetchedMsg{}
	}
}

func (m PricesTabModel) fetchHistory() tea.Cmd {
	database := m.db
	langIdx := m.langIdx
	return func() tea.Msg {
		lang := db.Languages[langIdx].Code
		tracked, err := db.GetTrackedItems(database, lang)
		if err != nil {
			return nil
		}
		if len(tracked) > 0 {
			api.FetchHistory(database, tracked)
		}
		return historyFetchedMsg{}
	}
}

func (m PricesTabModel) refreshPrices() tea.Cmd {
	langIdx := m.langIdx
	database := m.db
	return func() tea.Msg {
		lang := db.Languages[langIdx].Code
		rows, err := db.GetPricesForTrackedItems(database, lang)
		if err != nil {
			return nil
		}

		groups := groupPrices(rows)
		return pricesLoadedMsg{groups: groups}
	}
}

func groupPrices(rows []models.PriceRow) []models.PriceItemGroup {
	groupMap := make(map[string][]models.PriceRow)

	for _, r := range rows {
		groupMap[r.UniqueName] = append(groupMap[r.UniqueName], r)
	}

	var groups []models.PriceItemGroup
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

func (m *PricesTabModel) SetSize(w, h int) {
	m.width = w
	m.height = h
}

func (m PricesTabModel) View() string {
	if m.width < 40 || m.height < 5 {
		return ""
	}

	header := styleSectionHeader.Render(fmt.Sprintf("── Tracked Items (%d) ──", len(m.groups)))
	padLen := m.width - lipgloss.Width(header)
	if padLen < 0 {
		padLen = 0
	}
	header += styleDimmed.Render(strings.Repeat("─", padLen))

	var lines []string
	lines = append(lines, header)

	if len(m.groups) == 0 {
		lines = append(lines, styleDimmed.Render("  No tracked items. Add items in the Items tab."))
		emptyLines := m.height - 1 - len(lines)
		for i := 0; i < emptyLines; i++ {
			lines = append(lines, "")
		}
		return strings.Join(lines, "\n")
	}

	usedLines := 1
	colHeader := fmt.Sprintf("  %-14s %7s %7s %7s %7s %7s %7s", "City", "Buy@", "Sell@", "Prft%", "24h", "7d", "4w")

	for i, group := range m.groups {
		if usedLines >= m.height-1 {
			break
		}

		isSelected := i == m.cursor
		itemPrefix := "  "
		if isSelected {
			itemPrefix = styleCursor.Render("▸ ")
		}

		name := group.Name
		if isSelected {
			name = styleSelected.Render(name)
		}
		lines = append(lines, itemPrefix+name)
		usedLines++

		if !group.HasData {
			if isSelected {
				lines = append(lines, styleNoData.Render("  ── No price data yet ──"))
				usedLines++
			}
			continue
		}

		if isSelected {
			lines = append(lines, styleHeaderCol.Render(colHeader))
			usedLines++

			visibleCities := m.height - usedLines
			if visibleCities > len(group.Cities) {
				visibleCities = len(group.Cities)
			}

			for c := 0; c < visibleCities; c++ {
				city := group.Cities[c]
				buyMax := city.BuyMax
				sellMin := city.SellMin

				buyStr := formatPrice(buyMax)
				sellStr := formatPrice(sellMin)
				avg24 := formatPrice(city.Avg24h)
				avg7 := formatPrice(city.Avg7d)
				avg4 := formatPrice(city.Avg4w)
				profitStr := ""

				isBest := c == group.BestCity
				if buyMax > 0 && sellMin > 0 {
					profitStr = formatProfit(city.Profit, isBest)
				}

				cityName := city.City
				if len(cityName) > 14 {
					cityName = cityName[:14]
				}

				buyStyled := stylePriceGold.Render(fmt.Sprintf("%7s", buyStr))
				sellStyled := stylePriceSilver.Render(fmt.Sprintf("%7s", sellStr))
				avg24Styled := styleDimmed.Render(fmt.Sprintf("%7s", avg24))
				avg7Styled := styleDimmed.Render(fmt.Sprintf("%7s", avg7))
				avg4Styled := styleDimmed.Render(fmt.Sprintf("%7s", avg4))

				if isBest && buyMax > 0 {
					buyStyled = styleProfitBest.Render(fmt.Sprintf("%7s", buyStr))
				}
				if isBest && sellMin > 0 {
					sellStyled = styleProfitBest.Render(fmt.Sprintf("%7s", sellStr))
				}

				line := fmt.Sprintf("  %-14s %s %s %7s %s %s %s",
					cityName, buyStyled, sellStyled, profitStr, avg24Styled, avg7Styled, avg4Styled)
				lines = append(lines, line)
				usedLines++
			}
		}
	}

	for usedLines < m.height {
		lines = append(lines, "")
		usedLines++
	}

	return strings.Join(lines, "\n")
}

func formatPrice(price int) string {
	if price <= 0 {
		return "-"
	}
	if price >= 1000000 {
		return fmt.Sprintf("%.2fM", float64(price)/1000000)
	}
	if price >= 1000 {
		return fmt.Sprintf("%.1fK", float64(price)/1000)
	}
	return fmt.Sprintf("%d", price)
}

func formatProfit(profit float64, best bool) string {
	sign := "+"
	if profit < 0 {
		sign = ""
	}
	text := fmt.Sprintf("%s%.1f%%", sign, profit)
	if best {
		return styleProfitBest.Render(text)
	}
	return styleProfitNormal.Render(text)
}
