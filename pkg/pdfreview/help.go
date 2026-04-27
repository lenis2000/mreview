package pdfreview

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// HelpPopup is the `?` keybinding overlay. Stateless; all rows come from
// HelpRows.
type HelpPopup struct{}

func (*HelpPopup) popup() {}

// CommentDetailPopup shows the full original_text plus metadata for the
// currently-selected comment. Stateless — the renderer reads the current
// selection — so j/k navigation keeps the popup in sync.
type CommentDetailPopup struct{}

func (*CommentDetailPopup) popup() {}

// HelpRow pairs a key binding with its one-line description.
type HelpRow struct {
	Keys, Desc string
}

// HelpRows returns the keybinding table, grouped with blank-Keys section
// headers. The renderer renders blanks as bold section labels.
func HelpRows() []HelpRow {
	return []HelpRow{
		{"", "Navigation"},
		{"j / k  ↓ / ↑", "next / prev comment"},
		{"} / {", "next / prev kind bucket"},
		{"g / G", "first / last comment"},
		{"enter, space", "jump PDF to current comment's page (highlight quote)"},
		{"] / [", "next / prev page (PDF only, decoupled from selection)"},
		{"+ / -", "zoom PDF in / out"},

		{"", "Status"},
		{"K", "mark current comment kept"},
		{"D", "mark dropped"},
		{"c", "cycle kind (comment → minor → framing-* → meta → …)"},

		{"", "Edit"},
		{"e", "edit original_text only in $EDITOR"},
		{"E", "edit YAML (text, page, quote, kind) in $EDITOR — re-anchor"},
		{"n", "new manual comment at the current page"},

		{"", "Output"},
		{"w", "write PAPER.review.md now (no exit)"},
		{"q  :q", "save JSON, write letter, exit"},
		{"Q", "save JSON only, no letter, exit"},

		{"", "Misc"},
		{"v", "view full comment text (non-modal; esc or v to close)"},
		{"S", "open current page in Skim (macOS)"},
		{"?", "toggle this help"},
	}
}

// RenderHelpBody formats the rows into a two-column block of width innerW.
func RenderHelpBody(innerW int, headerStyle lipgloss.Style) string {
	rows := HelpRows()
	keyW := 0
	for _, r := range rows {
		if r.Keys != "" && len(r.Keys) > keyW {
			keyW = len(r.Keys)
		}
	}
	if keyW < 6 {
		keyW = 6
	}
	var sb strings.Builder
	for i, r := range rows {
		if r.Keys == "" {
			if i > 0 {
				sb.WriteByte('\n')
			}
			sb.WriteString(headerStyle.Render(r.Desc))
		} else {
			sb.WriteString(r.Keys)
			sb.WriteString(strings.Repeat(" ", keyW-len(r.Keys)+2))
			sb.WriteString(r.Desc)
		}
		if i < len(rows)-1 {
			sb.WriteByte('\n')
		}
	}
	return sb.String()
}
