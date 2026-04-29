package ui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"mreview/pkg/parser"
	"mreview/pkg/persist"
)

// --- findSourceMatch --------------------------------------------------------

func TestFindSourceMatch_ForwardFromBeginning(t *testing.T) {
	doc := annotDoc(t)
	abs, wrapped, ok := findSourceMatch(doc, 1, "Statement A", true)
	require.True(t, ok)
	assert.False(t, wrapped)
	assert.Equal(t, "Statement A.", lineAt(doc, abs))
}

func TestFindSourceMatch_ForwardWraps(t *testing.T) {
	doc := annotDoc(t)
	first, _, ok := findSourceMatch(doc, 1, "Statement A", true)
	require.True(t, ok)
	abs, wrapped, ok := findSourceMatch(doc, first+1, "Statement A", true)
	require.True(t, ok, "search should wrap to the start")
	assert.True(t, wrapped, "should report wrap when match required scanning past EOF")
	assert.Equal(t, first, abs)
}

func TestFindSourceMatch_Backward(t *testing.T) {
	doc := annotDoc(t)
	end := docLineCount(doc)
	abs, _, ok := findSourceMatch(doc, end, "Statement", false)
	require.True(t, ok)
	assert.Contains(t, lineAt(doc, abs), "Statement B")
}

func TestFindSourceMatch_SmartCase(t *testing.T) {
	doc := annotDoc(t)
	abs, _, ok := findSourceMatch(doc, 1, "statement", true)
	require.True(t, ok)
	assert.Contains(t, lineAt(doc, abs), "Statement")
}

func TestFindSourceMatch_NoMatch(t *testing.T) {
	doc := annotDoc(t)
	_, _, ok := findSourceMatch(doc, 1, "xyzzy_no_such_word", true)
	assert.False(t, ok)
}

// --- SearchPopup lifecycle --------------------------------------------------

func TestSearch_SlashOpensPopup(t *testing.T) {
	m, _ := newTestModel(t)
	res, _ := m.Update(rkey('/'))
	m = res.(Model)
	require.NotNil(t, m.Popup)
	_, ok := m.Popup.(*SearchPopup)
	assert.True(t, ok, "expected SearchPopup, got %T", m.Popup)
}

func TestSearch_EscCloses(t *testing.T) {
	m, _ := newTestModel(t)
	res, _ := m.Update(rkey('/'))
	m = res.(Model)
	require.NotNil(t, m.Popup)

	res, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = res.(Model)
	assert.Nil(t, m.Popup)
}

func TestSearch_EnterJumpsAndClosesPopup(t *testing.T) {
	m, _ := newTestModel(t)
	// Pin the cursor on a section so the jump target differs.
	m.CursorBlockID = firstBlockOfKind(annotDoc(t), parser.KindSection)
	origin := m.CursorBlockID
	res, _ := m.Update(rkey('/'))
	m = res.(Model)

	for _, r := range "Statement B" {
		res, _ = m.Update(rkey(r))
		m = res.(Model)
	}
	res, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = res.(Model)

	assert.Nil(t, m.Popup)
	assert.Equal(t, "Statement B", m.LastSearch)
	assert.NotEqual(t, origin, m.CursorBlockID, "cursor should jump to a different block")
	assert.Contains(t, m.JumpStack.Back, origin, "jump stack should record origin")
}

func TestSearch_EnterEmptyClosesNoMove(t *testing.T) {
	m, _ := newTestModel(t)
	origin := m.CursorBlockID
	res, _ := m.Update(rkey('/'))
	m = res.(Model)

	res, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = res.(Model)
	assert.Nil(t, m.Popup)
	assert.Equal(t, origin, m.CursorBlockID)
	assert.Empty(t, m.LastSearch)
}

func TestSearch_NoMatchReportsStatus(t *testing.T) {
	m, _ := newTestModel(t)
	origin := m.CursorBlockID
	res, _ := m.Update(rkey('/'))
	m = res.(Model)

	for _, r := range "xyzzynotfound" {
		res, _ = m.Update(rkey(r))
		m = res.(Model)
	}
	res, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = res.(Model)
	assert.Nil(t, m.Popup)
	assert.Equal(t, origin, m.CursorBlockID)
	assert.Contains(t, m.Status, "not found")
}

// --- n / N repeat -----------------------------------------------------------

func TestSearch_NRepeatsForward(t *testing.T) {
	m, _ := newTestModel(t)
	m.LastSearch = "Statement"
	// Sit at the top of the document.
	m.CursorBlockID = firstBlockOfKind(annotDoc(t), parser.KindSection)
	m.SourceLineCursor = 1
	res, _ := m.Update(rkey('n'))
	m = res.(Model)
	abs, _ := absoluteCursorLine(m)
	assert.Contains(t, lineAt(m.Doc, abs), "Statement A")

	res, _ = m.Update(rkey('n'))
	m = res.(Model)
	abs, _ = absoluteCursorLine(m)
	assert.Contains(t, lineAt(m.Doc, abs), "Statement B")
}

func TestSearch_NoPriorSearchIsNoop(t *testing.T) {
	m, _ := newTestModel(t)
	origin := m.CursorBlockID
	res, _ := m.Update(rkey('n'))
	m = res.(Model)
	assert.Equal(t, origin, m.CursorBlockID)
	assert.Contains(t, m.Status, "no previous pattern")
}

// --- helpers ----------------------------------------------------------------

func lineAt(doc *parser.Document, n int) string {
	if doc == nil || n < 1 {
		return ""
	}
	src := string(doc.Source)
	start := 0
	cur := 1
	for i := 0; i < len(src); i++ {
		if cur == n && (src[i] == '\n' || i == len(src)-1) {
			end := i
			if src[i] == '\n' {
				return src[start:end]
			}
			return src[start : end+1]
		}
		if src[i] == '\n' {
			cur++
			start = i + 1
		}
	}
	if cur == n {
		return src[start:]
	}
	return ""
}

func docLineCount(doc *parser.Document) int {
	src := string(doc.Source)
	if src == "" {
		return 0
	}
	n := 1
	for i := 0; i < len(src); i++ {
		if src[i] == '\n' && i < len(src)-1 {
			n++
		}
	}
	return n
}

// --- Annotation list popup --------------------------------------------------

func TestAnnotList_EmptyShowsStatus(t *testing.T) {
	m, _ := newTestModel(t)
	res, _ := m.Update(rkey('@'))
	m = res.(Model)
	assert.Nil(t, m.Popup)
	assert.Contains(t, m.Status, "no annotations")
}

func TestAnnotList_OpensWithItems(t *testing.T) {
	m, _ := newTestModel(t)
	m.Sidecar.Annotations = []persist.Annotation{
		{BlockID: "thm:a", Breadcrumb: "Theorem 1", Note: "first note\nsecond line"},
		{BlockID: "thm:b", Breadcrumb: "Theorem 2", Note: "other"},
	}
	res, _ := m.Update(rkey('@'))
	m = res.(Model)
	require.NotNil(t, m.Popup)
	p, ok := m.Popup.(*AnnotListPopup)
	require.True(t, ok)
	require.Len(t, p.Items, 2)
	assert.Equal(t, "first note", p.Items[0].FirstLine)
}

func TestAnnotList_DisplayFormat(t *testing.T) {
	it := AnnotListItem{Breadcrumb: "Theorem 3.2", FirstLine: "needs citation"}
	assert.Equal(t, "Theorem 3.2 — needs citation", it.Display())
}

func TestAnnotList_OrderedByDocumentPosition(t *testing.T) {
	doc := annotDoc(t)
	side := &persist.Sidecar{
		Annotations: []persist.Annotation{
			{BlockID: "thm:b", Breadcrumb: "Theorem 2", Note: "late"},
			{BlockID: "thm:a", Breadcrumb: "Theorem 1", Note: "early"},
		},
	}
	items := BuildAnnotListItems(doc, side)
	require.Len(t, items, 2)
	assert.Equal(t, "thm:a", items[0].BlockID)
	assert.Equal(t, "thm:b", items[1].BlockID)
}

func TestAnnotList_DetachedBlockAppearsLast(t *testing.T) {
	doc := annotDoc(t)
	side := &persist.Sidecar{
		Annotations: []persist.Annotation{
			{BlockID: "missing-id", Breadcrumb: "Detached", Note: "orphan"},
			{BlockID: "thm:a", Breadcrumb: "Theorem 1", Note: "present"},
		},
	}
	items := BuildAnnotListItems(doc, side)
	require.Len(t, items, 2)
	assert.Equal(t, "thm:a", items[0].BlockID)
	assert.Equal(t, "missing-id", items[1].BlockID)
}

func TestAnnotList_EnterJumps(t *testing.T) {
	m, _ := newTestModel(t)
	origin := m.CursorBlockID
	m.Sidecar.Annotations = []persist.Annotation{
		{BlockID: "thm:a", Breadcrumb: "Theorem 1", Note: "note a"},
		{BlockID: "thm:b", Breadcrumb: "Theorem 2", Note: "note b"},
	}
	if origin == "thm:a" {
		m.CursorBlockID = firstBlockOfKind(annotDoc(t), parser.KindSection)
		origin = m.CursorBlockID
	}
	res, _ := m.Update(rkey('@'))
	m = res.(Model)
	require.NotNil(t, m.Popup)

	res, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = res.(Model)
	assert.Nil(t, m.Popup)
	assert.Equal(t, "thm:a", m.CursorBlockID)
	assert.Contains(t, m.JumpStack.Back, origin)
}

func TestAnnotList_JKNavigates(t *testing.T) {
	m, _ := newTestModel(t)
	m.Sidecar.Annotations = []persist.Annotation{
		{BlockID: "thm:a", Breadcrumb: "A", Note: "a"},
		{BlockID: "thm:b", Breadcrumb: "B", Note: "b"},
	}
	res, _ := m.Update(rkey('@'))
	m = res.(Model)
	res, _ = m.Update(rkey('j'))
	m = res.(Model)
	p := m.Popup.(*AnnotListPopup)
	assert.Equal(t, 1, p.Cursor)

	res, _ = m.Update(rkey('k'))
	m = res.(Model)
	p = m.Popup.(*AnnotListPopup)
	assert.Equal(t, 0, p.Cursor)
}

func TestAnnotList_EscCloses(t *testing.T) {
	m, _ := newTestModel(t)
	m.Sidecar.Annotations = []persist.Annotation{{BlockID: "thm:a", Note: "x"}}
	res, _ := m.Update(rkey('@'))
	m = res.(Model)
	require.NotNil(t, m.Popup)
	res, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = res.(Model)
	assert.Nil(t, m.Popup)
}

func TestAnnotList_EOpensEditPopup(t *testing.T) {
	m, _ := newTestModel(t)
	m.Sidecar.Annotations = []persist.Annotation{
		{BlockID: "thm:a", Breadcrumb: "A", Note: "existing"},
	}
	res, _ := m.Update(rkey('@'))
	m = res.(Model)
	res, _ = m.Update(rkey('e'))
	m = res.(Model)

	require.NotNil(t, m.Popup)
	p, ok := m.Popup.(*AnnotationPopup)
	require.True(t, ok, "e in @ popup should open edit annotation popup")
	assert.Equal(t, "thm:a", p.TargetID)
	assert.True(t, p.Editing)
	assert.Equal(t, "existing", p.TA.Value())
	assert.Equal(t, "thm:a", m.CursorBlockID)
}

func TestAnnotList_DBeginsDeleteConfirm(t *testing.T) {
	m, _ := newTestModel(t)
	m.Sidecar.Annotations = []persist.Annotation{
		{BlockID: "thm:a", Breadcrumb: "A", Note: "gone"},
	}
	res, _ := m.Update(rkey('@'))
	m = res.(Model)
	res, _ = m.Update(rkey('d'))
	m = res.(Model)

	assert.Nil(t, m.Popup, "@ popup should close on d")
	require.NotNil(t, m.Pending, "d should arm a pending delete")
	assert.Equal(t, "thm:a", m.Pending.TargetID)
	assert.Len(t, m.Sidecar.Annotations, 1)

	res, _ = m.Update(rkey('y'))
	m = res.(Model)
	assert.Empty(t, m.Sidecar.Annotations)
}

// --- Annotation delete + undo (round-trip) ----------------------------------

func TestAnnotDelete_UndoRestoresAtSameIndex(t *testing.T) {
	m, _ := newTestModel(t)
	m.Sidecar.Annotations = []persist.Annotation{
		{BlockID: "thm:a", Breadcrumb: "A", Note: "first"},
		{BlockID: "thm:b", Breadcrumb: "B", Note: "second"},
	}
	m.CursorBlockID = "thm:a"
	res, _ := m.Update(rkey('d'))
	m = res.(Model)
	require.NotNil(t, m.Pending)
	res, _ = m.Update(rkey('y'))
	m = res.(Model)
	require.Len(t, m.Sidecar.Annotations, 1)
	assert.Equal(t, "thm:b", m.Sidecar.Annotations[0].BlockID)

	res, _ = m.Update(rkey('u'))
	m = res.(Model)
	require.Len(t, m.Sidecar.Annotations, 2)
	assert.Equal(t, "thm:a", m.Sidecar.Annotations[0].BlockID, "undo must restore original order")
	assert.Equal(t, "thm:b", m.Sidecar.Annotations[1].BlockID)
}

func TestAnnotDelete_UndoRedoRoundTrip(t *testing.T) {
	m, _ := newTestModel(t)
	m.Sidecar.Annotations = []persist.Annotation{
		{BlockID: "thm:a", Note: "first"},
	}
	m.CursorBlockID = "thm:a"
	res, _ := m.Update(rkey('d'))
	m = res.(Model)
	res, _ = m.Update(rkey('y'))
	m = res.(Model)
	assert.Empty(t, m.Sidecar.Annotations)

	res, _ = m.Update(rkey('u'))
	m = res.(Model)
	require.Len(t, m.Sidecar.Annotations, 1)

	res, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlR})
	m = res.(Model)
	assert.Empty(t, m.Sidecar.Annotations)
}
