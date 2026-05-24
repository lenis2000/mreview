package diffreview

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefaultSidecarPathUsesNewFileAndBaseRev(t *testing.T) {
	review := &Review{
		Old: Endpoint{Kind: GitBlob, Spec: "master:paper.tex", Label: "master:paper.tex"},
		New: Endpoint{Kind: WorkingFile, Spec: "paper.tex", Path: filepath.Join(t.TempDir(), "paper.tex")},
	}

	got := DefaultSidecarPath(review)
	assert.Equal(t, review.New.Path+".mreview-diff.master.md", got)
}

func TestDiffSidecarSaveLoadAndRemap(t *testing.T) {
	oldSrc := `\section{Intro}

\begin{theorem}
\label{thm:main}
Old statement.
\end{theorem}
`
	newSrc := `\section{Intro}

\begin{theorem}
\label{thm:main}
New statement.
\end{theorem}
`
	review := buildReviewForTest(t, oldSrc, newSrc)
	pair := review.ByID["thm:main"]
	require.NotNil(t, pair)

	side := NewSidecar(review)
	side.CursorPairID = pair.ID
	side.SetReviewed(pair.ID, true)
	side.UpsertAnnotation(AnnotationForPair(review, pair, "check the changed statement"))

	path := filepath.Join(t.TempDir(), "paper.tex.mreview-diff.master.md")
	require.NoError(t, SaveSidecar(path, side))

	loaded, err := LoadSidecar(path)
	require.NoError(t, err)
	remapped := RemapSidecar(loaded, review)

	assert.Equal(t, review.Old.Spec, remapped.OldSpec)
	assert.Equal(t, review.New.Spec, remapped.NewSpec)
	assert.Equal(t, pair.ID, remapped.CursorPairID)
	assert.Equal(t, []string{pair.ID}, remapped.Reviewed)
	require.Len(t, remapped.Annotations, 1)
	assert.Equal(t, pair.ID, remapped.Annotations[0].PairID)
	assert.Equal(t, "check the changed statement", remapped.Annotations[0].Note)
	assert.Contains(t, remapped.Annotations[0].SourceQuote, "New statement")
}

func TestDiffSidecarRemapPreservesDetachedAnnotations(t *testing.T) {
	review := buildReviewForTest(t, "\\section{Intro}\n\nCurrent text.\n", "\\section{Intro}\n\nChanged current text.\n")
	loaded := &Sidecar{
		Annotations: []Annotation{{
			PairID:      "missing-pair",
			Status:      "changed",
			Side:        "new",
			SourceQuote: "old quote",
			Note:        "keep me",
		}},
		Detached: []Annotation{{
			PairID: "already-detached",
			Note:   "keep me too",
		}},
	}

	remapped := RemapSidecar(loaded, review)

	assert.Empty(t, remapped.Annotations)
	require.Len(t, remapped.Detached, 2)
	assert.Equal(t, "missing-pair", remapped.Detached[0].PairID)
	assert.Equal(t, "already-detached", remapped.Detached[1].PairID)
}

func TestDiffPairIDsPreferLabelAndNewBlockID(t *testing.T) {
	review := buildReviewForTest(t, "\\section{Intro}\n\n\\begin{theorem}\n\\label{thm:x}\nOld.\n\\end{theorem}\n", "\\section{Intro}\n\n\\begin{theorem}\n\\label{thm:x}\nNew.\n\\end{theorem}\n")
	require.NotNil(t, review.ByID["thm:x"])

	added := buildReviewForTest(t, "\\section{Intro}\n", "\\section{Intro}\n\nAdded paragraph with enough words to become one semantic block.\n")
	var addedID string
	for _, pair := range added.Pairs {
		if pair.Status == Added && pair.New != nil && strings.Contains(pair.New.Source, "Added paragraph") {
			addedID = pair.ID
		}
	}
	require.NotEmpty(t, addedID)
	assert.NotContains(t, addedID, "new:")

	deleted := buildReviewForTest(t, "\\section{Intro}\n\nDeleted paragraph with enough words to become one semantic block.\n", "\\section{Intro}\n")
	var deletedID string
	for _, pair := range deleted.Pairs {
		if pair.Status == Deleted && pair.Old != nil && strings.Contains(pair.Old.Source, "Deleted paragraph") {
			deletedID = pair.ID
		}
	}
	require.NotEmpty(t, deletedID)
	assert.True(t, strings.HasPrefix(deletedID, "old:"))
}

func TestDiffStdoutMarkdownContainsSpecsAndPairStatuses(t *testing.T) {
	review := buildReviewForTest(t, "\\section{Intro}\n\nOld paragraph with enough words for matching.\n", "\\section{Intro}\n\nNew paragraph with enough words for matching.\n")
	side := NewSidecar(review)

	var out bytes.Buffer
	require.NoError(t, EmitMarkdown(&out, side, review))
	text := out.String()

	assert.Contains(t, text, "Old: old.tex")
	assert.Contains(t, text, "New: new.tex")
	assert.Contains(t, text, "## Pair statuses")
	assert.Contains(t, text, "- changed")
}

func TestDiffStdoutJSONContainsPairs(t *testing.T) {
	review := buildReviewForTest(t, "\\section{Intro}\n\nOld paragraph with enough words for matching.\n", "\\section{Intro}\n\nNew paragraph with enough words for matching.\n")
	side := NewSidecar(review)

	var out bytes.Buffer
	require.NoError(t, Emit(&out, side, review, StdoutJSON))

	assert.Contains(t, out.String(), `"old_spec": "old.tex"`)
	assert.Contains(t, out.String(), `"status": "changed"`)
}
