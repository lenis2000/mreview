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
	pdf := m.renderPane("PDF", m.pdfPaneBody(), pdfW, paneHeight, m.Focus == PanePDF)

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
	switch p := m.Popup.(type) {
	case *AnnotationPopup:
		title = m.Styles.PaneTitle.Render(annotationPaneTitle(m.Doc, p))
		body = renderAnnotationBody(p, innerW, bodyH)
	case *SearchPopup:
		title = m.Styles.PaneTitle.Render("Search")
		body = renderSearchBody(p, innerW, bodyH, m.Styles)
	case *AnnotListPopup:
		title = m.Styles.PaneTitle.Render("Annotations")
		body = renderAnnotListBody(p, innerW, bodyH, m.Styles)
	case *RefListPopup:
		title = m.Styles.PaneTitle.Render("Referrers — " + p.Label)
		body = renderRefListBody(m.Doc, p, innerW, bodyH, m.Styles)
	case *BibPopup:
		title = m.Styles.PaneTitle.Render("Bibliography — " + p.Key)
		body = RenderBibBody(p, innerW, bodyH, m.Styles)
	case *HelpPopup:
		title = m.Styles.PaneTitle.Render("Help")
		body = RenderHelpBody(innerW)
		_ = p
	default:
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

// renderSearchBody lays out the fuzzy-search popup inside the source pane:
// a text input on the first line, a blank separator, and up to (bodyH-3)
// result rows with the cursor highlighted.
func renderSearchBody(p *SearchPopup, innerW, bodyH int, styles Styles) string {
	if bodyH < 1 {
		bodyH = 1
	}
	hint := "[Enter jump · Esc cancel · ↑/↓ select]"
	w := innerW
	if w > 2 {
		w -= 2
	}
	if p.Input.Width != w {
		p.Input.Width = w
	}
	resultH := bodyH - 3 // input + separator + hint
	if resultH < 1 {
		resultH = 1
	}
	var b strings.Builder
	b.WriteString(p.Input.View())
	b.WriteByte('\n')
	b.WriteString(renderPopupList(popupRows(p.Results, p.Cursor, innerW), innerW, resultH, styles))
	b.WriteByte('\n')
	b.WriteString(styles.OutlineMuted.Render(truncateToWidth(hint, innerW)))
	return b.String()
}

// popupRows extracts {label, selected?} pairs from the search results.
func popupRows(results []SearchEntry, cursor, width int) []popupRow {
	rows := make([]popupRow, len(results))
	for i, r := range results {
		rows[i] = popupRow{Text: r.Display, Selected: i == cursor}
	}
	_ = width
	return rows
}

// popupRow is a lightweight descriptor for renderPopupList.
type popupRow struct {
	Text     string
	Selected bool
}

// renderPopupList renders rows within the given (width, height) region,
// scrolling so the selected row stays visible. Empty result sets display a
// muted "(no matches)" line.
func renderPopupList(rows []popupRow, width, height int, styles Styles) string {
	if height < 1 {
		height = 1
	}
	if width < 1 {
		width = 1
	}
	if len(rows) == 0 {
		return styles.OutlineMuted.Render("(no matches)")
	}
	sel := -1
	for i, r := range rows {
		if r.Selected {
			sel = i
			break
		}
	}
	offset := 0
	if sel >= 0 && sel >= height {
		offset = sel - height + 1
	}
	end := offset + height
	if end > len(rows) {
		end = len(rows)
	}
	var b strings.Builder
	for i := offset; i < end; i++ {
		line := truncateToWidth(rows[i].Text, width)
		if rows[i].Selected {
			line = styles.OutlineCursor.Width(width).Render(line)
		}
		b.WriteString(line)
		if i < end-1 {
			b.WriteByte('\n')
		}
	}
	return b.String()
}

// renderAnnotListBody renders the annotation list. Selected row inverted.
func renderAnnotListBody(p *AnnotListPopup, innerW, bodyH int, styles Styles) string {
	if bodyH < 1 {
		bodyH = 1
	}
	hint := "[Enter jump · e edit · d delete · Esc close]"
	rows := make([]popupRow, len(p.Items))
	for i, it := range p.Items {
		rows[i] = popupRow{Text: it.Display(), Selected: i == p.Cursor}
	}
	resultH := bodyH - 1
	if resultH < 1 {
		resultH = 1
	}
	var b strings.Builder
	b.WriteString(renderPopupList(rows, innerW, resultH, styles))
	b.WriteByte('\n')
	b.WriteString(styles.OutlineMuted.Render(truncateToWidth(hint, innerW)))
	return b.String()
}

// renderRefListBody renders the referrer popup rows.
func renderRefListBody(doc *parser.Document, p *RefListPopup, innerW, bodyH int, styles Styles) string {
	if bodyH < 1 {
		bodyH = 1
	}
	hint := "[Enter jump · Esc close]"
	rows := make([]popupRow, len(p.BlockIDs))
	for i, id := range p.BlockIDs {
		bc := AnnotationBreadcrumb(doc, id)
		if bc == "" {
			bc = id
		}
		rows[i] = popupRow{Text: bc, Selected: i == p.Index}
	}
	resultH := bodyH - 1
	if resultH < 1 {
		resultH = 1
	}
	var b strings.Builder
	b.WriteString(renderPopupList(rows, innerW, resultH, styles))
	b.WriteByte('\n')
	b.WriteString(styles.OutlineMuted.Render(truncateToWidth(hint, innerW)))
	return b.String()
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
