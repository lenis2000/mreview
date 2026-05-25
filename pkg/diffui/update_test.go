package diffui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestCursorMovementIsPairBased(t *testing.T) {
	m := New(fixtureReview(), Options{})
	if currentID(m) != "changed" {
		t.Fatalf("default cursor = %s, want changed", currentID(m))
	}

	m = pressKey(t, m, "j")
	if currentID(m) != "added" {
		t.Fatalf("j moved cursor to %s, want added", currentID(m))
	}

	m = pressKey(t, m, "J")
	if currentID(m) != "moved" {
		t.Fatalf("J moved cursor to %s, want moved", currentID(m))
	}

	m = pressKey(t, m, "K")
	if currentID(m) != "changed" {
		t.Fatalf("K moved cursor to %s, want changed", currentID(m))
	}

	m = pressKey(t, m, "G")
	if currentID(m) != "moved" {
		t.Fatalf("G moved cursor to %s, want moved", currentID(m))
	}

	m = pressKey(t, m, "g")
	m = pressKey(t, m, "g")
	if currentID(m) != "changed" {
		t.Fatalf("gg moved cursor to %s, want changed", currentID(m))
	}
}

func TestSectionNavigationUsesPairSectionPaths(t *testing.T) {
	m := New(fixtureReview(), Options{})
	if currentID(m) != "changed" {
		t.Fatalf("default cursor = %s, want changed", currentID(m))
	}

	m = pressKey(t, m, "}")
	if currentID(m) != "deleted" {
		t.Fatalf("} moved cursor to %s, want first pair in Methods", currentID(m))
	}

	m = pressKey(t, m, "{")
	if currentID(m) != "added" {
		t.Fatalf("{ moved cursor to %s, want last pair in Intro", currentID(m))
	}
}

func TestHelpIncludesDiffSpecificKeys(t *testing.T) {
	help := RenderHelpBody(120, false)
	for _, needle := range []string{
		"e/E edit new file only when --allow-modifications is supplied",
		"ctrl+a edit annotation",
		"d delete annotation",
		"Z opens old+new in Zed",
		"u undo last diff-mode edit",
		"ctrl+r redo undone diff-mode edit",
		"[/] select previous/next source line (PDF anchor)",
	} {
		if !strings.Contains(help, needle) {
			t.Fatalf("help missing %q in:\n%s", needle, help)
		}
	}
}

func TestToggleReviewedAutoAdvancesChangedAndUnreviewedFilters(t *testing.T) {
	m := New(fixtureReview(), Options{})
	if currentID(m) != "changed" {
		t.Fatalf("default cursor = %s, want changed", currentID(m))
	}

	m = pressKey(t, m, " ")
	if !m.Reviewed["changed"] {
		t.Fatalf("changed pair was not marked reviewed")
	}
	if currentID(m) != "added" {
		t.Fatalf("space under changed filter moved to %s, want added", currentID(m))
	}
	if got := m.Sidecar.ReviewedSet(); !got["changed"] {
		t.Fatalf("sidecar reviewed set was not updated")
	}

	m.Filter = FilterUnreviewed
	m = pressKey(t, m, " ")
	if !m.Reviewed["added"] {
		t.Fatalf("added pair was not marked reviewed")
	}
	if currentID(m) != "deleted" {
		t.Fatalf("space under unreviewed filter moved to %s, want deleted", currentID(m))
	}
}

func TestAnnotationAddEditAndDelete(t *testing.T) {
	m := New(fixtureReview(), Options{})

	m = pressKey(t, m, "a")
	if m.Popup == nil || m.Popup.PairID != "changed" {
		t.Fatalf("expected annotation popup for changed pair")
	}
	m = pressRunes(t, m, "first note")
	m = pressSpecial(t, m, tea.KeyEnter)
	if got := m.Annotations["changed"]; got != "first note" {
		t.Fatalf("annotation note = %q, want first note", got)
	}
	if notes := m.Sidecar.AnnotationNotes(); notes["changed"] != "first note" {
		t.Fatalf("sidecar annotation was not updated: %#v", notes)
	}

	m = pressSpecial(t, m, tea.KeyCtrlA)
	m.Popup.TA.SetValue("updated note")
	m = pressSpecial(t, m, tea.KeyEnter)
	if got := m.Annotations["changed"]; got != "updated note" {
		t.Fatalf("annotation note = %q, want updated note", got)
	}

	m = pressKey(t, m, "d")
	if m.Pending == nil {
		t.Fatalf("expected pending delete confirmation")
	}
	m = pressKey(t, m, "y")
	if _, ok := m.Annotations["changed"]; ok {
		t.Fatalf("annotation was not removed from map")
	}
	if notes := m.Sidecar.AnnotationNotes(); notes["changed"] != "" {
		t.Fatalf("annotation was not removed from sidecar: %#v", notes)
	}
}

func TestLayoutToggleAndPaneResize(t *testing.T) {
	m := New(fixtureReview(), Options{})
	m.Width = 140
	m.Height = 30

	m = pressKey(t, m, "\\")
	if m.Layout != LayoutStacked {
		t.Fatalf("\\ should switch to stacked layout")
	}
	view := m.View()
	if !strings.Contains(view, "Old source") || !strings.Contains(view, "New source") || !strings.Contains(view, "PDF") {
		t.Fatalf("stacked view should retain old/new top panes and PDF pane:\n%s", view)
	}

	oldSplit := m.SourceSplitFrac
	m = pressKey(t, m, "right") // outline -> old
	if m.Focus != PaneOldSource {
		t.Fatalf("focus after right = %s, want old", m.Focus)
	}
	m = pressKey(t, m, "left") // old -> outline
	if m.Focus != PaneOutline {
		t.Fatalf("focus after left = %s, want outline", m.Focus)
	}
	m = pressKey(t, m, "l") // outline -> old
	if m.Focus != PaneOldSource {
		t.Fatalf("focus after l = %s, want old", m.Focus)
	}
	m = pressKey(t, m, ">")
	if m.SourceSplitFrac <= oldSplit {
		t.Fatalf("> on old source should grow old side split: before %.2f after %.2f", oldSplit, m.SourceSplitFrac)
	}

	m.Focus = PanePDF
	oldTop := m.StackedTopFrac
	m = pressKey(t, m, ">")
	if m.StackedTopFrac >= oldTop {
		t.Fatalf("> on stacked PDF should grow bottom PDF by shrinking top: before %.2f after %.2f", oldTop, m.StackedTopFrac)
	}
}

func TestFocusedSourcePaneScrollsWithinChunk(t *testing.T) {
	m := New(fixtureReview(), Options{})
	m.Cursor = pairIndexByID(m.Review, "changed")
	m.Focus = PaneNewSource
	m = pressKey(t, m, "j")
	if got := m.SourceLineCursor; got != 2 {
		t.Fatalf("j with source focus should scroll source line, got cursor %d", got)
	}
	if currentID(m) != "changed" {
		t.Fatalf("source scroll should not move semantic pair, got %s", currentID(m))
	}
}

func TestSourceLineSelectionDrivesInlineEditLine(t *testing.T) {
	m := New(fixtureReview(), Options{AllowModifications: true, RequestedAllowMods: true})
	m.Cursor = pairIndexByID(m.Review, "changed")
	if got := m.currentNewLine(); got != 3 {
		t.Fatalf("initial selected line = %d, want 3", got)
	}
	m = pressKey(t, m, "]")
	if got := m.currentNewLine(); got != 4 {
		t.Fatalf("after ] selected line = %d, want 4", got)
	}
	if !strings.Contains(m.Status, "4") {
		t.Fatalf("source-line status = %q, want line 4", m.Status)
	}
	m = pressKey(t, m, "j")
	if got := m.SourceLineCursor; got != 1 {
		t.Fatalf("pair navigation should reset source cursor to 1, got %d", got)
	}
}

func pressKey(t *testing.T, m Model, key string) Model {
	t.Helper()
	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)}
	if len([]rune(key)) == 1 && key[0] < 32 {
		msg = tea.KeyMsg{Type: tea.KeyType(key[0])}
	}
	next, _ := m.Update(msg)
	nm, ok := next.(Model)
	if !ok {
		t.Fatalf("unexpected model type %T", next)
	}
	return nm
}

func pressRunes(t *testing.T, m Model, value string) Model {
	t.Helper()
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(value)})
	nm, ok := next.(Model)
	if !ok {
		t.Fatalf("unexpected model type %T", next)
	}
	return nm
}

func pressSpecial(t *testing.T, m Model, typ tea.KeyType) Model {
	t.Helper()
	next, _ := m.Update(tea.KeyMsg{Type: typ})
	nm, ok := next.(Model)
	if !ok {
		t.Fatalf("unexpected model type %T", next)
	}
	return nm
}

func currentID(m Model) string {
	pair := m.CurrentPair()
	if pair == nil {
		return ""
	}
	return pair.ID
}
