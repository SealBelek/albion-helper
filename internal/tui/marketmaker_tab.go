package tui

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"albion-helper/internal/models"
	"albion-helper/internal/service"
)

type mmFocus int

const (
	mmFocusOpportunities mmFocus = iota
	mmFocusPositions
)

const mmTickSeconds = 60

var (
	styleReady    = lipgloss.NewStyle().Foreground(lipgloss.Color("46")).Bold(true)
	styleNotReady = lipgloss.NewStyle().Foreground(lipgloss.Color("208"))
)

type mmLoadedMsg struct {
	ops       []models.Opportunity
	total     int
	positions []models.Position
	cities    []string
}

type mmTickMsg struct{}

type mmActionMsg struct{}

type mmRefreshDoneMsg struct{}

type MarketMakerTabModel struct {
	mmSvc    service.MarketMakerService
	width    int
	height   int
	langIdx  int

	cities   []string
	cityIdx  int
	page     int
	total    int
	ops      []models.Opportunity
	positions []models.Position

	focus       mmFocus
	oppCursor   int
	posCursor   int

	buying    bool
	buyInput  textinput.Model
	buyTarget *models.Opportunity

	refreshing  bool
	refreshedAt time.Time
}

func NewMarketMakerTabModel(svc service.MarketMakerService) MarketMakerTabModel {
	ti := textinput.New()
	ti.CharLimit = 10
	ti.Width = 8
	return MarketMakerTabModel{
		mmSvc:    svc,
		langIdx:  0,
		focus:    mmFocusOpportunities,
		buyInput: ti,
	}
}

func (m *MarketMakerTabModel) SetLangIdx(idx int) {
	m.langIdx = idx
}

func (m *MarketMakerTabModel) SetSize(w, h int) {
	m.width = w
	m.height = h
}

func (m MarketMakerTabModel) Init() tea.Cmd {
	return tea.Batch(m.loadAll(), m.tick())
}

func (m MarketMakerTabModel) tickCmd() tea.Cmd {
	return tea.Tick(mmTickSeconds*time.Second, func(time.Time) tea.Msg {
		return mmTickMsg{}
	})
}

func (m MarketMakerTabModel) tick() tea.Cmd {
	return m.tickCmd()
}

func (m MarketMakerTabModel) loadAll() tea.Cmd {
	svc := m.mmSvc
	langIdx := m.langIdx
	cityIdx := m.cityIdx
	page := m.page
	getCity := func(cities []string) string {
		if cityIdx < 0 || cityIdx >= len(cities) {
			return ""
		}
		return cities[cityIdx]
	}
	return func() tea.Msg {
		lang := models.Languages[langIdx].Code
		cities, _ := svc.GetCities()
		city := getCity(cities)
		var ops []models.Opportunity
		var total int
		var positions []models.Position
		if city != "" {
			ops, total, _ = svc.GetOpportunities(city, lang, page)
		}
		positions, _ = svc.GetOpenPositions(lang)
		return mmLoadedMsg{ops: ops, total: total, positions: positions, cities: cities}
	}
}

func (m MarketMakerTabModel) Update(msg tea.Msg) (MarketMakerTabModel, tea.Cmd) {
	switch msg := msg.(type) {
	case mmLoadedMsg:
		m.cities = msg.cities
		if m.cityIdx >= len(m.cities) {
			m.cityIdx = 0
		}
		m.ops = msg.ops
		m.total = msg.total
		m.positions = msg.positions
		if m.oppCursor >= len(m.ops) {
			m.oppCursor = len(m.ops) - 1
		}
		if m.oppCursor < 0 {
			m.oppCursor = 0
		}
		if m.posCursor >= len(m.positions) {
			m.posCursor = len(m.positions) - 1
		}
		if m.posCursor < 0 {
			m.posCursor = 0
		}
		wasLoading := m.refreshing
		m.refreshing = false
		if wasLoading {
			m.refreshedAt = time.Now()
			return m, tea.Tick(time.Second, func(time.Time) tea.Msg {
				return mmRefreshDoneMsg{}
			})
		}
		return m, nil

	case mmTickMsg:
		return m, tea.Batch(m.loadAll(), m.tick())

	case mmActionMsg:
		return m, m.loadAll()

	case mmRefreshDoneMsg:
		m.refreshedAt = time.Time{}
		return m, nil
	}

	if m.buying {
		switch msg := msg.(type) {
		case tea.KeyMsg:
			switch msg.String() {
			case "enter":
				target := m.buyTarget
				if target == nil {
					m.buying = false
					return m, nil
				}
				qty := int(target.SuggestQty)
				if qty <= 0 {
					qty = 10
				}
				if v, err := strconv.Atoi(m.buyInput.Value()); err == nil && v > 0 {
					qty = v
				}
				itemBase, enchant := parseEnchantLocal(target.ItemID)
				_ = itemBase
				cmd := m.markBuy(target.ItemID, enchant, target.Quality, target.City, target.BuyPrice, qty)
				m.buying = false
				m.buyTarget = nil
				m.buyInput.Blur()
				return m, cmd
			case "esc":
				m.buying = false
				m.buyTarget = nil
				m.buyInput.Blur()
				return m, nil
			}
			var cmd tea.Cmd
			m.buyInput, cmd = m.buyInput.Update(msg)
			return m, cmd
		}
		return m, nil
	}

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "c":
			return m.cycleCity(1)
		case "C":
			return m.cycleCity(-1)
		case "n":
			maxPage := 0
			if m.total > 0 {
				maxPage = (m.total - 1) / m.mmSvc.PageSize()
			}
			if m.page < maxPage {
				m.page++
				return m, m.loadAll()
			}
		case "p":
			if m.page > 0 {
				m.page--
				return m, m.loadAll()
			}
		case "r":
			m.refreshing = true
			return m, m.loadAll()
		case "up":
			return m.moveCursor(-1)
		case "down":
			return m.moveCursor(1)
		case "tab":
			m.focus = mmFocus(int(m.focus)+1) % 2
			return m, nil
		case "b":
			return m.startBuy()
		case "s":
			return m.markSell()
		}
	}

	return m, nil
}

func (m MarketMakerTabModel) cycleCity(dir int) (MarketMakerTabModel, tea.Cmd) {
	if len(m.cities) == 0 {
		return m, nil
	}
	m.cityIdx = (m.cityIdx + dir + len(m.cities)) % len(m.cities)
	m.page = 0
	return m, m.loadAll()
}

func (m MarketMakerTabModel) moveCursor(dir int) (MarketMakerTabModel, tea.Cmd) {
	if m.focus == mmFocusOpportunities {
		m.oppCursor += dir
		if m.oppCursor >= len(m.ops) {
			m.oppCursor = len(m.ops) - 1
			if len(m.positions) > 0 {
				m.focus = mmFocusPositions
				m.posCursor = 0
			}
		}
		if m.oppCursor < 0 {
			m.oppCursor = 0
		}
	} else {
		m.posCursor += dir
		if m.posCursor >= len(m.positions) {
			m.posCursor = len(m.positions) - 1
		}
		if m.posCursor < 0 {
			m.posCursor = 0
			if len(m.ops) > 0 {
				m.focus = mmFocusOpportunities
				m.oppCursor = len(m.ops) - 1
			}
		}
	}
	return m, nil
}

func (m MarketMakerTabModel) startBuy() (MarketMakerTabModel, tea.Cmd) {
	if len(m.ops) == 0 || m.oppCursor >= len(m.ops) {
		return m, nil
	}
	target := &m.ops[m.oppCursor]
	m.buying = true
	m.buyTarget = target
	m.buyInput.SetValue(strconv.FormatInt(max64(target.SuggestQty, 10), 10))
	m.buyInput.Focus()
	return m, textinput.Blink
}

func (m MarketMakerTabModel) markBuy(itemID string, enchantment, quality int, city string, buyPrice, qty int) tea.Cmd {
	svc := m.mmSvc
	return func() tea.Msg {
		_ = svc.MarkBuy(itemID, enchantment, quality, city, buyPrice, qty)
		return mmActionMsg{}
	}
}

func (m MarketMakerTabModel) markSell() (MarketMakerTabModel, tea.Cmd) {
	if len(m.positions) == 0 || m.posCursor >= len(m.positions) {
		return m, nil
	}
	if m.focus != mmFocusPositions {
		return m, nil
	}
	id := m.positions[m.posCursor].ID
	svc := m.mmSvc
	cmd := func() tea.Msg {
		_ = svc.MarkSell(id)
		return mmActionMsg{}
	}
	return m, cmd
}

func (m MarketMakerTabModel) View() string {
	if m.width < 40 || m.height < 5 {
		return ""
	}

	city := ""
	if m.cityIdx < len(m.cities) {
		city = m.cities[m.cityIdx]
	}
	pageCount := 1
	if m.total > 0 {
		pageCount = (m.total + m.mmSvc.PageSize() - 1) / m.mmSvc.PageSize()
	}
	if pageCount < 1 {
		pageCount = 1
	}
	var status string
	if m.refreshing {
		status = "  ⟳ Loading..."
	} else if !m.refreshedAt.IsZero() && time.Since(m.refreshedAt) <= time.Second {
		status = "  ✓ Refreshed"
	}
	header := styleSectionHeader.Render(fmt.Sprintf("── Market Maker — City: %s  Page %d/%d (%d)%s ──", city, m.page+1, pageCount, m.total, status))
	padLen := m.width - lipgloss.Width(header)
	if padLen < 0 {
		padLen = 0
	}
	header += styleDimmed.Render(strings.Repeat("─", padLen))

	lines := make([]string, 0, m.height+2)
	lines = append(lines, header)
	used := 1

	nameWidth := m.width - 58
	if nameWidth < 15 {
		nameWidth = 15
	}

	colNum := lipgloss.NewStyle().Width(4).Align(lipgloss.Left)
	colName := lipgloss.NewStyle().Width(nameWidth).Align(lipgloss.Left)
	colPrice7 := lipgloss.NewStyle().Width(7).Align(lipgloss.Right)
	colProfit9 := lipgloss.NewStyle().Width(9).Align(lipgloss.Right)
	colQty6 := lipgloss.NewStyle().Width(6).Align(lipgloss.Right)

	headerCells := []string{
		colNum.Render("#"),
		colName.Render("Item"),
		colPrice7.Render("Bid"),
		colPrice7.Render("Ask"),
		colPrice7.Render("Buy@"),
		colPrice7.Render("Sell@"),
		colProfit9.Render("Prft%"),
		colPrice7.Render("Vol24h"),
		colQty6.Render("Qty"),
	}
	lines = append(lines, "  "+styleHeaderCol.Render(lipgloss.JoinHorizontal(lipgloss.Left, headerCells...)))
	used++

	if len(m.ops) == 0 {
		empty := styleDimmed.Render("  No opportunities in this city (waiting for market data).")
		lines = append(lines, empty)
		used++
	}

	oppRows := len(m.ops)
	if oppRows > m.height/2 {
		oppRows = m.height / 2
	}

	for i := 0; i < oppRows; i++ {
		if used >= m.height-3 {
			break
		}
		o := m.ops[i]
		selected := m.focus == mmFocusOpportunities && i == m.oppCursor && !m.buying
		prefix := "  "
		if selected {
			prefix = styleCursor.Render("▸ ")
		}
		name := o.Name
		if o.Enchantment > 0 {
			name = fmt.Sprintf("%s@%d", name, o.Enchantment)
		}
		name = fmt.Sprintf("%s q%d", name, o.Quality)
		styledName := colName.Render(name)
		if selected {
			styledName = styleSelected.Render(styledName)
		}

		bidStyled := stylePriceGold.Render(colPrice7.Render(formatPrice(o.BestBid)))
		askStyled := stylePriceSilver.Render(colPrice7.Render(formatPrice(o.BestAsk)))
		buyStyled := stylePriceGold.Render(colPrice7.Render(formatPrice(o.BuyPrice)))
		sellStyled := stylePriceSilver.Render(colPrice7.Render(formatPrice(o.SellPrice)))

		profitRaw := fmt.Sprintf("%+.1f%%", o.ProfitPct)
		profitStyled := colProfit9.Render(profitRaw)
		if o.Profit > 0 {
			profitStyled = styleProfitBest.Render(colProfit9.Render(profitRaw))
		}

		volStyled := colPrice7.Render(formatPrice(int(o.DailyVolume)))
		qtyStyled := colQty6.Render(strconv.FormatInt(o.SuggestQty, 10))

		dataCells := []string{
			colNum.Render(fmt.Sprintf("%d", i+1+m.page*m.mmSvc.PageSize())),
			styledName,
			bidStyled,
			askStyled,
			buyStyled,
			sellStyled,
			profitStyled,
			volStyled,
			qtyStyled,
		}
		line := prefix + lipgloss.JoinHorizontal(lipgloss.Left, dataCells...)
		if o.HasPosition {
			line += styleTrackedMark.Render(" ★")
		}
		lines = append(lines, line)
		used++
	}

	posHeader := styleSectionHeader.Render(fmt.Sprintf("── Open Positions (%d) ──", len(m.positions)))
	padLen = m.width - lipgloss.Width(posHeader)
	if padLen < 0 {
		padLen = 0
	}
	posHeader += styleDimmed.Render(strings.Repeat("─", padLen))
	lines = append(lines, posHeader)
	used++

	remaining := m.height - used
	if remaining < 0 {
		remaining = 0
	}

	if len(m.positions) == 0 {
		lines = append(lines, styleDimmed.Render("  No open positions."))
		used++
	} else {
		start := m.posCursor - remaining/2
		if start < 0 {
			start = 0
		}
		if start+remaining > len(m.positions) {
			start = len(m.positions) - remaining
			if start < 0 {
				start = 0
			}
		}
		end := start + remaining
		if end > len(m.positions) {
			end = len(m.positions)
		}
		for i := start; i < end; i++ {
			p := m.positions[i]
			selected := m.focus == mmFocusPositions && i == m.posCursor && !m.buying
			prefix := "  "
			if selected {
				prefix = styleCursor.Render("▸ ")
			}
			name := p.Name
			if p.Enchantment > 0 {
				name = fmt.Sprintf("%s@%d", name, p.Enchantment)
			}
			name = fmt.Sprintf("%s q%d", name, p.Quality)
			name = padToWidth(name, nameWidth)
			if selected {
				name = styleSelected.Render(name)
			}

			askFmt := fmt.Sprintf("%7s", formatPrice(p.CurrentAsk))
			if p.CurrentAsk == 0 {
				askFmt = fmt.Sprintf("%7s", "-")
			}
			beFmt := fmt.Sprintf("%7s", formatPrice(p.BreakEven))
			buyFmt := fmt.Sprintf("%7s", formatPrice(p.BuyPrice))
			cityFmt := fmt.Sprintf("%-10s", p.City)

			stateStr := styleNotReady.Render("WAIT")
			if p.Ready {
				stateStr = styleReady.Render("READY")
			}
			line := prefix + fmt.Sprintf("%s %s buy %s BE %s now %s %s",
				name, cityFmt, buyFmt, beFmt, askFmt, stateStr)
			lines = append(lines, line)
			used++
		}
	}

	for used < m.height {
		lines = append(lines, "")
		used++
	}

	if m.buying && m.buyTarget != nil {
		target := m.buyTarget
		prompt := fmt.Sprintf("  Buy %s q%d @ %s in %s",
			target.Name, target.Quality, formatPrice(target.BuyPrice), target.City)
		hint := styleDimmed.Render("  Qty: " + m.buyInput.View() + "  Enter confirm  Esc cancel")
		return strings.Join(lines[:len(lines)-2], "\n") + "\n" + styleEnchantPrompt.Render(prompt) + hint
	}

	return strings.Join(lines, "\n")
}

func padToWidth(s string, w int) string {
	runes := []rune(s)
	var (
		out   []rune
		width int
	)
	for _, r := range runes {
		rw := lipgloss.Width(string(r))
		if rw == 0 {
			out = append(out, r)
			continue
		}
		if width+rw > w {
			break
		}
		out = append(out, r)
		width += rw
	}
	return string(out) + strings.Repeat(" ", w-width)
}

func max64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

func parseEnchantLocal(itemID string) (string, int) {
	if idx := strings.IndexByte(itemID, '@'); idx >= 0 {
		var enchant int
		fmt.Sscanf(itemID[idx+1:], "%d", &enchant)
		return itemID[:idx], enchant
	}
	return itemID, 0
}