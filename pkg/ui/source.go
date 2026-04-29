package ui

import (
	"strings"

	"github.com/mattn/go-runewidth"

	"mreview/pkg/parser"
	"mreview/pkg/persist"
)

// RenderSource returns the rendered body of the source pane for the current
// cursor block. See renderSourceWithEditor — this is the no-editor path.
func RenderSource(doc *parser.Document, cursor string, width, height int, styles Styles, softWrap bool, lineCursor int, annotations []persist.Annotation) string {
	return renderSourceWithEditor(doc, cursor, width, height, styles, softWrap, lineCursor, annotations, nil)
}

// renderSourceWithEditor is RenderSource plus an optional inline annotation
// editor. When editor != nil, its textarea is spliced into the source flow
// at the editor's anchor line (just below for line-pinned, just above the
// block's first line for block-level). The saved annotation that the editor
// is replacing — keyed on (BlockID, LineOffset) — is suppressed so the user
// sees one live editor rather than the editor *and* its old version side
// by side.
func renderSourceWithEditor(doc *parser.Document, cursor string, width, height int, styles Styles, softWrap bool, lineCursor int, annotations []persist.Annotation, editor *AnnotationPopup) string {
	if doc == nil || cursor == "" {
		return styles.OutlineMuted.Render("(no block selected)")
	}
	b := doc.ByID[cursor]
	if b == nil {
		return styles.OutlineMuted.Render("(unknown block)")
	}
	if b.StartLine == 0 || b.EndLine == 0 {
		return styles.OutlineMuted.Render("(" + b.Kind.String() + " — no source)")
	}
	if height < 1 {
		height = 1
	}
	allLines := strings.Split(string(doc.Source), "\n")
	total := len(allLines)
	if total == 0 {
		return styles.OutlineMuted.Render("(no source)")
	}

	// Render a window wider than `height` so the post-render scroll step
	// always has rows on both sides of the cursor block to choose from.
	// Worst case is one wrapped row per source line, so `height` lines on
	// each side suffices.
	startLine := b.StartLine - height
	if startLine < 1 {
		startLine = 1
	}
	endLine := b.EndLine + height
	if endLine > total {
		endLine = total
	}

	gutterW := lineNumWidth(endLine)
	bodyWidth := width - gutterW - 1
	if bodyWidth < 1 {
		bodyWidth = 1
	}

	// Absolute line for the highlighted row. lineCursor is an offset from
	// b.StartLine; values outside [1, blockLineCount] are legitimate when
	// the user has scrolled the source pane into a gap between leaf blocks
	// (scrollSource deliberately keeps CursorBlockID steady so the outline
	// cursor doesn't pop up to an ancestor section). No clamp to b.EndLine
	// — the row-walk below already includes a height-sized margin on each
	// side of the block, and a cursorLine outside [1, totalLines] simply
	// never matches any ln.
	cursorLine := b.StartLine + lineCursor - 1
	if cursorLine < 1 {
		cursorLine = 0
	}

	// Editor anchor line — where the textarea should splice in. For a
	// block-level edit, that's one row above the block's first line (same
	// as where the saved block-level note would render).
	editorAnchor := -1
	editorActive := editor != nil && editor.TargetID == b.ID
	if editorActive {
		if editor.LineOffset > 0 {
			editorAnchor = b.StartLine + editor.LineOffset - 1
			if editorAnchor > b.EndLine {
				editorAnchor = b.EndLine
			}
		} else {
			editorAnchor = b.StartLine - 1
		}
	}

	// Group every annotation that anchors anywhere in the visible window
	// (the cursor block AND the dimmed before/after context blocks). For
	// each annotation we look up its target block in the parsed doc so we
	// can resolve its anchor line; orphans (block id missing from doc) are
	// ignored. The annotation currently being edited is suppressed so the
	// live editor isn't doubled up by its previous saved text.
	notesByLine := map[int][]persist.Annotation{}
	for _, a := range annotations {
		if editorActive && a.BlockID == editor.TargetID && a.LineOffset == editor.LineOffset {
			continue
		}
		ab := doc.ByID[a.BlockID]
		if ab == nil || ab.StartLine == 0 || ab.EndLine == 0 {
			continue
		}
		var ln int
		if a.LineOffset > 0 {
			ln = ab.StartLine + a.LineOffset - 1
			if ln > ab.EndLine {
				ln = ab.EndLine
			}
		} else {
			ln = ab.StartLine - 1
		}
		// Drop annotations whose anchor falls outside the visible window —
		// they'd never render anyway and skipping them keeps the map small.
		if ln < startLine-1 || ln > endLine {
			continue
		}
		notesByLine[ln] = append(notesByLine[ln], a)
	}

	// Render every row in the candidate range into a flat slice, tracking
	// where the cursor block lives. Then we slice the slice to keep the
	// block visible and roughly centred. This is more correct than the
	// older line-budget heuristic, which mis-estimated capacity whenever
	// soft-wrap or inline annotations multiplied the rows-per-line ratio.
	var rendered []string
	blockFirstRow := -1
	blockLastRow := -1
	editorFirstRow := -1
	editorLastRow := -1
	pushNotes := func(forLine int) {
		for _, n := range notesByLine[forLine] {
			rendered = append(rendered, annotationRows(n, gutterW, bodyWidth, styles)...)
		}
	}
	pushEditor := func(forLine int) {
		if !editorActive || editorAnchor != forLine {
			return
		}
		if editorFirstRow < 0 {
			editorFirstRow = len(rendered)
		}
		rendered = append(rendered, editorRows(editor, gutterW, bodyWidth, styles)...)
		editorLastRow = len(rendered) - 1
	}

	// Block-level header annotations and a block-level editor land above
	// line 1.
	pushNotes(b.StartLine - 1)
	pushEditor(b.StartLine - 1)

	cursorFirstRow := -1
	for ln := startLine; ln <= endLine; ln++ {
		if ln-1 >= len(allLines) {
			break
		}
		if ln == b.StartLine {
			blockFirstRow = len(rendered)
		}
		raw := allLines[ln-1]
		inBlock := ln >= b.StartLine && ln <= b.EndLine
		isCursor := ln == cursorLine
		segments := wrapOrClip(raw, bodyWidth, softWrap, inBlock, styles)
		for i, seg := range segments {
			var gutter string
			if i == 0 {
				gutter = padLeft(itoa(ln), gutterW)
			} else {
				gutter = strings.Repeat(" ", gutterW)
			}
			gutterStyled := styles.SourceGutter.Render(gutter)
			rowText := gutterStyled + " " + seg
			if isCursor {
				rowText = styles.OutlineCursor.Width(width).Render(stripANSI(rowText))
				if cursorFirstRow < 0 {
					cursorFirstRow = len(rendered)
				}
			}
			rendered = append(rendered, rowText)
		}
		if ln == b.EndLine {
			blockLastRow = len(rendered) - 1
		}
		pushNotes(ln)
		pushEditor(ln)
	}

	if len(rendered) <= height {
		return strings.Join(rendered, "\n")
	}

	// Centre the line cursor when one is set; otherwise centre the cursor
	// block. The line cursor wins because user-driven j/k motion expects
	// "the highlighted row stays roughly in the middle as I scroll" — if we
	// centred on the block instead, line motion within a tall block could
	// push the highlighted row off the bottom edge.
	offset := 0
	switch {
	case cursorFirstRow >= 0:
		offset = cursorFirstRow - height/2
	case blockFirstRow >= 0:
		blockRows := blockLastRow - blockFirstRow + 1
		if blockRows >= height {
			offset = blockFirstRow
		} else {
			offset = blockFirstRow - (height-blockRows)/2
		}
	}
	// When the inline annotation editor is active, the user's focus is the
	// textarea — keep it on screen even if the cursor block is too tall to
	// fit. Block-level editors anchor above the block, so plain centring on
	// the cursor would push the editor off the top edge.
	if editorActive && editorFirstRow >= 0 {
		if editorLastRow >= offset+height {
			offset = editorLastRow - height + 1
		}
		if editorFirstRow < offset {
			offset = editorFirstRow
		}
	}
	if offset < 0 {
		offset = 0
	}
	if offset > len(rendered)-height {
		offset = len(rendered) - height
	}
	if offset < 0 {
		offset = 0
	}
	end := offset + height
	if end > len(rendered) {
		end = len(rendered)
	}
	return strings.Join(rendered[offset:end], "\n")
}

// editorRows formats the live annotation textarea as a block of inline rows
// indented under the gutter and prefixed with a sigil so it visually echoes
// the saved-annotation row format. The textarea's own multi-line View() is
// used as-is; per-line styling is applied uniformly so the editor stays
// recognisable as commentary while typing. The style mirrors the saved
// annotation: paragraph (block-level) edits get SourceAnnotationBlock,
// line-pinned edits get SourceAnnotation — so the live colour previews the
// kind of note the user is writing.
func editorRows(editor *AnnotationPopup, gutterW, bodyWidth int, styles Styles) []string {
	if editor == nil {
		return nil
	}
	const sigil = "▸ "
	indent := strings.Repeat(" ", gutterW) + " "
	noteWidth := bodyWidth - runewidth.StringWidth(sigil)
	if noteWidth < 1 {
		noteWidth = 1
	}
	if editor.TA.Width() != noteWidth {
		editor.TA.SetWidth(noteWidth)
	}
	view := editor.TA.View()
	hint := "[Ctrl-S submit · Ctrl-C cancel]"
	rows := strings.Split(view, "\n")
	out := make([]string, 0, len(rows)+1)
	style := styles.SourceAnnotation
	if editor.LineOffset == 0 {
		style = styles.SourceAnnotationBlock
	}
	for i, row := range rows {
		var prefix string
		if i == 0 {
			prefix = sigil
		} else {
			prefix = strings.Repeat(" ", runewidth.StringWidth(sigil))
		}
		out = append(out, indent+style.Render(prefix+row))
	}
	out = append(out, indent+styles.OutlineMuted.Render(hint))
	return out
}

// annotationRows formats one annotation as one or more inline display rows.
// The first row carries a `▸` sigil; continuation rows align under the note
// text. Long notes wrap on word boundaries within bodyWidth so the inline
// preview never overflows the pane. Paragraph (block-level, LineOffset 0)
// notes use SourceAnnotationBlock; line-pinned notes use SourceAnnotation —
// the two hues let the reader tell the kinds apart at a glance.
func annotationRows(a persist.Annotation, gutterW, bodyWidth int, styles Styles) []string {
	const sigil = "▸ "
	indent := strings.Repeat(" ", gutterW) + " "
	noteWidth := bodyWidth - runewidth.StringWidth(sigil)
	if noteWidth < 1 {
		noteWidth = 1
	}
	noteText := strings.TrimSpace(a.Note)
	if noteText == "" {
		noteText = "(empty)"
	}
	// Collapse internal newlines to spaces so each note is one paragraph in
	// the inline view; the full multi-line body still lives in the sidecar.
	noteText = strings.Join(strings.Fields(noteText), " ")
	wrapped := wrapWords(noteText, noteWidth)
	style := styles.SourceAnnotation
	if a.LineOffset == 0 {
		style = styles.SourceAnnotationBlock
	}
	rows := make([]string, 0, len(wrapped))
	for i, line := range wrapped {
		var prefix string
		if i == 0 {
			prefix = sigil
		} else {
			prefix = strings.Repeat(" ", runewidth.StringWidth(sigil))
		}
		rows = append(rows, indent+style.Render(prefix+line))
	}
	return rows
}

// wrapWords breaks a single-paragraph string into width-bounded rows, splitting
// on whitespace where possible. Used for inline annotation rendering so a
// long note doesn't trip the pane border.
func wrapWords(s string, width int) []string {
	if width < 1 {
		width = 1
	}
	if s == "" {
		return []string{""}
	}
	var rows []string
	rem := s
	for runewidth.StringWidth(rem) > 0 {
		take := takeCells(rem, width)
		if take == "" {
			break
		}
		rows = append(rows, take)
		rem = strings.TrimLeft(rem[len(take):], " \t")
	}
	if len(rows) == 0 {
		rows = append(rows, s)
	}
	return rows
}

// wrapOrClip turns one source line into one or more visible row strings.
// When softWrap is false the line is truncated to a single row; otherwise it
// is split into width-sized pieces along plain-text columns and each piece is
// re-colorised independently. Context lines (inBlock=false) are dimmed.
func wrapOrClip(line string, width int, softWrap, inBlock bool, styles Styles) []string {
	if width < 1 {
		width = 1
	}
	colorize := func(s string) string {
		if inBlock {
			return colorizeLaTeXLine(s, styles)
		}
		return styles.OutlineMuted.Render(s)
	}
	if !softWrap {
		return []string{clipToWidth(colorize(line), width)}
	}
	if line == "" {
		return []string{""}
	}
	var rows []string
	remaining := line
	first := true
	for runewidth.StringWidth(remaining) > 0 {
		// Continuation rows should not start with the whitespace we used as
		// a wrap point. The very first row keeps its indentation.
		if !first {
			remaining = strings.TrimLeft(remaining, " \t")
			if remaining == "" {
				break
			}
		}
		first = false
		take := takeCells(remaining, width)
		if take == "" {
			break
		}
		rows = append(rows, colorize(take))
		remaining = remaining[len(take):]
	}
	if len(rows) == 0 {
		rows = append(rows, "")
	}
	return rows
}

// takeCells returns the longest prefix of s that fits in width display cells
// AND ends on a word boundary when possible. A word boundary is a run of
// spaces or tabs; if the line has no boundary inside the budget (e.g. a long
// URL or unspaced math), we fall back to a hard cell cut so we still make
// forward progress. When the whole string fits within width, the whole
// string is returned — no spurious wrap at an interior whitespace.
func takeCells(s string, width int) string {
	w := 0
	hardEnd := 0 // longest prefix ending at a rune boundary that fits
	softEnd := 0 // longest prefix ending at the last interior whitespace
	sawNonSpace := false
	overflowed := false
	for i, r := range s {
		rw := runewidth.RuneWidth(r)
		if w+rw > width {
			overflowed = true
			break
		}
		w += rw
		hardEnd = i + len(string(r))
		if r == ' ' || r == '\t' {
			if sawNonSpace {
				softEnd = hardEnd
			}
		} else {
			sawNonSpace = true
		}
	}
	// The whole string fits — take all of it; softEnd is only a wrap hint,
	// useful once we've actually exceeded the budget.
	if !overflowed {
		return s
	}
	end := softEnd
	if end == 0 {
		end = hardEnd
	}
	if end == 0 && len(s) > 0 {
		// Width too small to fit even one rune — advance by one byte so the
		// caller's loop still terminates.
		_, sz := runeAt(s)
		end = sz
	}
	return s[:end]
}

func runeAt(s string) (rune, int) {
	for _, r := range s {
		return r, len(string(r))
	}
	return 0, 1
}

func lineNumWidth(n int) int {
	if n <= 0 {
		return 1
	}
	w := 0
	for n > 0 {
		n /= 10
		w++
	}
	return w
}

func padLeft(s string, width int) string {
	diff := width - len(s)
	if diff <= 0 {
		return s
	}
	return strings.Repeat(" ", diff) + s
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var digits [20]byte
	i := len(digits)
	for n > 0 {
		i--
		digits[i] = byte('0' + n%10)
		n /= 10
	}
	s := string(digits[i:])
	if neg {
		s = "-" + s
	}
	return s
}

// keywordCommands are LaTeX control sequences rendered with the
// SourceKeyword (bold magenta) style so structural markup stands out from
// ordinary commands. Kept small — purely the ones readers scan for.
var keywordCommands = map[string]bool{
	"begin": true, "end": true,
	"section": true, "subsection": true, "subsubsection": true,
	"chapter": true, "part": true, "paragraph": true, "subparagraph": true,
	"title": true, "author": true, "date": true, "maketitle": true,
	"newtheorem": true, "theoremstyle": true,
	"label": true, "ref": true, "cref": true, "Cref": true,
	"eqref": true, "cite": true,
	"input": true, "include": true,
	"usepackage": true, "documentclass": true,
	"newcommand": true, "renewcommand": true, "providecommand": true,
	"item": true,
}

// colorizeLaTeXLine tokenises a single source line into styled fragments
// with a vim-like LaTeX palette. Tracks inline math state so command
// tokens inside math pick up SourceMathText context but commands outside
// math use SourceCommand/SourceKeyword. Also specialises arguments to
// \begin{} and \end{} to render the environment name in SourceEnvName.
func colorizeLaTeXLine(line string, styles Styles) string {
	if line == "" {
		return ""
	}
	var b strings.Builder
	runes := []rune(line)
	i := 0
	inMath := false
	expectEnvArg := false
	for i < len(runes) {
		r := runes[i]
		switch {
		case r == '%':
			b.WriteString(styles.SourceComment.Render(string(runes[i:])))
			return b.String()
		case r == '\\':
			if i+1 >= len(runes) {
				// Trailing backslash with no following rune. Happens when
				// soft-wrap splits a line mid-command (e.g. between `\` and
				// `bigr` in `\bigr`) and we colorise the first chunk on its
				// own. The default branch breaks on `\` without advancing,
				// so without this early-out the outer loop spins forever.
				b.WriteString(styles.SourceCommand.Render(string(r)))
				i++
				continue
			}
			next := runes[i+1]
			if next == '(' || next == '[' {
				b.WriteString(styles.SourceMath.Render(string(runes[i : i+2])))
				i += 2
				inMath = true
				continue
			}
			if next == ')' || next == ']' {
				b.WriteString(styles.SourceMath.Render(string(runes[i : i+2])))
				i += 2
				inMath = false
				continue
			}
			if isLatexLetter(next) {
				j := i + 1
				for j < len(runes) && isLatexLetter(runes[j]) {
					j++
				}
				cmd := string(runes[i+1 : j])
				style := styles.SourceCommand
				if keywordCommands[cmd] {
					style = styles.SourceKeyword
				}
				if inMath && !keywordCommands[cmd] {
					// Inside math, ordinary commands (\alpha, \sum, \cdot…)
					// should sit in the math palette so the mode is visually
					// distinct.
					style = styles.SourceMath
				}
				b.WriteString(style.Render(string(runes[i:j])))
				if cmd == "begin" || cmd == "end" {
					expectEnvArg = true
				}
				i = j
				continue
			}
			b.WriteString(styles.SourceCommand.Render(string(runes[i : i+2])))
			i += 2
		case r == '$':
			if i+1 < len(runes) && runes[i+1] == '$' {
				b.WriteString(styles.SourceMath.Render("$$"))
				i += 2
				inMath = !inMath
				continue
			}
			b.WriteString(styles.SourceMath.Render("$"))
			i++
			inMath = !inMath
		case r == '{' || r == '}':
			if expectEnvArg && r == '{' {
				// Consume the env-name argument: everything up to the next '}'.
				j := i + 1
				for j < len(runes) && runes[j] != '}' {
					j++
				}
				b.WriteString(styles.SourceBrace.Render("{"))
				if j > i+1 {
					b.WriteString(styles.SourceEnvName.Render(string(runes[i+1 : j])))
				}
				if j < len(runes) {
					b.WriteString(styles.SourceBrace.Render("}"))
					i = j + 1
				} else {
					i = j
				}
				expectEnvArg = false
				continue
			}
			b.WriteString(styles.SourceBrace.Render(string(r)))
			i++
		default:
			j := i
			for j < len(runes) {
				c := runes[j]
				if c == '\\' || c == '%' || c == '$' || c == '{' || c == '}' {
					break
				}
				j++
			}
			seg := runes[i:j]
			if inMath {
				b.WriteString(styles.SourceMathText.Render(string(seg)))
			} else {
				b.WriteString(highlightNumbers(string(seg), styles))
			}
			i = j
		}
	}
	return b.String()
}

// highlightNumbers walks s and wraps runs of ASCII digits in the number
// style, leaving other characters untouched. Used only outside math mode
// so line numbers, counters, and years stand out without disturbing math
// subscripts/superscripts (which are already coloured as math content).
func highlightNumbers(s string, styles Styles) string {
	if s == "" {
		return ""
	}
	var b strings.Builder
	runes := []rune(s)
	i := 0
	for i < len(runes) {
		if runes[i] >= '0' && runes[i] <= '9' {
			j := i
			for j < len(runes) && runes[j] >= '0' && runes[j] <= '9' {
				j++
			}
			b.WriteString(styles.SourceNumber.Render(string(runes[i:j])))
			i = j
			continue
		}
		j := i
		for j < len(runes) && !(runes[j] >= '0' && runes[j] <= '9') {
			j++
		}
		b.WriteString(string(runes[i:j]))
		i = j
	}
	return b.String()
}

func isLatexLetter(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')
}

// clipToWidth hard-truncates a possibly-styled string to width display cells.
// If the input exceeds the budget an ellipsis replaces the last cell.
//
// The styled input includes ANSI escape sequences; runewidth.Truncate honours
// those by ignoring zero-width control runs, so we feed the rendered string
// directly. When width <= 0 we return empty.
func clipToWidth(s string, width int) string {
	if width <= 0 {
		return ""
	}
	if runewidth.StringWidth(stripANSI(s)) <= width {
		return s
	}
	// Conservative fallback: strip ANSI, truncate, re-render plainly.
	return truncateToWidth(stripANSI(s), width)
}

// stripANSI removes CSI escape sequences. A minimal implementation is enough
// for the subset lipgloss emits (ESC [ ... m).
func stripANSI(s string) string {
	var b strings.Builder
	runes := []rune(s)
	for i := 0; i < len(runes); i++ {
		if runes[i] == 0x1b && i+1 < len(runes) && runes[i+1] == '[' {
			j := i + 2
			for j < len(runes) {
				c := runes[j]
				if c >= '@' && c <= '~' {
					j++
					break
				}
				j++
			}
			i = j - 1
			continue
		}
		b.WriteRune(runes[i])
	}
	return b.String()
}
