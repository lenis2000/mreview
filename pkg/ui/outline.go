package ui

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/mattn/go-runewidth"

	"mreview/pkg/format"
	"mreview/pkg/parser"
	"mreview/pkg/persist"
)

// Outline icons — one per Kind. Kinds with no visual marker fall back to a
// space so rows stay vertically aligned.
const (
	IconSection      = "§"
	IconTheorem      = "⊞"
	IconProof        = "⊢"
	IconProofStep    = "·"
	IconFigure       = "▤"
	IconDisplay      = "≡"
	IconParagraph    = " "
	IconBibliography = "⎙"
	IconOther        = " "
	IconAbstract     = "✶"
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
func BuildOutline(doc *parser.Document, side *persist.Sidecar, filter Filter, extIssues ...map[string][]format.ReportDiag) []OutlineRow {
	if doc == nil || doc.Root == nil {
		return nil
	}
	var ext map[string][]format.ReportDiag
	if len(extIssues) > 0 {
		ext = extIssues[0]
	}
	syncAvailable := anyBlockHasRegion(doc)
	rows := make([]OutlineRow, 0, len(doc.Blocks))
	var walk func(id string, depth int)
	walk = func(id string, depth int) {
		b := doc.ByID[id]
		if b == nil {
			return
		}
		if !outlineSuppressed(b, doc, side, ext) && blockMatchesFilter(b, side, filter, ext) {
			rows = append(rows, OutlineRow{
				BlockID: b.ID,
				Depth:   depth,
				Icon:    iconFor(b.Kind),
				Title:   titleFor(b),
				Markers: markersFor(b, side, syncAvailable, ext),
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

// noisyInnerEnvs lists environments that are structural building blocks
// of math / figure constructs and should not appear as standalone outline
// rows. They live inside a parent block whose label (Theorem 3.1, Figure
// 2, etc.) is the navigable target; surfacing the inner env name as well
// just adds duplicate rows that read "split", "tikzpicture", "scope".
var noisyInnerEnvs = map[string]bool{
	"split":          true,
	"aligned":        true,
	"alignedat":      true,
	"gathered":       true,
	"cases":          true,
	"dcases":         true,
	"rcases":         true,
	"matrix":         true,
	"pmatrix":        true,
	"bmatrix":        true,
	"vmatrix":        true,
	"smallmatrix":    true,
	"subarray":       true,
	"array":          true,
	"tikzpicture":    true,
	"scope":          true,
	"tikzcd":         true,
	"pgfpicture":     true,
	"axis":           true,
	"semilogxaxis":   true,
	"semilogyaxis":   true,
	"loglogaxis":     true,
}

// outlineSuppressed reports whether b should be omitted from the outline
// even though its children may still be walked. Two cases:
//
//   - A KindParagraph carrying KindParagraph children is the holder of
//     a sentence-split set produced by segmentLongParagraphs. The first
//     child duplicates the parent's first-snippet title, so emitting both
//     is duplicate-looking noise; emit only the children.
//   - A KindOther block whose env name is a known inner-math / inner-
//     figure construct (split, tikzpicture, scope, …) AND whose ancestor
//     chain includes a labeled parent (Theorem, Figure, Display, Proof)
//     or another noisy inner env. A standalone tikzpicture at document
//     level — its own primary navigable unit — is NOT suppressed.
//
// Either rule is overridden if the block carries user-visible state
// (annotation, reviewed mark, unresolved ref, external diagnostic).
// Suppressing a stateful block would silently hide its marker — the
// per-block filters and markersFor are the only places that state is
// surfaced, so dropping the row drops the user's work from the outline
// even under FilterAll or FilterAnnotated.
func outlineSuppressed(b *parser.Block, doc *parser.Document, side *persist.Sidecar, ext map[string][]format.ReportDiag) bool {
	if b == nil || doc == nil {
		return false
	}
	if blockHasOutlineState(b, side, ext) {
		return false
	}
	if b.Kind == parser.KindParagraph && len(b.ChildIDs) > 0 {
		allParagraph := true
		for _, cid := range b.ChildIDs {
			c := doc.ByID[cid]
			if c == nil || c.Kind != parser.KindParagraph {
				allParagraph = false
				break
			}
		}
		if allParagraph {
			return true
		}
	}
	if b.Kind == parser.KindOther && noisyInnerEnvs[b.EnvName] && hasNoisyInnerAncestor(b, doc) {
		return true
	}
	return false
}

// blockHasOutlineState reports whether b carries any state the outline
// is uniquely positioned to surface. Used to override structural
// suppression so a sentence-split parent or an inner-env block that the
// user has annotated stays visible.
func blockHasOutlineState(b *parser.Block, side *persist.Sidecar, ext map[string][]format.ReportDiag) bool {
	if hasAnnotation(side, b.ID) {
		return true
	}
	if isReviewed(side, b.ID) {
		return true
	}
	if blockHasUnresolved(b) {
		return true
	}
	if ext != nil && blockHasExternalIssue(ext, b.ID) {
		return true
	}
	return false
}

// hasNoisyInnerAncestor walks b's parent chain looking for a labeled
// container whose own outline row makes b's redundant: a Figure /
// Display / TheoremLike / Proof, or another noisy inner env (so a
// `scope` inside a `tikzpicture` inside a `figure` is suppressed too).
// A b with no such ancestor — e.g. a top-level tikzpicture — is the
// primary navigable unit at its location and stays visible.
func hasNoisyInnerAncestor(b *parser.Block, doc *parser.Document) bool {
	pid := b.ParentID
	for pid != "" && pid != "root" {
		p := doc.ByID[pid]
		if p == nil {
			return false
		}
		switch p.Kind {
		case parser.KindFigure, parser.KindDisplay, parser.KindTheoremLike, parser.KindProof:
			return true
		case parser.KindOther:
			if noisyInnerEnvs[p.EnvName] {
				return true
			}
		}
		pid = p.ParentID
	}
	return false
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
		return IconBibliography
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

// firstSnippet returns at most maxRunes runes from the first prose-bearing
// line of s, collapsing internal whitespace to single spaces. Leading
// whitespace, blank lines, and lines consisting entirely of LaTeX
// formatting commands (\medskip, \par, \vspace{…}, …) are skipped so an
// outline row never reads "\medskip" instead of the actual content that
// follows it.
func firstSnippet(s string, maxRunes int) string {
	for _, line := range strings.Split(s, "\n") {
		t := strings.TrimSpace(line)
		if t == "" {
			continue
		}
		if isFormattingOnlyLine(t) {
			continue
		}
		t = collapseSpaces(t)
		if runewidth.StringWidth(t) <= maxRunes {
			return t
		}
		return truncateToWidth(t, maxRunes)
	}
	return ""
}

// formattingOnlyRe matches a line whose entire content is one or more
// LaTeX layout/spacing commands chained together (with any whitespace
// between). Anything else — even a formatting command followed by prose
// on the same line — does NOT match, so we don't strip a line that
// genuinely contains content the user might recognise.
var formattingOnlyRe = regexp.MustCompile(
	`^\s*(?:` +
		`\\(?:medskip|smallskip|bigskip|par|noindent|indent|hfill|vfill|hrulefill|newpage|clearpage|pagebreak|linebreak|centering|raggedright|raggedleft|maketitle)\b\s*\*?` +
		`|\\(?:vspace|hspace|vskip|hskip|smash|phantom|hphantom|vphantom)\*?\s*\{[^{}]*\}` +
		`)+\s*$`,
)

func isFormattingOnlyLine(s string) bool {
	return formattingOnlyRe.MatchString(s)
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
func markersFor(b *parser.Block, side *persist.Sidecar, syncAvailable bool, ext ...map[string][]format.ReportDiag) string {
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
	if len(ext) > 0 {
		parts = append(parts, externalMarkersFor(ext[0], b.ID)...)
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

// scrollAnchorIndex returns the row index that should anchor the
// outline's scroll position. When the cursor block has its own row,
// that's the answer. When the cursor block is filtered out (e.g. it
// was reviewed and the filter is "unreviewed") or structurally
// suppressed (sentence-split parent, nested noisy env), we fall back
// to the visible row whose block most closely precedes the cursor's
// source line. Without this fallback the outline would snap to row 0
// every time navigation lands on a hidden block, decoupling it from
// the source pane the user is reading. Returns -1 when no row applies.
func scrollAnchorIndex(rows []OutlineRow, doc *parser.Document, cursorID string) int {
	if cur := cursorOutlineIndex(rows, cursorID); cur >= 0 {
		return cur
	}
	if doc == nil || cursorID == "" {
		return -1
	}
	cursor := doc.ByID[cursorID]
	if cursor == nil {
		return -1
	}
	best := -1
	for i, r := range rows {
		b := doc.ByID[r.BlockID]
		if b == nil {
			continue
		}
		if b.StartLine <= cursor.StartLine {
			best = i
			continue
		}
		break
	}
	return best
}

// RenderOutline produces the bordered-pane body for the outline. The width
// argument is the inner width of the pane (without border). The cursor row
// is highlighted via styles.OutlineCursor.
//
// doc is consulted only to resolve the scroll anchor when the cursor's
// own row isn't visible (filtered out, or structurally suppressed):
// without it, navigating to a hidden block would snap the outline to
// the top of the document. Pass nil to disable the fallback.
func RenderOutline(rows []OutlineRow, doc *parser.Document, cursor string, width, height int, focused bool, styles Styles) string {
	if height < 1 {
		height = 1
	}
	if width < 1 {
		width = 1
	}
	if len(rows) == 0 {
		return styles.OutlineMuted.Render("(no blocks)")
	}
	// Cursor row index for the highlight; scroll anchor for the
	// viewport. They diverge when the cursor block is filtered out —
	// the row vanishes but we still want the outline near it.
	cur := cursorOutlineIndex(rows, cursor)
	anchor := scrollAnchorIndex(rows, doc, cursor)
	offset := 0
	if anchor >= 0 && anchor >= height {
		offset = anchor - height + 1
	}
	end := offset + height
	if end > len(rows) {
		end = len(rows)
	}
	// Relative-number gutter reference: prefer the actual cursor row when
	// it's visible (so "0" really means cursor); otherwise fall back to
	// the scroll anchor so the user still sees a coherent count from the
	// row mreview is keeping in view.
	ref := cur
	if ref < 0 {
		ref = anchor
	}
	var b strings.Builder
	for i := offset; i < end; i++ {
		r := rows[i]
		gutter := relativeGutter(i, ref)
		line := formatOutlineLine(r, gutter, width, styles)
		if i == cur {
			style := styles.OutlineCursor
			if !focused {
				style = styles.OutlineActive
			}
			// Strip inner ANSI so the highlight bg paints the whole row —
			// otherwise the per-token bg (OutlineIcon/Marker bg=vimBg)
			// punches black holes through the cursor row and the selection
			// only shows up on the unstyled text between them.
			line = style.Width(width).Render(stripANSI(stripRight(line)))
		}
		b.WriteString(line)
		if i < end-1 {
			b.WriteByte('\n')
		}
	}
	return b.String()
}

// relativeGutter formats the row-distance prefix for an outline row.
// Cursor row reads " 0", others show absolute distance from the cursor.
// Fixed at 3 cells (2-digit number + trailing space) so titles align
// regardless of the count. Distances ≥ 100 are clamped to "**" rather
// than widening the gutter, since with 100+ rows in either direction
// the user will paginate via {/} or G rather than count single steps.
func relativeGutter(rowIdx, ref int) string {
	if ref < 0 {
		return "   "
	}
	d := rowIdx - ref
	if d < 0 {
		d = -d
	}
	if d > 99 {
		return "** "
	}
	return fmt.Sprintf("%2d ", d)
}

// formatOutlineLine composes a single row: gutter + indent + icon +
// space + title, padded on the left with the marker cluster right-
// aligned into `width`. Width is the inner pane width; gutter is the
// fixed-width relative-distance prefix.
func formatOutlineLine(r OutlineRow, gutter string, width int, styles Styles) string {
	gutterW := runewidth.StringWidth(gutter)
	gutterStyled := styles.OutlineMuted.Render(gutter)
	indent := strings.Repeat("  ", r.Depth)
	iconStyled := styles.OutlineIcon.Render(r.Icon)
	// Compute the budget for the title: width - gutter - indent - icon(1)
	// - space(1) - markers(width-of-markers) - trailing space.
	markers := r.Markers
	markerW := runewidth.StringWidth(markers)
	iconW := runewidth.StringWidth(r.Icon)
	leadW := gutterW + runewidth.StringWidth(indent) + iconW + 1 // +1 for the space after icon
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
	line := gutterStyled + indent + iconStyled + " " + title
	if markers != "" {
		pad := width - gutterW - runewidth.StringWidth(indent) - iconW - 1 - runewidth.StringWidth(title) - markerW
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
