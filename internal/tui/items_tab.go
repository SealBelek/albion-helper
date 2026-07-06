package tui

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"albion-helper/internal/models"
	"albion-helper/internal/service"
)

type focusZone int

const (
	focusSearch focusZone = iota
	focusResults
	focusTracked
	focusEnchant
)

const (
	searchResultLimit   = 30
	resultsVisibleLines = 5
)

var (
	styleSectionHeader = lipgloss.NewStyle().Foreground(lipgloss.Color("62")).Bold(true)
	styleCursor        = lipgloss.NewStyle().Foreground(lipgloss.Color("220")).Bold(true)
	styleTrackedMark   = lipgloss.NewStyle().Foreground(lipgloss.Color("220"))
	styleDimmed        = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	styleNormal        = lipgloss.NewStyle()
	styleSelected      = lipgloss.NewStyle().Background(lipgloss.Color("62")).Foreground(lipgloss.Color("255")).Bold(true)
	styleEnchantPrompt = lipgloss.NewStyle().Foreground(lipgloss.Color("220")).Bold(true)
)

type ItemsTabModel struct {
	itemSvc  service.ItemService
	focus    focusZone
	langIdx  int
	width    int
	height   int

	searchInput   textinput.Model
	lastQuery     string
	results       []models.SearchResult
	resultsCursor int

	tracked       []models.TrackedItem
	trackedCursor int

	choosingEnchant bool
	pickingQuality  bool
	enchantLevel    int
	qualityLevel    int
	enchantItemID   string
}

func NewItemsTabModel(svc service.ItemService) ItemsTabModel {
	ti := textinput.New()
	ti.Placeholder = "Search items..."
	ti.CharLimit = 60
	ti.Focus()

	langIdx := 0
	if saved := svc.GetSetting("lang_idx"); saved != "" {
		if v, err := strconv.Atoi(saved); err == nil && v >= 0 && v < len(models.Languages) {
			langIdx = v
		}
	}

	return ItemsTabModel{
		itemSvc:     svc,
		focus:       focusSearch,
		langIdx:     langIdx,
		searchInput: ti,
	}
}

func (m *ItemsTabModel) Init() tea.Cmd {
	return tea.Batch(
		textinput.Blink,
		m.loadTracked(),
	)
}

func (m *ItemsTabModel) loadTracked() tea.Cmd {
	langIdx := m.langIdx
	svc := m.itemSvc
	return func() tea.Msg {
		lang := models.Languages[langIdx].Code
		items, err := svc.GetTracked(lang)
		if err != nil {
			return nil
		}
		return trackedLoadedMsg(items)
	}
}

type trackedLoadedMsg []models.TrackedItem

func (m *ItemsTabModel) SetSize(w, h int) {
	m.width = w
	m.height = h
	langLabel := fmt.Sprintf("Lang: %s", models.Languages[m.langIdx].Code)
	langWidth := lipgloss.Width(styleDimmed.Render(langLabel))
	inputWidth := w - langWidth - 4
	if inputWidth < 10 {
		inputWidth = 10
	}
	m.searchInput.Width = inputWidth
}

func (m *ItemsTabModel) HandleLeftRight(dir int) (bool, tea.Cmd) {
	if m.choosingEnchant {
		if m.pickingQuality {
			m.qualityLevel += dir
			if m.qualityLevel < 1 {
				m.qualityLevel = 1
			}
			if m.qualityLevel > 5 {
				m.qualityLevel = 5
			}
		} else {
			m.enchantLevel += dir
			if m.enchantLevel < 0 {
				m.enchantLevel = 0
			}
			if m.enchantLevel > 4 {
				m.enchantLevel = 4
			}
		}
		return true, nil
	}

	if m.focus == focusSearch && m.searchInput.Value() == "" {
		m.langIdx += dir
		if m.langIdx < 0 {
			m.langIdx = len(models.Languages) - 1
		}
		if m.langIdx >= len(models.Languages) {
			m.langIdx = 0
		}
		langIdx := m.langIdx
		svc := m.itemSvc
		setCmd := func() tea.Msg {
			_ = svc.SetSetting("lang_idx", strconv.Itoa(langIdx))
			return nil
		}
		m.results = nil
		m.resultsCursor = 0
		m.lastQuery = ""
		m.searchInput.Reset()
		return true, tea.Batch(m.loadTracked(), setCmd)
	}
	return false, nil
}

func (m *ItemsTabModel) CycleFocus(dir int) {
	if m.choosingEnchant {
		return
	}
	m.focus = focusZone((int(m.focus) + dir + 3) % 3)
	if m.focus == focusSearch {
		m.searchInput.Focus()
	} else {
		m.searchInput.Blur()
	}
}

func (m ItemsTabModel) Update(msg tea.Msg) (ItemsTabModel, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case trackedLoadedMsg:
		m.tracked = msg
		if m.trackedCursor >= len(m.tracked) && len(m.tracked) > 0 {
			m.trackedCursor = len(m.tracked) - 1
		}
		cmds = append(cmds, m.recheckTracked())
		return m, tea.Batch(cmds...)

	case searchResultsMsg:
		m.results = msg
		if m.resultsCursor >= len(m.results) {
			m.resultsCursor = len(m.results) - 1
		}
		if m.resultsCursor < 0 {
			m.resultsCursor = 0
		}
		return m, nil
	}

	if m.choosingEnchant {
		switch msg := msg.(type) {
		case tea.KeyMsg:
			switch msg.String() {
			case "enter":
				if !m.pickingQuality {
					m.pickingQuality = true
					m.qualityLevel = 1
				} else {
					m.choosingEnchant = false
					m.pickingQuality = false
					cmds = append(cmds, m.trackItem(m.enchantItemID, m.enchantLevel, m.qualityLevel))
				}
				return m, tea.Batch(cmds...)

			case "esc":
				if m.pickingQuality {
					m.pickingQuality = false
				} else {
					m.choosingEnchant = false
					m.focus = focusResults
				}
				return m, nil
			}
		}
		return m, nil
	}

	if m.focus == focusSearch {
		switch msg := msg.(type) {
		case tea.KeyMsg:
			switch msg.String() {
			case "enter":
				if len(m.results) > 0 {
					m.focus = focusResults
					m.searchInput.Blur()
					return m, nil
				}
			}
		}

		var cmd tea.Cmd
		m.searchInput, cmd = m.searchInput.Update(msg)
		cmds = append(cmds, cmd)

		if m.searchInput.Value() != m.lastQuery {
			m.lastQuery = m.searchInput.Value()
			m.resultsCursor = 0
			cmds = append(cmds, m.doSearch(m.lastQuery))
		}
	} else {
		switch msg := msg.(type) {
		case tea.KeyMsg:
			switch msg.String() {
			case "up":
				if m.focus == focusResults && m.resultsCursor > 0 {
					m.resultsCursor--
				} else if m.focus == focusTracked && m.trackedCursor > 0 {
					m.trackedCursor--
				}

			case "down":
				if m.focus == focusResults && m.resultsCursor < len(m.results)-1 {
					m.resultsCursor++
				} else if m.focus == focusTracked && m.trackedCursor < len(m.tracked)-1 {
					m.trackedCursor++
				}

			case "enter":
				if m.focus == focusResults && len(m.results) > 0 && m.resultsCursor < len(m.results) {
					r := m.results[m.resultsCursor]
					m.choosingEnchant = true
					m.pickingQuality = false
					m.enchantLevel = 0
					m.enchantItemID = r.UniqueName
				}

			case "d":
				if m.focus == focusTracked && len(m.tracked) > 0 && m.trackedCursor < len(m.tracked) {
					t := m.tracked[m.trackedCursor]
					cmds = append(cmds, m.untrackItem(t.UniqueName, t.Enchantment, t.Quality))
				}

			case "delete", "backspace":
				if m.focus == focusTracked && len(m.tracked) > 0 && m.trackedCursor < len(m.tracked) {
					t := m.tracked[m.trackedCursor]
					cmds = append(cmds, m.untrackItem(t.UniqueName, t.Enchantment, t.Quality))
				}
			}
		}
	}

	return m, tea.Batch(cmds...)
}

func (m *ItemsTabModel) recheckTracked() tea.Cmd {
	langIdx := m.langIdx
	svc := m.itemSvc
	lastQuery := m.lastQuery
	return func() tea.Msg {
		lang := models.Languages[langIdx].Code
		results, err := svc.Search(lang, lastQuery, searchResultLimit)
		if err != nil {
			return nil
		}
		return searchResultsMsg(results)
	}
}

func (m *ItemsTabModel) doSearch(query string) tea.Cmd {
	langIdx := m.langIdx
	svc := m.itemSvc
	return func() tea.Msg {
		lang := models.Languages[langIdx].Code
		results, err := svc.Search(lang, query, searchResultLimit)
		if err != nil {
			return nil
		}
		return searchResultsMsg(results)
	}
}

type searchResultsMsg []models.SearchResult

func (m *ItemsTabModel) trackItem(uniqueName string, enchantment, quality int) tea.Cmd {
	svc := m.itemSvc
	langIdx := m.langIdx
	return func() tea.Msg {
		items, err := svc.TrackItem(uniqueName, enchantment, quality, models.Languages[langIdx].Code)
		if err != nil {
			return nil
		}
		return trackedLoadedMsg(items)
	}
}

func (m *ItemsTabModel) untrackItem(uniqueName string, enchantment, quality int) tea.Cmd {
	svc := m.itemSvc
	langIdx := m.langIdx
	return func() tea.Msg {
		items, err := svc.UntrackItem(uniqueName, enchantment, quality, models.Languages[langIdx].Code)
		if err != nil {
			return nil
		}
		return trackedLoadedMsg(items)
	}
}

func (m ItemsTabModel) View() string {
	if m.width < 20 {
		return "Window too small"
	}

	searchLine := m.renderSearchLine()
	resultsSection := m.renderResultsSection()

	if m.choosingEnchant {
		pickerLine := m.renderPicker()
		used := 1 + (resultsVisibleLines + 1) + 1
		availableTracked := m.height - used
		if availableTracked < 2 {
			availableTracked = 2
		}
		trackedSection := m.renderTrackedSection(availableTracked)
		return lipgloss.JoinVertical(lipgloss.Top,
			searchLine,
			resultsSection,
			pickerLine,
			trackedSection,
		)
	}

	availableTracked := m.height - 1 - (resultsVisibleLines + 1)
	if availableTracked < 3 {
		availableTracked = 3
	}
	trackedSection := m.renderTrackedSection(availableTracked)

	return lipgloss.JoinVertical(lipgloss.Top,
		searchLine,
		resultsSection,
		trackedSection,
	)
}

func (m ItemsTabModel) renderPicker() string {
	if !m.pickingQuality {
		levels := make([]string, 5)
		for i := 0; i < 5; i++ {
			label := fmt.Sprintf(" @%d ", i)
			if i == m.enchantLevel {
				levels[i] = styleEnchantPrompt.Render(label)
			} else {
				levels[i] = styleDimmed.Render(label)
			}
		}
		bar := strings.Join(levels, "")
		hint := styleDimmed.Render(" ←→ pick  Enter confirm  Esc cancel")
		return "  Enchantment: " + bar + "  " + hint
	}

	qualityNames := []string{"1", "2", "3", "4", "5"}
	levels := make([]string, 5)
	for i := 0; i < 5; i++ {
		label := fmt.Sprintf(" %s ", qualityNames[i])
		if i+1 == m.qualityLevel {
			levels[i] = styleEnchantPrompt.Render(label)
		} else {
			levels[i] = styleDimmed.Render(label)
		}
	}
	bar := strings.Join(levels, "")
	hint := styleDimmed.Render(" ←→ pick  Enter confirm  Esc cancel")
	return "  Quality:     " + bar + "  " + hint
}

func (m ItemsTabModel) renderSearchLine() string {
	lang := models.Languages[m.langIdx]
	langLabel := fmt.Sprintf("Lang: %s", lang.Code)
	if m.focus == focusSearch && m.searchInput.Value() == "" {
		langLabel = styleSelected.Render(langLabel)
	} else {
		langLabel = styleDimmed.Render(langLabel)
	}

	sep := styleDimmed.Render(" │ ")
	line := langLabel + sep + m.searchInput.View()

	padding := m.width - lipgloss.Width(line)
	if padding > 0 {
		line += strings.Repeat(" ", padding)
	}

	return line
}

func (m ItemsTabModel) renderResultsSection() string {
	header := styleSectionHeader.Render(fmt.Sprintf("── Results (%d) ──", len(m.results)))
	padLen := m.width - lipgloss.Width(header)
	if padLen < 0 {
		padLen = 0
	}
	header += styleDimmed.Render(strings.Repeat("─", padLen))

	lines := make([]string, 0, resultsVisibleLines+2)
	lines = append(lines, header)

	if len(m.results) == 0 && m.lastQuery != "" {
		lines = append(lines, styleDimmed.Render("  No items found"))
	} else {
		start := m.resultsCursor - resultsVisibleLines/2
		if start < 0 {
			start = 0
		}
		if start+resultsVisibleLines > len(m.results) {
			start = len(m.results) - resultsVisibleLines
			if start < 0 {
				start = 0
			}
		}

		end := start + resultsVisibleLines
		if end > len(m.results) {
			end = len(m.results)
		}

		for i := start; i < end; i++ {
			r := m.results[i]
			prefix := "  "
			if m.focus == focusResults && i == m.resultsCursor && !m.choosingEnchant {
				prefix = styleCursor.Render("> ")
			}

			name := r.Name
			if r.Tracked {
				prefix += styleTrackedMark.Render("★ ")
			}

			if m.focus == focusResults && i == m.resultsCursor && !m.choosingEnchant {
				name = styleSelected.Render(name)
			}

			entry := prefix + name
			entry += "  " + styleDimmed.Render(r.UniqueName)

			entryWidth := lipgloss.Width(entry)
			if entryWidth > m.width {
				entry = styleDimmed.Render("  " + r.Name + " " + r.UniqueName)
				if m.focus == focusResults && i == m.resultsCursor && !m.choosingEnchant {
					entry = styleCursor.Render("> ") + styleSelected.Render(r.Name) + " " + styleDimmed.Render(r.UniqueName)
				}
			}

			lines = append(lines, entry)
		}
	}

	return strings.Join(lines, "\n")
}

func (m ItemsTabModel) renderTrackedSection(maxHeight int) string {
	header := styleSectionHeader.Render(fmt.Sprintf("── Tracked (%d) ──", len(m.tracked)))
	padLen := m.width - lipgloss.Width(header)
	if padLen < 0 {
		padLen = 0
	}
	header += styleDimmed.Render(strings.Repeat("─", padLen))

	lines := make([]string, 0, maxHeight+1)
	lines = append(lines, header)

	if len(m.tracked) == 0 {
		lines = append(lines, styleDimmed.Render("  No items tracked"))
		return strings.Join(lines, "\n")
	}

	availableLines := maxHeight - 1
	if availableLines < 1 {
		availableLines = 1
	}

	start := m.trackedCursor - availableLines/2
	if start < 0 {
		start = 0
	}
	if start+availableLines > len(m.tracked) {
		start = len(m.tracked) - availableLines
		if start < 0 {
			start = 0
		}
	}

	end := start + availableLines
	if end > len(m.tracked) {
		end = len(m.tracked)
	}

	for i := start; i < end; i++ {
		t := m.tracked[i]
		prefix := "  "
		if m.focus == focusTracked && i == m.trackedCursor && !m.choosingEnchant {
			prefix = styleCursor.Render("> ")
		}

		name := t.Name
		if m.focus == focusTracked && i == m.trackedCursor && !m.choosingEnchant {
			name = styleSelected.Render(name)
		}

		entry := prefix + styleTrackedMark.Render("★ ") + name

		if t.Enchantment > 0 {
			entry += styleEnchantPrompt.Render(fmt.Sprintf("@%d", t.Enchantment))
		}
		entry += styleDimmed.Render(fmt.Sprintf(" q%d", t.Quality))

		entry += "  " + styleDimmed.Render(t.UniqueName)

		lines = append(lines, entry)
	}

	return strings.Join(lines, "\n")
}