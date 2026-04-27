package pdfreview

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

const (
	listFrac = 0.40
	pdfFrac  = 0.60

	statusBarHeight = 1
)

// View renders the three-region layout (list / pdf / status) and overlays
// the popup if any.
func (m Model) View() string {
	if m.quitting {
		return ""
	}
	if m.Width <= 0 || m.Height <= 0 {
		return "loading…"
	}

	paneH := m.Height - statusBarHeight
	if paneH < 1 {
		paneH = 1
	}
	listW, pdfW := splitWidths(m.Width)

	listPane := m.renderListPane(listW, paneH)
	pdfPane := m.renderPDFPane(pdfW, paneH)
	main := lipgloss.JoinHorizontal(lipgloss.Top, listPane, pdfPane)
	status := m.StatusStyle.Width(m.Width).Render(m.statusText())
	frame := lipgloss.JoinVertical(lipgloss.Left, main, status)

	if m.Popup != nil {
		return overlayCenter(frame, m.renderPopup(), m.Width, m.Height)
	}
	return frame
}

func splitWidths(total int) (int, int) {
	if total < 6 {
		return total, 0
	}
	listW := int(float64(total) * listFrac)
	if listW < 20 {
		listW = 20
	}
	if listW > total-10 {
		listW = total - 10
	}
	return listW, total - listW
}

func (m Model) renderListPane(w, h int) string {
	border := m.BorderIdle
	innerW := w - 2 // borders
	if innerW < 4 {
		innerW = 4
	}
	innerH := h - 2
	if innerH < 1 {
		innerH = 1
	}
	body := m.renderListBody(innerW, innerH)
	titled := m.HeaderStyle.Render(" Comments ") + "\n" + body
	return border.Width(w - 2).Height(h - 2).Render(titled)
}

func (m Model) renderListBody(w, h int) string {
	disp := m.displayedOrder()
	if len(disp) == 0 {
		return m.DimStyle.Render("(no comments — run pdf-comments first)")
	}

	var lines []string
	curIdx := m.indexOfCurrent(disp)
	prevKind := ""
	for i, c := range disp {
		if c.Kind != prevKind {
			label := kindLabel(c.Kind)
			if c.Kind == KindMeta {
				label += "  (excluded from export)"
			}
			lines = append(lines, m.SectionLabel.Render(label))
			prevKind = c.Kind
		}
		marker := "  "
		if i == curIdx {
			marker = "▶ "
		}
		icon := statusIcon(c.Status)
		page := ""
		if c.Page > 0 {
			page = fmt.Sprintf("p.%d ", c.Page)
		}
		summary := summarize(c.OriginalText, w-len(marker)-len(icon)-len(page)-2)
		row := fmt.Sprintf("%s%s %s%s", marker, icon, page, summary)
		if i == curIdx {
			row = m.CursorStyle.Render(truncateRight(row, w))
		} else {
			row = truncateRight(row, w)
		}
		lines = append(lines, row)
	}
	// Quote pane footer (the currently-selected comment's quote, dimmed).
	if c := m.currentComment(); c != nil && c.Quote != "" {
		lines = append(lines, "")
		quote := summarize(`Quote: "`+c.Quote+`"`, (w*3)/2)
		lines = append(lines, m.DimStyle.Render(wrapLines(quote, w, 2)))
	}

	body := strings.Join(lines, "\n")
	body = clampLines(body, h)
	return body
}

func (m Model) renderPDFPane(w, h int) string {
	border := m.BorderIdle
	innerW := w - 2
	if innerW < 4 {
		innerW = 4
	}
	innerH := h - 2
	if innerH < 1 {
		innerH = 1
	}

	title := fmt.Sprintf(" %s · p.%d / %d ", baseName(m.PDFPath), m.Page, m.NumPages)
	if m.HighlightApprox {
		title += "· ⚠ anchor approximate "
	}

	body := m.renderPDFBody(innerW, innerH)
	titled := m.HeaderStyle.Render(title) + "\n" + body
	return border.Width(w - 2).Height(h - 2).Render(titled)
}

func (m Model) renderPDFBody(w, h int) string {
	// h is body height in cells (not counting title). We have one less line
	// than innerH because the title line is above us.
	bodyH := h - 1
	if bodyH < 1 {
		bodyH = 1
	}
	if !m.KittyAvailable {
		return m.renderPDFBodyText(w, bodyH)
	}
	if m.pdfEsc == "" {
		return m.DimStyle.Render("(rendering…)")
	}
	// A blank canvas the right size, with the kitty escape written into
	// the first line. The terminal places the image at cursor position.
	pad := strings.Repeat(strings.Repeat(" ", w)+"\n", bodyH-1) + strings.Repeat(" ", w)
	return m.pdfEsc + pad
}

// renderPDFBodyText is the no-kitty fallback: show the page's plain text
// with the quote (if any) reverse-video'd.
func (m Model) renderPDFBodyText(w, h int) string {
	if m.BBox == nil {
		return m.DimStyle.Render("(no PDF)")
	}
	pb, err := m.BBox.Page(m.Page)
	if err != nil || pb == nil {
		return m.DimStyle.Render(fmt.Sprintf("(page %d text unavailable)", m.Page))
	}
	var lines []string
	cur := []string{}
	prevY := -1.0
	for _, wd := range pb.Words {
		if prevY >= 0 && absf(wd.YMin-prevY) > 2 {
			lines = append(lines, strings.Join(cur, " "))
			cur = cur[:0]
		}
		cur = append(cur, wd.Text)
		prevY = wd.YMin
	}
	if len(cur) > 0 {
		lines = append(lines, strings.Join(cur, " "))
	}
	if c := m.currentComment(); c != nil && c.Quote != "" {
		mark := lipgloss.NewStyle().Reverse(true)
		for i, ln := range lines {
			if idx := strings.Index(ln, c.Quote); idx >= 0 {
				lines[i] = ln[:idx] + mark.Render(c.Quote) + ln[idx+len(c.Quote):]
			}
		}
	}
	body := strings.Join(lines, "\n")
	return clampLines(body, h)
}

func (m Model) renderPopup() string {
	switch m.Popup.(type) {
	case *HelpPopup:
		return m.renderHelpPopup()
	}
	return ""
}

func (m Model) renderHelpPopup() string {
	w := 70
	if w > m.Width-4 {
		w = m.Width - 4
	}
	body := RenderHelpBody(w-4, m.SectionLabel)
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("12")).
		Padding(0, 1).
		Width(w).
		Render(m.HeaderStyle.Render("mreview pdf-review · keybindings") + "\n\n" + body + "\n\n" + m.DimStyle.Render("(any key to dismiss)"))
}

// overlayCenter draws overlay centered over base. base must already be a
// rendered string of (totalW × totalH); the result has the same shape.
func overlayCenter(base, overlay string, totalW, totalH int) string {
	overlayLines := strings.Split(overlay, "\n")
	oH := len(overlayLines)
	oW := 0
	for _, l := range overlayLines {
		if w := lipgloss.Width(l); w > oW {
			oW = w
		}
	}
	startY := (totalH - oH) / 2
	startX := (totalW - oW) / 2
	if startY < 0 {
		startY = 0
	}
	if startX < 0 {
		startX = 0
	}
	baseLines := strings.Split(base, "\n")
	for i, ol := range overlayLines {
		y := startY + i
		if y < 0 || y >= len(baseLines) {
			continue
		}
		baseLines[y] = overlayInLine(baseLines[y], ol, startX)
	}
	return strings.Join(baseLines, "\n")
}

func overlayInLine(base, overlay string, x int) string {
	// Cell-precise overlay over a possibly-styled base. Easiest: just
	// truncate base to x cells, append overlay, then pad. Some style state
	// will be lost on the line — acceptable for a help modal.
	left := truncateRight(base, x)
	pad := x - lipgloss.Width(left)
	if pad > 0 {
		left += strings.Repeat(" ", pad)
	}
	return left + overlay
}

// --- helpers ---------------------------------------------------------------

func kindLabel(k string) string {
	switch k {
	case KindComment:
		return "[ SUBSTANTIVE ]"
	case KindMinor:
		return "[ MINOR ]"
	case KindFramingIntro:
		return "[ FRAMING — intro ]"
	case KindFramingOutro:
		return "[ FRAMING — outro ]"
	case KindMeta:
		return "[ META ]"
	}
	return "[ " + k + " ]"
}

func statusIcon(s string) string {
	switch s {
	case StatusKept:
		return "✓"
	case StatusEdited:
		return "⏵"
	case StatusDropped:
		return "✗"
	}
	return "·"
}

func summarize(text string, maxW int) string {
	if maxW < 8 {
		maxW = 8
	}
	flat := strings.Join(strings.Fields(text), " ")
	if len([]rune(flat)) <= maxW {
		return flat
	}
	r := []rune(flat)
	return string(r[:maxW-1]) + "…"
}

func truncateRight(s string, w int) string {
	if w <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= w {
		return s
	}
	// Lipgloss doesn't expose an ANSI-aware truncate; fall back to a rune
	// count that's a good-enough approximation for our content (no wide
	// emoji, no embedded CJK in the list).
	r := []rune(stripANSIBasic(s))
	if len(r) <= w {
		return string(r)
	}
	return string(r[:w-1]) + "…"
}

func stripANSIBasic(s string) string {
	// Remove CSI sequences \x1b[…m. Sufficient for our list rendering;
	// help-modal overlay tolerates the loss already.
	var b strings.Builder
	skip := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if skip {
			if c == 'm' {
				skip = false
			}
			continue
		}
		if c == 0x1b && i+1 < len(s) && s[i+1] == '[' {
			skip = true
			i++
			continue
		}
		b.WriteByte(c)
	}
	return b.String()
}

func wrapLines(s string, w, maxLines int) string {
	if w < 8 {
		w = 8
	}
	flat := strings.Join(strings.Fields(s), " ")
	r := []rune(flat)
	var lines []string
	for len(r) > 0 && len(lines) < maxLines {
		take := w
		if take > len(r) {
			take = len(r)
		}
		// Try to break on the last space within the window.
		if take < len(r) {
			for i := take; i > 8; i-- {
				if r[i-1] == ' ' {
					take = i
					break
				}
			}
		}
		lines = append(lines, strings.TrimRight(string(r[:take]), " "))
		r = r[take:]
		for len(r) > 0 && r[0] == ' ' {
			r = r[1:]
		}
	}
	if len(r) > 0 && len(lines) > 0 {
		last := []rune(lines[len(lines)-1])
		if len(last) > w-1 {
			last = last[:w-1]
		}
		lines[len(lines)-1] = string(last) + "…"
	}
	return strings.Join(lines, "\n")
}

func clampLines(s string, h int) string {
	if h <= 0 {
		return ""
	}
	lines := strings.Split(s, "\n")
	if len(lines) <= h {
		return s
	}
	return strings.Join(lines[:h], "\n")
}

func baseName(p string) string {
	for i := len(p) - 1; i >= 0; i-- {
		if p[i] == '/' {
			return p[i+1:]
		}
	}
	return p
}
