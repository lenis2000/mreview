package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// Pane width ratios — outline 25%, source 40%, PDF 35%. The percentages are
// fixed for the MVP; resizing is delegated to terminal-width changes only.
const (
	outlineFrac = 0.25
	sourceFrac  = 0.40
	pdfFrac     = 0.35
)

// statusBarHeight is the number of rows reserved for the bottom status row.
const statusBarHeight = 1

// View renders the three-pane layout plus the status bar. Until the first
// WindowSizeMsg lands the geometry is unknown, so a "loading…" placeholder
// stands in.
func (m Model) View() string {
	if m.quitting {
		return ""
	}
	if m.Width <= 0 || m.Height <= 0 {
		return "loading…"
	}

	paneHeight := m.Height - statusBarHeight
	if paneHeight < 1 {
		paneHeight = 1
	}

	outlineW, sourceW, pdfW := paneWidths(m.Width)

	outline := m.renderPane("Outline", m.outlinePlaceholder(), outlineW, paneHeight, m.Focus == PaneOutline)
	source := m.renderPane("Source", m.sourcePlaceholder(), sourceW, paneHeight, m.Focus == PaneSource)
	pdf := m.renderPane("PDF", m.pdfPlaceholder(), pdfW, paneHeight, m.Focus == PanePDF)

	main := lipgloss.JoinHorizontal(lipgloss.Top, outline, source, pdf)
	status := m.Styles.StatusBar.Width(m.Width).Render(m.statusText())
	return lipgloss.JoinVertical(lipgloss.Left, main, status)
}

// paneWidths splits a total width into outline/source/pdf widths. Any rounding
// remainder is absorbed by the source pane so the row totals exactly to width.
func paneWidths(width int) (outline, source, pdf int) {
	if width <= 0 {
		return 0, 0, 0
	}
	outline = int(float64(width) * outlineFrac)
	pdf = int(float64(width) * pdfFrac)
	source = width - outline - pdf
	if outline < 1 {
		outline = 1
	}
	if pdf < 1 {
		pdf = 1
	}
	if source < 1 {
		source = 1
	}
	return outline, source, pdf
}

// renderPane wraps content in a bordered box of (width, height) cells. The
// title row is styled distinctly; remaining rows display body text. Borders
// add 2 cells in each axis, so the inner area is (width-2, height-2).
func (m Model) renderPane(title, body string, width, height int, focused bool) string {
	style := m.Styles.Pane
	if focused {
		style = m.Styles.PaneFocused
	}
	innerW := width - 2
	if innerW < 1 {
		innerW = 1
	}
	innerH := height - 2
	if innerH < 1 {
		innerH = 1
	}
	content := m.Styles.PaneTitle.Render(title) + "\n" + body
	return style.Width(innerW).Height(innerH).Render(content)
}

func (m Model) outlinePlaceholder() string {
	if m.Doc == nil {
		return "(no document)"
	}
	count := len(m.Doc.Blocks) - 1 // exclude synthetic root
	if count < 0 {
		count = 0
	}
	return fmt.Sprintf("%d block(s)\nfilter: %s", count, m.Filter)
}

func (m Model) sourcePlaceholder() string {
	if m.Doc == nil || m.CursorBlockID == "" {
		return "(no block selected)"
	}
	b, ok := m.Doc.ByID[m.CursorBlockID]
	if !ok || b == nil {
		return "(unknown block)"
	}
	src := b.Source
	if src == "" {
		return fmt.Sprintf("[%s] (empty source)", b.Kind)
	}
	// Skeleton preview only — Task 10 introduces real LaTeX-aware rendering.
	const preview = 400
	if len(src) > preview {
		src = src[:preview] + "…"
	}
	return src
}

func (m Model) pdfPlaceholder() string {
	if m.Doc == nil || m.CursorBlockID == "" {
		return "(no PDF region)"
	}
	return "[PDF crop placeholder]"
}

// statusText composes the bottom row: focus, cursor breadcrumb, filter,
// transient status, and quit hint. Sections are joined with " · ".
func (m Model) statusText() string {
	parts := []string{
		m.Styles.StatusKey.Render(focusLabel(m.Focus)),
		m.Styles.StatusFilter.Render("filter:" + m.Filter.String()),
	}
	if m.CursorBlockID != "" {
		parts = append(parts, "cursor:"+m.CursorBlockID)
	}
	if m.Status != "" {
		parts = append(parts, m.Status)
	}
	parts = append(parts, "q quit")
	return " " + strings.Join(parts, " · ")
}

func focusLabel(p Pane) string {
	switch p {
	case PaneOutline:
		return "outline"
	case PaneSource:
		return "source"
	case PanePDF:
		return "pdf"
	}
	return "?"
}
