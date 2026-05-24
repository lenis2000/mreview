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
		"Z opens old+new in Zed",
	} {
		if !strings.Contains(help, needle) {
			t.Fatalf("help missing %q in:\n%s", needle, help)
		}
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
