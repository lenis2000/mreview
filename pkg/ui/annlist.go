package ui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"mreview/pkg/parser"
	"mreview/pkg/persist"
)

// AnnotListItem is one row in the `@` popup: a block ID, a breadcrumb, and
// the first line of the annotation note. Display format:
// "<breadcrumb> — <first-line>".
type AnnotListItem struct {
	BlockID    string
	Breadcrumb string
	FirstLine  string
}

// Display is the rendered row text.
func (it AnnotListItem) Display() string {
	if it.FirstLine == "" {
		return it.Breadcrumb
	}
	if it.Breadcrumb == "" {
		return it.FirstLine
	}
	return it.Breadcrumb + " — " + it.FirstLine
}

// AnnotListPopup lists every annotation in the sidecar in document order.
// j/k navigate, Enter jumps to the selected block, e opens the edit popup on
// the selected annotation, d begins the delete-confirm flow, Esc closes.
type AnnotListPopup struct {
	Items  []AnnotListItem
	Cursor int
}

// popup marks AnnotListPopup as a Popup.
func (*AnnotListPopup) popup() {}

// BuildAnnotListItems collects one item per annotation, ordered by the
// annotation's block position in the document so the list reads top-to-bottom.
// Annotations whose BlockID has been detached (not present in doc.ByID) are
// appended at the end so they remain reachable.
func BuildAnnotListItems(doc *parser.Document, side *persist.Sidecar) []AnnotListItem {
	if side == nil || len(side.Annotations) == 0 {
		return nil
	}
	present := make([]AnnotListItem, 0, len(side.Annotations))
	detached := make([]AnnotListItem, 0)
	for _, a := range side.Annotations {
		item := AnnotListItem{
			BlockID:    a.BlockID,
			Breadcrumb: a.Breadcrumb,
			FirstLine:  firstNoteLine(a.Note),
		}
		if doc != nil && doc.ByID[a.BlockID] != nil {
			present = append(present, item)
		} else {
			detached = append(detached, item)
		}
	}
	// Sort present items by document position.
	orderOf := func(id string) int {
		return positionOf(doc, id)
	}
	// Simple insertion sort — list is small in practice.
	for i := 1; i < len(present); i++ {
		j := i
		for j > 0 && orderOf(present[j].BlockID) < orderOf(present[j-1].BlockID) {
			present[j], present[j-1] = present[j-1], present[j]
			j--
		}
	}
	return append(present, detached...)
}

// firstNoteLine returns the first non-empty line of a note, trimmed.
func firstNoteLine(note string) string {
	for _, ln := range strings.Split(note, "\n") {
		ln = strings.TrimSpace(ln)
		if ln != "" {
			return ln
		}
	}
	return ""
}

// NewAnnotListPopup builds a popup. When no annotations exist, returns nil so
// callers can post a status message instead of opening an empty modal.
func NewAnnotListPopup(doc *parser.Document, side *persist.Sidecar) *AnnotListPopup {
	items := BuildAnnotListItems(doc, side)
	if len(items) == 0 {
		return nil
	}
	return &AnnotListPopup{Items: items}
}

// Move clamps at the ends (no wrap) — matches SearchPopup.
func (p *AnnotListPopup) Move(delta int) {
	if len(p.Items) == 0 {
		p.Cursor = 0
		return
	}
	p.Cursor += delta
	if p.Cursor < 0 {
		p.Cursor = 0
	}
	if p.Cursor >= len(p.Items) {
		p.Cursor = len(p.Items) - 1
	}
}

// Selected returns the BlockID of the highlighted row, or "" when empty.
func (p *AnnotListPopup) Selected() string {
	if len(p.Items) == 0 {
		return ""
	}
	if p.Cursor < 0 || p.Cursor >= len(p.Items) {
		return ""
	}
	return p.Items[p.Cursor].BlockID
}

// OpenAnnotList opens the `@` popup. Posts a status when the sidecar has no
// annotations; otherwise shows the modal.
func (m Model) OpenAnnotList() (tea.Model, tea.Cmd) {
	m.CountBuf = ""
	m.PendingG = false
	p := NewAnnotListPopup(m.Doc, m.Sidecar)
	if p == nil {
		m.Status = "@: no annotations"
		return m, nil
	}
	m.Popup = p
	m.Status = ""
	return m, nil
}

// jumpFromAnnotList closes the popup and focuses the selected annotation's
// block. Pushes the jump stack. Missing / detached targets are dropped with
// a status message.
func (m Model) jumpFromAnnotList(p *AnnotListPopup) (tea.Model, tea.Cmd) {
	target := p.Selected()
	m.Popup = nil
	if target == "" {
		return m, nil
	}
	if m.Doc == nil || m.Doc.ByID[target] == nil {
		m.Status = "@: annotation's block no longer in document"
		return m, nil
	}
	if target != m.CursorBlockID {
		m.JumpStack.Push(m.CursorBlockID)
		m.CursorBlockID = target
	}
	return m, nil
}

// editFromAnnotList closes the popup and opens the annotation edit textarea
// for the selected block.
func (m Model) editFromAnnotList(p *AnnotListPopup) (tea.Model, tea.Cmd) {
	target := p.Selected()
	m.Popup = nil
	if target == "" {
		return m, nil
	}
	if m.Doc == nil || m.Doc.ByID[target] == nil {
		m.Status = "@: annotation's block no longer in document"
		return m, nil
	}
	if target != m.CursorBlockID {
		m.JumpStack.Push(m.CursorBlockID)
		m.CursorBlockID = target
	}
	return m.StartAnnotation(false)
}

// deleteFromAnnotList closes the popup and begins the [y/N] delete-confirm
// flow for the selected annotation's block. Honours the existing cursor-
// based delete path (target = cursor block) so the confirmation prompt
// wording stays consistent.
func (m Model) deleteFromAnnotList(p *AnnotListPopup) (tea.Model, tea.Cmd) {
	target := p.Selected()
	m.Popup = nil
	if target == "" {
		return m, nil
	}
	if m.Doc == nil || m.Doc.ByID[target] == nil {
		m.Status = "@: annotation's block no longer in document"
		return m, nil
	}
	if target != m.CursorBlockID {
		m.JumpStack.Push(m.CursorBlockID)
		m.CursorBlockID = target
	}
	return m.BeginDelete(), nil
}
