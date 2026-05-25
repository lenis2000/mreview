package diffui

import (
	"fmt"
	"strings"

	"mreview/pkg/diffreview"
	"mreview/pkg/parser"
)

// OutlineRow is one visible row in the diff outline. Selectable rows point to
// a semantic pair plus a source-line anchor; group rows are section headers.
type OutlineRow struct {
	PairID     string
	PairIndex  int
	AnchorLine int
	HunkIndex  int
	HunkCount  int
	Marker     string
	Status     diffreview.PairStatus
	Title      string
	Section    string
	Reviewed   bool
	Annotated  bool
	Issues     bool
	Group      bool
	Depth      int
}

// BuildOutline returns visible section headers and diff chunks under the
// selected filter. Container section pairs are rendered as folded-looking
// group headers; selectable rows point at individual diff hunks.
func BuildOutline(
	review *diffreview.Review,
	filter Filter,
	reviewed map[string]bool,
	annotations map[string]string,
	issues map[string][]string,
) []OutlineRow {
	if review == nil {
		return nil
	}
	rows := make([]OutlineRow, 0, len(review.Pairs))
	currentGroup := ""
	for i, pair := range review.Pairs {
		if !pairMatchesFilter(pair, filter, reviewed, annotations, issues) {
			continue
		}
		if isSectionPair(pair) {
			label := pairTitle(pair)
			if label == "" {
				label = outlineGroupLabel(pair)
			}
			if label != "" && label != currentGroup {
				rows = append(rows, OutlineRow{PairIndex: -1, Marker: "▾", Title: label, Group: true})
				currentGroup = label
			}
			continue
		}
		group := outlineGroupLabel(pair)
		if group == "" {
			currentGroup = ""
		} else if group != currentGroup {
			rows = append(rows, OutlineRow{PairIndex: -1, Marker: "▾", Title: group, Group: true})
			currentGroup = group
		}
		infos := outlineHunkInfos(pair)
		depth := 0
		if currentGroup != "" {
			depth = 1
		}
		for h, info := range infos {
			rows = append(rows, OutlineRow{
				PairID:     pair.ID,
				PairIndex:  i,
				AnchorLine: info.AnchorLine,
				HunkIndex:  h + 1,
				HunkCount:  len(infos),
				Marker:     StatusMarker(pair.Status),
				Status:     pair.Status,
				Title:      outlineHunkTitle(pair, info, h+1, len(infos)),
				Reviewed:   reviewed[pair.ID],
				Annotated:  annotations[pair.ID] != "",
				Issues:     len(issues[pair.ID]) > 0,
				Depth:      depth,
			})
		}
	}
	return rows
}

// StatusMarker returns the compact marker used for a pair status.
func StatusMarker(status diffreview.PairStatus) string {
	switch status {
	case diffreview.Unchanged:
		return "≡"
	case diffreview.FormatOnly:
		return "fmt"
	case diffreview.Changed:
		return "~"
	case diffreview.Added:
		return "+"
	case diffreview.Deleted:
		return "-"
	case diffreview.Moved:
		return "↷"
	default:
		return "?"
	}
}

func (m Model) renderOutline(width, height int) string {
	rows := BuildOutline(m.Review, m.Filter, m.Reviewed, m.Annotations, m.Issues)
	stats := reviewStats(m.Review)
	header := fmt.Sprintf(
		"stats total:%d %s:%d ~:%d +:%d -:%d fmt:%d ↷:%d",
		stats.Total,
		StatusMarker(diffreview.Unchanged),
		stats.Unchanged,
		stats.Changed,
		stats.Added,
		stats.Deleted,
		stats.FormatOnly,
		stats.Moved,
	)
	if height <= 1 {
		return clipLine(header, width)
	}
	body := RenderOutlineAt(rows, m.Cursor, m.SourceLineCursor, width, height-1)
	if body == "" {
		body = "(no pairs)"
	}
	return clipLine(header, width) + "\n" + body
}

// RenderOutline renders already-built outline rows. The cursor is the index
// into Review.Pairs, not an index into the filtered row slice.
func RenderOutline(rows []OutlineRow, cursorPairIndex int, width, height int) string {
	return RenderOutlineAt(rows, cursorPairIndex, 1, width, height)
}

// RenderOutlineAt also receives the current source-line cursor so the outline
// can show the active internal diff hunk when a semantic pair contains several
// independent change groups.
func RenderOutlineAt(rows []OutlineRow, cursorPairIndex, sourceLineCursor int, width, height int) string {
	if height < 1 {
		height = 1
	}
	if width < 1 {
		width = 1
	}
	if len(rows) == 0 {
		return clipLine("(no pairs)", width)
	}

	cursorRow := outlineCursorRow(rows, cursorPairIndex, sourceLineCursor)
	start := cursorRow - height/2
	if start < 0 {
		start = 0
	}
	if start > len(rows)-height {
		start = len(rows) - height
		if start < 0 {
			start = 0
		}
	}
	end := start + height
	if end > len(rows) {
		end = len(rows)
	}

	rendered := make([]string, 0, end-start)
	for i, row := range rows[start:end] {
		absoluteRow := start + i
		if row.Group {
			line := fmt.Sprintf("  %s %s", row.Marker, row.Title)
			rendered = append(rendered, clipLine(line, width))
			continue
		}
		cursor := " "
		if absoluteRow == cursorRow {
			cursor = ">"
		}
		flags := "   "
		if row.Reviewed || row.Annotated || row.Issues {
			marks := []rune{' ', ' ', ' '}
			if row.Reviewed {
				marks[0] = '✓'
			}
			if row.Annotated {
				marks[1] = '*'
			}
			if row.Issues {
				marks[2] = '!'
			}
			flags = string(marks)
		}
		indent := strings.Repeat("  ", row.Depth)
		line := fmt.Sprintf("%s %s %-3s %s%s", cursor, flags, row.Marker, indent, row.Title)
		if row.Section != "" {
			line += " [" + row.Section + "]"
		}
		rendered = append(rendered, clipLine(line, width))
	}
	return strings.Join(rendered, "\n")
}

func outlineCursorRow(rows []OutlineRow, cursorPairIndex, sourceLineCursor int) int {
	if len(rows) == 0 {
		return 0
	}
	if sourceLineCursor < 1 {
		sourceLineCursor = 1
	}
	fallback := -1
	best := -1
	bestAnchor := -1
	for i, row := range rows {
		if row.Group || row.PairIndex != cursorPairIndex {
			continue
		}
		if fallback < 0 {
			fallback = i
		}
		anchor := row.AnchorLine
		if anchor < 1 {
			anchor = 1
		}
		if anchor <= sourceLineCursor && anchor >= bestAnchor {
			best = i
			bestAnchor = anchor
		}
	}
	if best >= 0 {
		return best
	}
	if fallback >= 0 {
		return fallback
	}
	for i, row := range rows {
		if !row.Group {
			return i
		}
	}
	return 0
}

func outlineHunkInfos(pair diffreview.Pair) []diffHunkInfo {
	infos := diffHunkInfos(&pair)
	if len(infos) == 0 {
		return []diffHunkInfo{{AnchorLine: 1, Title: pairTitle(pair)}}
	}
	for i := range infos {
		if infos[i].AnchorLine < 1 {
			infos[i].AnchorLine = 1
		}
		if strings.TrimSpace(infos[i].Title) == "" {
			infos[i].Title = pairTitle(pair)
		}
	}
	return infos
}

func outlineHunkTitle(pair diffreview.Pair, info diffHunkInfo, index, total int) string {
	title := strings.TrimSpace(info.Title)
	if title == "" {
		title = pairTitle(pair)
	}
	if total > 1 {
		return fmt.Sprintf("chunk %d/%d: %s", index, total, title)
	}
	return title
}

func outlineGroupLabel(pair diffreview.Pair) string {
	path := pair.SectionPathNew
	if len(path) == 0 {
		path = pair.SectionPathOld
	}
	if len(path) > 0 {
		return path[0]
	}
	if isSectionPair(pair) {
		return pairTitle(pair)
	}
	return ""
}

func isSectionPair(pair diffreview.Pair) bool {
	block := pair.New
	if block == nil {
		block = pair.Old
	}
	return block != nil && block.Kind == parser.KindSection
}

func pairTitle(pair diffreview.Pair) string {
	block := pair.New
	if block == nil {
		block = pair.Old
	}
	if block == nil {
		return "(missing block)"
	}
	if block.Label != "" {
		return block.Label
	}
	if block.Title != "" {
		return block.Title
	}
	first := firstNonBlankLine(block.Source)
	if first != "" {
		return first
	}
	return block.Kind.String()
}

func sectionLabel(pair diffreview.Pair) string {
	path := pair.SectionPathNew
	if len(path) == 0 {
		path = pair.SectionPathOld
	}
	if len(path) == 0 {
		return ""
	}
	return strings.Join(path, " / ")
}

func blockLineCount(source string) int {
	source = strings.TrimSuffix(source, "\n")
	if source == "" {
		return 1
	}
	return strings.Count(source, "\n") + 1
}

func firstNonBlankLine(source string) string {
	for _, line := range strings.Split(source, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			return line
		}
	}
	return ""
}

type stats struct {
	Total      int
	Unchanged  int
	FormatOnly int
	Changed    int
	Added      int
	Deleted    int
	Moved      int
}

func reviewStats(review *diffreview.Review) stats {
	if review == nil {
		return stats{}
	}
	out := stats{}
	for _, pair := range review.Pairs {
		out.Total++
		switch pair.Status {
		case diffreview.Unchanged:
			out.Unchanged++
		case diffreview.FormatOnly:
			out.FormatOnly++
		case diffreview.Changed:
			out.Changed++
		case diffreview.Added:
			out.Added++
		case diffreview.Deleted:
			out.Deleted++
		case diffreview.Moved:
			out.Moved++
		}
	}
	return out
}
