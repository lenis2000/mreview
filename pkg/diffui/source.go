package diffui

import (
	"fmt"
	"strings"

	"github.com/pmezard/go-difflib/difflib"

	"mreview/pkg/diffreview"
	"mreview/pkg/parser"
)

type sourceRow struct {
	oldMark string
	oldLine int
	oldText string
	newMark string
	newLine int
	newText string
}

// RenderPairSource renders the old/new source for one semantic pair.
func RenderPairSource(pair *diffreview.Pair, width, height int) string {
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

	if len(rows) > height {
		rows = rows[:height]
	}
	rendered := make([]string, 0, len(rows))
	for _, row := range rows {
		old := formatSourceCell(row.oldMark, row.oldLine, row.oldText)
		new := formatSourceCell(row.newMark, row.newLine, row.newText)
		rendered = append(rendered, clipLine(old, oldW)+" │ "+clipLine(new, newW))
	}
	return strings.Join(rendered, "\n")
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
			oldN := op.I2 - op.I1
			newN := op.J2 - op.J1
			for off := 0; off < max(oldN, newN); off++ {
				row := sourceRow{}
				if off < oldN {
					row.oldMark = "~"
					row.oldLine = sourceLineNumber(oldBlock, op.I1+off)
					row.oldText = oldLines[op.I1+off]
				}
				if off < newN {
					row.newMark = "~"
					row.newLine = sourceLineNumber(newBlock, op.J1+off)
					row.newText = newLines[op.J1+off]
				}
				if off >= oldN {
					row.newMark = "+"
				}
				if off >= newN {
					row.oldMark = "-"
				}
				rows = append(rows, row)
			}
		}
	}
	return rows
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
	if mark == "" {
		mark = " "
	}
	if line > 0 {
		return fmt.Sprintf("%s %4d %s", mark, line, text)
	}
	return fmt.Sprintf("%s      %s", mark, text)
}
