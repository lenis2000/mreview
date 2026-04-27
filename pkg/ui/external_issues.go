package ui

import (
	"os"

	"mreview/pkg/format"
	"mreview/pkg/parser"
)

// MarkerExternal is the fallback marker for fmt-report diagnostics
// whose RuleID we don't have a dedicated emoji for. Per-rule markers
// (see markerForRule) take precedence; this only shows up when a new
// lint check is added without a corresponding entry here.
const MarkerExternal = "🔧"

// IssueMarker pairs a fmt-report rule family with the emoji rendered
// in the outline and listed in the help legend. Source-of-truth for
// both the per-block marker rendering and the help table — adding a
// row here is the single change needed to teach mreview about a new
// rule.
type IssueMarker struct {
	RuleID string
	Glyph  string
	Desc   string
}

// IssueMarkers lists the lint diagnostics that drive the issues filter
// and their per-rule glyphs. Order is the order shown in the help
// legend; per-block render order matches this so multiple markers on
// one block stay visually consistent across rows.
func IssueMarkers() []IssueMarker {
	return []IssueMarker{
		{"lint.label-unused", "🏷", "unused label (\\label declared, never \\ref'd)"},
		{"lint.label-duplicate", "👯", "duplicate \\label"},
		{"lint.ref-undefined", "🔗", "undefined \\ref / \\Cref"},
		{"lint.ref-should-eqref", "✏️", "\\ref to equation should be \\eqref"},
		{"lint.cite-undefined", "📚", "undefined \\cite"},
		{"lint.thm-unlabeled", "❓", "theorem-like has no \\label"},
		{"lint.thm-no-proof", "📐", "theorem-like has no following proof"},
		{"lint.thm-orphan-proof", "👻", "proof with no preceding theorem"},
		{"lint.block-too-long", "📏", "block exceeds line budget"},
		{"lint.todo-marker", "🚧", "TODO / FIXME marker"},
	}
}

// markerForRule returns the per-rule emoji for ruleID, falling back
// to MarkerExternal when the rule has no dedicated entry.
func markerForRule(ruleID string) string {
	for _, m := range IssueMarkers() {
		if m.RuleID == ruleID {
			return m.Glyph
		}
	}
	return MarkerExternal
}

// externalMarkersFor returns the distinct per-rule markers for the
// fmt-report diagnostics attached to this block, preserving the order
// declared in IssueMarkers so multi-marker rows look consistent.
// Returns nil when the block has no diagnostics.
func externalMarkersFor(issues map[string][]format.ReportDiag, id string) []string {
	diags := issues[id]
	if len(diags) == 0 {
		return nil
	}
	seen := make(map[string]bool, len(diags))
	for _, d := range diags {
		seen[markerForRule(d.RuleID)] = true
	}
	out := make([]string, 0, len(seen))
	for _, m := range IssueMarkers() {
		if seen[m.Glyph] {
			out = append(out, m.Glyph)
			delete(seen, m.Glyph)
		}
	}
	// Any glyphs not in IssueMarkers (i.e. fallback MarkerExternal) get
	// appended last, also de-duplicated by the seen map.
	if seen[MarkerExternal] {
		out = append(out, MarkerExternal)
	}
	return out
}

// LoadExternalIssues loads a fmt-report.md file and maps its diagnostics
// to owning blocks by line number. Returns nil (not an error) when the
// report file does not exist.
func LoadExternalIssues(reportPath string, doc *parser.Document) (map[string][]format.ReportDiag, error) {
	if _, err := os.Stat(reportPath); err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	rpt, err := format.LoadReport(reportPath)
	if err != nil {
		return nil, err
	}
	if len(rpt.Diags) == 0 {
		return nil, nil
	}
	return mapDiagsToBlocks(rpt.Diags, doc), nil
}

// mapDiagsToBlocks maps each ReportDiag to the block whose [StartLine, EndLine]
// range contains the diagnostic's line. Diagnostics that don't fall in any
// block are mapped to the nearest block by line distance.
func mapDiagsToBlocks(diags []format.ReportDiag, doc *parser.Document) map[string][]format.ReportDiag {
	if doc == nil {
		return nil
	}
	m := make(map[string][]format.ReportDiag)
	for _, d := range diags {
		bid := findOwningBlock(d.Line, doc)
		m[bid] = append(m[bid], d)
	}
	return m
}

// findOwningBlock returns the ID of the deepest block whose line range
// contains line. When no block's range contains the line (e.g. a diagnostic
// in the preamble or after \end{document}), the function falls back to the
// nearest block by line distance so the diagnostic remains visible in the
// outline's issues filter. Returns "root" only when the document has no
// non-root blocks at all.
func findOwningBlock(line int, doc *parser.Document) string {
	if doc == nil || line <= 0 {
		return "root"
	}
	// Walk all blocks and find the narrowest (deepest) one containing line.
	bestID := "root"
	bestSpan := int(^uint(0) >> 1) // max int
	for _, b := range doc.Blocks {
		if b == nil || b.ID == "root" {
			continue
		}
		if b.StartLine <= line && line <= b.EndLine {
			span := b.EndLine - b.StartLine
			if span < bestSpan {
				bestSpan = span
				bestID = b.ID
			}
		}
	}
	if bestID != "root" {
		return bestID
	}
	// Fallback: assign to the nearest block by line distance so the
	// diagnostic is visible in the issues filter (the outline never
	// renders the synthetic root node).
	nearestID := "root"
	nearestDist := int(^uint(0) >> 1)
	for _, b := range doc.Blocks {
		if b == nil || b.ID == "root" {
			continue
		}
		var dist int
		if line < b.StartLine {
			dist = b.StartLine - line
		} else {
			dist = line - b.EndLine
		}
		if dist < nearestDist {
			nearestDist = dist
			nearestID = b.ID
		}
	}
	return nearestID
}

// blockHasExternalIssue reports whether the block has any fmt-report diagnostics.
func blockHasExternalIssue(issues map[string][]format.ReportDiag, id string) bool {
	return len(issues[id]) > 0
}
