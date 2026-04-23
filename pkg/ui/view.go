package ui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"

	"mreview/pkg/parser"
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

	outline := m.renderOutlinePane(outlineW, paneHeight)
	source := m.renderSourcePane(sourceW, paneHeight)
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

// renderOutlinePane is the bordered outline pane using the real tree builder
// and cursor-aware row renderer.
func (m Model) renderOutlinePane(width, height int) string {
	style := m.Styles.Pane
	if m.Focus == PaneOutline {
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
	title := m.Styles.PaneTitle.Render("Outline")
	rows := BuildOutline(m.Doc, m.Sidecar, m.Filter)
	// Reserve 2 inner rows for the title + blank separator; the rest is rows.
	bodyH := innerH - 1
	if bodyH < 1 {
		bodyH = 1
	}
	body := RenderOutline(rows, m.CursorBlockID, innerW, bodyH, m.Focus == PaneOutline, m.Styles)
	content := title + "\n" + body
	return style.Width(innerW).Height(innerH).Render(content)
}

// renderSourcePane wraps RenderSource in a bordered pane. When an annotation
// popup is active, its textarea and title replace the source-pane body so
// the user's typing is visible without a separate overlay.
func (m Model) renderSourcePane(width, height int) string {
	style := m.Styles.Pane
	if m.Focus == PaneSource {
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
	title := m.Styles.PaneTitle.Render("Source")
	bodyH := innerH - 1
	if bodyH < 1 {
		bodyH = 1
	}
	var body string
	if p, ok := m.Popup.(*AnnotationPopup); ok {
		title = m.Styles.PaneTitle.Render(annotationPaneTitle(m.Doc, p))
		body = renderAnnotationBody(p, innerW, bodyH)
	} else {
		body = RenderSource(m.Doc, m.CursorBlockID, innerW, bodyH, m.Styles)
	}
	content := title + "\n" + body
	return style.Width(innerW).Height(innerH).Render(content)
}

// annotationPaneTitle formats "Annotation · <breadcrumb>" for the source-pane
// title while the popup is active.
func annotationPaneTitle(doc *parser.Document, p *AnnotationPopup) string {
	bc := AnnotationBreadcrumb(doc, p.TargetID)
	if bc == "" {
		if p.Editing {
			return "Edit annotation"
		}
		return "Annotation"
	}
	prefix := "Annotation"
	if p.Editing {
		prefix = "Edit annotation"
	}
	return prefix + " · " + bc
}

// renderAnnotationBody lays out the textarea and a help hint within the
// source pane's inner area.
func renderAnnotationBody(p *AnnotationPopup, innerW, innerH int) string {
	hint := "[Ctrl-S/Esc submit · Ctrl-C cancel]"
	taH := innerH - 1
	if taH < 1 {
		taH = 1
	}
	if p.TA.Height() != taH {
		p.TA.SetHeight(taH)
	}
	w := innerW
	if w > 2 {
		w -= 2
	}
	if p.TA.Width() != w {
		p.TA.SetWidth(w)
	}
	return p.TA.View() + "\n" + hint
}

func (m Model) pdfPlaceholder() string {
	if m.Doc == nil || m.CursorBlockID == "" {
		return "(no PDF region)"
	}
	return "[PDF crop placeholder]"
}

// statusText composes the bottom row: focus, breadcrumb, locator, filter,
// transient status, and quit hint. Sections are joined with " · ".
func (m Model) statusText() string {
	parts := []string{
		m.Styles.StatusKey.Render(focusLabel(m.Focus)),
		m.Styles.StatusFilter.Render("filter:" + m.Filter.String()),
	}
	if bc := BreadcrumbFor(m.Doc, m.CursorBlockID); bc != "" {
		parts = append(parts, bc)
	}
	if loc := LocatorFor(m.Doc, m.CursorBlockID); loc != "" {
		parts = append(parts, loc)
	}
	if m.Pending != nil {
		parts = append(parts, "[y/N] delete annotation?")
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
