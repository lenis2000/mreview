package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"mreview/pkg/parser"
	"mreview/pkg/persist"
)

// Pane width ratios — outline 22%, source 50%, PDF 28%. Source gets the
// lion's share because long prose lines in the .tex (full paragraphs on
// one physical line) wrap badly at narrower widths; the PDF pane only
// needs enough room to show the cursor-block crop legibly.
//
// These are vars (not consts) so the user can resize panes interactively
// with `<` / `>`. Bounds are enforced in clampLayoutFracs; the source
// fraction is always derived as 1 - outline - pdf so it absorbs the
// remainder. stackedTopFrac is the source pane's height share in
// LayoutStacked (the rest goes to the PDF pane below it).
var (
	outlineFrac    = 0.22
	pdfFrac        = 0.28
	stackedTopFrac = 0.50
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

	var main string
	switch m.Layout {
	case LayoutStacked:
		outlineW, rightW := stackedWidths(m.Width)
		topH, botH := stackedHeights(paneHeight)
		outline := m.renderOutlinePane(outlineW, paneHeight)
		source := m.renderSourcePane(rightW, topH)
		pdf := m.renderPane("PDF", m.pdfPaneBody(), rightW, botH, m.Focus == PanePDF)
		right := lipgloss.JoinVertical(lipgloss.Left, source, pdf)
		main = lipgloss.JoinHorizontal(lipgloss.Top, outline, right)
	default:
		outlineW, sourceW, pdfW := paneWidths(m.Width)
		outline := m.renderOutlinePane(outlineW, paneHeight)
		source := m.renderSourcePane(sourceW, paneHeight)
		pdf := m.renderPane("PDF", m.pdfPaneBody(), pdfW, paneHeight, m.Focus == PanePDF)
		main = lipgloss.JoinHorizontal(lipgloss.Top, outline, source, pdf)
	}
	status := m.Styles.StatusBar.Width(m.Width).Render(m.statusText())
	return lipgloss.JoinVertical(lipgloss.Left, main, status)
}

// stackedWidths splits total width between the outline (left) and the
// source/pdf stack (right) for LayoutStacked. Outline gets the same fraction
// it has in the 3-col layout so the user's eye doesn't have to retrain.
func stackedWidths(width int) (outline, right int) {
	if width <= 0 {
		return 0, 0
	}
	outline = int(float64(width) * outlineFrac)
	if outline < 1 {
		outline = 1
	}
	right = width - outline
	if right < 1 {
		right = 1
	}
	return outline, right
}

// stackedHeights splits the right column's height between source (top) and
// pdf (bottom). Source gets a slight majority so prose stays readable; the
// PDF pane scales down with cell-aware aspect-fit anyway.
func stackedHeights(height int) (top, bot int) {
	if height <= 0 {
		return 0, 0
	}
	top = int(float64(height) * stackedTopFrac)
	if top < 1 {
		top = 1
	}
	if top >= height {
		top = height - 1
		if top < 1 {
			top = 1
		}
	}
	bot = height - top
	if bot < 1 {
		bot = 1
	}
	return top, bot
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
	rows := BuildOutline(m.Doc, m.Sidecar, m.Filter, m.ExternalIssues)
	// Reserve 2 inner rows for the title + blank separator; the rest is rows.
	bodyH := innerH - 1
	if bodyH < 1 {
		bodyH = 1
	}
	body := RenderOutline(rows, m.Doc, m.CursorBlockID, innerW, bodyH, m.Focus == PaneOutline, m.Styles)
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
		// Render the source pane normally with the editor spliced in at the
		// annotation's anchor line so the user sees the surrounding source
		// while typing — no full-pane takeover. The inline editor replaces
		// only the rows it needs.
		title = m.Styles.PaneTitle.Render(annotationPaneTitle(m.Doc, p))
		var anns []persist.Annotation
		if m.Sidecar != nil {
			anns = m.Sidecar.Annotations
		}
		body = renderSourceWithEditor(m.Doc, m.CursorBlockID, innerW, bodyH, m.Styles, m.SoftWrap, m.SourceLineCursor, anns, p, m.LastSearch)
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
		body = RenderHelpBody(innerW, m.AllowModifications)
		_ = p
	case *LineEditPopup:
		title = m.Styles.PaneTitle.Render(fmt.Sprintf("Edit line %d · %s", p.AbsoluteLine, m.Doc.File))
		body = renderLineEditBody(p, innerW, bodyH, m.Styles)
	default:
		var anns []persist.Annotation
		if m.Sidecar != nil {
			anns = m.Sidecar.Annotations
		}
		body = renderSourceWithEditor(m.Doc, m.CursorBlockID, innerW, bodyH, m.Styles, m.SoftWrap, m.SourceLineCursor, anns, nil, m.LastSearch)
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

// renderLineEditBody lays out the inline single-line editor plus its
// hint inside the source pane. We intentionally don't show the full
// source here — this popup is for one-line wording fixes, not
// navigation, so clearing the pane keeps the user focused on exactly
// what they're rewriting.
func renderLineEditBody(p *LineEditPopup, innerW, innerH int, styles Styles) string {
	var hint string
	if p.NormalMode {
		tail := ""
		if p.Count != "" {
			tail = " · " + p.Count
		}
		if p.Pending != "" {
			tail += " · " + p.Pending
		}
		hint = "[NORMAL · w/b/e/0/$/h/l · dw/dW · u undo · Ctrl-R redo · i/a insert · Enter submit · Esc cancel" + tail + "]"
	} else {
		hint = "[INSERT · Ctrl-R redo · Enter submit · Esc → normal · Ctrl-C cancel]"
	}
	// Reserve columns for the "NNNN " gutter prefix (5 visible cells) plus
	// one for the textinput's trailing cursor cell — without this budget
	// the TI renders wider than the pane and lipgloss wraps the line,
	// making the content look like it's being eaten onto a second row.
	prefix := styles.SourceGutter.Render(fmt.Sprintf("%4d ", p.AbsoluteLine))
	// Render the original indent verbatim with tabs expanded so the
	// editing surface visually lines up with the rest of the source
	// pane. The indent is hidden from bubbles (which sanitises tabs);
	// SubmitLineEdit re-prepends it on save, so what the user sees
	// here is exactly what hits disk.
	indent := strings.ReplaceAll(p.Indent, "\t", "    ")
	indentW := len([]rune(indent))
	w := innerW - 6 - indentW
	if w < 10 {
		w = 10
	}
	if p.TI.Width != w {
		p.TI.Width = w
	}
	body := prefix + indent + p.TI.View()
	_ = innerH
	return body + "\n\n" + styles.OutlineMuted.Render(hint)
}

// renderSearchBody lays out the vim-style search prompt inside the source
// pane: a slash sigil, the text input, and a one-line hint. There is no
// result list — submitting jumps directly; n / N repeat afterwards.
func renderSearchBody(p *SearchPopup, innerW, bodyH int, styles Styles) string {
	_ = bodyH
	hint := "[Enter jump · Esc cancel · n / N repeat after]"
	w := innerW
	if w > 4 {
		w -= 4
	}
	if p.Input.Width != w {
		p.Input.Width = w
	}
	var b strings.Builder
	b.WriteString("/" + p.Input.View())
	b.WriteByte('\n')
	b.WriteString(styles.OutlineMuted.Render(truncateToWidth(hint, innerW)))
	return b.String()
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
