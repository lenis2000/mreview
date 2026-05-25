package diffui

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/charmbracelet/lipgloss"
	"github.com/pmezard/go-difflib/difflib"

	"mreview/pkg/diffreview"
	"mreview/pkg/parser"
)

type sourcePartKind int

const (
	sourcePartEqual sourcePartKind = iota
	sourcePartDelete
	sourcePartAdd
	sourcePartChange
)

type sourcePart struct {
	Text string
	Kind sourcePartKind
}

type sourceRow struct {
	oldMark  string
	oldLine  int
	oldText  string
	oldParts []sourcePart
	newMark  string
	newLine  int
	newText  string
	newParts []sourcePart
}

const intralinePairThreshold = 0.40

var (
	diffDeleteLineStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("224")).Background(lipgloss.Color("52"))
	diffAddLineStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("194")).Background(lipgloss.Color("22"))
	diffChangeLineStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("230")).Background(lipgloss.Color("58"))
	diffDeleteTokenStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("231")).Background(lipgloss.Color("160")).Bold(true)
	diffAddTokenStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("16")).Background(lipgloss.Color("82")).Bold(true)
	diffChangeTokenStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("16")).Background(lipgloss.Color("226")).Bold(true)
)

// RenderPairSource renders the old/new source for one semantic pair. Long
// physical TeX lines are soft-wrapped inside their side of the diff so content
// does not disappear behind an ellipsis on narrow panes.
func RenderPairSource(pair *diffreview.Pair, width, height int) string {
	return RenderPairSourceAt(pair, width, height, 0, 0)
}

// RenderPairSourceAt is RenderPairSource with an optional source-line anchor.
// When an anchor is supplied, the rendered window scrolls so that line is
// visible inside long semantic blocks.
func RenderPairSourceAt(pair *diffreview.Pair, width, height, oldAnchorLine, newAnchorLine int) string {
	return renderPairSource(pair, width, height, oldAnchorLine, newAnchorLine, false)
}

// RenderPairSourceHighlighted is the TUI variant: same geometry as
// RenderPairSourceAt, but changed/added/deleted lines get FileMerge-like
// full-row highlighting and paired changed lines get token-level highlights.
func RenderPairSourceHighlighted(pair *diffreview.Pair, width, height, oldAnchorLine, newAnchorLine int) string {
	return renderPairSource(pair, width, height, oldAnchorLine, newAnchorLine, true)
}

func renderPairSource(pair *diffreview.Pair, width, height, oldAnchorLine, newAnchorLine int, highlight bool) string {
	if width < 1 {
		width = 1
	}
	if height < 1 {
		height = 1
	}
	rows := sourceRows(pair)
	if len(rows) == 0 {
		return "(no source)"
	}
	oldW := (width - 3) / 2
	if oldW < 1 {
		oldW = 1
	}
	newW := width - oldW - 3
	if newW < 1 {
		newW = 1
	}

	rendered := make([]string, 0, max(height, len(rows)))
	anchorRendered := -1
	for rowIndex, row := range rows {
		rowStart := len(rendered)
		if anchorRendered < 0 && sourceRowMatchesAnchor(row, rowIndex, oldAnchorLine, newAnchorLine) {
			anchorRendered = rowStart
		}
		oldLines := renderSourceCell(row.oldMark, row.oldLine, row.oldText, row.oldParts, oldW, true, highlight)
		newLines := renderSourceCell(row.newMark, row.newLine, row.newText, row.newParts, newW, false, highlight)
		lineCount := max(len(oldLines), len(newLines))
		for i := 0; i < lineCount; i++ {
			oldCell := strings.Repeat(" ", oldW)
			if i < len(oldLines) {
				oldCell = oldLines[i]
			}
			newCell := ""
			if i < len(newLines) {
				newCell = newLines[i]
			}
			rendered = append(rendered, oldCell+" │ "+newCell)
		}
	}
	return visibleRenderedLines(rendered, height, anchorRendered)
}

// RenderPairSourceSide renders one side of a semantic pair for the wide
// four-pane layout.
func RenderPairSourceSide(pair *diffreview.Pair, oldSide bool, width, height int) string {
	return RenderPairSourceSideAt(pair, oldSide, width, height, 0, 0)
}

// RenderPairSourceSideAt is RenderPairSourceSide with an optional source-line
// anchor used by the TUI to scroll within long blocks.
func RenderPairSourceSideAt(pair *diffreview.Pair, oldSide bool, width, height, oldAnchorLine, newAnchorLine int) string {
	return renderPairSourceSide(pair, oldSide, width, height, oldAnchorLine, newAnchorLine, false)
}

// RenderPairSourceSideHighlighted is the TUI variant with FileMerge-like
// full-row and token-level highlights.
func RenderPairSourceSideHighlighted(pair *diffreview.Pair, oldSide bool, width, height, oldAnchorLine, newAnchorLine int) string {
	return renderPairSourceSide(pair, oldSide, width, height, oldAnchorLine, newAnchorLine, true)
}

func renderPairSourceSide(pair *diffreview.Pair, oldSide bool, width, height, oldAnchorLine, newAnchorLine int, highlight bool) string {
	if width < 1 {
		width = 1
	}
	if height < 1 {
		height = 1
	}
	rows := sourceRows(pair)
	if len(rows) == 0 {
		return "(no source)"
	}
	rendered := make([]string, 0, max(height, len(rows)))
	anchorRendered := -1
	for rowIndex, row := range rows {
		rowStart := len(rendered)
		if anchorRendered < 0 && sourceRowMatchesAnchor(row, rowIndex, oldAnchorLine, newAnchorLine) {
			anchorRendered = rowStart
		}
		var lines []string
		if oldSide {
			lines = renderSourceCell(row.oldMark, row.oldLine, row.oldText, row.oldParts, width, true, highlight)
		} else {
			lines = renderSourceCell(row.newMark, row.newLine, row.newText, row.newParts, width, false, highlight)
		}
		rendered = append(rendered, lines...)
	}
	return visibleRenderedLines(rendered, height, anchorRendered)
}

func sourceRows(pair *diffreview.Pair) []sourceRow {
	if pair == nil {
		return []sourceRow{{oldText: "(no pair selected)", newText: "(no pair selected)"}}
	}
	switch pair.Status {
	case diffreview.Added:
		return rowsForAdded(pair.New)
	case diffreview.Deleted:
		return rowsForDeleted(pair.Old)
	default:
		return rowsForMatched(pair.Old, pair.New)
	}
}

func rowsForAdded(newBlock *parser.Block) []sourceRow {
	newLines := blockSourceLines(newBlock)
	rows := make([]sourceRow, 0, max(1, len(newLines)))
	if len(newLines) == 0 {
		return []sourceRow{{oldText: "(added in new)", newText: "(no source)"}}
	}
	for i, line := range newLines {
		row := sourceRow{newMark: "+", newLine: sourceLineNumber(newBlock, i), newText: line}
		if i == 0 {
			row.oldText = "(added in new)"
		}
		rows = append(rows, row)
	}
	return rows
}

func rowsForDeleted(oldBlock *parser.Block) []sourceRow {
	oldLines := blockSourceLines(oldBlock)
	rows := make([]sourceRow, 0, max(1, len(oldLines)))
	if len(oldLines) == 0 {
		return []sourceRow{{oldText: "(no source)", newText: "(deleted from new)"}}
	}
	for i, line := range oldLines {
		row := sourceRow{oldMark: "-", oldLine: sourceLineNumber(oldBlock, i), oldText: line}
		if i == 0 {
			row.newText = "(deleted from new)"
		}
		rows = append(rows, row)
	}
	return rows
}

func rowsForMatched(oldBlock, newBlock *parser.Block) []sourceRow {
	oldLines := blockSourceLines(oldBlock)
	newLines := blockSourceLines(newBlock)
	if len(oldLines) == 0 && len(newLines) == 0 {
		return []sourceRow{{oldText: "(no old source)", newText: "(no new source)"}}
	}
	matcher := difflib.NewMatcher(oldLines, newLines)
	var rows []sourceRow
	for _, op := range matcher.GetOpCodes() {
		switch op.Tag {
		case 'e':
			for i, j := op.I1, op.J1; i < op.I2 && j < op.J2; i, j = i+1, j+1 {
				rows = append(rows, sourceRow{
					oldMark: " ",
					oldLine: sourceLineNumber(oldBlock, i),
					oldText: oldLines[i],
					newMark: " ",
					newLine: sourceLineNumber(newBlock, j),
					newText: newLines[j],
				})
			}
		case 'd':
			for i := op.I1; i < op.I2; i++ {
				rows = append(rows, sourceRow{
					oldMark: "-",
					oldLine: sourceLineNumber(oldBlock, i),
					oldText: oldLines[i],
				})
			}
		case 'i':
			for j := op.J1; j < op.J2; j++ {
				rows = append(rows, sourceRow{
					newMark: "+",
					newLine: sourceLineNumber(newBlock, j),
					newText: newLines[j],
				})
			}
		case 'r':
			rows = append(rows, alignReplaceLineBlock(oldBlock, newBlock, oldLines, newLines, op.I1, op.I2, op.J1, op.J2)...)
		}
	}
	return rows
}

func alignReplaceLineBlock(oldBlock, newBlock *parser.Block, oldLines, newLines []string, oldStart, oldEnd, newStart, newEnd int) []sourceRow {
	oldN := oldEnd - oldStart
	newN := newEnd - newStart
	if oldN == 0 && newN == 0 {
		return nil
	}
	sim := make([][]float64, oldN)
	for i := 0; i < oldN; i++ {
		sim[i] = make([]float64, newN)
		for j := 0; j < newN; j++ {
			sim[i][j] = lineSimilarity(oldLines[oldStart+i], newLines[newStart+j])
		}
	}
	dp := make([][]float64, oldN+1)
	choice := make([][]byte, oldN+1)
	for i := range dp {
		dp[i] = make([]float64, newN+1)
		choice[i] = make([]byte, newN+1)
	}
	for i := oldN; i >= 0; i-- {
		for j := newN; j >= 0; j-- {
			if i == oldN && j == newN {
				continue
			}
			best := -1.0
			if i < oldN {
				best = dp[i+1][j]
				choice[i][j] = 'd'
			}
			if j < newN && dp[i][j+1] > best {
				best = dp[i][j+1]
				choice[i][j] = 'i'
			}
			if i < oldN && j < newN && sim[i][j] >= intralinePairThreshold {
				score := sim[i][j] + dp[i+1][j+1]
				if score >= best {
					best = score
					choice[i][j] = 'p'
				}
			}
			dp[i][j] = best
		}
	}
	var rows []sourceRow
	for i, j := 0, 0; i < oldN || j < newN; {
		switch choice[i][j] {
		case 'p':
			oldText := oldLines[oldStart+i]
			newText := newLines[newStart+j]
			oldParts, newParts := intralineDiffParts(oldText, newText)
			rows = append(rows, sourceRow{
				oldMark:  "~",
				oldLine:  sourceLineNumber(oldBlock, oldStart+i),
				oldText:  oldText,
				oldParts: oldParts,
				newMark:  "~",
				newLine:  sourceLineNumber(newBlock, newStart+j),
				newText:  newText,
				newParts: newParts,
			})
			i++
			j++
		case 'i':
			rows = append(rows, sourceRow{
				newMark: "+",
				newLine: sourceLineNumber(newBlock, newStart+j),
				newText: newLines[newStart+j],
			})
			j++
		default:
			rows = append(rows, sourceRow{
				oldMark: "-",
				oldLine: sourceLineNumber(oldBlock, oldStart+i),
				oldText: oldLines[oldStart+i],
			})
			i++
		}
	}
	return rows
}

func intralineDiffParts(oldLine, newLine string) ([]sourcePart, []sourcePart) {
	oldTokens := tokenizeLatex(oldLine)
	newTokens := tokenizeLatex(newLine)
	matcher := difflib.NewMatcher(oldTokens, newTokens)
	var oldParts, newParts []sourcePart
	for _, op := range matcher.GetOpCodes() {
		oldText := strings.Join(oldTokens[op.I1:op.I2], "")
		newText := strings.Join(newTokens[op.J1:op.J2], "")
		switch op.Tag {
		case 'e':
			oldParts = appendPart(oldParts, sourcePartEqual, oldText)
			newParts = appendPart(newParts, sourcePartEqual, newText)
		case 'd':
			oldParts = appendPart(oldParts, sourcePartDelete, oldText)
		case 'i':
			newParts = appendPart(newParts, sourcePartAdd, newText)
		case 'r':
			oldParts = appendPart(oldParts, sourcePartChange, oldText)
			newParts = appendPart(newParts, sourcePartChange, newText)
		}
	}
	return oldParts, newParts
}

func appendPart(parts []sourcePart, kind sourcePartKind, text string) []sourcePart {
	if text == "" {
		return parts
	}
	if len(parts) > 0 && parts[len(parts)-1].Kind == kind {
		parts[len(parts)-1].Text += text
		return parts
	}
	return append(parts, sourcePart{Text: text, Kind: kind})
}

func lineSimilarity(oldLine, newLine string) float64 {
	oldTokens := visibleTokens(tokenizeLatex(oldLine))
	newTokens := visibleTokens(tokenizeLatex(newLine))
	if len(oldTokens) == 0 && len(newTokens) == 0 {
		return 1
	}
	if len(oldTokens) == 0 || len(newTokens) == 0 {
		return 0
	}
	lcs := lcsLength(oldTokens, newTokens)
	return float64(2*lcs) / float64(len(oldTokens)+len(newTokens))
}

func visibleTokens(tokens []string) []string {
	out := make([]string, 0, len(tokens))
	for _, tok := range tokens {
		if strings.TrimSpace(tok) == "" {
			continue
		}
		out = append(out, strings.ToLower(tok))
	}
	return out
}

func lcsLength(a, b []string) int {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	dp := make([][]int, len(a)+1)
	for i := range dp {
		dp[i] = make([]int, len(b)+1)
	}
	for i := len(a) - 1; i >= 0; i-- {
		for j := len(b) - 1; j >= 0; j-- {
			if a[i] == b[j] {
				dp[i][j] = 1 + dp[i+1][j+1]
			} else if dp[i+1][j] >= dp[i][j+1] {
				dp[i][j] = dp[i+1][j]
			} else {
				dp[i][j] = dp[i][j+1]
			}
		}
	}
	return dp[0][0]
}

func tokenizeLatex(s string) []string {
	var tokens []string
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		if r == utf8.RuneError && size == 0 {
			break
		}
		start := i
		switch {
		case r == '\\':
			i += size
			if i < len(s) {
				next, nextSize := utf8.DecodeRuneInString(s[i:])
				if unicode.IsLetter(next) {
					for i < len(s) {
						next, nextSize = utf8.DecodeRuneInString(s[i:])
						if !unicode.IsLetter(next) {
							break
						}
						i += nextSize
					}
				} else {
					i += nextSize
				}
			}
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			i += size
			for i < len(s) {
				next, nextSize := utf8.DecodeRuneInString(s[i:])
				if !unicode.IsLetter(next) && !unicode.IsDigit(next) {
					break
				}
				i += nextSize
			}
		case unicode.IsSpace(r):
			i += size
			for i < len(s) {
				next, nextSize := utf8.DecodeRuneInString(s[i:])
				if !unicode.IsSpace(next) {
					break
				}
				i += nextSize
			}
		default:
			i += size
		}
		tokens = append(tokens, s[start:i])
	}
	return tokens
}

func blockSourceLines(block *parser.Block) []string {
	if block == nil || block.Source == "" {
		return nil
	}
	source := strings.TrimSuffix(block.Source, "\n")
	if source == "" {
		return []string{""}
	}
	return strings.Split(source, "\n")
}

func sourceLineNumber(block *parser.Block, offset int) int {
	if block == nil || block.StartLine <= 0 {
		return 0
	}
	return block.StartLine + offset
}

func formatSourceCell(mark string, line int, text string) string {
	prefix := sourceCellPrefix(mark, line)
	return prefix + text
}

func renderSourceCell(mark string, line int, text string, parts []sourcePart, width int, oldSide bool, highlight bool) []string {
	if !highlight {
		return wrapSourceCell(mark, line, text, width)
	}
	return wrapSourceCellStyled(mark, line, text, parts, width, oldSide)
}

func wrapSourceCell(mark string, line int, text string, width int) []string {
	if width < 1 {
		width = 1
	}
	prefix := sourceCellPrefix(mark, line)
	prefixW := len([]rune(prefix))
	if prefixW >= width {
		return []string{clipLine(prefix+text, width)}
	}
	textW := width - prefixW
	chunks := wrapTextRunes(text, textW)
	out := make([]string, 0, len(chunks))
	for i, chunk := range chunks {
		p := prefix
		if i > 0 {
			p = continuationPrefix(prefixW)
		}
		out = append(out, p+chunk)
	}
	return out
}

func wrapSourceCellStyled(mark string, line int, text string, parts []sourcePart, width int, oldSide bool) []string {
	if width < 1 {
		width = 1
	}
	prefix := sourceCellPrefix(mark, line)
	prefixW := len([]rune(prefix))
	if prefixW >= width {
		return []string{styleSourceParts([]sourcePart{{Text: clipLine(prefix+text, width)}}, mark, oldSide)}
	}
	if len(parts) == 0 {
		parts = []sourcePart{{Text: text, Kind: sourcePartEqual}}
	}
	contentW := width - prefixW
	chunks := wrapPartsHard(parts, contentW)
	if len(chunks) == 0 {
		chunks = [][]sourcePart{{}}
	}
	out := make([]string, 0, len(chunks))
	for i, chunk := range chunks {
		p := prefix
		if i > 0 {
			p = continuationPrefix(prefixW)
		}
		lineParts := append([]sourcePart{{Text: p, Kind: sourcePartEqual}}, chunk...)
		if visiblePartsWidth(lineParts) < width {
			lineParts = append(lineParts, sourcePart{Text: strings.Repeat(" ", width-visiblePartsWidth(lineParts)), Kind: sourcePartEqual})
		}
		out = append(out, styleSourceParts(lineParts, mark, oldSide))
	}
	return out
}

func sourceCellPrefix(mark string, line int) string {
	if mark == "" {
		mark = " "
	}
	if line > 0 {
		return fmt.Sprintf("%s %4d ", mark, line)
	}
	return fmt.Sprintf("%s      ", mark)
}

func continuationPrefix(width int) string {
	if width <= 0 {
		return ""
	}
	if width == 1 {
		return "·"
	}
	return strings.Repeat(" ", width-1) + "·"
}

func wrapTextRunes(text string, width int) []string {
	if width < 1 {
		width = 1
	}
	runes := []rune(text)
	if len(runes) == 0 {
		return []string{""}
	}
	out := make([]string, 0, (len(runes)+width-1)/width)
	for len(runes) > width {
		breakAt := wrapBreakIndex(runes, width)
		out = append(out, string(runes[:breakAt]))
		runes = trimLeadingWrapSpace(runes[breakAt:])
	}
	out = append(out, string(runes))
	return out
}

func wrapPartsHard(parts []sourcePart, width int) [][]sourcePart {
	if width < 1 {
		width = 1
	}
	var out [][]sourcePart
	var cur []sourcePart
	curW := 0
	flush := func() {
		out = append(out, cur)
		cur = nil
		curW = 0
	}
	for _, part := range parts {
		runes := []rune(part.Text)
		for len(runes) > 0 {
			space := width - curW
			if space <= 0 {
				flush()
				space = width
			}
			take := len(runes)
			if take > space {
				take = space
			}
			cur = appendPart(cur, part.Kind, string(runes[:take]))
			curW += take
			runes = runes[take:]
			if curW >= width && len(runes) > 0 {
				flush()
			}
		}
	}
	if cur != nil || len(out) == 0 {
		out = append(out, cur)
	}
	return out
}

func visiblePartsWidth(parts []sourcePart) int {
	w := 0
	for _, part := range parts {
		w += len([]rune(part.Text))
	}
	return w
}

func wrapBreakIndex(runes []rune, width int) int {
	if len(runes) <= width {
		return len(runes)
	}
	for i := width; i > 0; i-- {
		if runes[i-1] == ' ' || runes[i-1] == '\t' {
			return i
		}
	}
	return width
}

func trimLeadingWrapSpace(runes []rune) []rune {
	for len(runes) > 0 && (runes[0] == ' ' || runes[0] == '\t') {
		runes = runes[1:]
	}
	return runes
}

func padToWidth(s string, width int) string {
	if width < 1 {
		return ""
	}
	runes := []rune(s)
	if len(runes) >= width {
		return s
	}
	return s + strings.Repeat(" ", width-len(runes))
}

func visibleRenderedLines(lines []string, height int, anchor int) string {
	if height < 1 {
		height = 1
	}
	if len(lines) <= height {
		return strings.Join(lines, "\n")
	}
	start := 0
	if anchor >= 0 {
		start = anchor - height/3
	}
	if start < 0 {
		start = 0
	}
	if start > len(lines)-height {
		start = len(lines) - height
	}
	return strings.Join(lines[start:start+height], "\n")
}

func sourceRowMatchesAnchor(row sourceRow, rowIndex int, oldAnchorLine, newAnchorLine int) bool {
	if newAnchorLine > 0 && row.newLine == newAnchorLine {
		return true
	}
	if oldAnchorLine > 0 && row.oldLine == oldAnchorLine {
		return true
	}
	return rowIndex == 0 && oldAnchorLine <= 0 && newAnchorLine <= 0
}

func styleSourceParts(parts []sourcePart, mark string, oldSide bool) string {
	var b strings.Builder
	for _, part := range parts {
		b.WriteString(styleForSourcePart(mark, part.Kind, oldSide).Render(part.Text))
	}
	return b.String()
}

func styleForSourcePart(mark string, kind sourcePartKind, oldSide bool) lipgloss.Style {
	switch kind {
	case sourcePartDelete:
		return diffDeleteTokenStyle
	case sourcePartAdd:
		return diffAddTokenStyle
	case sourcePartChange:
		if oldSide {
			return diffDeleteTokenStyle
		}
		return diffAddTokenStyle
	}
	switch mark {
	case "-":
		return diffDeleteLineStyle
	case "+":
		return diffAddLineStyle
	case "~":
		return diffChangeLineStyle
	default:
		return lipgloss.NewStyle()
	}
}
