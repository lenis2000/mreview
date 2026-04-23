// Package ui implements the bubbletea TUI for mreview: a three-pane layout
// (outline / source / PDF crop) plus a bottom status line. Task 9 wires the
// skeleton; later tasks layer on outline rendering, navigation, annotation,
// and PDF rendering.
package ui

import tea "github.com/charmbracelet/bubbletea"

// Keymap centralises the small set of key bindings the TUI recognises. Fields
// hold the bubbletea `KeyMsg.String()` forms so tests can construct synthetic
// events without a real reader. Later tasks extend the struct rather than
// scatter string literals across handlers.
type Keymap struct {
	Quit        []string
	ForceQuit   []string
	CycleFilter []string

	// Navigation (Task 11).
	NavNextOuter []string // j — next outer sibling
	NavPrevOuter []string // k — prev outer sibling
	NavNextInner []string // J — next in DFS (includes proof-steps, etc.)
	NavPrevInner []string // K — prev in DFS
	NavNextSec   []string // } — next section
	NavPrevSec   []string // { — prev section
	NavLast      []string // G — last visible
	NavPrefixG   []string // g — prefix for gg / go / gu

	JumpBack    []string // ctrl+o
	JumpForward []string // ctrl+i, tab
}

// DefaultKeymap returns the built-in bindings.
func DefaultKeymap() Keymap {
	return Keymap{
		Quit:         []string{"q"},
		ForceQuit:    []string{"ctrl+c"},
		CycleFilter:  []string{"f"},
		NavNextOuter: []string{"j", "down"},
		NavPrevOuter: []string{"k", "up"},
		NavNextInner: []string{"J"},
		NavPrevInner: []string{"K"},
		NavNextSec:   []string{"}"},
		NavPrevSec:   []string{"{"},
		NavLast:      []string{"G"},
		NavPrefixG:   []string{"g"},
		JumpBack:     []string{"ctrl+o"},
		JumpForward:  []string{"ctrl+i", "tab"},
	}
}

// matches reports whether the given key string is bound to any of the listed
// actions.
func matches(key string, bindings []string) bool {
	for _, b := range bindings {
		if b == key {
			return true
		}
	}
	return false
}

// isQuitKey reports whether the key event should terminate the program.
func (k Keymap) isQuitKey(msg tea.KeyMsg) bool {
	s := msg.String()
	return matches(s, k.Quit) || matches(s, k.ForceQuit)
}

// isFilterKey reports whether the key event should cycle the outline filter.
func (k Keymap) isFilterKey(msg tea.KeyMsg) bool {
	return matches(msg.String(), k.CycleFilter)
}
