package ui

import (
	"strings"

	"github.com/mattn/go-runewidth"

	"mreview/pkg/parser"
)

// RenderSource returns the rendered body of the source pane for the current
// cursor block. It pulls a window of lines from the full document so the
// cursor block has visible context above and below (dimmed); the block
// itself is colorised normally and the row addressed by lineCursor (1-based
// offset within the block) is highlighted as the line cursor.
//
// width is the inner pane width; height is the inner pane height. softWrap
// controls whether long lines wrap to additional rows or get truncated.
func RenderSource(doc *parser.Document, cursor string, width, height int, styles Styles, softWrap bool, lineCursor int) string {
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

	var out strings.Builder
	rows := 0
	for ln := startLine; ln <= endLine && rows < height; ln++ {
		if ln-1 >= len(allLines) {
			break
		}
		raw := allLines[ln-1]
		inBlock := ln >= b.StartLine && ln <= b.EndLine
		isCursor := ln == cursorLine
		segments := wrapOrClip(raw, bodyWidth, softWrap, inBlock, styles)
		for i, seg := range segments {
			if rows >= height {
				break
			}
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
			out.WriteString(rowText)
			rows++
			if rows < height && (i < len(segments)-1 || ln < endLine) {
				out.WriteByte('\n')
			}
		}
	}
	return out.String()
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
