package ui

import "strings"

// HelpPopup is the `?` keybinding-table overlay. It carries no state — the
// rendered rows come straight from HelpRows.
type HelpPopup struct{}

// popup is the Popup marker method.
func (*HelpPopup) popup() {}

// OpenHelp toggles the help popup on.
func (m Model) OpenHelp() Model {
	m.CountBuf = ""
	m.PendingG = false
	m.Popup = &HelpPopup{}
	m.Status = ""
	return m
}

// HelpRow pairs a key binding with its one-line description.
type HelpRow struct {
	Keys string
	Desc string
}

// HelpRows returns the keybinding table presented by the help overlay. The
// rows are deterministic so tests can assert on individual entries.
func HelpRows() []HelpRow {
	return []HelpRow{
		{"j / k", "next / prev outer sibling"},
		{"J / K", "next / prev inner block (proof-step, display, …)"},
		{"{ / }", "previous / next section"},
		{"gg / G", "first / last visible block"},
		{"<N><motion>", "repeat motion N times"},
		{"go", "jump to first resolved ref"},
		{"gu", "list blocks referring to current label"},
		{"gd", "show bib entry for first cite in block"},
		{"Ctrl-O / Ctrl-I", "jump back / forward"},
		{"a / A", "annotate current block / enclosing env"},
		{"e", "edit existing annotation"},
		{"d", "delete annotation (y/N)"},
		{"space", "toggle reviewed (auto-advance on unreviewed filter)"},
		{"/", "fuzzy search"},
		{"@", "annotation list"},
		{"f", "cycle filter (all / unreviewed / annotated / issues)"},
		{"h / l", "focus outline / source pane"},
		{"?", "toggle this help overlay"},
		{"q", "quit and emit annotations"},
	}
}

// RenderHelpBody formats the rows into a two-column table of width innerW.
// When innerW is too small to fit the full line, the description is
// truncated; keys are never truncated.
func RenderHelpBody(innerW int) string {
	rows := HelpRows()
	keyW := 0
	for _, r := range rows {
		if n := len(r.Keys); n > keyW {
			keyW = n
		}
	}
	var sb strings.Builder
	for i, r := range rows {
		line := r.Keys + strings.Repeat(" ", keyW-len(r.Keys)+2) + r.Desc
		sb.WriteString(truncateToWidth(line, innerW))
		if i < len(rows)-1 {
			sb.WriteByte('\n')
		}
	}
	return sb.String()
}
