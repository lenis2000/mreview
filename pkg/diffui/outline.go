package diffui

import (
	"fmt"
	"strings"

	"mreview/pkg/diffreview"
)

// OutlineRow is one visible semantic pair in the diff outline.
type OutlineRow struct {
	PairID    string
	PairIndex int
	Marker    string
	Status    diffreview.PairStatus
	Title     string
	Section   string
	Reviewed  bool
	Annotated bool
	Issues    bool
}

// BuildOutline returns rows for visible semantic pairs under the selected
// filter.
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
	for i, pair := range review.Pairs {
		if !pairMatchesFilter(pair, filter, reviewed, annotations, issues) {
			continue
		}
		rows = append(rows, OutlineRow{
			PairID:    pair.ID,
			PairIndex: i,
			Marker:    StatusMarker(pair.Status),
			Status:    pair.Status,
			Title:     pairTitle(pair),
			Section:   sectionLabel(pair),
			Reviewed:  reviewed[pair.ID],
			Annotated: annotations[pair.ID] != "",
			Issues:    len(issues[pair.ID]) > 0,
		})
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
	body := RenderOutline(rows, m.Cursor, width, height-1)
	if body == "" {
		body = "(no pairs)"
	}
	return clipLine(header, width) + "\n" + body
}

// RenderOutline renders already-built outline rows. The cursor is the index
// into Review.Pairs, not an index into the filtered row slice.
func RenderOutline(rows []OutlineRow, cursorPairIndex int, width, height int) string {
	if height < 1 {
		height = 1
	}
	if width < 1 {
		width = 1
	}
	if len(rows) == 0 {
		return clipLine("(no pairs)", width)
	}

	cursorRow := 0
	for i, row := range rows {
		if row.PairIndex == cursorPairIndex {
			cursorRow = i
			break
		}
	}
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
	for _, row := range rows[start:end] {
		cursor := " "
		if row.PairIndex == cursorPairIndex {
			cursor = ">"
		}
		flags := ""
		if row.Reviewed {
			flags += "✓"
		}
		if row.Annotated {
			flags += "*"
		}
		if row.Issues {
			flags += "!"
		}
		if flags != "" {
			flags = " " + flags
		}
		line := fmt.Sprintf("%s %-3s %s%s", cursor, row.Marker, row.Title, flags)
		if row.Section != "" {
			line += " [" + row.Section + "]"
		}
		rendered = append(rendered, clipLine(line, width))
	}
	return strings.Join(rendered, "\n")
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
