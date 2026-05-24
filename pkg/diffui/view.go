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
	outlineW, sourceW, pdfW := paneWidths(m.Width)
	outline := m.renderPane("Outline", m.renderOutline(outlineW-2, bodyHeight-2), outlineW, bodyHeight)
	sourceTitle := "Source"
	sourceBody := RenderPairSource(m.CurrentPair(), sourceW-2, bodyHeight-2)
	if m.ShowHelp {
		sourceTitle = "Help"
		sourceBody = RenderHelpBody(sourceW-2, m.AllowModifications)
	} else if m.LineEdit != nil {
		sourceTitle = "Line Edit"
		sourceBody = m.LineEdit.TI.View()
	} else if m.Popup != nil {
		sourceTitle = "Annotation"
		sourceBody = m.Popup.TA.View()
	}
	source := m.renderPane(sourceTitle, sourceBody, sourceW, bodyHeight)
	pdf := m.renderPane("PDF", m.pdfPaneBody(), pdfW, bodyHeight)
	main := lipgloss.JoinHorizontal(lipgloss.Top, outline, source, pdf)
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

func paneWidths(width int) (outline, source, pdf int) {
	if width < 3 {
		return 1, 1, 1
	}
	outline = width / 4
	pdf = width / 4
	source = width - outline - pdf
	if outline < 1 {
		outline = 1
	}
	if source < 1 {
		source = 1
	}
	if pdf < 1 {
		pdf = 1
	}
	return outline, source, pdf
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
