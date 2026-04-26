package persist

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"mreview/pkg/parser"
)

func makeDoc(t *testing.T, src string) *parser.Document {
	t.Helper()
	doc, err := parser.Parse([]byte(src))
	require.NoError(t, err)
	return doc
}

const sampleTex = `\documentclass{amsart}
\begin{document}

\section{Intro}\label{sec:intro}

\begin{theorem}\label{thm:main}
Let $X$ be a set. Then $X = X$.
\end{theorem}

\begin{proof}
By reflexivity, $X = X$.
\end{proof}

\end{document}
`

func TestRemapExactIDMatch(t *testing.T) {
	doc := makeDoc(t, sampleTex)
	b, ok := doc.ByLabel["thm:main"]
	require.True(t, ok)

	old := &Sidecar{
		Annotations: []Annotation{{
			BlockID:     b.ID,
			Breadcrumb:  "Theorem",
			File:        "paper.tex",
			StartLine:   99,
			EndLine:     100,
			SourceQuote: "old quote",
			Note:        "note",
		}},
	}
	out, detached := Remap(old, doc)
	assert.Empty(t, detached)
	require.Len(t, out.Annotations, 1)
	assert.Equal(t, b.ID, out.Annotations[0].BlockID)
	assert.Equal(t, b.StartLine, out.Annotations[0].StartLine)
	assert.Equal(t, b.EndLine, out.Annotations[0].EndLine)
}

func TestRemapLabelRescue(t *testing.T) {
	doc := makeDoc(t, sampleTex)
	b := doc.ByLabel["thm:main"]

	// Old annotation stored with the LaTeX label as its BlockID. The new
	// document's internal ID may differ, but the label index resolves it.
	old := &Sidecar{
		Annotations: []Annotation{{
			BlockID:     "thm:main",
			Breadcrumb:  "Theorem",
			File:        "paper.tex",
			StartLine:   42,
			EndLine:     43,
			SourceQuote: "unrelated",
			Note:        "note",
		}},
	}
	out, detached := Remap(old, doc)
	assert.Empty(t, detached)
	require.Len(t, out.Annotations, 1)
	assert.Equal(t, b.ID, out.Annotations[0].BlockID)
}

func TestRemapLineRangeRescue(t *testing.T) {
	doc := makeDoc(t, sampleTex)
	thm := doc.ByLabel["thm:main"]
	require.NotNil(t, thm)

	// Annotation references a stale BlockID and a stale label, but its
	// line range exactly matches the theorem's. The line-range stage
	// should reattach it to the theorem block.
	old := &Sidecar{
		Annotations: []Annotation{
			{
				BlockID:     "stale-id-not-in-doc",
				StartLine:   thm.StartLine,
				EndLine:     thm.EndLine,
				SourceQuote: "totally unrelated text that won't match by similarity",
			},
		},
	}
	mapped, detached := Remap(old, doc)
	assert.Empty(t, detached)
	require.Len(t, mapped.Annotations, 1)
	assert.Equal(t, thm.ID, mapped.Annotations[0].BlockID)
}

func TestRemapLineRangeRescueOffByOne(t *testing.T) {
	doc := makeDoc(t, sampleTex)
	thm := doc.ByLabel["thm:main"]
	require.NotNil(t, thm)

	// The annotation's saved EndLine extends one past the theorem's
	// actual end — simulates a reformat that trimmed a trailing blank.
	// Containment fails but overlap score is still high, so the rescue
	// holds.
	old := &Sidecar{
		Annotations: []Annotation{
			{
				BlockID:     "stale-id",
				StartLine:   thm.StartLine,
				EndLine:     thm.EndLine + 1,
				SourceQuote: "no match",
			},
		},
	}
	mapped, detached := Remap(old, doc)
	assert.Empty(t, detached)
	require.Len(t, mapped.Annotations, 1)
	assert.Equal(t, thm.ID, mapped.Annotations[0].BlockID)
}

func TestRemapSimilarityRescue(t *testing.T) {
	doc := makeDoc(t, sampleTex)
	proof := findKind(t, doc, parser.KindProof)

	// Use the true source of the proof so similarity = 1.0, ensuring it
	// passes the 0.85 threshold. BlockID and label are intentionally
	// mismatched to force the similarity path.
	old := &Sidecar{
		Annotations: []Annotation{{
			BlockID:     "nonexistent-id",
			Breadcrumb:  "Proof",
			File:        "paper.tex",
			StartLine:   1,
			EndLine:     2,
			SourceQuote: proof.Source,
			Note:        "note",
		}},
	}
	out, detached := Remap(old, doc)
	assert.Empty(t, detached)
	require.Len(t, out.Annotations, 1)
	assert.Equal(t, proof.ID, out.Annotations[0].BlockID)
}

func TestRemapUnmatchedAnnotationDetached(t *testing.T) {
	doc := makeDoc(t, sampleTex)

	old := &Sidecar{
		Annotations: []Annotation{{
			BlockID:     "gone",
			Breadcrumb:  "Deleted block",
			File:        "paper.tex",
			StartLine:   1,
			EndLine:     2,
			SourceQuote: "The quick brown fox jumps over the lazy dog.",
			Note:        "orphan",
		}},
	}
	out, detached := Remap(old, doc)
	assert.Empty(t, out.Annotations)
	require.Len(t, detached, 1)
	assert.Equal(t, "gone", detached[0].BlockID)
}

func TestRemapReviewedFilteredToExisting(t *testing.T) {
	doc := makeDoc(t, sampleTex)
	b := doc.ByLabel["thm:main"]

	old := &Sidecar{
		Reviewed: []string{b.ID, "thm:main", "gone-id"},
	}
	out, _ := Remap(old, doc)
	// Direct ID hit + label rescue collapse to one entry; "gone-id" is
	// dropped. Deduping matters because ToggleReviewed removes only the
	// first matching ID, so a legacy label + stable ID pair would leave
	// a block reviewed after one toggle.
	require.Len(t, out.Reviewed, 1)
	assert.Equal(t, b.ID, out.Reviewed[0])
}

func TestRemapReattachesDetachedWhenBlockReturns(t *testing.T) {
	doc := makeDoc(t, sampleTex)
	b := doc.ByLabel["thm:main"]

	// Simulate a prior session where thm:main had temporarily vanished
	// (e.g. the user was on a branch without that theorem) and the note
	// landed in Detached. Now the block is back — Remap must try the
	// same resolver against Detached entries, not just Annotations.
	old := &Sidecar{
		Detached: []Annotation{{
			BlockID:     "thm:main",
			Breadcrumb:  "Theorem",
			File:        "paper.tex",
			StartLine:   99,
			EndLine:     100,
			SourceQuote: "old quote",
			Note:        "note",
		}},
	}
	out, detached := Remap(old, doc)
	assert.Empty(t, detached, "matching block should pull the note out of detached")
	require.Len(t, out.Annotations, 1)
	assert.Equal(t, b.ID, out.Annotations[0].BlockID)
}

func TestRemapDetachedStaysDetachedWhenBlockAbsent(t *testing.T) {
	doc := makeDoc(t, sampleTex)
	old := &Sidecar{
		Detached: []Annotation{{
			BlockID:     "long-gone",
			Breadcrumb:  "Gone",
			File:        "paper.tex",
			StartLine:   1,
			EndLine:     2,
			SourceQuote: "nothing resembling current doc",
			Note:        "note",
		}},
	}
	out, detached := Remap(old, doc)
	assert.Empty(t, out.Annotations)
	require.Len(t, detached, 1)
	assert.Equal(t, "long-gone", detached[0].BlockID)
}

func TestRemapCursorRescueAndDrop(t *testing.T) {
	doc := makeDoc(t, sampleTex)
	b := doc.ByLabel["thm:main"]

	t.Run("rescue-via-label", func(t *testing.T) {
		s, _ := Remap(&Sidecar{Cursor: "thm:main"}, doc)
		assert.Equal(t, b.ID, s.Cursor)
	})
	t.Run("drop-missing", func(t *testing.T) {
		s, _ := Remap(&Sidecar{Cursor: "gone"}, doc)
		assert.Empty(t, s.Cursor)
	})
	t.Run("keep-existing", func(t *testing.T) {
		s, _ := Remap(&Sidecar{Cursor: b.ID}, doc)
		assert.Equal(t, b.ID, s.Cursor)
	})
}

func TestSimilarityHelpers(t *testing.T) {
	assert.InDelta(t, 1.0, similarity("abc", "abc"), 1e-9)
	assert.InDelta(t, 0.0, similarity("abc", ""), 1e-9)
	assert.Greater(t, similarity(normaliseForSimilarity("hello world"), normaliseForSimilarity("hello  world\n")), 0.85)
	assert.Less(t, similarity("abc", "xyz123"), 0.5)
	assert.Equal(t, "hello world", normaliseForSimilarity("hello \n\t world"))
}

func findKind(t *testing.T, doc *parser.Document, k parser.Kind) *parser.Block {
	t.Helper()
	for _, b := range doc.Blocks {
		if b.Kind == k {
			return b
		}
	}
	t.Fatalf("no block of kind %v", k)
	return nil
}
