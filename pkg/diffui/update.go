package diffui

import (
	tea "github.com/charmbracelet/bubbletea"

	"mreview/pkg/diffreview"
)

// Update implements tea.Model.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.Width = msg.Width
		m.Height = msg.Height
		return m, nil
	case tea.KeyMsg:
		return m.updateKey(msg)
	default:
		return m, nil
	}
}

func (m Model) updateKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()
	if key == "ctrl+c" || key == "q" {
		m.quitting = true
		return m, tea.Quit
	}
	if key == "?" {
		m.ShowHelp = !m.ShowHelp
		m.pendingG = false
		return m, nil
	}
	if m.ShowHelp {
		return m, nil
	}

	if m.pendingG {
		m.pendingG = false
		if key == "g" {
			m.moveToFirst()
			return m, nil
		}
	}

	switch key {
	case "f":
		m.Filter = CycleFilter(m.Filter)
		m.snapCursor()
	case "j", "down":
		m.moveVisible(1)
	case "k", "up":
		m.moveVisible(-1)
	case "J", "pgdown":
		m.moveVisible(5)
	case "K", "pgup":
		m.moveVisible(-5)
	case "g":
		m.pendingG = true
	case "home":
		m.moveToFirst()
	case "G", "end":
		m.moveToLast()
	case "}":
		m.moveSection(1)
	case "{":
		m.moveSection(-1)
	}
	return m, nil
}

func (m *Model) moveVisible(delta int) {
	visible := m.visibleIndices()
	if len(visible) == 0 {
		m.Status = "no pairs match filter"
		return
	}
	pos := m.visiblePosition(visible)
	pos += delta
	if pos < 0 {
		pos = 0
	}
	if pos >= len(visible) {
		pos = len(visible) - 1
	}
	m.Cursor = visible[pos]
	m.Status = ""
}

func (m *Model) moveToFirst() {
	visible := m.visibleIndices()
	if len(visible) == 0 {
		m.Status = "no pairs match filter"
		return
	}
	m.Cursor = visible[0]
	m.Status = ""
}

func (m *Model) moveToLast() {
	visible := m.visibleIndices()
	if len(visible) == 0 {
		m.Status = "no pairs match filter"
		return
	}
	m.Cursor = visible[len(visible)-1]
	m.Status = ""
}

func (m Model) visiblePosition(visible []int) int {
	if len(visible) == 0 {
		return 0
	}
	for i, idx := range visible {
		if idx == m.Cursor {
			return i
		}
	}
	for i, idx := range visible {
		if idx > m.Cursor {
			return i
		}
	}
	return len(visible) - 1
}

func (m *Model) moveSection(direction int) {
	visible := m.visibleIndices()
	if len(visible) == 0 {
		m.Status = "no pairs match filter"
		return
	}
	if m.Review == nil {
		return
	}
	pos := m.visiblePosition(visible)
	current := sectionKey(m.Review.Pairs[visible[pos]])
	if current == "" {
		m.Status = "no section information for current pair"
		return
	}
	for i := pos + direction; i >= 0 && i < len(visible); i += direction {
		next := sectionKey(m.Review.Pairs[visible[i]])
		if next != "" && next != current {
			m.Cursor = visible[i]
			m.Status = ""
			return
		}
	}
	m.Status = "no more sections"
}

func sectionKey(pair diffreview.Pair) string {
	path := pair.SectionPathNew
	if len(path) == 0 {
		path = pair.SectionPathOld
	}
	if len(path) == 0 {
		return ""
	}
	out := ""
	for i, part := range path {
		if i > 0 {
			out += "\x00"
		}
		out += part
	}
	return out
}
