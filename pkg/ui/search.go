package ui

import (
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/sahilm/fuzzy"

	"mreview/pkg/parser"
)

// searchHaystackLimit caps how many source characters feed the fuzzy matcher
// per block, so a long proof does not dominate ranking.
const searchHaystackLimit = 200

// searchResultLimit caps the visible result list. Blocks beyond this do not
// appear; the user can refine the query to surface them.
const searchResultLimit = 50

// searchInputCharLimit bounds the query string.
const searchInputCharLimit = 120

// searchInputWidth is the fallback width for the text input when the model
// has not yet received a WindowSizeMsg.
const searchInputWidth = 40

// SearchEntry is one row in the fuzzy-search index: a block ID plus the
// haystack string the matcher scores against (title | label | number |
// first N chars of source).
type SearchEntry struct {
	BlockID  string
	Display  string // "<breadcrumb> — <snippet>"
	Haystack string
}

// SearchIndex is a preprocessed slice of search entries for a document.
// Entries are in document order; unfiltered display is the same order.
type SearchIndex struct {
	Entries []SearchEntry
}

// BuildSearchIndex walks the document in order and emits one SearchEntry per
// non-root block. The root is skipped. Blocks without meaningful text still
// appear but score poorly unless the query matches the haystack.
func BuildSearchIndex(doc *parser.Document) SearchIndex {
	if doc == nil {
		return SearchIndex{}
	}
	out := make([]SearchEntry, 0, len(doc.Blocks))
	for _, b := range doc.Blocks {
		if b == nil || b.ID == "" || b.ID == "root" {
			continue
		}
		out = append(out, SearchEntry{
			BlockID:  b.ID,
			Display:  searchDisplay(doc, b),
			Haystack: searchHaystack(b),
		})
	}
	return SearchIndex{Entries: out}
}

// searchHaystack builds the scoring string for a block: title, label, number,
// env-name and the first searchHaystackLimit characters of source, all joined
// with a separator that fuzzy.Find treats as a word boundary.
func searchHaystack(b *parser.Block) string {
	var parts []string
	if b.Title != "" {
		parts = append(parts, b.Title)
	}
	if b.Label != "" {
		parts = append(parts, b.Label)
	}
	if b.Number != "" {
		parts = append(parts, b.Number)
	}
	if b.EnvName != "" {
		parts = append(parts, b.EnvName)
	}
	src := b.Source
	if len(src) > searchHaystackLimit {
		src = src[:searchHaystackLimit]
	}
	src = strings.ReplaceAll(src, "\n", " ")
	if src != "" {
		parts = append(parts, src)
	}
	return strings.Join(parts, " / ")
}

// searchDisplay is the user-facing row for an entry: the breadcrumb followed
// by a short snippet of the block contents so near-duplicates are
// distinguishable.
func searchDisplay(doc *parser.Document, b *parser.Block) string {
	bc := AnnotationBreadcrumb(doc, b.ID)
	if bc == "" {
		bc = b.Kind.String()
	}
	snippet := firstSnippet(b.Source, 60)
	if snippet != "" && snippet != bc {
		return bc + " — " + snippet
	}
	return bc
}

// Match ranks the index against a query, returning entries in descending
// score order (best first). An empty or whitespace-only query returns all
// entries in document order.
func (idx SearchIndex) Match(query string) []SearchEntry {
	q := strings.TrimSpace(query)
	if q == "" {
		out := make([]SearchEntry, len(idx.Entries))
		copy(out, idx.Entries)
		return out
	}
	pool := make([]string, len(idx.Entries))
	for i, e := range idx.Entries {
		pool[i] = e.Haystack
	}
	matches := fuzzy.Find(q, pool)
	out := make([]SearchEntry, 0, len(matches))
	for _, m := range matches {
		if m.Index < 0 || m.Index >= len(idx.Entries) {
			continue
		}
		out = append(out, idx.Entries[m.Index])
	}
	return out
}

// SearchPopup hosts the `/` fuzzy-search modal: a text input for the query
// and a scrollable list of ranked matches. Navigation: j/k / up/down / Ctrl-N
// / Ctrl-P move the cursor; Enter jumps; Esc / Ctrl-C closes.
type SearchPopup struct {
	Input   textinput.Model
	Index   SearchIndex
	Results []SearchEntry
	Cursor  int
}

// popup marks SearchPopup as a Popup.
func (*SearchPopup) popup() {}

// NewSearchPopup builds a focused search popup over the document. The
// textinput starts empty; the initial Results list is the full index so an
// immediate Enter selects the first block.
func NewSearchPopup(doc *parser.Document) (*SearchPopup, tea.Cmd) {
	ti := textinput.New()
	ti.Placeholder = "fuzzy search…"
	ti.CharLimit = searchInputCharLimit
	ti.Width = searchInputWidth
	cmd := ti.Focus()
	idx := BuildSearchIndex(doc)
	p := &SearchPopup{Input: ti, Index: idx}
	p.refresh()
	return p, cmd
}

// refresh re-ranks Results against the current input. Called after every
// query mutation.
func (p *SearchPopup) refresh() {
	p.Results = p.Index.Match(p.Input.Value())
	if len(p.Results) > searchResultLimit {
		p.Results = p.Results[:searchResultLimit]
	}
	if p.Cursor >= len(p.Results) {
		p.Cursor = 0
	}
	if p.Cursor < 0 {
		p.Cursor = 0
	}
}

// Move shifts the cursor within Results, clamping at the ends (no wrap).
// Returning to the first / last row on over-shoot keeps arrow-repeat sane.
func (p *SearchPopup) Move(delta int) {
	if len(p.Results) == 0 {
		p.Cursor = 0
		return
	}
	p.Cursor += delta
	if p.Cursor < 0 {
		p.Cursor = 0
	}
	if p.Cursor >= len(p.Results) {
		p.Cursor = len(p.Results) - 1
	}
}

// Selected returns the BlockID of the highlighted row, or "" when Results is
// empty.
func (p *SearchPopup) Selected() string {
	if len(p.Results) == 0 {
		return ""
	}
	if p.Cursor < 0 || p.Cursor >= len(p.Results) {
		return ""
	}
	return p.Results[p.Cursor].BlockID
}

// OpenSearch opens the search popup and returns the initial focus command.
func (m Model) OpenSearch() (tea.Model, tea.Cmd) {
	if m.Doc == nil {
		return m, nil
	}
	p, cmd := NewSearchPopup(m.Doc)
	m.Popup = p
	m.CountBuf = ""
	m.PendingG = false
	m.Status = ""
	return m, cmd
}

// submitSearch jumps to the currently-selected result, pushing the jump
// stack, and closes the popup. A no-result or same-block submit just closes.
func (m Model) submitSearch() (tea.Model, tea.Cmd) {
	p, ok := m.Popup.(*SearchPopup)
	if !ok {
		return m, nil
	}
	target := p.Selected()
	m.Popup = nil
	if target == "" || target == m.CursorBlockID {
		return m, nil
	}
	m.JumpStack.Push(m.CursorBlockID)
	m.CursorBlockID = target
	m.SourceLineCursor = 1
	return m, nil
}
