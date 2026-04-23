package ui

import (
	tea "github.com/charmbracelet/bubbletea"

	"mreview/pkg/parser"
	"mreview/pkg/persist"
)

// handleMouse turns mouse events into focus + cursor moves. We only act on
// the press half of a click (Action == MouseActionPress, Button == left) to
// avoid double-firing on press+release pairs and to ignore wheel/motion
// events the alt-screen capture also delivers.
//
// Behaviour by pane:
//   - outline: focus + move block cursor to the clicked row.
//   - source:  focus + move block cursor to the block containing the clicked
//     line (if different) and set the source line cursor to that line.
//   - pdf:     focus only — kitty graphics make per-row hit-testing
//     unreliable so we don't try to map clicks to PDF features.
func (m Model) handleMouse(msg tea.MouseMsg) Model {
	if msg.Action != tea.MouseActionPress || msg.Button != tea.MouseButtonLeft {
		return m
	}
	if m.Width <= 0 || m.Height <= 0 {
		return m
	}
	pane, innerY, ok := paneAtPoint(m.Width, m.Height, m.Layout, msg.X, msg.Y)
	if !ok {
		return m
	}
	m.Focus = pane
	switch pane {
	case PaneOutline:
		if id := outlineRowAt(m.Doc, m.Sidecar, m.Filter, innerY); id != "" {
			if id != m.CursorBlockID {
				m.CursorBlockID = id
				m.SourceLineCursor = 1
			}
		}
	case PaneSource:
		blockID, lineOff := sourceLineAt(m.Doc, m.CursorBlockID, m.Width, m.Height, m.Layout, innerY)
		if blockID != "" {
			if blockID != m.CursorBlockID {
				m.CursorBlockID = blockID
				m.SourceLineCursor = 1
			}
			if lineOff > 0 {
				m.SourceLineCursor = lineOff
			}
		}
	}
	return m
}

// paneAtPoint returns which pane covers the (x, y) terminal cell along with
// the y-offset *inside* that pane's body (after the title/border insets), or
// ok=false if the point is outside the panes (e.g. the status row). Mirrors
// the layout math in view.go.
func paneAtPoint(termW, termH int, layout LayoutMode, x, y int) (Pane, int, bool) {
	if y < 0 || y >= termH-statusBarHeight {
		return 0, 0, false
	}
	switch layout {
	case LayoutStacked:
		outlineW, _ := stackedWidths(termW)
		topH, _ := stackedHeights(termH - statusBarHeight)
		if x < outlineW {
			return PaneOutline, paneInnerY(y, termH-statusBarHeight), true
		}
		if y < topH {
			return PaneSource, paneInnerY(y, topH), true
		}
		return PanePDF, paneInnerY(y-topH, termH-statusBarHeight-topH), true
	default:
		outlineW, sourceW, _ := paneWidths(termW)
		if x < outlineW {
			return PaneOutline, paneInnerY(y, termH-statusBarHeight), true
		}
		if x < outlineW+sourceW {
			return PaneSource, paneInnerY(y, termH-statusBarHeight), true
		}
		return PanePDF, paneInnerY(y, termH-statusBarHeight), true
	}
}

// paneInnerY converts a pane-relative y into a body-row index, returning -1
// when the click landed on the border or the title row. The lipgloss bordered
// box adds 1 row on top + 1 on bottom; the title takes 1 row inside the
// border so the body starts at y == 2.
func paneInnerY(y, paneH int) int {
	const borderTop = 1
	const titleRow = 1
	innerY := y - borderTop - titleRow
	if innerY < 0 {
		return -1
	}
	bodyH := paneH - 2 - 1
	if innerY >= bodyH {
		return -1
	}
	return innerY
}

// outlineRowAt resolves a body-row index to the BlockID at that row. We
// don't have the same scroll-offset state RenderOutline computes from body
// height, so we treat the click position as an index into the unscrolled
// row list — accurate when the outline fits without scrolling and a
// reasonable approximation otherwise. Refining this would mean threading
// the body height through the click handler.
func outlineRowAt(doc *parser.Document, side *persist.Sidecar, filter Filter, row int) string {
	if row < 0 {
		return ""
	}
	rows := BuildOutline(doc, side, filter)
	if row >= len(rows) {
		return ""
	}
	return rows[row].BlockID
}

// sourceLineAt maps a click in the source pane body to (blockID, lineOffset).
// The mapping mirrors RenderSource: the rendered window starts at
// block.StartLine - topCtx and extends for at most bodyH rows, so a click at
// inner-y N corresponds to the (startLine + N)-th source line. Soft-wrap
// makes this approximate; we accept the imprecision rather than re-deriving
// the exact wrap layout.
func sourceLineAt(doc *parser.Document, cursor string, termW, termH int, layout LayoutMode, row int) (string, int) {
	if row < 0 || doc == nil || cursor == "" {
		return "", 0
	}
	b := doc.ByID[cursor]
	if b == nil || b.StartLine == 0 {
		return "", 0
	}
	bodyH := sourcePaneInnerH(termH, layout)
	if bodyH <= 0 {
		return "", 0
	}
	blockH := b.EndLine - b.StartLine + 1
	if blockH < 1 {
		blockH = 1
	}
	ctxBudget := bodyH - blockH
	if ctxBudget < 0 {
		ctxBudget = 0
	}
	topCtx := ctxBudget / 2
	startLine := b.StartLine - topCtx
	if startLine < 1 {
		startLine = 1
	}
	absLine := startLine + row
	if absLine < 1 {
		return "", 0
	}
	// Find the block containing absLine. If it's the cursor block, return a
	// LineOffset; if it's a different block, the caller jumps to that block.
	if absLine >= b.StartLine && absLine <= b.EndLine {
		return cursor, absLine - b.StartLine + 1
	}
	if other := blockContainingLine(doc, absLine); other != nil {
		return other.ID, absLine - other.StartLine + 1
	}
	return cursor, 0
}

// sourcePaneInnerH replicates the inner body height computation that view.go
// applies for the source pane (border top/bottom + title row).
func sourcePaneInnerH(termH int, layout LayoutMode) int {
	paneH := termH - statusBarHeight
	if paneH < 1 {
		paneH = 1
	}
	if layout == LayoutStacked {
		paneH, _ = stackedHeights(paneH)
		_ = paneH
		topH, _ := stackedHeights(termH - statusBarHeight)
		paneH = topH
	}
	bodyH := paneH - 2 - 1
	if bodyH < 1 {
		bodyH = 1
	}
	return bodyH
}

// blockContainingLine returns the most-specific block whose [StartLine, EndLine]
// range contains the given absolute line. Inner blocks (proof steps) win over
// their containing env so a click on a step line lands on the step.
func blockContainingLine(doc *parser.Document, line int) *parser.Block {
	if doc == nil {
		return nil
	}
	var best *parser.Block
	for _, b := range doc.Blocks {
		if b == nil || b == doc.Root {
			continue
		}
		if b.StartLine == 0 || b.EndLine == 0 {
			continue
		}
		if line < b.StartLine || line > b.EndLine {
			continue
		}
		if best == nil {
			best = b
			continue
		}
		// Prefer the tighter range (smaller span).
		if (b.EndLine - b.StartLine) < (best.EndLine - best.StartLine) {
			best = b
		}
	}
	return best
}
