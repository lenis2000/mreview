package ui

import (
	"strings"

	"github.com/mattn/go-runewidth"

	"mreview/pkg/parser"
)

// RenderSource returns the rendered body of the source pane for the current
// cursor block. It wraps lines at pane width and applies lightweight LaTeX
// coloring: `\commands`, math delimiters, and line-leading `%` comments.
//
// width is the inner pane width; height is the inner pane height. The block
// source is sliced from its StartLine so that visible rows carry the right
// absolute line numbers even when the cursor is on an inner block.
func RenderSource(doc *parser.Document, cursor string, width, height int, styles Styles) string {
	if doc == nil || cursor == "" {
		return styles.OutlineMuted.Render("(no block selected)")
	}
	b := doc.ByID[cursor]
	if b == nil {
		return styles.OutlineMuted.Render("(unknown block)")
	}
	if strings.TrimSpace(b.Source) == "" {
		return styles.OutlineMuted.Render("(" + b.Kind.String() + " — empty source)")
	}
	lines := strings.Split(b.Source, "\n")
	if height < 1 {
		height = 1
	}
	if len(lines) > height {
		lines = lines[:height]
	}
	gutterW := lineNumWidth(b.EndLine)
	var out strings.Builder
	for i, line := range lines {
		lineNo := b.StartLine + i
		gutter := padLeft(itoa(lineNo), gutterW)
		gutterStyled := styles.SourceGutter.Render(gutter)
		body := clipToWidth(colorizeLaTeXLine(line, styles), width-gutterW-1)
		out.WriteString(gutterStyled)
		out.WriteByte(' ')
		out.WriteString(body)
		if i < len(lines)-1 {
			out.WriteByte('\n')
		}
	}
	return out.String()
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
