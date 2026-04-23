package ui

import tea "github.com/charmbracelet/bubbletea"

// Update is the bubbletea state-transition function. The skeleton handles
// only window resizes and the quit keys; later tasks dispatch to navigation,
// annotation, and popup-specific handlers.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.Width = msg.Width
		m.Height = msg.Height
		return m, nil
	case tea.KeyMsg:
		if m.Keymap.isQuitKey(msg) {
			m.quitting = true
			return m, tea.Quit
		}
		if m.Keymap.isFilterKey(msg) {
			m.Filter = CycleFilter(m.Filter)
			return m, nil
		}
	}
	return m, nil
}
