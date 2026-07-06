package tui

import (
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"albion-helper/internal/service"
)

type tab int

const (
	tabItems tab = iota
	tabPrices
)

var (
	styleTabActive   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("255")).Background(lipgloss.Color("62")).Padding(0, 1)
	styleTabInactive = lipgloss.NewStyle().Foreground(lipgloss.Color("240")).Padding(0, 1)
	styleTabBar      = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	styleHelp        = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
)

var modelViewCache struct {
	gen  uint64
	view string
}

type Model struct {
	itemSvc  service.ItemService
	priceSvc service.PriceService
	active   tab
	width    int
	height   int

	itemsTab  ItemsTabModel
	pricesTab PricesTabModel

	viewGen uint64
}

func NewModel(itemSvc service.ItemService, priceSvc service.PriceService) Model {
	it := NewItemsTabModel(itemSvc)
	pt := NewPricesTabModel(priceSvc)
	pt.SetLangIdx(it.langIdx)

	return Model{
		itemSvc:   itemSvc,
		priceSvc:  priceSvc,
		active:    tabItems,
		itemsTab:  it,
		pricesTab: pt,
		viewGen:   1,
	}
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(
		m.itemsTab.Init(),
		m.pricesTab.Init(),
	)
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.itemsTab.SetSize(msg.Width, msg.Height-3)
		m.pricesTab.SetSize(msg.Width, msg.Height-3)
		m.viewGen++
		return m, nil

	case pricesLoadedMsg:
		newTab, cmd := m.pricesTab.Update(msg)
		m.pricesTab = newTab
		m.viewGen++
		return m, cmd

	case tickMsg:
		newTab, cmd := m.pricesTab.Update(msg)
		m.pricesTab = newTab
		return m, cmd

	case apiCheckMsg:
		newTab, cmd := m.pricesTab.Update(msg)
		m.pricesTab = newTab
		return m, cmd

	case apiFetchedMsg:
		newTab, cmd := m.pricesTab.Update(msg)
		m.pricesTab = newTab
		return m, cmd

	case historyCheckMsg:
		newTab, cmd := m.pricesTab.Update(msg)
		m.pricesTab = newTab
		return m, cmd

	case historyFetchedMsg:
		newTab, cmd := m.pricesTab.Update(msg)
		m.pricesTab = newTab
		return m, cmd

	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "esc":
			return m, tea.Quit

		case "left":
			handled, cmd := m.itemsTab.HandleLeftRight(-1)
			if handled {
				m.pricesTab.SetLangIdx(m.itemsTab.langIdx)
				m.viewGen++
				return m, tea.Batch(cmd, m.pricesTab.refreshPrices())
			}
			if m.active != tabItems {
				m.active = tabItems
				m.viewGen++
				return m, nil
			}

		case "right":
			handled, cmd := m.itemsTab.HandleLeftRight(1)
			if handled {
				m.pricesTab.SetLangIdx(m.itemsTab.langIdx)
				m.viewGen++
				return m, tea.Batch(cmd, m.pricesTab.refreshPrices())
			}
			if m.active != tabPrices {
				m.active = tabPrices
				m.viewGen++
				return m, nil
			}

		case "tab":
			if m.active == tabItems {
				m.itemsTab.CycleFocus(1)
			}
			return m, nil

		case "shift+tab":
			if m.active == tabItems {
				m.itemsTab.CycleFocus(-1)
			}
			return m, nil
		}
	}

	switch m.active {
	case tabItems:
		newTab, cmd := m.itemsTab.Update(msg)
		m.itemsTab = newTab
		if tmsg, ok := msg.(trackedLoadedMsg); ok {
			m.pricesTab.lastHistory = time.Time{}
			_, cmd2 := m.pricesTab.Update(apiCheckMsg{})
			m.viewGen++
			_ = tmsg
			return m, tea.Batch(cmd, cmd2, m.pricesTab.refreshPrices())
		}
		if _, ok := msg.(searchResultsMsg); ok {
			m.viewGen++
		}
		return m, cmd
	case tabPrices:
		newTab, cmd := m.pricesTab.Update(msg)
		m.pricesTab = newTab
		if _, ok := msg.(tea.KeyMsg); ok {
			m.viewGen++
		}
		return m, cmd
	}

	return m, nil
}

func (m Model) View() string {
	if m.active == tabPrices && m.viewGen == modelViewCache.gen && modelViewCache.view != "" {
		return modelViewCache.view
	}

	tabBar := m.renderTabBar()

	var content string
	switch m.active {
	case tabItems:
		content = m.itemsTab.View()
	case tabPrices:
		content = m.pricesTab.View()
	}

	help := m.renderHelp()

	result := lipgloss.JoinVertical(lipgloss.Top, tabBar, content, help)
	if m.active == tabPrices {
		modelViewCache.gen = m.viewGen
		modelViewCache.view = result
	}
	return result
}

func (m Model) renderTabBar() string {
	itemsLabel := styleTabActive.Render(" Items ")
	pricesLabel := styleTabInactive.Render(" Prices ")
	if m.active == tabPrices {
		itemsLabel = styleTabInactive.Render(" Items ")
		pricesLabel = styleTabActive.Render(" Prices ")
	}

	tabs := itemsLabel + pricesLabel
	fillWidth := m.width - lipgloss.Width(tabs)
	if fillWidth < 0 {
		fillWidth = 0
	}
	tabs += styleTabBar.Render(strings.Repeat("─", fillWidth))

	return tabs
}

func (m Model) renderHelp() string {
	switch m.active {
	case tabItems:
		var keys string
		switch m.itemsTab.focus {
		case focusSearch:
			keys = "←→: lang  Tab: next  Ctrl+C: quit"
		case focusResults:
			keys = "↑↓: navigate  Enter: track  Tab: next  ←→: switch tab  Ctrl+C: quit"
		case focusTracked:
			keys = "↑↓: navigate  d: delete  Tab: next  ←→: switch tab  Ctrl+C: quit"
		}
		return makeHelpLine(keys, m.width)

	case tabPrices:
		keys := "↑↓: items  r: refresh  ←→: switch tab  Ctrl+C: quit"
		return makeHelpLine(keys, m.width)
	}

	return styleHelp.Render(strings.Repeat("─", m.width))
}

func makeHelpLine(keys string, width int) string {
	padding := width - lipgloss.Width(keys)
	if padding < 0 {
		padding = 0
	}
	return styleHelp.Render(strings.Repeat("─", padding) + keys)
}