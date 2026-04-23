package ui

import "github.com/charmbracelet/lipgloss"

// Styles bundles the small set of lipgloss styles the skeleton needs. Task 10
// will extend this with focus markers, syntax colors, and outline icons; for
// now the panes share a thin border and the status bar is a single inverted
// row at the bottom.
type Styles struct {
	Pane         lipgloss.Style
	PaneFocused  lipgloss.Style
	PaneTitle    lipgloss.Style
	StatusBar    lipgloss.Style
	StatusKey    lipgloss.Style
	StatusFilter lipgloss.Style
}

// DefaultStyles returns the baseline visual treatment used by the skeleton.
// Color choices mirror revdiff's two-pane layout (subtle border, inverted
// status row) so the look-and-feel is consistent with sister tools.
func DefaultStyles() Styles {
	border := lipgloss.NewStyle().Border(lipgloss.NormalBorder()).BorderForeground(lipgloss.Color("240"))
	focus := lipgloss.NewStyle().Border(lipgloss.NormalBorder()).BorderForeground(lipgloss.Color("39"))
	return Styles{
		Pane:         border,
		PaneFocused:  focus,
		PaneTitle:    lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("230")),
		StatusBar:    lipgloss.NewStyle().Foreground(lipgloss.Color("230")).Background(lipgloss.Color("236")),
		StatusKey:    lipgloss.NewStyle().Foreground(lipgloss.Color("214")).Bold(true),
		StatusFilter: lipgloss.NewStyle().Foreground(lipgloss.Color("39")),
	}
}
