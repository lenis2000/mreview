package ui

import (
	"strings"

	"github.com/mattn/go-runewidth"
)

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

// HelpRow pairs a key binding (or marker glyph) with its one-line
// description. A row with empty Keys *and* empty Desc renders as a
// blank separator between sections; a row with empty Keys and
// non-empty Desc renders as a bold section header.
type HelpRow struct {
	Keys string
	Desc string
}

// HelpRows returns the keybinding table presented by the help overlay. The
// rows are deterministic so tests can assert on individual entries.
//
// allowMods controls a banner pair at the top: when false (the default
// read-only mode) the banner explains that `e` / `E` are gated behind
// --allow-modifications. The edit-key rows themselves are kept either
// way so the binding table stays a complete reference.
func HelpRows(allowMods bool) []HelpRow {
	var rows []HelpRow
	if !allowMods {
		rows = append(rows,
			HelpRow{"", "READ-ONLY — pass --allow-modifications to enable e / E"},
			HelpRow{"", ""},
		)
	}
	rows = append(rows, []HelpRow{
		{"j / k", "next / prev outer sibling"},
		{"[ / ]", "prev / next block (works in source pane too)"},
		{"J / K", "next / prev inner block (proof-step, display, …)"},
		{"{ / }", "previous / next section"},
		{"gg / G", "first / last visible block"},
		{"<N><motion>", "repeat motion N times"},
		{"go", "jump to first resolved ref"},
		{"gu", "list blocks referring to current label"},
		{"gd", "show bib entry for first cite in block"},
		{"Ctrl-O / Ctrl-I", "jump back / forward"},
		{"a / A", "annotate current line / block"},
		{"Ctrl-A", "edit existing annotation at cursor"},
		{"e / E", "inline edit source line / open $EDITOR on paper.tex"},
		{"u", "undo last in-place edit (e or E)"},
		{"Ctrl-R", "redo (replay an undone edit)"},
		{"d", "delete annotation (y/N) — line-pinned preferred"},
		{"D", "delete paragraph (block-level) annotation (y/N)"},
		{"space", "toggle reviewed (auto-advance on unreviewed filter)"},
		{"/", "search source (vim-style; n / N repeat)"},
		{"n / N", "repeat last search forward / backward"},
		{"@", "annotation list"},
		{"f", "cycle filter (all / unreviewed / annotated / issues)"},
		{"h / l / ← / →", "focus pane left / right (outline ↔ source ↔ PDF)"},
		{"< / >", "shrink / grow focused pane (saved globally)"},
		{"S", "open current PDF in Skim at cursor line"},
		{"R", "reload PDF in Skim and TUI pane (revert without jumping)"},
		{"B", "build (reparse + latexmk; lmkf-aware)"},
		{"?", "toggle this help overlay"},
		{"q", "quit and emit annotations"},
		{"", ""},
		{"", "Outline markers"},
		{MarkerAnnotated, "block has an annotation"},
		{MarkerReviewed, "block marked reviewed"},
		{MarkerUnresolved, "block has an unresolved \\ref / \\cite"},
		{MarkerNoRegion, "no PDF region (SyncTeX miss)"},
		{"", ""},
		{"", "Issue markers (filter:issues)"},
	}...)
	for _, m := range IssueMarkers() {
		rows = append(rows, HelpRow{Keys: m.Glyph, Desc: m.Desc})
	}
	rows = append(rows, HelpRow{Keys: MarkerExternal, Desc: "other fmt-report diagnostic (rule with no dedicated marker)"})
	return rows
}

// RenderHelpBody formats the rows into a two-column table of width innerW.
// When innerW is too small to fit the full line, the description is
// truncated; keys are never truncated. Display width (runewidth) is
// used so emoji-keyed marker rows align with text-keyed binding rows.
// Rows with empty Keys render as section headers (or blank lines if
// Desc is also empty).
func RenderHelpBody(innerW int, allowMods bool) string {
	rows := HelpRows(allowMods)
	keyW := 0
	for _, r := range rows {
		if r.Keys == "" {
			continue
		}
		if w := runewidth.StringWidth(r.Keys); w > keyW {
			keyW = w
		}
	}
	var sb strings.Builder
	for i, r := range rows {
		var line string
		switch {
		case r.Keys == "" && r.Desc == "":
			line = ""
		case r.Keys == "":
			line = r.Desc
		default:
			pad := keyW - runewidth.StringWidth(r.Keys) + 2
			if pad < 1 {
				pad = 1
			}
			line = r.Keys + strings.Repeat(" ", pad) + r.Desc
		}
		sb.WriteString(truncateToWidth(line, innerW))
		if i < len(rows)-1 {
			sb.WriteByte('\n')
		}
	}
	return sb.String()
}
