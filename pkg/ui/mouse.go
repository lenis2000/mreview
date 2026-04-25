package ui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"mreview/pkg/format"
	"mreview/pkg/parser"
	"mreview/pkg/persist"
)

// handleMouse turns mouse events into focus, cursor moves, and source
// scrolling. Click (left press): set focus, and on outline/source jump
// the relevant cursor to the clicked row. Wheel up/down on the source
// pane: shift the source line cursor; crossing the block boundary
// advances to the previous/next block so wheel scroll feels continuous.
// Wheel up/down on the outline pane: move the block cursor.
func (m Model) handleMouse(msg tea.MouseMsg) Model {
	if msg.Action != tea.MouseActionPress {
		return m
	}
	if m.Width <= 0 || m.Height <= 0 {
		return m
	}
	pane, innerY, ok := paneAtPoint(m.Width, m.Height, m.Layout, msg.X, msg.Y)
	if !ok {
		return m
	}
	switch msg.Button {
	case tea.MouseButtonLeft:
		m.Focus = pane
		switch pane {
		case PaneOutline:
			bodyH := outlinePaneInnerH(m.Height, m.Layout)
			if id := outlineRowAt(m.Doc, m.Sidecar, m.Filter, m.ExternalIssues, m.CursorBlockID, bodyH, innerY); id != "" {
				if id != m.CursorBlockID {
					m.CursorBlockID = id
					m.SourceLineCursor = 1
				}
			}
		case PaneSource:
			blockID, lineOff := sourceLineAt(m.Doc, m.CursorBlockID, m.Width, m.Height, m.Layout, m.SoftWrap, innerY)
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
	case tea.MouseButtonWheelUp:
		switch pane {
		case PaneSource:
			m = m.scrollSource(-1)
		case PaneOutline:
			id := PrevSibling(m.Doc, m.Sidecar, m.Filter, m.CursorBlockID, 1, m.ExternalIssues)
			if id != "" {
				m.CursorBlockID = id
				m.SourceLineCursor = 1
			}
		}
	case tea.MouseButtonWheelDown:
		switch pane {
		case PaneSource:
			m = m.scrollSource(+1)
		case PaneOutline:
			id := NextSibling(m.Doc, m.Sidecar, m.Filter, m.CursorBlockID, 1, m.ExternalIssues)
			if id != "" {
				m.CursorBlockID = id
				m.SourceLineCursor = 1
			}
		}
	}
	return m
}

// scrollSource shifts the source line cursor by delta *absolute* source
// lines. Walks the raw line numbers so no source line is ever skipped,
// but re-homes CursorBlockID only when the new line lands inside a leaf
// block (paragraph, figure, display, proof, …). Promoting to an
// ancestor section on gap lines would make the outline cursor jump up
// to the section heading, which feels like a huge vertical skip even
// though the source pane scrolled by one line.
func (m Model) scrollSource(delta int) Model {
	if m.Doc == nil || m.CursorBlockID == "" {
		return m
	}
	b := m.Doc.ByID[m.CursorBlockID]
	if b == nil || b.StartLine == 0 {
		return m
	}
	curAbs := b.StartLine + m.SourceLineCursor - 1
	newAbs := curAbs + delta
	if newAbs < 1 {
		newAbs = 1
	}
	total := sourceLineTotal(m.Doc)
	if total > 0 && newAbs > total {
		newAbs = total
	}
	// Prefer the tightest leaf block covering newAbs — a neighbouring
	// paragraph, figure, theorem, or display. Snapping to a leaf sibling
	// is what the user expects in the common case (scrolling out of one
	// paragraph into the next).
	if leaf := leafContainingLine(m.Doc, newAbs); leaf != nil {
		m.CursorBlockID = leaf.ID
		m.SourceLineCursor = newAbs - leaf.StartLine + 1
		return m
	}
	// Gap line covered only by an ancestor (e.g. a blank between a
	// paragraph and a figure both inside a section). Snap to the
	// tightest containing block so the outline reflects where we are.
	// The line cursor remains at the correct absolute line — Update's
	// "reset to 1 on block change" is guarded by a beforeLine==afterLine
	// check, so the offset we set here survives.
	if other := blockContainingLine(m.Doc, newAbs); other != nil {
		m.CursorBlockID = other.ID
		m.SourceLineCursor = newAbs - other.StartLine + 1
		return m
	}
	// Unreachable for well-formed docs (root covers everything).
	m.SourceLineCursor = newAbs - b.StartLine + 1
	return m
}

// leafContainingLine returns the tightest block with no children whose
// [StartLine, EndLine] range contains `line`. Used by scrollSource so a
// line cursor never snaps to an ancestor section block — that change
// would shove the outline cursor way up to the section heading.
func leafContainingLine(doc *parser.Document, line int) *parser.Block {
	if doc == nil {
		return nil
	}
	var best *parser.Block
	for _, b := range doc.Blocks {
		if b == nil || b == doc.Root {
			continue
		}
		if len(b.ChildIDs) > 0 {
			continue
		}
		if b.StartLine == 0 || b.EndLine == 0 {
			continue
		}
		if line < b.StartLine || line > b.EndLine {
			continue
		}
		if best == nil || (b.EndLine-b.StartLine) < (best.EndLine-best.StartLine) {
			best = b
		}
	}
	return best
}

// sourceLineTotal returns the number of source lines in the document, or 0
// when the source is empty. Matches the line count the renderer sees after
// strings.Split on "\n".
func sourceLineTotal(doc *parser.Document) int {
	if doc == nil {
		return 0
	}
	src := string(doc.Source)
	if src == "" {
		return 0
	}
	n := strings.Count(src, "\n") + 1
	if src[len(src)-1] == '\n' {
		n--
	}
	if n < 1 {
		return 1
	}
	return n
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

// outlineRowAt resolves a body-row index to the BlockID at that row,
// accounting for the same scroll offset RenderOutline applies when the
// cursor would otherwise fall off the bottom of the visible window.
// Clicks on a scrolled-out-of-view row therefore land on the right
// block instead of the unscrolled first row.
func outlineRowAt(doc *parser.Document, side *persist.Sidecar, filter Filter, ext map[string][]format.ReportDiag, cursor string, bodyH, row int) string {
	if row < 0 {
		return ""
	}
	rows := BuildOutline(doc, side, filter, ext)
	offset := outlineScrollOffset(rows, cursor, bodyH)
	idx := offset + row
	if idx < 0 || idx >= len(rows) {
		return ""
	}
	return rows[idx].BlockID
}

// outlineScrollOffset mirrors the scroll-to-keep-cursor-visible math in
// RenderOutline so click-hit-testing and render see the same first row.
func outlineScrollOffset(rows []OutlineRow, cursor string, bodyH int) int {
	if bodyH < 1 {
		return 0
	}
	cur := cursorOutlineIndex(rows, cursor)
	if cur >= 0 && cur >= bodyH {
		return cur - bodyH + 1
	}
	return 0
}

// outlinePaneInnerH replicates the inner body height computation that
// view.go applies for the outline pane (border top/bottom + title row).
func outlinePaneInnerH(termH int, layout LayoutMode) int {
	_ = layout // outline spans the full height in both layouts
	paneH := termH - statusBarHeight
	if paneH < 1 {
		paneH = 1
	}
	bodyH := paneH - 2 - 1
	if bodyH < 1 {
		bodyH = 1
	}
	return bodyH
}

// sourceLineAt maps a click in the source pane body to (blockID, lineOffset).
// The mapping mirrors renderSourceWithEditor: the renderer walks source
// lines from `startLine`, expands each through wrapOrClip, and emits one
// or more rendered rows per source line. To produce the same row→line
// inverse, we replay the per-line wrap row count and stop when the
// click row falls inside the current line's row span.
//
// Inline annotation/editor rows are not yet accounted for — they
// generally appear next to the cursor block and account for at most a
// handful of rows; the soft-wrap mismatch was the dominant source of
// mis-clicks. If a click lands on an annotation row the resolved line
// will be the next source line below, which is acceptable.
func sourceLineAt(doc *parser.Document, cursor string, termW, termH int, layout LayoutMode, softWrap bool, row int) (string, int) {
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
	innerW := sourcePaneInnerW(termW, layout)
	if innerW <= 0 {
		return "", 0
	}
	src := strings.Split(string(doc.Source), "\n")
	total := len(src)
	if total == 0 {
		return "", 0
	}

	// Mirror renderSourceWithEditor's window: render up to `bodyH` rows
	// on each side of the cursor block so the post-render scroll has
	// candidates around the centred block.
	startLine := b.StartLine - bodyH
	if startLine < 1 {
		startLine = 1
	}
	endLine := b.EndLine + bodyH
	if endLine > total {
		endLine = total
	}

	gutterW := lineNumWidth(endLine)
	bodyWidth := innerW - gutterW - 1
	if bodyWidth < 1 {
		bodyWidth = 1
	}

	// Walk lines, accumulating wrap row counts; stop when row falls in
	// the current line's row span.
	rowsUsed := 0
	hitLine := 0
	for ln := startLine; ln <= endLine; ln++ {
		if ln-1 >= total {
			break
		}
		n := 1
		if softWrap {
			// wrapOrClip's row count doesn't depend on styles or the
			// inBlock flag — pass zero values.
			n = len(wrapOrClip(src[ln-1], bodyWidth, true, true, Styles{}))
			if n < 1 {
				n = 1
			}
		}
		if row < rowsUsed+n {
			hitLine = ln
			break
		}
		rowsUsed += n
	}
	if hitLine == 0 {
		return "", 0
	}

	if hitLine >= b.StartLine && hitLine <= b.EndLine {
		return cursor, hitLine - b.StartLine + 1
	}
	if other := blockContainingLine(doc, hitLine); other != nil {
		return other.ID, hitLine - other.StartLine + 1
	}
	return cursor, 0
}

// sourcePaneInnerW mirrors view.go's source pane width math: the source
// column width less 2 cells of border. Used by sourceLineAt to compute
// the same body width the renderer wraps against.
func sourcePaneInnerW(termW int, layout LayoutMode) int {
	if termW <= 0 {
		return 0
	}
	var paneW int
	switch layout {
	case LayoutStacked:
		_, paneW = stackedWidths(termW)
	default:
		_, paneW, _ = paneWidths(termW)
	}
	innerW := paneW - 2
	if innerW < 1 {
		innerW = 1
	}
	return innerW
}

// sourcePaneInnerH replicates the inner body height computation that view.go
// applies for the source pane (border top/bottom + title row).
func sourcePaneInnerH(termH int, layout LayoutMode) int {
	paneH := termH - statusBarHeight
	if paneH < 1 {
		paneH = 1
	}
	if layout == LayoutStacked {
		paneH, _ = stackedHeights(termH - statusBarHeight)
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
