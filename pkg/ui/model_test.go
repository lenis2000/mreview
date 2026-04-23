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

const sampleTeX = `\documentclass{amsart}
\newtheorem{theorem}{Theorem}
\begin{document}
\section{Intro}
Some text.

\begin{theorem}\label{thm:main}
A statement.
\end{theorem}

\begin{proof}
First step.

Second step.
\end{proof}
\end{document}
`

func parsedSample(t *testing.T) *parser.Document {
	t.Helper()
	doc, err := parser.Parse([]byte(sampleTeX))
	require.NoError(t, err)
	require.NotNil(t, doc)
	return doc
}

func TestNew_DefaultsCursorToFirstBlock(t *testing.T) {
	doc := parsedSample(t)
	m := New(doc, nil)
	require.NotNil(t, m.Sidecar, "nil sidecar must be replaced by an empty one")
	assert.NotEmpty(t, m.CursorBlockID)
	assert.Equal(t, doc.Root.ChildIDs[0], m.CursorBlockID)
	assert.Equal(t, FilterAll, m.Filter)
	assert.Equal(t, PaneOutline, m.Focus)
}

func TestNew_HonorsSidecarCursor(t *testing.T) {
	doc := parsedSample(t)
	// pick a deeper block to confirm New honors a real ID, not just root child.
	require.GreaterOrEqual(t, len(doc.Blocks), 3)
	target := doc.Blocks[2].ID
	side := &persist.Sidecar{Cursor: target}
	m := New(doc, side)
	assert.Equal(t, target, m.CursorBlockID)
}

func TestNew_FallsBackOnUnknownCursor(t *testing.T) {
	doc := parsedSample(t)
	side := &persist.Sidecar{Cursor: "nope-not-real"}
	m := New(doc, side)
	assert.Equal(t, doc.Root.ChildIDs[0], m.CursorBlockID)
}

func TestInit_ReturnsNil(t *testing.T) {
	m := New(parsedSample(t), nil)
	assert.Nil(t, m.Init())
}

func TestUpdate_QuitKey(t *testing.T) {
	m := New(parsedSample(t), nil)
	for _, key := range []string{"q", "ctrl+c"} {
		t.Run(key, func(t *testing.T) {
			km := tea.KeyMsg{Type: keyType(key), Runes: keyRunes(key)}
			updated, cmd := m.Update(km)
			require.NotNil(t, cmd, "%s should issue a quit cmd", key)
			out := updated.(Model)
			assert.True(t, out.quitting)
			// the quit cmd is tea.Quit — invoking it returns tea.QuitMsg.
			msg := cmd()
			_, ok := msg.(tea.QuitMsg)
			assert.True(t, ok, "expected QuitMsg, got %T", msg)
		})
	}
}

func TestUpdate_UnboundKeyIsNoOp(t *testing.T) {
	m := New(parsedSample(t), nil)
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	assert.Nil(t, cmd)
	assert.False(t, updated.(Model).quitting)
}

func TestUpdate_WindowSize(t *testing.T) {
	m := New(parsedSample(t), nil)
	updated, cmd := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	assert.Nil(t, cmd)
	out := updated.(Model)
	assert.Equal(t, 120, out.Width)
	assert.Equal(t, 40, out.Height)
}

func TestView_LoadingBeforeSize(t *testing.T) {
	m := New(parsedSample(t), nil)
	v := m.View()
	assert.Contains(t, v, "loading")
}

func TestView_ThreePanesAndStatusBar(t *testing.T) {
	m := New(parsedSample(t), nil)
	m.Width = 120
	m.Height = 30
	v := m.View()

	assert.Contains(t, v, "Outline")
	assert.Contains(t, v, "Source")
	assert.Contains(t, v, "PDF")
	assert.Contains(t, v, "filter:all")
	assert.Contains(t, v, "q quit")

	// status row should be present at the bottom; output split into rows
	// must have at least Height rows. Lipgloss may add ANSI styling so we
	// check on rendered line count, not byte count.
	rows := strings.Split(v, "\n")
	assert.GreaterOrEqual(t, len(rows), 5, "should render multiple rows of pane content + status")
}

func TestPaneWidths_Splits(t *testing.T) {
	tests := []struct{ width int }{{width: 80}, {width: 100}, {width: 200}}
	for _, tc := range tests {
		o, s, p := paneWidths(tc.width)
		assert.Equal(t, tc.width, o+s+p, "widths should sum exactly to total")
		assert.Greater(t, o, 0)
		assert.Greater(t, s, 0)
		assert.Greater(t, p, 0)
		// outline ~25%, pdf ~35%, source ~40% — confirm rough proportions.
		assert.InDelta(t, float64(tc.width)*0.25, float64(o), 1.5)
		assert.InDelta(t, float64(tc.width)*0.35, float64(p), 1.5)
	}
}

func TestPaneWidths_Tiny(t *testing.T) {
	o, s, p := paneWidths(3)
	// each pane must get at least 1 column even when total is too small for
	// the percentage split.
	assert.GreaterOrEqual(t, o, 1)
	assert.GreaterOrEqual(t, s, 1)
	assert.GreaterOrEqual(t, p, 1)
}

func TestFilter_String(t *testing.T) {
	assert.Equal(t, "all", FilterAll.String())
	assert.Equal(t, "unreviewed", FilterUnreviewed.String())
	assert.Equal(t, "annotated", FilterAnnotated.String())
	assert.Equal(t, "issues", FilterIssues.String())
}

// keyType / keyRunes synthesise a tea.KeyMsg. The KeyMsg.String()
// implementation uses (Type, Runes) so we set both consistently.
func keyType(s string) tea.KeyType {
	switch s {
	case "ctrl+c":
		return tea.KeyCtrlC
	default:
		return tea.KeyRunes
	}
}

func keyRunes(s string) []rune {
	if strings.HasPrefix(s, "ctrl+") {
		return nil
	}
	return []rune(s)
}
