package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"mreview/pkg/parser"
	"mreview/pkg/persist"
)

const annotSampleTeX = `\documentclass{amsart}
\newtheorem{theorem}{Theorem}
\begin{document}
\section{Intro}

\begin{theorem}\label{thm:a}
Statement A.
\end{theorem}

\begin{proof}
Step one.

Step two.
\end{proof}

\section{Body}

\begin{theorem}\label{thm:b}
Statement B.
\end{theorem}
\end{document}
`

func annotDoc(t *testing.T) *parser.Document {
	t.Helper()
	doc, err := parser.Parse([]byte(annotSampleTeX))
	require.NoError(t, err)
	require.NotNil(t, doc)
	return doc
}

// newTestModel wires a Model with an in-memory SaveFn that records call
// counts. Returned counter pointer lets tests assert save-on-submit.
func newTestModel(t *testing.T) (Model, *int) {
	t.Helper()
	doc := annotDoc(t)
	side := &persist.Sidecar{}
	m := New(doc, side)
	saves := 0
	m.SaveFn = func(s *persist.Sidecar) error {
		saves++
		return nil
	}
	return m, &saves
}

func firstBlockOfKind(doc *parser.Document, k parser.Kind) string {
	for _, b := range doc.Blocks {
		if b != nil && b.Kind == k {
			return b.ID
		}
	}
	return ""
}

func rkey(r rune) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}}
}

// --- Annotation popup lifecycle ----------------------------------------------

func TestAnnotation_StartOpensPopup(t *testing.T) {
	m, _ := newTestModel(t)
	res, _ := m.Update(rkey('a'))
	out := res.(Model)
	require.NotNil(t, out.Popup)
	_, ok := out.Popup.(*AnnotationPopup)
	assert.True(t, ok, "expected AnnotationPopup, got %T", out.Popup)
}

func TestAnnotation_SubmitPersists(t *testing.T) {
	m, saves := newTestModel(t)
	res, _ := m.Update(rkey('a'))
	m = res.(Model)
	p := m.Popup.(*AnnotationPopup)
	p.TA.SetValue("needs citation")

	res, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlS})
	m = res.(Model)

	assert.Nil(t, m.Popup)
	assert.Equal(t, 1, *saves, "submit should persist exactly once")
	require.Len(t, m.Sidecar.Annotations, 1)
	a := m.Sidecar.Annotations[0]
	assert.Equal(t, m.CursorBlockID, a.BlockID)
	assert.Equal(t, "needs citation", a.Note)
	assert.NotEmpty(t, a.Breadcrumb, "breadcrumb should be populated")
}

func TestAnnotation_EmptySubmitIsCancel(t *testing.T) {
	m, saves := newTestModel(t)
	res, _ := m.Update(rkey('a'))
	m = res.(Model)

	res, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = res.(Model)

	assert.Nil(t, m.Popup)
	assert.Equal(t, 0, *saves)
	assert.Empty(t, m.Sidecar.Annotations)
}

func TestAnnotation_CtrlCCancels(t *testing.T) {
	m, saves := newTestModel(t)
	res, _ := m.Update(rkey('a'))
	m = res.(Model)
	m.Popup.(*AnnotationPopup).TA.SetValue("discarded")

	res, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	m = res.(Model)

	assert.Nil(t, m.Popup)
	assert.Equal(t, 0, *saves)
	assert.Empty(t, m.Sidecar.Annotations)
}

func TestAnnotation_EditReplaces(t *testing.T) {
	m, saves := newTestModel(t)
	// first annotation
	m.Sidecar.Annotations = []persist.Annotation{{BlockID: m.CursorBlockID, Note: "original"}}

	res, _ := m.Update(rkey('e'))
	m = res.(Model)
	p := m.Popup.(*AnnotationPopup)
	assert.Equal(t, "original", p.TA.Value(), "edit should pre-fill existing note")
	assert.True(t, p.Editing)

	p.TA.SetValue("updated")
	res, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlS})
	m = res.(Model)

	assert.Equal(t, 1, *saves)
	require.Len(t, m.Sidecar.Annotations, 1, "edit should replace, not append")
	assert.Equal(t, "updated", m.Sidecar.Annotations[0].Note)
}

func TestAnnotation_EditWithoutExistingIsNoop(t *testing.T) {
	m, _ := newTestModel(t)
	res, cmd := m.Update(rkey('e'))
	assert.Nil(t, cmd)
	assert.Nil(t, res.(Model).Popup)
}

// --- Delete + confirmation ----------------------------------------------------

func TestDelete_RequiresConfirmation(t *testing.T) {
	m, _ := newTestModel(t)
	m.Sidecar.Annotations = []persist.Annotation{{BlockID: m.CursorBlockID, Note: "x"}}

	res, _ := m.Update(rkey('d'))
	m = res.(Model)
	require.NotNil(t, m.Pending, "d should arm a pending delete")
	// annotation must still exist before y/N
	assert.Len(t, m.Sidecar.Annotations, 1)
}

func TestDelete_ConfirmRemoves(t *testing.T) {
	m, saves := newTestModel(t)
	m.Sidecar.Annotations = []persist.Annotation{{BlockID: m.CursorBlockID, Note: "x"}}
	res, _ := m.Update(rkey('d'))
	m = res.(Model)

	res, _ = m.Update(rkey('y'))
	m = res.(Model)

	assert.Nil(t, m.Pending)
	assert.Empty(t, m.Sidecar.Annotations)
	assert.Equal(t, 1, *saves)
}

func TestDelete_CancelKeepsAnnotation(t *testing.T) {
	m, saves := newTestModel(t)
	m.Sidecar.Annotations = []persist.Annotation{{BlockID: m.CursorBlockID, Note: "x"}}
	res, _ := m.Update(rkey('d'))
	m = res.(Model)

	res, _ = m.Update(rkey('n'))
	m = res.(Model)

	assert.Nil(t, m.Pending)
	assert.Len(t, m.Sidecar.Annotations, 1)
	assert.Equal(t, 0, *saves, "cancelled delete should not persist")
}

func TestDelete_WithoutAnnotationIsNoop(t *testing.T) {
	m, _ := newTestModel(t)
	res, _ := m.Update(rkey('d'))
	m = res.(Model)
	assert.Nil(t, m.Pending)
}

// --- Reviewed toggle ---------------------------------------------------------

func TestReviewed_ToggleAdds(t *testing.T) {
	m, saves := newTestModel(t)
	res, _ := m.Update(rkey(' '))
	m = res.(Model)
	assert.Contains(t, m.Sidecar.Reviewed, m.CursorBlockID)
	assert.Equal(t, 1, *saves)
}

func TestReviewed_ToggleRemovesOnRepeat(t *testing.T) {
	m, _ := newTestModel(t)
	res, _ := m.Update(rkey(' '))
	m = res.(Model)
	res, _ = m.Update(rkey(' '))
	m = res.(Model)
	assert.NotContains(t, m.Sidecar.Reviewed, m.CursorBlockID)
}

func TestReviewed_AutoAdvanceUnderUnreviewedFilter(t *testing.T) {
	m, _ := newTestModel(t)
	m.Filter = FilterUnreviewed
	before := m.CursorBlockID
	res, _ := m.Update(rkey(' '))
	m = res.(Model)
	assert.Contains(t, m.Sidecar.Reviewed, before)
	assert.NotEqual(t, before, m.CursorBlockID, "cursor should advance past freshly-reviewed block")
}

func TestReviewed_NoAutoAdvanceUnderAllFilter(t *testing.T) {
	m, _ := newTestModel(t)
	m.Filter = FilterAll
	before := m.CursorBlockID
	res, _ := m.Update(rkey(' '))
	m = res.(Model)
	assert.Equal(t, before, m.CursorBlockID)
}

// --- Breadcrumb generator ----------------------------------------------------

func TestBreadcrumb_Theorem(t *testing.T) {
	doc := annotDoc(t)
	// find theorem with label thm:a
	var id string
	for _, b := range doc.Blocks {
		if b != nil && b.Label == "thm:a" {
			id = b.ID
			break
		}
	}
	require.NotEmpty(t, id)
	bc := AnnotationBreadcrumb(doc, id)
	assert.Contains(t, bc, "Theorem")
	// With aux-populated numbers this would include "1.1"; without aux, the
	// number is empty — assert only on the head.
}

func TestBreadcrumb_ProofStep(t *testing.T) {
	doc := annotDoc(t)
	// find first proof step
	id := firstBlockOfKind(doc, parser.KindProofStep)
	require.NotEmpty(t, id, "sample must contain a proof step")
	bc := AnnotationBreadcrumb(doc, id)
	assert.Contains(t, bc, "Proof of Theorem", "breadcrumb should reference the enclosing proof")
	assert.Contains(t, bc, "step [1]", "first proof step should be indexed [1]")
}

func TestBreadcrumb_Section(t *testing.T) {
	doc := annotDoc(t)
	id := firstBlockOfKind(doc, parser.KindSection)
	require.NotEmpty(t, id)
	bc := AnnotationBreadcrumb(doc, id)
	assert.True(t, strings.HasPrefix(bc, "Section"), "got %q", bc)
}

// --- EnclosingEnv -----------------------------------------------------------

func TestEnclosingEnv_FromProofStepReturnsProof(t *testing.T) {
	doc := annotDoc(t)
	step := firstBlockOfKind(doc, parser.KindProofStep)
	require.NotEmpty(t, step)
	proof := firstBlockOfKind(doc, parser.KindProof)
	require.NotEmpty(t, proof)
	assert.Equal(t, proof, EnclosingEnv(doc, step))
}

func TestEnclosingEnv_OnTheoremReturnsSelf(t *testing.T) {
	doc := annotDoc(t)
	thm := firstBlockOfKind(doc, parser.KindTheoremLike)
	require.NotEmpty(t, thm)
	assert.Equal(t, thm, EnclosingEnv(doc, thm))
}

// --- `A` is a block annotation on the cursor block ---------------------------

func TestAnnotate_CapitalA_TargetsCursorBlock(t *testing.T) {
	m, _ := newTestModel(t)
	step := firstBlockOfKind(m.Doc, parser.KindProofStep)
	require.NotEmpty(t, step)
	m.CursorBlockID = step

	res, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'A'}})
	m = res.(Model)
	p, ok := m.Popup.(*AnnotationPopup)
	require.True(t, ok)
	assert.Equal(t, step, p.TargetID, "A targets the cursor block (no env walk)")
	assert.Equal(t, 0, p.LineOffset, "A is block-level: LineOffset 0")
}

// --- Save path uses persist.Save when no SaveFn ------------------------------

func TestSaveSidecar_UsesPersistSaveWhenNoFn(t *testing.T) {
	doc := annotDoc(t)
	m := New(doc, &persist.Sidecar{})
	m.SaveFn = nil
	m.SidecarPath = t.TempDir() + "/side.mreview.md"

	res, _ := m.Update(rkey('a'))
	m = res.(Model)
	m.Popup.(*AnnotationPopup).TA.SetValue("note")
	res, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlS})
	m = res.(Model)

	got, err := persist.Load(m.SidecarPath)
	require.NoError(t, err)
	require.Len(t, got.Annotations, 1)
	assert.Equal(t, "note", got.Annotations[0].Note)
}

// --- updatePopup routing -----------------------------------------------------

func TestPopup_TextareaReceivesTyping(t *testing.T) {
	m, _ := newTestModel(t)
	res, _ := m.Update(rkey('a'))
	m = res.(Model)
	// simulate typing 'x'
	res, _ = m.Update(rkey('x'))
	m = res.(Model)
	p := m.Popup.(*AnnotationPopup)
	assert.Equal(t, "x", p.TA.Value())
}

// sanity: quit key still works when no popup
func TestAnnotation_DoesNotBreakQuit(t *testing.T) {
	m, _ := newTestModel(t)
	res, cmd := m.Update(rkey('q'))
	require.NotNil(t, cmd)
	assert.True(t, res.(Model).quitting)
}
