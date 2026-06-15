package tui

import (
	"database/sql"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
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

type Model struct {
	db     *sql.DB
	active tab
	width  int
	height int

	itemsTab  ItemsTabModel
	pricesTab PricesTabModel
}

func NewModel(database *sql.DB) Model {
	it := NewItemsTabModel(database)
	pt := NewPricesTabModel(database)
	pt.SetLangIdx(it.langIdx)

	return Model{
		db:        database,
		active:    tabItems,
		itemsTab:  it,
		pricesTab: pt,
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
		return m, nil

	case pricesLoadedMsg, tickMsg, apiCheckMsg, apiFetchedMsg, historyCheckMsg, historyFetchedMsg:
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
				return m, tea.Batch(cmd, m.pricesTab.refreshPrices())
			}
			if m.active != tabItems {
				m.active = tabItems
				return m, nil
			}

		case "right":
			handled, cmd := m.itemsTab.HandleLeftRight(1)
			if handled {
				m.pricesTab.SetLangIdx(m.itemsTab.langIdx)
				return m, tea.Batch(cmd, m.pricesTab.refreshPrices())
			}
			if m.active != tabPrices {
				m.active = tabPrices
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
		if _, ok := msg.(trackedLoadedMsg); ok {
			_, cmd2 := m.pricesTab.Update(apiCheckMsg{})
			return m, tea.Batch(cmd, cmd2)
		}
		return m, cmd
	case tabPrices:
		newTab, cmd := m.pricesTab.Update(msg)
		m.pricesTab = newTab
		return m, cmd
	}

	return m, nil
}

func (m Model) View() string {
	tabBar := m.renderTabBar()

	var content string
	switch m.active {
	case tabItems:
		content = m.itemsTab.View()
	case tabPrices:
		content = m.pricesTab.View()
	}

	help := m.renderHelp()

	return lipgloss.JoinVertical(lipgloss.Top, tabBar, content, help)
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
