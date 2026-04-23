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

// navSampleTeX gives us two sections, each with a labeled theorem + proof so
// tests can exercise outer/inner motions, gg/G, {/}, ref jumps, and gu.
const navSampleTeX = `\documentclass{amsart}
\newtheorem{theorem}{Theorem}
\begin{document}
\section{Intro}

\begin{theorem}\label{thm:a}
Statement A.
\end{theorem}

\begin{proof}
Step one uses \ref{thm:a}.

Step two.
\end{proof}

\section{Body}

\begin{theorem}\label{thm:b}
Statement B.
\end{theorem}

\begin{proof}
Only step \cref{thm:a}.
\end{proof}
\end{document}
`

func navDoc(t *testing.T) *parser.Document {
	t.Helper()
	doc, err := parser.Parse([]byte(navSampleTeX))
	require.NoError(t, err)
	require.NotNil(t, doc)
	return doc
}

func kindOf(doc *parser.Document, id string) parser.Kind {
	if b := doc.ByID[id]; b != nil {
		return b.Kind
	}
	return parser.KindOther
}

// --- visibleOrder / outerOrder -------------------------------------------------

func TestVisibleOrder_IncludesAllBlocksUnderFilterAll(t *testing.T) {
	doc := navDoc(t)
	order := visibleOrder(doc, nil, FilterAll)
	// At minimum: 2 sections + 2 theorems + 2 proofs + 3 proof-steps.
	var sections, theorems, proofs, steps int
	for _, id := range order {
		switch kindOf(doc, id) {
		case parser.KindSection:
			sections++
		case parser.KindTheoremLike:
			theorems++
		case parser.KindProof:
			proofs++
		case parser.KindProofStep:
			steps++
		}
	}
	assert.Equal(t, 2, sections)
	assert.Equal(t, 2, theorems)
	assert.Equal(t, 2, proofs)
	assert.Equal(t, 3, steps)
}

func TestOuterOrder_SkipsProofSteps(t *testing.T) {
	doc := navDoc(t)
	order := outerOrder(doc, nil, FilterAll)
	for _, id := range order {
		assert.NotEqual(t, parser.KindProofStep, kindOf(doc, id), "outer order must skip proof-steps (got %s)", id)
	}
}

// --- sibling / inner navigation -----------------------------------------------

func TestNextSibling_FromTheoremReachesProofThenSection(t *testing.T) {
	doc := navDoc(t)
	// Cursor on thm:a — next outer is its proof.
	next := NextSibling(doc, nil, FilterAll, "thm:a", 1)
	assert.Equal(t, "thm:a.proof", next)
	// Two outer steps forward from thm:a → section "Body".
	next2 := NextSibling(doc, nil, FilterAll, "thm:a", 2)
	assert.Equal(t, parser.KindSection, kindOf(doc, next2))
}

func TestPrevSibling_ClampsAtFirstOuter(t *testing.T) {
	doc := navDoc(t)
	first := FirstVisible(doc, nil, FilterAll)
	got := PrevSibling(doc, nil, FilterAll, first, 5)
	assert.Equal(t, first, got, "prev at first should stay at first")
}

func TestNextSibling_FromInnerBlockAnchorsToOuterAncestor(t *testing.T) {
	doc := navDoc(t)
	// Cursor on proof step inside thm:a's proof — stepping should treat
	// the proof as the anchor.
	step := "thm:a.proof.step.1"
	require.NotNil(t, doc.ByID[step])
	next := NextSibling(doc, nil, FilterAll, step, 1)
	// Next outer after thm:a.proof is section Body.
	assert.Equal(t, parser.KindSection, kindOf(doc, next))
}

func TestNextInner_WalksEveryVisibleBlock(t *testing.T) {
	doc := navDoc(t)
	first := FirstVisible(doc, nil, FilterAll)
	cur := first
	// Walk forward 4 times: section, theorem, proof, step-1, step-2.
	seq := []parser.Kind{}
	for i := 0; i < 4; i++ {
		cur = NextInner(doc, nil, FilterAll, cur, 1)
		seq = append(seq, kindOf(doc, cur))
	}
	assert.Equal(t, []parser.Kind{
		parser.KindTheoremLike,
		parser.KindProof,
		parser.KindProofStep,
		parser.KindProofStep,
	}, seq)
}

func TestPrevInner_WalksBackward(t *testing.T) {
	doc := navDoc(t)
	last := LastVisible(doc, nil, FilterAll)
	prev := PrevInner(doc, nil, FilterAll, last, 1)
	assert.NotEqual(t, last, prev)
}

// --- section motion ----------------------------------------------------------

func TestNextSection_SkipsToNextSectionBlock(t *testing.T) {
	doc := navDoc(t)
	first := FirstVisible(doc, nil, FilterAll)
	got := NextSection(doc, nil, FilterAll, first, 1)
	assert.Equal(t, parser.KindSection, kindOf(doc, got))
	assert.NotEqual(t, first, got)
}

func TestPrevSection_FromBodyReachesIntro(t *testing.T) {
	doc := navDoc(t)
	// Find the second section in order.
	order := visibleOrder(doc, nil, FilterAll)
	var second string
	seen := 0
	for _, id := range order {
		if kindOf(doc, id) == parser.KindSection {
			seen++
			if seen == 2 {
				second = id
				break
			}
		}
	}
	require.NotEmpty(t, second)
	got := PrevSection(doc, nil, FilterAll, second, 1)
	assert.Equal(t, parser.KindSection, kindOf(doc, got))
	assert.NotEqual(t, second, got)
}

// --- filter-aware motion -----------------------------------------------------

func TestNextSibling_RespectsFilter(t *testing.T) {
	doc := navDoc(t)
	// Mark the proof thm:a.proof as reviewed; with FilterUnreviewed, j from
	// thm:a should skip the proof and land on section Body.
	side := &persist.Sidecar{Reviewed: []string{"thm:a.proof"}}
	got := NextSibling(doc, side, FilterUnreviewed, "thm:a", 1)
	assert.Equal(t, parser.KindSection, kindOf(doc, got), "should skip filtered-out proof")
}

// --- ref jumping -------------------------------------------------------------

func TestFirstResolvedRef_FindsLabel(t *testing.T) {
	doc := navDoc(t)
	b := doc.ByID["thm:a.proof.step.1"]
	require.NotNil(t, b)
	target, ok := FirstResolvedRef(b)
	require.True(t, ok)
	assert.Equal(t, "thm:a", target)
}

func TestFirstResolvedRef_NoneWhenBlockEmpty(t *testing.T) {
	b := &parser.Block{}
	_, ok := FirstResolvedRef(b)
	assert.False(t, ok)
}

// A resolved cite is not a valid `go` target (cite keys live in BibEntries,
// not ByLabel). When a cite precedes a label ref in the same block, the label
// must still be picked.
func TestFirstResolvedRef_SkipsCiteInFavorOfLabel(t *testing.T) {
	b := &parser.Block{
		RefsOut: []parser.Ref{
			{Kind: "cite", Target: "Knuth1984", Resolved: true},
			{Kind: "ref", Target: "thm:main", Resolved: true},
		},
	}
	target, ok := FirstResolvedRef(b)
	require.True(t, ok)
	assert.Equal(t, "thm:main", target)
}

// A block whose only resolved refs are cites returns no target.
func TestFirstResolvedRef_NoneWhenOnlyCites(t *testing.T) {
	b := &parser.Block{
		RefsOut: []parser.Ref{
			{Kind: "cite", Target: "Knuth1984", Resolved: true},
		},
	}
	_, ok := FirstResolvedRef(b)
	assert.False(t, ok)
}

func TestBlocksReferencing_ReturnsBothReferrers(t *testing.T) {
	doc := navDoc(t)
	ids := BlocksReferencing(doc, "thm:a")
	// Both proof-steps that \ref / \cref thm:a should appear; thm:a itself
	// (the target) must NOT.
	assert.NotContains(t, ids, "thm:a")
	assert.Contains(t, ids, "thm:a.proof.step.1")
	assert.Contains(t, ids, "thm:b.proof.step.1")
}

// --- jump stack --------------------------------------------------------------

func TestJumpStack_PushPopRedo(t *testing.T) {
	s := &JumpStack{}
	s.Push("a")
	s.Push("b")
	// Pop from "c" — back becomes ["a"], forward becomes ["c"], target "b".
	target, ok := s.Pop("c")
	require.True(t, ok)
	assert.Equal(t, "b", target)
	assert.Equal(t, []string{"a"}, s.Back)
	assert.Equal(t, []string{"c"}, s.Forward)
	// Redo from "b" — forward empties, back grows to ["a","b"], target "c".
	target, ok = s.Redo("b")
	require.True(t, ok)
	assert.Equal(t, "c", target)
	assert.Equal(t, []string{"a", "b"}, s.Back)
	assert.Empty(t, s.Forward)
}

func TestJumpStack_EmptyPop(t *testing.T) {
	s := &JumpStack{}
	_, ok := s.Pop("x")
	assert.False(t, ok)
	_, ok = s.Redo("x")
	assert.False(t, ok)
}

func TestJumpStack_BoundedAtLimit(t *testing.T) {
	s := &JumpStack{Limit: 3}
	for i := 0; i < 10; i++ {
		s.Push(string(rune('a' + i)))
	}
	assert.Len(t, s.Back, 3, "Back must be capped at Limit")
	// The retained entries should be the three most recent pushes.
	assert.Equal(t, []string{"h", "i", "j"}, s.Back)
}

func TestJumpStack_PushClearsForward(t *testing.T) {
	s := &JumpStack{}
	s.Push("a")
	s.Pop("b")
	require.Len(t, s.Forward, 1)
	s.Push("c")
	assert.Empty(t, s.Forward, "any new Push must invalidate the redo history")
}

// --- parseCount --------------------------------------------------------------

func TestParseCount(t *testing.T) {
	cases := map[string]int{
		"":    1,
		"3":   3,
		"12":  12,
		"0":   1,
		"bad": 1,
	}
	for in, want := range cases {
		assert.Equal(t, want, parseCount(in), "parseCount(%q)", in)
	}
}

// --- RefListPopup ------------------------------------------------------------

func TestRefListPopup_MoveWraps(t *testing.T) {
	p := &RefListPopup{BlockIDs: []string{"a", "b", "c"}}
	p.Move(1)
	assert.Equal(t, 1, p.Index)
	p.Move(-2)
	assert.Equal(t, 2, p.Index, "negative wrap")
	p.Move(5)
	assert.Equal(t, 1, p.Index, "positive wrap")
}

func TestRefListPopup_SelectedEmpty(t *testing.T) {
	p := &RefListPopup{}
	assert.Equal(t, "", p.Selected())
}

// --- Update dispatch: motion counts -----------------------------------------

func keyMsg(s string) tea.KeyMsg {
	if s == "ctrl+c" {
		return tea.KeyMsg{Type: tea.KeyCtrlC}
	}
	if s == "ctrl+o" {
		return tea.KeyMsg{Type: tea.KeyCtrlO}
	}
	if s == "ctrl+i" {
		return tea.KeyMsg{Type: tea.KeyCtrlI}
	}
	if s == "enter" {
		return tea.KeyMsg{Type: tea.KeyEnter}
	}
	if s == "esc" {
		return tea.KeyMsg{Type: tea.KeyEsc}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

func pressAll(t *testing.T, m Model, keys ...string) Model {
	t.Helper()
	for _, k := range keys {
		res, _ := m.Update(keyMsg(k))
		m = res.(Model)
	}
	return m
}

func TestUpdate_CountPrefixRepeatsMotion(t *testing.T) {
	doc := navDoc(t)
	m := New(doc, nil)
	// Place cursor on the first visible block.
	m.CursorBlockID = FirstVisible(doc, nil, FilterAll)
	start := m.CursorBlockID

	// "2J" should move the cursor two inner steps forward.
	m = pressAll(t, m, "2", "J")
	assert.Equal(t, "", m.CountBuf, "count buffer must reset after motion")
	expected := NextInner(doc, nil, FilterAll, start, 2)
	assert.Equal(t, expected, m.CursorBlockID)
}

func TestUpdate_LeadingZeroDoesNotStartCount(t *testing.T) {
	doc := navDoc(t)
	m := New(doc, nil)
	m.CursorBlockID = FirstVisible(doc, nil, FilterAll)
	start := m.CursorBlockID

	// "0" alone is a reset (empty buffer), so the following "J" is a
	// single-step motion.
	m = pressAll(t, m, "0", "J")
	expected := NextInner(doc, nil, FilterAll, start, 1)
	assert.Equal(t, expected, m.CursorBlockID)
}

func TestUpdate_ZeroExtendsExistingCount(t *testing.T) {
	doc := navDoc(t)
	m := New(doc, nil)
	// After a digit, "0" appends to the buffer: "1","0" → 10.
	m = pressAll(t, m, "1", "0")
	assert.Equal(t, "10", m.CountBuf)
}

// --- Update dispatch: gg / G / go / gu -------------------------------------

func TestUpdate_GGJumpsToFirstAndPushesStack(t *testing.T) {
	doc := navDoc(t)
	m := New(doc, nil)
	// Move cursor away from the first block, then gg back.
	m.CursorBlockID = LastVisible(doc, nil, FilterAll)
	moved := m.CursorBlockID
	m = pressAll(t, m, "g", "g")
	first := FirstVisible(doc, nil, FilterAll)
	assert.Equal(t, first, m.CursorBlockID)
	// Jump stack should have recorded the origin.
	assert.Equal(t, []string{moved}, m.JumpStack.Back)
}

func TestUpdate_CapitalGJumpsToLast(t *testing.T) {
	doc := navDoc(t)
	m := New(doc, nil)
	last := LastVisible(doc, nil, FilterAll)
	start := m.CursorBlockID

	m = pressAll(t, m, "G")
	assert.Equal(t, last, m.CursorBlockID)
	assert.Equal(t, []string{start}, m.JumpStack.Back)
}

func TestUpdate_GoJumpsToFirstResolvedRef(t *testing.T) {
	doc := navDoc(t)
	m := New(doc, nil)
	m.CursorBlockID = "thm:a.proof.step.1"
	m = pressAll(t, m, "g", "o")
	assert.Equal(t, "thm:a", m.CursorBlockID)
	assert.Contains(t, m.JumpStack.Back, "thm:a.proof.step.1")
}

func TestUpdate_GoWithoutRefPostsStatus(t *testing.T) {
	doc := navDoc(t)
	m := New(doc, nil)
	// Section Intro carries no outgoing resolved refs in its direct source.
	m.CursorBlockID = FirstVisible(doc, nil, FilterAll)
	m = pressAll(t, m, "g", "o")
	assert.Contains(t, m.Status, "no resolved ref")
	assert.Empty(t, m.JumpStack.Back, "go without target must not push")
}

func TestUpdate_GuOpensPopupWithReferrers(t *testing.T) {
	doc := navDoc(t)
	m := New(doc, nil)
	m.CursorBlockID = "thm:a"
	m = pressAll(t, m, "g", "u")
	require.NotNil(t, m.Popup)
	p, ok := m.Popup.(*RefListPopup)
	require.True(t, ok)
	assert.Equal(t, "thm:a", p.Label)
	assert.NotEmpty(t, p.BlockIDs)
}

func TestUpdate_GuWithoutLabelPostsStatus(t *testing.T) {
	doc := navDoc(t)
	m := New(doc, nil)
	// The first section has no \label.
	m.CursorBlockID = FirstVisible(doc, nil, FilterAll)
	m = pressAll(t, m, "g", "u")
	assert.Nil(t, m.Popup)
	assert.True(t, strings.HasPrefix(m.Status, "gu:"))
}

func TestUpdate_GuPopupSelectAndEnterJumps(t *testing.T) {
	doc := navDoc(t)
	m := New(doc, nil)
	m.CursorBlockID = "thm:a"
	m = pressAll(t, m, "g", "u")
	require.NotNil(t, m.Popup)
	// Move selection down once, then press enter.
	m = pressAll(t, m, "j", "enter")
	assert.Nil(t, m.Popup, "enter should close the popup")
	assert.NotEqual(t, "thm:a", m.CursorBlockID, "cursor should jump to the selected referrer")
	assert.Contains(t, m.JumpStack.Back, "thm:a")
}

func TestUpdate_GuPopupEscClosesWithoutJumping(t *testing.T) {
	doc := navDoc(t)
	m := New(doc, nil)
	m.CursorBlockID = "thm:a"
	m = pressAll(t, m, "g", "u", "esc")
	assert.Nil(t, m.Popup)
	assert.Equal(t, "thm:a", m.CursorBlockID, "esc must not jump")
	assert.Empty(t, m.JumpStack.Back)
}

// --- Update dispatch: ctrl-o / ctrl-i ---------------------------------------

func TestUpdate_CtrlOCtrlICycleJumpStack(t *testing.T) {
	doc := navDoc(t)
	m := New(doc, nil)
	start := m.CursorBlockID
	m = pressAll(t, m, "G") // pushes start
	after := m.CursorBlockID
	require.NotEqual(t, start, after)

	m = pressAll(t, m, "ctrl+o")
	assert.Equal(t, start, m.CursorBlockID, "ctrl-o should pop to the origin")

	m = pressAll(t, m, "ctrl+i")
	assert.Equal(t, after, m.CursorBlockID, "ctrl-i should redo forward")
}

func TestUpdate_CtrlOEmptyStackPostsStatus(t *testing.T) {
	doc := navDoc(t)
	m := New(doc, nil)
	m = pressAll(t, m, "ctrl+o")
	assert.Contains(t, m.Status, "jump stack empty")
}
