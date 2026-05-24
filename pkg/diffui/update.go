package diffui

import (
	"strings"

	"github.com/charmbracelet/bubbles/textarea"
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
	case diffEditFinishedMsg:
		return m.applyEditFinished(msg)
	case tea.KeyMsg:
		if m.LineEdit != nil {
			return m.updateLineEditPopup(msg)
		}
		if m.Popup != nil {
			return m.updateAnnotationPopup(msg)
		}
		return m.updateKey(msg)
	default:
		return m, nil
	}
}

func (m Model) updateKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()
	if m.Pending != nil {
		if key == "ctrl+c" {
			m.Pending = nil
			m.quitting = true
			return m, tea.Quit
		}
		return m.confirmDelete(key == "y" || key == "Y"), nil
	}
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
	case " ", "space":
		return m.toggleReviewed(), nil
	case "a":
		return m.startAnnotation(false)
	case "ctrl+a":
		return m.startAnnotation(true)
	case "d":
		return m.beginDelete(), nil
	case "E":
		return m.editInExternalEditor()
	case "e":
		return m.startLineEdit()
	case "u":
		return m.undoEdit()
	case "ctrl+r":
		return m.redoEdit()
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

func (m Model) toggleReviewed() Model {
	pair := m.CurrentPair()
	if pair == nil {
		m.Status = "no pair selected"
		return m
	}
	visibleBefore := m.visibleIndices()
	posBefore := m.visiblePosition(visibleBefore)
	was := m.Reviewed[pair.ID]
	now := !was
	m.Reviewed[pair.ID] = now
	m.ensureSidecar().SetReviewed(pair.ID, now)
	m.Status = ""
	if now && (m.Filter == FilterChanged || m.Filter == FilterUnreviewed) {
		m.advanceAfterReviewed(visibleBefore, posBefore)
	}
	return m
}

func (m *Model) advanceAfterReviewed(visibleBefore []int, posBefore int) {
	if len(visibleBefore) == 0 {
		return
	}
	switch m.Filter {
	case FilterChanged:
		if posBefore+1 < len(visibleBefore) {
			m.Cursor = visibleBefore[posBefore+1]
		}
	case FilterUnreviewed:
		visibleAfter := m.visibleIndices()
		if len(visibleAfter) == 0 {
			m.Status = "all visible pairs reviewed"
			return
		}
		if posBefore >= len(visibleAfter) {
			posBefore = len(visibleAfter) - 1
		}
		m.Cursor = visibleAfter[posBefore]
	}
}

func (m Model) startAnnotation(editing bool) (tea.Model, tea.Cmd) {
	pair := m.CurrentPair()
	if pair == nil {
		m.Status = "no pair selected"
		return m, nil
	}
	initial := m.Annotations[pair.ID]
	if editing && strings.TrimSpace(initial) == "" {
		m.Status = "no annotation on current pair"
		return m, nil
	}
	popup, cmd := newAnnotationPopup(pair.ID, initial, editing)
	m.Popup = popup
	m.pendingG = false
	m.Status = ""
	return m, cmd
}

func newAnnotationPopup(pairID, initial string, editing bool) (*AnnotationPopup, tea.Cmd) {
	ta := textarea.New()
	ta.Prompt = "| "
	ta.ShowLineNumbers = false
	ta.CharLimit = 4000
	ta.SetWidth(60)
	ta.SetHeight(6)
	if initial != "" {
		ta.SetValue(initial)
	}
	cmd := ta.Focus()
	return &AnnotationPopup{TA: ta, PairID: pairID, Editing: editing}, cmd
}

func (m Model) updateAnnotationPopup(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEnter:
		return m.submitAnnotation(), nil
	case tea.KeyEsc, tea.KeyCtrlC:
		m.Popup = nil
		m.Status = ""
		return m, nil
	}
	if msg.String() == "ctrl+s" {
		return m.submitAnnotation(), nil
	}
	var cmd tea.Cmd
	m.Popup.TA, cmd = m.Popup.TA.Update(msg)
	return m, cmd
}

func (m Model) submitAnnotation() Model {
	if m.Popup == nil {
		return m
	}
	pairID := m.Popup.PairID
	note := strings.TrimSpace(m.Popup.TA.Value())
	if note != "" {
		note = strings.Join(strings.Fields(note), " ")
	}
	m.Popup = nil
	if note == "" {
		m.Status = ""
		return m
	}
	pair := m.pairByID(pairID)
	if pair == nil {
		m.Status = "pair no longer exists"
		return m
	}
	m.ensureSidecar().UpsertAnnotation(diffreview.AnnotationForPair(m.Review, pair, note))
	m.Annotations[pairID] = note
	m.Status = ""
	return m
}

func (m Model) beginDelete() Model {
	pair := m.CurrentPair()
	if pair == nil {
		m.Status = "no pair selected"
		return m
	}
	if strings.TrimSpace(m.Annotations[pair.ID]) == "" {
		m.Status = "no annotation on current pair"
		return m
	}
	m.Pending = &PendingDelete{PairID: pair.ID}
	m.Status = "[y/N] delete annotation?"
	return m
}

func (m Model) confirmDelete(yes bool) Model {
	pending := m.Pending
	m.Pending = nil
	if pending == nil {
		return m
	}
	if !yes {
		m.Status = ""
		return m
	}
	m.ensureSidecar().DeleteAnnotation(pending.PairID)
	delete(m.Annotations, pending.PairID)
	m.Status = ""
	return m
}

func (m *Model) ensureSidecar() *diffreview.Sidecar {
	if m.Sidecar == nil {
		m.Sidecar = diffreview.NewSidecar(m.Review)
	}
	return m.Sidecar
}

func (m Model) pairByID(pairID string) *diffreview.Pair {
	if m.Review == nil || pairID == "" {
		return nil
	}
	if pair := m.Review.ByID[pairID]; pair != nil {
		return pair
	}
	for i := range m.Review.Pairs {
		if m.Review.Pairs[i].ID == pairID {
			return &m.Review.Pairs[i]
		}
	}
	return nil
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
