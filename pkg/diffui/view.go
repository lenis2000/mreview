package diffui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

const statusBarHeight = 1

// View renders the diff outline, old/new source diff, PDF placeholder, and
// status bar.
func (m Model) View() string {
	if m.quitting {
		return ""
	}
	if m.Width <= 0 || m.Height <= 0 {
		return "loading..."
	}
	bodyHeight := m.Height - statusBarHeight
	if bodyHeight < 1 {
		bodyHeight = 1
	}
	outlineW, oldW, newW, sourceW, pdfW, combined := paneWidths(m.Width)
	outline := m.renderPane("Outline", m.renderOutline(outlineW-2, bodyHeight-2), outlineW, bodyHeight)
	if m.ShowHelp {
		source := m.renderPane("Help", RenderHelpBody(sourceW-2, m.AllowModifications), sourceW, bodyHeight)
		pdf := m.renderPDFPane(pdfW, bodyHeight)
		main := lipgloss.JoinHorizontal(lipgloss.Top, outline, source, pdf)
		status := clipLine(m.statusText(), m.Width)
		return lipgloss.JoinVertical(lipgloss.Left, main, status)
	}
	if m.LineEdit != nil {
		source := m.renderPane("Line Edit", m.LineEdit.TI.View(), sourceW, bodyHeight)
		pdf := m.renderPDFPane(pdfW, bodyHeight)
		main := lipgloss.JoinHorizontal(lipgloss.Top, outline, source, pdf)
		status := clipLine(m.statusText(), m.Width)
		return lipgloss.JoinVertical(lipgloss.Left, main, status)
	}
	if m.Popup != nil {
		source := m.renderPane("Annotation", m.Popup.TA.View(), sourceW, bodyHeight)
		pdf := m.renderPDFPane(pdfW, bodyHeight)
		main := lipgloss.JoinHorizontal(lipgloss.Top, outline, source, pdf)
		status := clipLine(m.statusText(), m.Width)
		return lipgloss.JoinVertical(lipgloss.Left, main, status)
	}
	var middle string
	if combined {
		middle = m.renderPane("Source", RenderPairSource(m.CurrentPair(), sourceW-2, bodyHeight-2), sourceW, bodyHeight)
	} else {
		oldSource := m.renderPane("Old source", RenderPairSourceSide(m.CurrentPair(), true, oldW-2, bodyHeight-2), oldW, bodyHeight)
		newSource := m.renderPane("New source", RenderPairSourceSide(m.CurrentPair(), false, newW-2, bodyHeight-2), newW, bodyHeight)
		middle = lipgloss.JoinHorizontal(lipgloss.Top, oldSource, newSource)
	}
	pdf := m.renderPDFPane(pdfW, bodyHeight)
	main := lipgloss.JoinHorizontal(lipgloss.Top, outline, middle, pdf)
	status := clipLine(m.statusText(), m.Width)
	return lipgloss.JoinVertical(lipgloss.Left, main, status)
}

func (m Model) renderPane(title, body string, width, height int) string {
	if width < 1 {
		width = 1
	}
	if height < 1 {
		height = 1
	}
	innerW := width - 2
	if innerW < 1 {
		innerW = 1
	}
	innerH := height - 2
	if innerH < 1 {
		innerH = 1
	}
	content := title
	if body != "" {
		content += "\n" + fitLines(body, innerW, innerH-1)
	}
	style := m.Styles.Pane
	return style.Width(innerW).Height(innerH).Border(lipgloss.NormalBorder()).Render(content)
}

func (m Model) renderPDFPane(width, height int) string {
	if width < 1 {
		width = 1
	}
	if height < 1 {
		height = 1
	}
	innerW := width - 2
	if innerW < 1 {
		innerW = 1
	}
	innerH := height - 2
	if innerH < 1 {
		innerH = 1
	}
	content := "PDF"
	if body := m.pdfPaneBody(); body != "" {
		content += "\n" + body
	}
	style := m.Styles.Pane
	return style.Width(innerW).Height(innerH).Border(lipgloss.NormalBorder()).Render(content)
}

func paneWidths(width int) (outline, oldSource, newSource, source, pdf int, combined bool) {
	if width < 3 {
		return 1, 0, 0, 1, 1, true
	}
	outline = width / 4
	pdf = width / 4
	source = width - outline - pdf
	combined = width < 120
	if !combined {
		oldSource = source / 2
		newSource = source - oldSource
	}
	if outline < 1 {
		outline = 1
	}
	if source < 1 {
		source = 1
	}
	if pdf < 1 {
		pdf = 1
	}
	if oldSource < 0 {
		oldSource = 0
	}
	if newSource < 0 {
		newSource = 0
	}
	return outline, oldSource, newSource, source, pdf, combined
}

// RenderHelpBody returns the diff-specific help text.
func RenderHelpBody(width int, allowModifications bool) string {
	lines := []string{
		"j/k move pair",
		"J/K jump pairs",
		"gg/G first/last pair",
		"{/} previous/next section",
		"f cycle filter",
		"space mark reviewed",
		"a annotate pair",
		"ctrl+a edit annotation",
		"d delete annotation",
		"e/E edit new file only when --allow-modifications is supplied",
		"[/] select previous/next new source line",
		"u undo last diff-mode edit",
		"ctrl+r redo undone diff-mode edit",
		"B rebuild/reload new PDF; use after Zed edits",
		"Z opens old+new in Zed",
		"? close help",
		"q quit",
	}
	if !allowModifications {
		lines = append(lines, "editing is currently disabled")
	}
	for i, line := range lines {
		lines[i] = clipLine(line, width)
	}
	return strings.Join(lines, "\n")
}

func fitLines(text string, width, height int) string {
	if width < 1 {
		width = 1
	}
	if height < 1 {
		height = 1
	}
	lines := strings.Split(text, "\n")
	if len(lines) > height {
		lines = lines[:height]
	}
	for i, line := range lines {
		lines[i] = clipLine(line, width)
	}
	return strings.Join(lines, "\n")
}

func clipLine(line string, width int) string {
	if width < 1 {
		return ""
	}
	runes := []rune(line)
	if len(runes) <= width {
		return line
	}
	if width == 1 {
		return string(runes[:1])
	}
	return string(runes[:width-1]) + "…"
}
