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

	blockH := b.EndLine - b.StartLine + 1
	if blockH < 1 {
		blockH = 1
	}
	// Split remaining vertical budget between context above and below the
	// block, biased slightly to the top so the block sits a touch lower than
	// dead-centre — matches the way readers naturally land on a target.
	ctxBudget := height - blockH
	if ctxBudget < 0 {
		ctxBudget = 0
	}
	topCtx := ctxBudget / 2
	botCtx := ctxBudget - topCtx
	startLine := b.StartLine - topCtx
	if startLine < 1 {
		startLine = 1
	}
	endLine := b.EndLine + botCtx
	if endLine > total {
		endLine = total
	}

	gutterW := lineNumWidth(endLine)
	bodyWidth := width - gutterW - 1
	if bodyWidth < 1 {
		bodyWidth = 1
	}

	cursorLine := 0
	if lineCursor > 0 {
		cursorLine = b.StartLine + lineCursor - 1
		if cursorLine > b.EndLine {
			cursorLine = b.EndLine
		}
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

	var out strings.Builder
	rows := 0
	emitRow := func(s string) bool {
		if rows >= height {
			return false
		}
		if rows > 0 {
			out.WriteByte('\n')
		}
		out.WriteString(s)
		rows++
		return rows < height
	}
	emitNotes := func(forLine int) {
		notes, ok := notesByLine[forLine]
		if !ok {
			return
		}
		for _, n := range notes {
			for _, r := range annotationRows(n, gutterW, bodyWidth, styles) {
				if !emitRow(r) {
					return
				}
			}
		}
	}
	emitEditor := func(forLine int) {
		if !editorActive || editorAnchor != forLine {
			return
		}
		for _, r := range editorRows(editor, gutterW, bodyWidth, styles) {
			if !emitRow(r) {
				return
			}
		}
	}

	// Block-level annotation header (if any) and a block-level editor
	// render above line 1.
	emitNotes(b.StartLine - 1)
	emitEditor(b.StartLine - 1)

	for ln := startLine; ln <= endLine && rows < height; ln++ {
		if ln-1 >= len(allLines) {
			break
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
				// Re-render the row with the outline-cursor style so the line
				// the next `a` will annotate stands out at a glance.
				rowText = styles.OutlineCursor.Width(width).Render(stripANSI(rowText))
			}
			if !emitRow(rowText) {
				return out.String()
			}
		}
		emitNotes(ln)
		emitEditor(ln)
	}
	return out.String()
}

// editorRows formats the live annotation textarea as a block of inline rows
// indented under the gutter and prefixed with a sigil so it visually echoes
// the saved-annotation row format. The textarea's own multi-line View() is
// used as-is; per-line styling is applied uniformly so the editor stays
// recognisable as commentary while typing.
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
	for i, row := range rows {
		var prefix string
		if i == 0 {
			prefix = sigil
		} else {
			prefix = strings.Repeat(" ", runewidth.StringWidth(sigil))
		}
		out = append(out, indent+styles.SourceAnnotation.Render(prefix+row))
	}
	out = append(out, indent+styles.OutlineMuted.Render(hint))
	return out
}

// annotationRows formats one annotation as one or more inline display rows.
// The first row carries a `▸` sigil; continuation rows align under the note
// text. Long notes wrap on word boundaries within bodyWidth so the inline
// preview never overflows the pane.
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
	rows := make([]string, 0, len(wrapped))
	for i, line := range wrapped {
		var prefix string
		if i == 0 {
			prefix = sigil
		} else {
			prefix = strings.Repeat(" ", runewidth.StringWidth(sigil))
		}
		rows = append(rows, indent+styles.SourceAnnotation.Render(prefix+line))
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
// forward progress.
func takeCells(s string, width int) string {
	w := 0
	hardEnd := 0      // longest prefix ending at a rune boundary that fits
	softEnd := 0      // longest prefix ending at the last whitespace within budget
	sawNonSpace := false
	for i, r := range s {
		rw := runewidth.RuneWidth(r)
		if w+rw > width {
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
	// Skip the trailing whitespace we used as the break point so the next
	// row doesn't begin with leading spaces.
	out := s[:end]
	return out
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

// colorizeLaTeXLine tokenises a single source line into styled fragments.
//
// Highlighted:
//   - `% ... (to end of line)` -> comment (muted)
//   - `\<letters>` -> command
//   - `$`, `$$`, `\(`, `\)`, `\[`, `\]` -> math delimiter
//
// Everything else is rendered with no styling. We use a hand-rolled scanner
// rather than pulling in chroma — chroma would be the right tool only if we
// needed full TeX lexing, which we do not for a three-pane MVP.
func colorizeLaTeXLine(line string, styles Styles) string {
	if line == "" {
		return ""
	}
	var b strings.Builder
	runes := []rune(line)
	i := 0
	for i < len(runes) {
		r := runes[i]
		switch {
		case r == '%':
			// Comment to end of line.
			b.WriteString(styles.SourceComment.Render(string(runes[i:])))
			return b.String()
		case r == '\\' && i+1 < len(runes):
			next := runes[i+1]
			// Math delimiters `\(`, `\)`, `\[`, `\]`.
			if next == '(' || next == ')' || next == '[' || next == ']' {
				b.WriteString(styles.SourceMath.Render(string(runes[i : i+2])))
				i += 2
				continue
			}
			// Command \<letters>+ (or a single non-letter control symbol).
			if isLatexLetter(next) {
				j := i + 1
				for j < len(runes) && isLatexLetter(runes[j]) {
					j++
				}
				b.WriteString(styles.SourceCommand.Render(string(runes[i:j])))
				i = j
				continue
			}
			// Escaped char — print as-is.
			b.WriteString(styles.SourceCommand.Render(string(runes[i : i+2])))
			i += 2
		case r == '$':
			// Handle `$$` as one token.
			if i+1 < len(runes) && runes[i+1] == '$' {
				b.WriteString(styles.SourceMath.Render("$$"))
				i += 2
				continue
			}
			b.WriteString(styles.SourceMath.Render("$"))
			i++
		default:
			j := i
			for j < len(runes) {
				c := runes[j]
				if c == '\\' || c == '%' || c == '$' {
					break
				}
				j++
			}
			b.WriteString(string(runes[i:j]))
			i = j
		}
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
				if (c >= '@' && c <= '~') {
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
