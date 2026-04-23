package ui

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

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
