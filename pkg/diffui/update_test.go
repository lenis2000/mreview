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

	next, _ := m.startAnnotation(false)
	m = next.(Model)
	if m.Popup == nil || m.Popup.PairID != "changed" {
		t.Fatalf("expected annotation popup for changed pair")
	}
	m.Popup.TA.SetValue("first note")
	m = m.submitAnnotation()
	if got := m.Annotations["changed"]; got != "first note" {
		t.Fatalf("annotation note = %q, want first note", got)
	}
	if notes := m.Sidecar.AnnotationNotes(); notes["changed"] != "first note" {
		t.Fatalf("sidecar annotation was not updated: %#v", notes)
	}

	next, _ = m.startAnnotation(true)
	m = next.(Model)
	m.Popup.TA.SetValue("updated note")
	m = m.submitAnnotation()
	if got := m.Annotations["changed"]; got != "updated note" {
		t.Fatalf("annotation note = %q, want updated note", got)
	}

	m = m.beginDelete()
	if m.Pending == nil {
		t.Fatalf("expected pending delete confirmation")
	}
	m = m.confirmDelete(true)
	if _, ok := m.Annotations["changed"]; ok {
		t.Fatalf("annotation was not removed from map")
	}
	if notes := m.Sidecar.AnnotationNotes(); notes["changed"] != "" {
		t.Fatalf("annotation was not removed from sidecar: %#v", notes)
	}
}

func pressKey(t *testing.T, m Model, key string) Model {
	t.Helper()
	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)}
	next, _ := m.Update(msg)
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
