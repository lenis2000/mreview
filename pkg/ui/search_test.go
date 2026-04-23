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

// --- SearchIndex / Match -----------------------------------------------------

func TestSearchIndex_IncludesAllNonRootBlocks(t *testing.T) {
	doc := annotDoc(t)
	idx := BuildSearchIndex(doc)
	require.NotEmpty(t, idx.Entries)
	for _, e := range idx.Entries {
		assert.NotEqual(t, "root", e.BlockID)
		assert.NotEmpty(t, e.BlockID)
	}
}

func TestSearchIndex_EmptyQueryReturnsAllInOrder(t *testing.T) {
	doc := annotDoc(t)
	idx := BuildSearchIndex(doc)
	res := idx.Match("")
	require.Len(t, res, len(idx.Entries))
	for i, e := range res {
		assert.Equal(t, idx.Entries[i].BlockID, e.BlockID)
	}
}

func TestSearchIndex_MatchRanksLabelHits(t *testing.T) {
	doc := annotDoc(t)
	idx := BuildSearchIndex(doc)
	res := idx.Match("thm:b")
	require.NotEmpty(t, res, "should match block with label thm:b")
	assert.Equal(t, "thm:b", res[0].BlockID)
}

func TestSearchIndex_MatchMissingReturnsEmpty(t *testing.T) {
	doc := annotDoc(t)
	idx := BuildSearchIndex(doc)
	res := idx.Match("xyzzy_not_here_qqq")
	assert.Empty(t, res)
}

// --- SearchPopup lifecycle ---------------------------------------------------

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

func TestSearch_TypingFiltersResults(t *testing.T) {
	m, _ := newTestModel(t)
	res, _ := m.Update(rkey('/'))
	m = res.(Model)
	p := m.Popup.(*SearchPopup)
	total := len(p.Results)

	// Type "thm:b" — should narrow to one match (the labeled theorem).
	for _, r := range "thm:b" {
		res, _ = m.Update(rkey(r))
		m = res.(Model)
	}
	p = m.Popup.(*SearchPopup)
	assert.NotZero(t, total)
	require.NotEmpty(t, p.Results, "query should produce at least one match")
	assert.Equal(t, "thm:b", p.Results[0].BlockID, "thm:b should be top result")
	assert.LessOrEqual(t, len(p.Results), total, "typing should narrow, not expand")
}

func TestSearch_EnterJumpsAndClosesPopup(t *testing.T) {
	m, _ := newTestModel(t)
	origin := m.CursorBlockID
	res, _ := m.Update(rkey('/'))
	m = res.(Model)

	for _, r := range "thm:b" {
		res, _ = m.Update(rkey(r))
		m = res.(Model)
	}

	res, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = res.(Model)

	assert.Nil(t, m.Popup)
	assert.Equal(t, "thm:b", m.CursorBlockID)
	assert.Contains(t, m.JumpStack.Back, origin, "jump stack should record origin")
}

func TestSearch_EnterNoResultClosesWithoutJump(t *testing.T) {
	m, _ := newTestModel(t)
	origin := m.CursorBlockID
	res, _ := m.Update(rkey('/'))
	m = res.(Model)

	for _, r := range "xyzzynotfound" {
		res, _ = m.Update(rkey(r))
		m = res.(Model)
	}
	p := m.Popup.(*SearchPopup)
	require.Empty(t, p.Results)

	res, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = res.(Model)
	assert.Nil(t, m.Popup)
	assert.Equal(t, origin, m.CursorBlockID)
	assert.Empty(t, m.JumpStack.Back)
}

func TestSearch_ArrowsMoveSelection(t *testing.T) {
	p := &SearchPopup{Results: []SearchEntry{{BlockID: "a"}, {BlockID: "b"}, {BlockID: "c"}}}
	p.Move(1)
	assert.Equal(t, 1, p.Cursor)
	p.Move(1)
	assert.Equal(t, 2, p.Cursor)
	p.Move(1) // clamp at end, no wrap
	assert.Equal(t, 2, p.Cursor)
	p.Move(-10)
	assert.Equal(t, 0, p.Cursor)
}

func TestSearch_SelectedEmpty(t *testing.T) {
	p := &SearchPopup{}
	assert.Equal(t, "", p.Selected())
}

// --- Annotation list popup ---------------------------------------------------

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
	// First line only, trimmed.
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
			// provided out of order
			{BlockID: "thm:b", Breadcrumb: "Theorem 2", Note: "late"},
			{BlockID: "thm:a", Breadcrumb: "Theorem 1", Note: "early"},
		},
	}
	items := BuildAnnotListItems(doc, side)
	require.Len(t, items, 2)
	// thm:a appears first in source, so should come first.
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
		// ensure origin differs from first target so jump-stack assertions hold
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
	assert.Equal(t, "thm:a", m.CursorBlockID, "cursor should follow to edited block")
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
	assert.Len(t, m.Sidecar.Annotations, 1, "annotation must not be removed yet")

	// Confirm with 'y'.
	res, _ = m.Update(rkey('y'))
	m = res.(Model)
	assert.Empty(t, m.Sidecar.Annotations)
}

// --- Display / formatting helpers --------------------------------------------

func TestSearch_DisplayContainsBreadcrumb(t *testing.T) {
	doc := annotDoc(t)
	idx := BuildSearchIndex(doc)
	var thmB *SearchEntry
	for i := range idx.Entries {
		if idx.Entries[i].BlockID == "thm:b" {
			thmB = &idx.Entries[i]
			break
		}
	}
	require.NotNil(t, thmB)
	assert.True(t,
		strings.Contains(thmB.Display, "Theorem") ||
			strings.Contains(strings.ToLower(thmB.Display), "statement"),
		"display should include breadcrumb-ish text; got %q", thmB.Display)
}
