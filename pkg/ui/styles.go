package ui

import "github.com/charmbracelet/lipgloss"

// Styles bundles the lipgloss styles used across panes. The concrete palette
// mirrors revdiff's two-pane layout (subtle border, inverted status row) so
// the look-and-feel is consistent with the sister tool.
type Styles struct {
	Pane         lipgloss.Style
	PaneFocused  lipgloss.Style
	PaneTitle    lipgloss.Style
	StatusBar    lipgloss.Style
	StatusKey    lipgloss.Style
	StatusFilter lipgloss.Style

	// Outline pane.
	OutlineIcon   lipgloss.Style
	OutlineMarker lipgloss.Style
	OutlineCursor lipgloss.Style
	OutlineActive lipgloss.Style
	OutlineMuted  lipgloss.Style

	// Source pane — vim-like LaTeX highlighting. Command / keyword /
	// math / env-name / brace / number / math-content get distinct
	// colours; comments stay muted italic. SourceAnnotation is the
	// inline-note style, not a source token.
	SourceGutter     lipgloss.Style
	SourceComment    lipgloss.Style
	SourceCommand    lipgloss.Style
	SourceKeyword    lipgloss.Style
	SourceMath       lipgloss.Style
	SourceMathText   lipgloss.Style
	SourceEnvName    lipgloss.Style
	SourceBrace      lipgloss.Style
	SourceNumber     lipgloss.Style
	SourceAnnotation lipgloss.Style
}

// DefaultStyles returns the baseline visual treatment. Colors are expressed
// as ANSI 256 indices so the palette works on most terminals without
// truecolor support; the kitty target handles both.
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

		OutlineIcon:   lipgloss.NewStyle().Foreground(lipgloss.Color("111")),
		OutlineMarker: lipgloss.NewStyle().Foreground(lipgloss.Color("178")),
		OutlineCursor: lipgloss.NewStyle().Foreground(lipgloss.Color("230")).Background(lipgloss.Color("24")).Bold(true),
		OutlineActive: lipgloss.NewStyle().Foreground(lipgloss.Color("230")).Background(lipgloss.Color("238")),
		OutlineMuted:  lipgloss.NewStyle().Foreground(lipgloss.Color("244")),

		SourceGutter:     lipgloss.NewStyle().Foreground(lipgloss.Color("244")),
		SourceComment:    lipgloss.NewStyle().Foreground(lipgloss.Color("244")).Italic(true),
		SourceCommand:    lipgloss.NewStyle().Foreground(lipgloss.Color("111")),
		SourceKeyword:    lipgloss.NewStyle().Foreground(lipgloss.Color("213")).Bold(true),
		SourceMath:       lipgloss.NewStyle().Foreground(lipgloss.Color("214")).Bold(true),
		SourceMathText:   lipgloss.NewStyle().Foreground(lipgloss.Color("222")),
		SourceEnvName:    lipgloss.NewStyle().Foreground(lipgloss.Color("114")),
		SourceBrace:      lipgloss.NewStyle().Foreground(lipgloss.Color("244")),
		SourceNumber:     lipgloss.NewStyle().Foreground(lipgloss.Color("216")),
		SourceAnnotation: lipgloss.NewStyle().Foreground(lipgloss.Color("228")).Background(lipgloss.Color("236")).Italic(true),
	}
}

// lightStyles returns a palette tuned for light-background terminals. The
// structural roles (pane border, status bar, cursor row) are unchanged — only
// the colour indices shift so foregrounds remain legible on a pale backdrop.
func lightStyles() Styles {
	border := lipgloss.NewStyle().Border(lipgloss.NormalBorder()).BorderForeground(lipgloss.Color("245"))
	focus := lipgloss.NewStyle().Border(lipgloss.NormalBorder()).BorderForeground(lipgloss.Color("26"))
	return Styles{
		Pane:         border,
		PaneFocused:  focus,
		PaneTitle:    lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("16")),
		StatusBar:    lipgloss.NewStyle().Foreground(lipgloss.Color("16")).Background(lipgloss.Color("253")),
		StatusKey:    lipgloss.NewStyle().Foreground(lipgloss.Color("130")).Bold(true),
		StatusFilter: lipgloss.NewStyle().Foreground(lipgloss.Color("26")),

		OutlineIcon:   lipgloss.NewStyle().Foreground(lipgloss.Color("25")),
		OutlineMarker: lipgloss.NewStyle().Foreground(lipgloss.Color("130")),
		OutlineCursor: lipgloss.NewStyle().Foreground(lipgloss.Color("231")).Background(lipgloss.Color("32")).Bold(true),
		OutlineActive: lipgloss.NewStyle().Foreground(lipgloss.Color("16")).Background(lipgloss.Color("251")),
		OutlineMuted:  lipgloss.NewStyle().Foreground(lipgloss.Color("243")),

		SourceGutter:     lipgloss.NewStyle().Foreground(lipgloss.Color("243")),
		SourceComment:    lipgloss.NewStyle().Foreground(lipgloss.Color("243")).Italic(true),
		SourceCommand:    lipgloss.NewStyle().Foreground(lipgloss.Color("25")),
		SourceKeyword:    lipgloss.NewStyle().Foreground(lipgloss.Color("126")).Bold(true),
		SourceMath:       lipgloss.NewStyle().Foreground(lipgloss.Color("130")).Bold(true),
		SourceMathText:   lipgloss.NewStyle().Foreground(lipgloss.Color("94")),
		SourceEnvName:    lipgloss.NewStyle().Foreground(lipgloss.Color("22")),
		SourceBrace:      lipgloss.NewStyle().Foreground(lipgloss.Color("243")),
		SourceNumber:     lipgloss.NewStyle().Foreground(lipgloss.Color("166")),
		SourceAnnotation: lipgloss.NewStyle().Foreground(lipgloss.Color("130")).Background(lipgloss.Color("230")).Italic(true),
	}
}
