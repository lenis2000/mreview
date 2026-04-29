package ui

import (
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"mreview/pkg/parser"
)

// searchInputCharLimit bounds the query string.
const searchInputCharLimit = 200

// searchInputWidth is the fallback width for the text input when the model
// has not yet received a WindowSizeMsg.
const searchInputWidth = 40

// SearchPopup hosts the `/` vim-style search prompt: a single text input.
// Submitting jumps to the next literal match in doc.Source from the cursor;
// the popup itself carries no result list — n/N repeat after closing.
type SearchPopup struct {
	Input textinput.Model
}

// popup marks SearchPopup as a Popup.
func (*SearchPopup) popup() {}

// NewSearchPopup builds a focused search prompt. The initial text is the
// previous query (vim's `//` repeats) so the user can either retype or
// just press Enter to re-search.
func NewSearchPopup(initial string) (*SearchPopup, tea.Cmd) {
	ti := textinput.New()
	ti.Placeholder = "search…"
	ti.CharLimit = searchInputCharLimit
	ti.Width = searchInputWidth
	if initial != "" {
		ti.SetValue(initial)
		ti.SetCursor(len([]rune(initial)))
	}
	cmd := ti.Focus()
	return &SearchPopup{Input: ti}, cmd
}

// OpenSearch opens the search prompt and pre-populates it with the last
// query so a bare Enter repeats the previous search.
func (m Model) OpenSearch() (tea.Model, tea.Cmd) {
	if m.Doc == nil {
		return m, nil
	}
	p, cmd := NewSearchPopup(m.LastSearch)
	m.Popup = p
	m.CountBuf = ""
	m.PendingG = false
	m.Status = ""
	return m, cmd
}

// submitSearch reads the prompt, stores the query as LastSearch, and jumps
// to the next forward match (wrapping at end of file). An empty query
// closes without moving.
func (m Model) submitSearch() (tea.Model, tea.Cmd) {
	p, ok := m.Popup.(*SearchPopup)
	if !ok {
		return m, nil
	}
	q := strings.TrimRight(p.Input.Value(), "\n")
	m.Popup = nil
	if q == "" {
		return m, nil
	}
	m.LastSearch = q
	return m.jumpToSearch(q, true, true), nil
}

// jumpToSearch moves the cursor to the next/previous occurrence of query
// in the source. forward selects the direction; advance=true skips the
// current cursor line so a repeated search makes progress (pressing n
// after the first match doesn't park on the same line).
func (m Model) jumpToSearch(query string, forward, advance bool) Model {
	if m.Doc == nil || query == "" {
		return m
	}
	startAbs, ok := absoluteCursorLine(m)
	if !ok {
		startAbs = 1
	}
	if advance {
		if forward {
			startAbs++
		} else {
			startAbs--
		}
	}
	abs, wrapped, found := findSourceMatch(m.Doc, startAbs, query, forward)
	if !found {
		m.Status = "search: pattern not found: " + query
		return m
	}
	prev := m.CursorBlockID
	if leaf := leafContainingLine(m.Doc, abs); leaf != nil {
		anchor := SnapToVisible(m.Doc, m.Sidecar, m.Filter, m.ExternalIssues, leaf.ID)
		m.CursorBlockID = anchor
		if ab := m.Doc.ByID[anchor]; ab != nil {
			m.SourceLineCursor = abs - ab.StartLine + 1
		} else {
			m.SourceLineCursor = abs - leaf.StartLine + 1
		}
	} else if other := blockContainingLine(m.Doc, abs); other != nil {
		anchor := SnapToVisible(m.Doc, m.Sidecar, m.Filter, m.ExternalIssues, other.ID)
		m.CursorBlockID = anchor
		if ab := m.Doc.ByID[anchor]; ab != nil {
			m.SourceLineCursor = abs - ab.StartLine + 1
		} else {
			m.SourceLineCursor = abs - other.StartLine + 1
		}
	}
	if prev != "" && prev != m.CursorBlockID {
		m.JumpStack.Push(prev)
	}
	switch {
	case wrapped && forward:
		m.Status = "search hit BOTTOM, continuing at TOP — /" + query
	case wrapped && !forward:
		m.Status = "search hit TOP, continuing at BOTTOM — ?" + query
	case forward:
		m.Status = "/" + query
	default:
		m.Status = "?" + query
	}
	return m
}

// findSourceMatch scans doc.Source for the next/previous line whose text
// contains query (smart-case: lowercase query is case-insensitive, mixed
// case is exact — vim's `set ignorecase + smartcase`). Wraps once around
// the document (vim's `wrapscan`). Returns the 1-based absolute line
// number plus whether the match required a wrap, so the caller can
// surface a "search hit BOTTOM…" status the way vim does.
func findSourceMatch(doc *parser.Document, fromAbs int, query string, forward bool) (line int, wrapped bool, ok bool) {
	if doc == nil || query == "" {
		return 0, false, false
	}
	lines := strings.Split(string(doc.Source), "\n")
	total := len(lines)
	if total == 0 {
		return 0, false, false
	}
	if fromAbs < 1 {
		fromAbs = 1
	}
	if fromAbs > total {
		fromAbs = total
	}
	needle := query
	hayCase := func(s string) string { return s }
	if isAllLower(query) {
		needle = strings.ToLower(query)
		hayCase = strings.ToLower
	}
	step := 1
	if !forward {
		step = -1
	}
	// Two passes: from fromAbs to either end, then wrap to the other end and
	// continue back to fromAbs. The second pass is the "wrapped" hit.
	for pass := 0; pass < 2; pass++ {
		i := fromAbs
		for i >= 1 && i <= total {
			if strings.Contains(hayCase(lines[i-1]), needle) {
				return i, pass == 1, true
			}
			i += step
		}
		if forward {
			fromAbs = 1
		} else {
			fromAbs = total
		}
	}
	return 0, false, false
}

// isAllLower reports whether s contains no uppercase letters. Used by the
// smart-case test in findSourceMatch.
func isAllLower(s string) bool {
	for _, r := range s {
		if r >= 'A' && r <= 'Z' {
			return false
		}
	}
	return true
}

// SearchAgain re-runs the most recent search starting from the cursor and
// stepping in the requested direction. Bound to n / N at the top level.
func (m Model) SearchAgain(forward bool) Model {
	if m.LastSearch == "" {
		m.Status = "search: no previous pattern"
		return m
	}
	return m.jumpToSearch(m.LastSearch, forward, true)
}
