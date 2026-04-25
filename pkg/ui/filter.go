package ui

import (
	"mreview/pkg/format"
	"mreview/pkg/parser"
	"mreview/pkg/persist"
)

// NumFilters is the number of distinct Filter values; used for cycling.
const NumFilters = 4

// CycleFilter returns the next Filter in the `all -> unreviewed -> annotated
// -> issues -> all` rotation.
func CycleFilter(f Filter) Filter {
	return Filter((int(f) + 1) % NumFilters)
}

// DefaultFilter returns `Unreviewed` when the sidecar already has at least
// one reviewed block, else `All`. The rule matches revdiff's behaviour where
// an empty sidecar shows everything and a mid-review sidecar hides finished
// work.
func DefaultFilter(side *persist.Sidecar) Filter {
	if side != nil && len(side.Reviewed) > 0 {
		return FilterUnreviewed
	}
	return FilterAll
}

// blockMatchesFilter reports whether a block should be visible under f.
// The filter is purely per-block: ancestors do not force children to be
// shown, and a filtered-out parent still contributes depth to any visible
// descendants.
func blockMatchesFilter(b *parser.Block, side *persist.Sidecar, f Filter, ext ...map[string][]format.ReportDiag) bool {
	if b == nil {
		return false
	}
	switch f {
	case FilterAll:
		return true
	case FilterUnreviewed:
		return !isReviewed(side, b.ID)
	case FilterAnnotated:
		return hasAnnotation(side, b.ID)
	case FilterIssues:
		if blockHasIssue(b) {
			return true
		}
		if len(ext) > 0 && blockHasExternalIssue(ext[0], b.ID) {
			return true
		}
		return false
	}
	return true
}

func isReviewed(side *persist.Sidecar, id string) bool {
	if side == nil {
		return false
	}
	for _, r := range side.Reviewed {
		if r == id {
			return true
		}
	}
	return false
}

func hasAnnotation(side *persist.Sidecar, id string) bool {
	if side == nil {
		return false
	}
	for _, a := range side.Annotations {
		if a.BlockID == id {
			return true
		}
	}
	return false
}

// blockHasIssue is true when the block carries any unresolved outgoing ref.
// The `⊘ no-region` marker is rendered but not treated as a filter-level
// issue, because until Task 15 wires the SyncTeX index every block lacks a
// region and the filter would degenerate into "show everything".
func blockHasIssue(b *parser.Block) bool {
	if b == nil {
		return false
	}
	for _, r := range b.RefsOut {
		if !r.Resolved {
			return true
		}
	}
	return false
}
