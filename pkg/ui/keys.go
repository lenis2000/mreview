// Package ui implements the bubbletea TUI for mreview: a three-pane layout
// (outline / source / PDF crop) plus a bottom status line. Task 9 wires the
// skeleton; later tasks layer on outline rendering, navigation, annotation,
// and PDF rendering.
package ui

import tea "github.com/charmbracelet/bubbletea"

// Keymap centralises the small set of key bindings the skeleton recognises.
// The fields are typed as string literals from the bubbletea KeyMsg.String()
// surface so tests can construct synthetic events without needing the real
// reader. Later tasks extend this struct rather than scattering string
// literals across handlers.
type Keymap struct {
	Quit       []string
	ForceQuit  []string
}

// DefaultKeymap returns the built-in bindings. `q` and `Ctrl-C` quit; the rest
// is a no-op until later tasks introduce navigation, annotation, and search.
func DefaultKeymap() Keymap {
	return Keymap{
		Quit:      []string{"q"},
		ForceQuit: []string{"ctrl+c"},
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
