package ui

import (
	"fmt"
	"strings"

	"github.com/mattn/go-runewidth"

	"mreview/pkg/parser"
	"mreview/pkg/persist"
)

// Outline icons — one per Kind. Kinds with no visual marker fall back to a
// space so rows stay vertically aligned.
const (
	IconSection    = "§"
	IconTheorem    = "⊞"
	IconProof      = "⊢"
	IconProofStep  = "·"
	IconFigure     = "▤"
	IconDisplay    = "≡"
	IconParagraph  = " "
	IconBibliograph = "⎙"
	IconOther      = " "
	IconAbstract   = "✶"
)

// Outline markers — suffixed to the row to surface per-block state.
const (
	MarkerAnnotated  = "●"
	MarkerReviewed   = "✓"
	MarkerUnresolved = "⚠"
	MarkerNoRegion   = "⊘"
)

// OutlineRow is one visible line in the outline pane.
type OutlineRow struct {
	BlockID string
	Depth   int
	Icon    string
	Title   string
	Markers string
}

// BuildOutline walks the document tree in pre-order and returns the rows
// that match the filter. The synthetic root is never emitted. Depth counts
// only *visible* ancestors are not considered — depth is the structural
// depth in the parse tree, so that a filtered-in child keeps its indent
// even when its parent is filtered out.
//
// The ⊘ per-block "no SyncTeX region" marker is suppressed when *no*
// block in the document has a region — in that case SyncTeX is
// unavailable session-wide (e.g. no .synctex.gz), and decorating every
// non-section row would be noise rather than signal.
func BuildOutline(doc *parser.Document, side *persist.Sidecar, filter Filter) []OutlineRow {
	if doc == nil || doc.Root == nil {
		return nil
	}
	syncAvailable := anyBlockHasRegion(doc)
	rows := make([]OutlineRow, 0, len(doc.Blocks))
	var walk func(id string, depth int)
	walk = func(id string, depth int) {
		b := doc.ByID[id]
		if b == nil {
			return
		}
		if blockMatchesFilter(b, side, filter) {
			rows = append(rows, OutlineRow{
				BlockID: b.ID,
				Depth:   depth,
				Icon:    iconFor(b.Kind),
				Title:   titleFor(b),
				Markers: markersFor(b, side, syncAvailable),
			})
		}
		for _, c := range b.ChildIDs {
			walk(c, depth+1)
		}
	}
	for _, id := range doc.Root.ChildIDs {
		walk(id, 0)
	}
	return rows
}

// anyBlockHasRegion reports whether at least one non-root block has a
// SyncTeX-populated PDFRegion. Used as a cheap session-level proxy for
// "SyncTeX is loaded"; if no regions exist, we don't flag individual
// blocks as missing regions.
func anyBlockHasRegion(doc *parser.Document) bool {
	for _, b := range doc.Blocks {
		if b == nil || b == doc.Root {
			continue
		}
		if b.PDFRegion != nil {
			return true
		}
	}
	return false
}

// iconFor picks the outline icon for a Kind.
func iconFor(k parser.Kind) string {
	switch k {
	case parser.KindSection:
		return IconSection
	case parser.KindAbstract:
		return IconAbstract
	case parser.KindTheoremLike:
		return IconTheorem
	case parser.KindProof:
		return IconProof
	case parser.KindProofStep:
		return IconProofStep
	case parser.KindFigure:
		return IconFigure
	case parser.KindDisplay:
		return IconDisplay
	case parser.KindParagraph:
		return IconParagraph
	case parser.KindBibliography:
		return IconBibliograph
	}
	return IconOther
}

// titleFor builds a human-readable label for a block.
func titleFor(b *parser.Block) string {
	if b == nil {
		return ""
	}
	switch b.Kind {
	case parser.KindSection:
		if b.Title != "" {
			return b.Title
		}
		return "Section"
	case parser.KindAbstract:
		return "Abstract"
	case parser.KindTheoremLike:
		head := titleCase(b.EnvName)
		if head == "" {
			head = "Theorem"
		}
		if b.Number != "" {
			head += " " + b.Number
		}
		if b.Title != "" {
			head += ": " + b.Title
		}
		return head
	case parser.KindProof:
		if b.Title != "" {
			return "Proof: " + b.Title
		}
		return "Proof"
	case parser.KindProofStep:
		return "step " + firstSnippet(b.Source, 40)
	case parser.KindFigure:
		head := "Figure"
		if b.Number != "" {
			head += " " + b.Number
		}
		if b.Title != "" {
			head += ": " + b.Title
		}
		return head
	case parser.KindDisplay:
		return firstSnippet(b.Source, 48)
	case parser.KindParagraph:
		return firstSnippet(b.Source, 48)
	case parser.KindBibliography:
		return "Bibliography"
	}
	if b.EnvName != "" {
		return b.EnvName
	}
	return firstSnippet(b.Source, 40)
}

// titleCase upper-cases the first rune of s, leaving the rest intact.
// Used for converting env names like `theorem` to `Theorem` in outline rows.
func titleCase(s string) string {
	if s == "" {
		return s
	}
	r := []rune(s)
	if r[0] >= 'a' && r[0] <= 'z' {
		r[0] -= 'a' - 'A'
	}
	return string(r)
}

// firstSnippet returns at most maxRunes runes from the first non-empty line
// of s, collapsing internal whitespace to single spaces.
func firstSnippet(s string, maxRunes int) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	s = strings.TrimSpace(s)
	s = collapseSpaces(s)
	if runewidth.StringWidth(s) <= maxRunes {
		return s
	}
	return truncateToWidth(s, maxRunes)
}

func collapseSpaces(s string) string {
	var b strings.Builder
	prevSpace := false
	for _, r := range s {
		if r == ' ' || r == '\t' {
			if !prevSpace {
				b.WriteRune(' ')
			}
			prevSpace = true
			continue
		}
		b.WriteRune(r)
		prevSpace = false
	}
	return b.String()
}

// truncateToWidth clips s to at most width display cells, appending an
// ellipsis when truncation happened. Assumes width >= 1.
func truncateToWidth(s string, width int) string {
	if width <= 0 {
		return ""
	}
	if runewidth.StringWidth(s) <= width {
		return s
	}
	if width == 1 {
		return "…"
	}
	runes := []rune(s)
	w := 0
	end := 0
	for i, r := range runes {
		rw := runewidth.RuneWidth(r)
		if w+rw > width-1 {
			break
		}
		w += rw
		end = i + 1
	}
	return string(runes[:end]) + "…"
}

// markersFor builds the space-separated marker string for a block.
// syncAvailable gates the ⊘ marker: when SyncTeX is entirely absent the
// marker is noise (every non-section block would carry it), so we only
// emit it when SyncTeX *did* load but this particular block couldn't be
// located.
func markersFor(b *parser.Block, side *persist.Sidecar, syncAvailable bool) string {
	var parts []string
	if hasAnnotation(side, b.ID) {
		parts = append(parts, MarkerAnnotated)
	}
	if isReviewed(side, b.ID) {
		parts = append(parts, MarkerReviewed)
	}
	if blockHasUnresolved(b) {
		parts = append(parts, MarkerUnresolved)
	}
	if syncAvailable && b.PDFRegion == nil && b.Kind != parser.KindSection && b.ID != "root" {
		parts = append(parts, MarkerNoRegion)
	}
	return strings.Join(parts, "")
}

func blockHasUnresolved(b *parser.Block) bool {
	for _, r := range b.RefsOut {
		if !r.Resolved {
			return true
		}
	}
	return false
}

// cursorOutlineIndex returns the row index of the cursor block in rows, or
// -1 when the cursor is not currently visible.
func cursorOutlineIndex(rows []OutlineRow, cursor string) int {
	for i, r := range rows {
		if r.BlockID == cursor {
			return i
		}
	}
	return -1
}

// RenderOutline produces the bordered-pane body for the outline. The width
// argument is the inner width of the pane (without border). The cursor row
// is highlighted via styles.OutlineCursor.
func RenderOutline(rows []OutlineRow, cursor string, width, height int, focused bool, styles Styles) string {
	if height < 1 {
		height = 1
	}
	if width < 1 {
		width = 1
	}
	if len(rows) == 0 {
		return styles.OutlineMuted.Render("(no blocks)")
	}
	// Scroll so the cursor is on-screen; keep a 2-row margin from the top/
	// bottom when possible.
	cur := cursorOutlineIndex(rows, cursor)
	offset := 0
	if cur >= 0 && cur >= height {
		offset = cur - height + 1
	}
	end := offset + height
	if end > len(rows) {
		end = len(rows)
	}
	var b strings.Builder
	for i := offset; i < end; i++ {
		r := rows[i]
		line := formatOutlineLine(r, width, styles)
		if i == cur {
			style := styles.OutlineCursor
			if !focused {
				style = styles.OutlineActive
			}
			line = style.Width(width).Render(stripRight(line))
		}
		b.WriteString(line)
		if i < end-1 {
			b.WriteByte('\n')
		}
	}
	return b.String()
}

// formatOutlineLine composes a single row: indent + icon + space + title,
// padded on the left with the marker cluster right-aligned into `width`.
// Width is the inner pane width.
func formatOutlineLine(r OutlineRow, width int, styles Styles) string {
	indent := strings.Repeat("  ", r.Depth)
	iconStyled := styles.OutlineIcon.Render(r.Icon)
	// Compute the budget for the title: width - indent - icon(1) - space(1)
	// - markers(width-of-markers) - trailing space.
	markers := r.Markers
	markerW := runewidth.StringWidth(markers)
	iconW := runewidth.StringWidth(r.Icon)
	leadW := runewidth.StringWidth(indent) + iconW + 1 // +1 for the space after icon
	trailW := markerW
	if markerW > 0 {
		trailW++ // the separating space
	}
	titleBudget := width - leadW - trailW
	if titleBudget < 1 {
		titleBudget = 1
	}
	title := r.Title
	if runewidth.StringWidth(title) > titleBudget {
		title = truncateToWidth(title, titleBudget)
	}
	line := indent + iconStyled + " " + title
	if markers != "" {
		pad := width - runewidth.StringWidth(indent) - iconW - 1 - runewidth.StringWidth(title) - markerW
		if pad < 1 {
			pad = 1
		}
		line += strings.Repeat(" ", pad) + styles.OutlineMarker.Render(markers)
	}
	return line
}

// stripRight trims trailing spaces but preserves styling-produced escape
// sequences. Lipgloss renders foreground-coloured runes that we do not want
// to chop, so the naive rune scan is safe only because the markers and icon
// strings contain no literal trailing spaces.
func stripRight(s string) string {
	return strings.TrimRight(s, " ")
}

// BreadcrumbFor walks up a block's ancestor chain and joins the parent
// titles with " > ". Returns an empty string for the root / missing block.
func BreadcrumbFor(doc *parser.Document, id string) string {
	if doc == nil {
		return ""
	}
	b := doc.ByID[id]
	if b == nil {
		return ""
	}
	var parts []string
	cur := b
	for cur != nil && cur.ID != "root" && cur.ID != "" {
		parts = append([]string{shortTitle(cur)}, parts...)
		if cur.ParentID == "" {
			break
		}
		cur = doc.ByID[cur.ParentID]
	}
	return strings.Join(parts, " > ")
}

// shortTitle is like titleFor but drops quoted bodies for leaf blocks so the
// status-line breadcrumb stays compact.
func shortTitle(b *parser.Block) string {
	switch b.Kind {
	case parser.KindParagraph, parser.KindDisplay, parser.KindProofStep:
		return b.Kind.String()
	}
	return titleFor(b)
}

// LocatorFor returns the "file:Lstart-Lend" suffix for a block, or an empty
// string for the root / missing block.
func LocatorFor(doc *parser.Document, id string) string {
	if doc == nil {
		return ""
	}
	b := doc.ByID[id]
	if b == nil {
		return ""
	}
	file := b.File
	if file == "" {
		file = doc.File
	}
	if file == "" {
		file = "-"
	}
	return fmt.Sprintf("%s:L%d-L%d", file, b.StartLine, b.EndLine)
}

