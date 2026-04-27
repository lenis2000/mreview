package ui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"mreview/pkg/parser"
	"mreview/pkg/persist"
)

// AnnotListItem is one row in the `@` popup: a block ID, a breadcrumb, and
// the first line of the annotation note. LineOffset carries the per-block
// line anchor (0 = block-level) so e/d/Enter can target the specific
// annotation the user highlighted, not whichever one shares the block ID.
// Detached marks rows that came from sidecar.Detached (the block has
// vanished); those rows can be deleted but not jumped to or edited.
// Display format: "<breadcrumb> — <first-line>" (with a "(detached) "
// prefix for detached entries).
type AnnotListItem struct {
	BlockID    string
	Breadcrumb string
	LineOffset int
	FirstLine  string
	Detached   bool
}

// Display is the rendered row text.
func (it AnnotListItem) Display() string {
	body := it.Breadcrumb
	if body == "" {
		body = it.FirstLine
	} else if it.FirstLine != "" {
		body = it.Breadcrumb + " — " + it.FirstLine
	}
	if it.Detached {
		return "(detached) " + body
	}
	return body
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
// appended at the end so they remain reachable. Items from side.Detached
// are *also* appended at the end, marked with Detached=true so the popup
// surfaces them for deletion (the startup banner already advertises their
// existence — the popup must not silently hide them).
func BuildAnnotListItems(doc *parser.Document, side *persist.Sidecar) []AnnotListItem {
	if side == nil || (len(side.Annotations) == 0 && len(side.Detached) == 0) {
		return nil
	}
	present := make([]AnnotListItem, 0, len(side.Annotations))
	detached := make([]AnnotListItem, 0)
	for _, a := range side.Annotations {
		item := AnnotListItem{
			BlockID:    a.BlockID,
			Breadcrumb: a.Breadcrumb,
			LineOffset: a.LineOffset,
			FirstLine:  firstNoteLine(a.Note),
		}
		if doc != nil && doc.ByID[a.BlockID] != nil {
			present = append(present, item)
		} else {
			// Block has vanished from the parsed doc: the annotation is
			// orphaned but still lives in side.Annotations (it hasn't
			// been migrated to side.Detached yet — that only happens at
			// reload remap time). Mark it Detached so the popup's `d`
			// path takes the immediate-delete branch instead of the
			// "annotation's block no longer in document" no-op.
			item.Detached = true
			detached = append(detached, item)
		}
	}
	for _, a := range side.Detached {
		detached = append(detached, AnnotListItem{
			BlockID:    a.BlockID,
			Breadcrumb: a.Breadcrumb,
			LineOffset: a.LineOffset,
			FirstLine:  firstNoteLine(a.Note),
			Detached:   true,
		})
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
	it, ok := p.SelectedItem()
	if !ok {
		return ""
	}
	return it.BlockID
}

// SelectedItem returns the full item under the cursor, including its
// LineOffset so callers can reopen the exact annotation (not just any
// annotation on the same block).
func (p *AnnotListPopup) SelectedItem() (AnnotListItem, bool) {
	if len(p.Items) == 0 {
		return AnnotListItem{}, false
	}
	if p.Cursor < 0 || p.Cursor >= len(p.Items) {
		return AnnotListItem{}, false
	}
	return p.Items[p.Cursor], true
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
	it, ok := p.SelectedItem()
	m.Popup = nil
	if !ok {
		return m, nil
	}
	if it.Detached {
		m.Status = "@: detached annotation — delete with d (no live block to jump to)"
		return m, nil
	}
	if m.Doc == nil || m.Doc.ByID[it.BlockID] == nil {
		m.Status = "@: annotation's block no longer in document"
		return m, nil
	}
	if it.BlockID != m.CursorBlockID {
		m.JumpStack.Push(m.CursorBlockID)
		m.CursorBlockID = it.BlockID
		m.SourceLineCursor = 1
	}
	if it.LineOffset > 0 {
		m.SourceLineCursor = clampLineCursor(m.Doc, it.BlockID, it.LineOffset)
	}
	return m, nil
}

// editFromAnnotList closes the popup and opens the edit popup on the exact
// annotation that was highlighted — block-level or line-pinned according to
// its LineOffset.
func (m Model) editFromAnnotList(p *AnnotListPopup) (tea.Model, tea.Cmd) {
	it, ok := p.SelectedItem()
	m.Popup = nil
	if !ok {
		return m, nil
	}
	if it.Detached {
		m.Status = "@: detached annotations cannot be edited (delete and re-annotate)"
		return m, nil
	}
	if m.Doc == nil || m.Doc.ByID[it.BlockID] == nil {
		m.Status = "@: annotation's block no longer in document"
		return m, nil
	}
	if it.BlockID != m.CursorBlockID {
		m.JumpStack.Push(m.CursorBlockID)
		m.CursorBlockID = it.BlockID
		m.SourceLineCursor = 1
	}
	if it.LineOffset > 0 {
		m.SourceLineCursor = clampLineCursor(m.Doc, it.BlockID, it.LineOffset)
		return m.StartLineAnnotation()
	}
	return m.StartBlockAnnotation()
}

// deleteFromAnnotList closes the popup and begins the [y/N] delete-confirm
// flow for the highlighted annotation, preserving its line anchor so a
// line-pinned note doesn't accidentally delete a block-level note on the
// same block (or vice versa). Detached items take a separate path: they
// are removed straight from sidecar.Detached without the [y/N] prompt
// (no live block exists to navigate to; an immediate delete is the only
// useful action).
func (m Model) deleteFromAnnotList(p *AnnotListPopup) (tea.Model, tea.Cmd) {
	it, ok := p.SelectedItem()
	m.Popup = nil
	if !ok {
		return m, nil
	}
	if it.Detached {
		if m.Sidecar == nil {
			return m, nil
		}
		// Orphaned items can live in either side.Detached (migrated by
		// reload remap) or side.Annotations (block vanished but no
		// remap has run yet — see BuildAnnotListItems). Strip from both
		// so the popup's `d` action always succeeds regardless of when
		// the orphaning happened.
		m.Sidecar.Detached = removeDetachedAnnotation(m.Sidecar.Detached, it.BlockID, it.LineOffset)
		m.Sidecar.Annotations = removeAnnotation(m.Sidecar.Annotations, it.BlockID, it.LineOffset)
		if err := m.saveSidecar(); err != nil {
			m.Status = "save failed: " + err.Error()
		} else {
			m.Status = "@: detached annotation removed"
		}
		return m, nil
	}
	if m.Doc == nil || m.Doc.ByID[it.BlockID] == nil {
		m.Status = "@: annotation's block no longer in document"
		return m, nil
	}
	if it.BlockID != m.CursorBlockID {
		m.JumpStack.Push(m.CursorBlockID)
		m.CursorBlockID = it.BlockID
		m.SourceLineCursor = 1
	}
	if it.LineOffset > 0 {
		m.SourceLineCursor = clampLineCursor(m.Doc, it.BlockID, it.LineOffset)
	}
	return m.BeginDelete(), nil
}

// removeDetachedAnnotation drops the first entry matching (blockID,
// lineOffset) from the detached slice. Used by the `d` action in the @
// popup so the user can prune ghost annotations without hand-editing
// the sidecar markdown.
func removeDetachedAnnotation(xs []persist.Annotation, blockID string, lineOffset int) []persist.Annotation {
	out := make([]persist.Annotation, 0, len(xs))
	dropped := false
	for _, x := range xs {
		if !dropped && x.BlockID == blockID && x.LineOffset == lineOffset {
			dropped = true
			continue
		}
		out = append(out, x)
	}
	return out
}
