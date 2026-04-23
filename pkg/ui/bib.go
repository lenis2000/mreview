package ui

import (
	"strings"

	"mreview/pkg/parser"
)

// BibPopup shows the bibliography entry resolved from a \cite{key} reference
// on (or near) the cursor block. When the cite target has no matching .bbl
// entry, Entry is nil and the body renders a muted placeholder.
type BibPopup struct {
	Key   string
	Entry *parser.BibEntry
}

// popup is the Popup marker method.
func (*BibPopup) popup() {}

// FirstCiteRef returns the first outgoing \cite target on b, or "" when b has
// no cite refs.
func FirstCiteRef(b *parser.Block) string {
	if b == nil {
		return ""
	}
	for _, r := range b.RefsOut {
		if r.Kind == "cite" && r.Target != "" {
			return r.Target
		}
	}
	return ""
}

// OpenBibPopup inspects the cursor block for an outgoing \cite ref. When one
// is present the popup opens with its .bbl entry attached (Entry may still be
// nil when the key is absent from the bibliography). Otherwise a status-line
// message reports that no cite was found.
func (m Model) OpenBibPopup() Model {
	m.CountBuf = ""
	if m.Doc == nil {
		m.Status = "gd: no cite under cursor"
		return m
	}
	b := m.Doc.ByID[m.CursorBlockID]
	key := FirstCiteRef(b)
	if key == "" {
		m.Status = "gd: no cite under cursor"
		return m
	}
	entry := m.Doc.BibEntries[key]
	m.Popup = &BibPopup{Key: key, Entry: entry}
	m.Status = ""
	return m
}

// RenderBibBody formats the popup content for the source-pane overlay.
func RenderBibBody(p *BibPopup, innerW, innerH int, styles Styles) string {
	if p == nil {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("key: ")
	sb.WriteString(p.Key)
	sb.WriteByte('\n')
	if p.Entry == nil {
		sb.WriteString(styles.OutlineMuted.Render("(no bibliography entry — .bbl missing or key undefined)"))
		sb.WriteByte('\n')
		sb.WriteString(styles.OutlineMuted.Render("[Esc close]"))
		return sb.String()
	}
	if p.Entry.Authors != "" {
		sb.WriteString("authors: ")
		sb.WriteString(p.Entry.Authors)
		sb.WriteByte('\n')
	}
	if p.Entry.Title != "" {
		sb.WriteString("title: ")
		sb.WriteString(p.Entry.Title)
		sb.WriteByte('\n')
	}
	sb.WriteByte('\n')
	sb.WriteString(p.Entry.Text)
	sb.WriteByte('\n')
	sb.WriteString(styles.OutlineMuted.Render("[Esc close]"))
	return sb.String()
}
