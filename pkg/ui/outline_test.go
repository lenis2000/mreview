package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"mreview/pkg/format"
	"mreview/pkg/parser"
	"mreview/pkg/persist"
)

const outlineSampleTeX = `\documentclass{amsart}
\newtheorem{theorem}{Theorem}
\newtheorem{lemma}[theorem]{Lemma}
\begin{document}
\section{Intro}
Some paragraph text.

\begin{theorem}\label{thm:main}
A statement referring to \ref{fig:missing}.
\end{theorem}

\begin{proof}
First step.

Second step.
\end{proof}

\begin{figure}
\caption{A figure}
\end{figure}
\end{document}
`

func outlineDoc(t *testing.T) *parser.Document {
	t.Helper()
	doc, err := parser.Parse([]byte(outlineSampleTeX))
	require.NoError(t, err)
	require.NotNil(t, doc)
	return doc
}

// TestBuildOutline_SuppressesSentenceSplitParent asserts that a long
// paragraph segmented into sentence-children doesn't appear twice in the
// outline (parent + first child both reading the same first-snippet).
// We emit the children only.
func TestBuildOutline_SuppressesSentenceSplitParent(t *testing.T) {
	// Build a long paragraph (10+ sentence-terminated lines) so
	// segmentLongParagraphs kicks in.
	src := `\documentclass{amsart}
\begin{document}
\section{Intro}
This is the first sentence.
This is the second sentence.
This is the third sentence.
This is the fourth sentence.
This is the fifth sentence.
This is the sixth sentence.
This is the seventh sentence.
This is the eighth sentence.
\end{document}
`
	doc, err := parser.Parse([]byte(src))
	require.NoError(t, err)

	rows := BuildOutline(doc, nil, FilterAll)

	// Walk doc.Blocks: a KindParagraph parent that has KindParagraph
	// children must NOT appear in rows; its children must.
	seenIDs := map[string]bool{}
	for _, r := range rows {
		seenIDs[r.BlockID] = true
	}
	suppressedAny := false
	for _, b := range doc.Blocks {
		if b.Kind != parser.KindParagraph || len(b.ChildIDs) == 0 {
			continue
		}
		// All children also paragraph?
		allP := true
		for _, cid := range b.ChildIDs {
			if c := doc.ByID[cid]; c == nil || c.Kind != parser.KindParagraph {
				allP = false
				break
			}
		}
		if !allP {
			continue
		}
		assert.Falsef(t, seenIDs[b.ID],
			"sentence-split parent %s should be suppressed", b.ID)
		// Children must still be rendered.
		for _, cid := range b.ChildIDs {
			assert.Truef(t, seenIDs[cid],
				"sentence-split child %s should still appear", cid)
		}
		suppressedAny = true
	}
	require.True(t, suppressedAny,
		"test fixture must produce at least one segmented paragraph")
}

// TestFirstSnippet_SkipsFormattingOnly asserts that a leading line of
// pure layout commands (\\medskip, \\par, \\vspace{1ex}) doesn't end up
// as the outline title; the next prose line is used instead.
func TestFirstSnippet_SkipsFormattingOnly(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want string
	}{
		{"medskip then prose", "\\medskip\nReal content here.", "Real content here."},
		{"vspace then prose", "\\vspace{1ex}\nReal content here.", "Real content here."},
		{"par+noindent then prose", "\\par\\noindent\nReal content here.", "Real content here."},
		{"prose first wins", "Hello world.\n\\medskip", "Hello world."},
		{"only formatting", "\\medskip\n\\par\n", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, firstSnippet(tc.src, 80))
		})
	}
}

func TestBuildOutline_AllFilter_IncludesEveryBlock(t *testing.T) {
	doc := outlineDoc(t)
	rows := BuildOutline(doc, nil, FilterAll)

	assert.NotEmpty(t, rows)
	// At minimum: section, theorem, proof, proof-steps, figure.
	kinds := map[parser.Kind]int{}
	for _, r := range rows {
		kinds[doc.ByID[r.BlockID].Kind]++
	}
	assert.GreaterOrEqual(t, kinds[parser.KindSection], 1)
	assert.GreaterOrEqual(t, kinds[parser.KindTheoremLike], 1)
	assert.GreaterOrEqual(t, kinds[parser.KindProof], 1)
	assert.GreaterOrEqual(t, kinds[parser.KindFigure], 1)
}

func TestBuildOutline_Icons(t *testing.T) {
	doc := outlineDoc(t)
	rows := BuildOutline(doc, nil, FilterAll)

	seen := map[string]bool{}
	for _, r := range rows {
		seen[r.Icon] = true
	}
	assert.True(t, seen[IconSection], "section icon should appear")
	assert.True(t, seen[IconTheorem], "theorem icon should appear")
	assert.True(t, seen[IconProof], "proof icon should appear")
	assert.True(t, seen[IconFigure], "figure icon should appear")
}

func TestBuildOutline_Depth_SectionShallowerThanChildren(t *testing.T) {
	doc := outlineDoc(t)
	rows := BuildOutline(doc, nil, FilterAll)

	var sectionDepth, theoremDepth int = -1, -1
	for _, r := range rows {
		b := doc.ByID[r.BlockID]
		switch b.Kind {
		case parser.KindSection:
			if sectionDepth < 0 {
				sectionDepth = r.Depth
			}
		case parser.KindTheoremLike:
			if theoremDepth < 0 {
				theoremDepth = r.Depth
			}
		}
	}
	require.GreaterOrEqual(t, sectionDepth, 0)
	require.GreaterOrEqual(t, theoremDepth, 0)
	assert.Greater(t, theoremDepth, sectionDepth, "theorem should be nested inside section")
}

func TestBuildOutline_ReviewedFilter_HidesReviewed(t *testing.T) {
	doc := outlineDoc(t)
	// Pick the first theorem block and mark it reviewed.
	var thm *parser.Block
	for _, b := range doc.Blocks {
		if b.Kind == parser.KindTheoremLike {
			thm = b
			break
		}
	}
	require.NotNil(t, thm)
	side := &persist.Sidecar{Reviewed: []string{thm.ID}}

	rows := BuildOutline(doc, side, FilterUnreviewed)
	for _, r := range rows {
		assert.NotEqual(t, thm.ID, r.BlockID, "reviewed block should not appear in unreviewed filter")
	}

	all := BuildOutline(doc, side, FilterAll)
	found := false
	for _, r := range all {
		if r.BlockID == thm.ID {
			found = true
			break
		}
	}
	assert.True(t, found, "FilterAll must still include reviewed blocks")
}

func TestBuildOutline_AnnotatedFilter_ShowsOnlyAnnotated(t *testing.T) {
	doc := outlineDoc(t)
	var proof *parser.Block
	for _, b := range doc.Blocks {
		if b.Kind == parser.KindProof {
			proof = b
			break
		}
	}
	require.NotNil(t, proof)

	side := &persist.Sidecar{
		Annotations: []persist.Annotation{{BlockID: proof.ID, Note: "hi"}},
	}
	rows := BuildOutline(doc, side, FilterAnnotated)
	require.Len(t, rows, 1)
	assert.Equal(t, proof.ID, rows[0].BlockID)
	assert.Contains(t, rows[0].Markers, MarkerAnnotated)
}

func TestBuildOutline_IssuesFilter_SurfacesUnresolvedRefs(t *testing.T) {
	doc := outlineDoc(t)
	rows := BuildOutline(doc, nil, FilterIssues)
	// The theorem block contains a \ref{fig:missing} which is unresolved.
	require.NotEmpty(t, rows, "issues filter should find the unresolved-ref block")
	for _, r := range rows {
		b := doc.ByID[r.BlockID]
		assert.True(t, blockHasUnresolved(b) || hasUnresolvedDescendant(doc, b))
	}
}

func hasUnresolvedDescendant(doc *parser.Document, b *parser.Block) bool {
	for _, id := range b.ChildIDs {
		c := doc.ByID[id]
		if c == nil {
			continue
		}
		if blockHasUnresolved(c) || hasUnresolvedDescendant(doc, c) {
			return true
		}
	}
	return false
}

func TestCycleFilter_Rotation(t *testing.T) {
	seq := []Filter{FilterAll, FilterUnreviewed, FilterAnnotated, FilterIssues, FilterAll}
	cur := FilterAll
	for i, want := range seq {
		assert.Equal(t, want, cur, "step %d", i)
		cur = CycleFilter(cur)
	}
}

func TestDefaultFilter_ReflectsSidecarReviewed(t *testing.T) {
	assert.Equal(t, FilterAll, DefaultFilter(nil))
	assert.Equal(t, FilterAll, DefaultFilter(&persist.Sidecar{}))
	assert.Equal(t, FilterUnreviewed, DefaultFilter(&persist.Sidecar{Reviewed: []string{"x"}}))
}

func TestRenderOutline_HighlightsCursor(t *testing.T) {
	doc := outlineDoc(t)
	rows := BuildOutline(doc, nil, FilterAll)
	require.NotEmpty(t, rows)
	cursor := rows[0].BlockID

	out := RenderOutline(rows, cursor, 40, 10, true, DefaultStyles())
	assert.NotEmpty(t, out)
	// The cursor row is always padded to the full pane width so the highlight
	// extends across the line. Any trailing spaces on row[0] prove Width() was
	// applied.
	lines := strings.Split(out, "\n")
	require.NotEmpty(t, lines)
	plain := stripANSI(lines[0])
	assert.True(t, strings.HasSuffix(plain, " "), "cursor row should be padded to full width")
	assert.Contains(t, plain, "Intro", "cursor row should carry the section title")
}

func TestRenderOutline_EmptyRows(t *testing.T) {
	out := RenderOutline(nil, "", 30, 5, true, DefaultStyles())
	assert.Contains(t, out, "no blocks")
}

func TestRenderOutline_MarkersAppear(t *testing.T) {
	doc := outlineDoc(t)
	var thm *parser.Block
	for _, b := range doc.Blocks {
		if b.Kind == parser.KindTheoremLike {
			thm = b
			break
		}
	}
	require.NotNil(t, thm)
	side := &persist.Sidecar{
		Reviewed:    []string{thm.ID},
		Annotations: []persist.Annotation{{BlockID: thm.ID, Note: "x"}},
	}
	rows := BuildOutline(doc, side, FilterAll)
	out := RenderOutline(rows, "", 60, 20, true, DefaultStyles())
	assert.Contains(t, out, MarkerAnnotated)
	assert.Contains(t, out, MarkerReviewed)
	// unresolved ref marker should also appear.
	assert.Contains(t, out, MarkerUnresolved)
}

func TestBreadcrumbFor_WalksAncestors(t *testing.T) {
	doc := outlineDoc(t)
	var proofStep *parser.Block
	for _, b := range doc.Blocks {
		if b.Kind == parser.KindProofStep {
			proofStep = b
			break
		}
	}
	require.NotNil(t, proofStep)
	bc := BreadcrumbFor(doc, proofStep.ID)
	assert.Contains(t, bc, "Proof")
	assert.Contains(t, bc, " > ")
}

func TestLocatorFor_FormatsFileLineRange(t *testing.T) {
	doc := outlineDoc(t)
	rows := BuildOutline(doc, nil, FilterAll)
	require.NotEmpty(t, rows)
	loc := LocatorFor(doc, rows[0].BlockID)
	assert.Contains(t, loc, ":L")
	assert.Regexp(t, `:L\d+-L\d+$`, loc)
}

func TestTruncateToWidth(t *testing.T) {
	assert.Equal(t, "abc…", truncateToWidth("abcdefg", 4))
	assert.Equal(t, "hello", truncateToWidth("hello", 10))
	assert.Equal(t, "…", truncateToWidth("abcdefg", 1))
}

// --- External issues integration tests ---

func TestBuildOutline_IssuesFilter_IncludesExternalIssues(t *testing.T) {
	doc := outlineDoc(t)

	// Find the proof block — it has no unresolved refs by default.
	var proof *parser.Block
	for _, b := range doc.Blocks {
		if b.Kind == parser.KindProof {
			proof = b
			break
		}
	}
	require.NotNil(t, proof)

	// Verify proof is not in issues filter without external issues.
	rowsWithout := BuildOutline(doc, nil, FilterIssues)
	for _, r := range rowsWithout {
		assert.NotEqual(t, proof.ID, r.BlockID, "proof should not appear in issues filter without external issues")
	}

	// Add an external issue mapped to the proof block.
	ext := map[string][]format.ReportDiag{
		proof.ID: {{RuleID: "lint.thm-orphan-proof", Line: proof.StartLine, Message: "orphan proof"}},
	}

	// Now the proof should appear in the issues filter.
	rowsWith := BuildOutline(doc, nil, FilterIssues, ext)
	found := false
	for _, r := range rowsWith {
		if r.BlockID == proof.ID {
			found = true
			break
		}
	}
	assert.True(t, found, "proof block with external issue should appear in issues filter")
}

func TestBuildOutline_ExternalIssueMarker(t *testing.T) {
	doc := outlineDoc(t)

	// Find the figure block.
	var fig *parser.Block
	for _, b := range doc.Blocks {
		if b.Kind == parser.KindFigure {
			fig = b
			break
		}
	}
	require.NotNil(t, fig)

	ext := map[string][]format.ReportDiag{
		fig.ID: {{RuleID: "lint.todo-marker", Line: fig.StartLine, Message: "TODO found"}},
	}

	rows := BuildOutline(doc, nil, FilterAll, ext)
	for _, r := range rows {
		if r.BlockID == fig.ID {
			assert.Contains(t, r.Markers, markerForRule("lint.todo-marker"), "figure with external issue should have its rule's marker")
			return
		}
	}
	t.Fatal("figure block not found in outline")
}

func TestBuildOutline_NilExternalIssues(t *testing.T) {
	doc := outlineDoc(t)
	// Passing nil external issues should work like before.
	rows := BuildOutline(doc, nil, FilterAll, nil)
	assert.NotEmpty(t, rows)
}

func TestLoadExternalIssues_FileNotFound(t *testing.T) {
	doc := outlineDoc(t)
	ext, err := LoadExternalIssues("/nonexistent/paper.tex.fmt-report.md", doc)
	assert.NoError(t, err)
	assert.Nil(t, ext)
}

func TestLoadExternalIssues_MapsDiagsToBlocks(t *testing.T) {
	doc := outlineDoc(t)

	// Find the theorem block's line range.
	var thm *parser.Block
	for _, b := range doc.Blocks {
		if b.Kind == parser.KindTheoremLike {
			thm = b
			break
		}
	}
	require.NotNil(t, thm)

	// Write a fake report with a diagnostic at the theorem's start line.
	dir := t.TempDir()
	reportPath := filepath.Join(dir, "paper.tex.fmt-report.md")
	rpt := format.Report{
		File: "paper.tex",
		Tier: "safe",
		Verify: "skipped",
		Diags: []format.ReportDiag{
			{RuleID: "lint.thm-unlabeled", Line: thm.StartLine, Message: fmt.Sprintf("unlabeled theorem at L%d", thm.StartLine)},
		},
	}
	require.NoError(t, format.WriteReport(reportPath, rpt))

	ext, err := LoadExternalIssues(reportPath, doc)
	require.NoError(t, err)
	require.NotNil(t, ext)

	// The diagnostic should be mapped to the theorem block.
	diags, ok := ext[thm.ID]
	assert.True(t, ok, "diagnostic should be mapped to theorem block")
	assert.Len(t, diags, 1)
	assert.Equal(t, "lint.thm-unlabeled", diags[0].RuleID)
}

func TestLoadExternalIssues_NoDiags(t *testing.T) {
	doc := outlineDoc(t)

	dir := t.TempDir()
	reportPath := filepath.Join(dir, "paper.tex.fmt-report.md")
	require.NoError(t, os.WriteFile(reportPath, []byte(`# mreview fmt report — paper.tex
tier: safe
verify: skipped
`), 0o644))

	ext, err := LoadExternalIssues(reportPath, doc)
	assert.NoError(t, err)
	assert.Nil(t, ext)
}

func TestFindOwningBlock(t *testing.T) {
	doc := outlineDoc(t)

	// Line 0 or negative should map to root.
	assert.Equal(t, "root", findOwningBlock(0, doc))
	assert.Equal(t, "root", findOwningBlock(-1, doc))

	// A valid line inside the theorem should map to the theorem block.
	var thm *parser.Block
	for _, b := range doc.Blocks {
		if b.Kind == parser.KindTheoremLike {
			thm = b
			break
		}
	}
	require.NotNil(t, thm)
	bid := findOwningBlock(thm.StartLine, doc)
	assert.Equal(t, thm.ID, bid)
}

