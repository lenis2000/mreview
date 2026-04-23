package ui

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"mreview/pkg/parser"
)

const bibSampleTeX = `\documentclass{amsart}
\newtheorem{theorem}{Theorem}
\begin{document}
\section{Intro}

\begin{theorem}\label{thm:plain}
A plain statement with no cite.
\end{theorem}

\begin{theorem}\label{thm:cited}
Extending \cite{Knuth}.
\end{theorem}

\end{document}
`

func bibDoc(t *testing.T) *parser.Document {
	t.Helper()
	doc, err := parser.Parse([]byte(bibSampleTeX))
	require.NoError(t, err)
	require.NotNil(t, doc)
	// Seed a fake bibliography entry so OpenBibPopup can resolve it.
	doc.BibEntries["Knuth"] = &parser.BibEntry{
		Key:     "Knuth",
		Authors: "D. E. Knuth",
		Title:   "The Art of Computer Programming",
		Text:    "D. E. Knuth. The Art of Computer Programming.",
	}
	return doc
}

func TestFirstCiteRef(t *testing.T) {
	doc := bibDoc(t)
	// find the block containing the cite
	var host *parser.Block
	for _, b := range doc.Blocks {
		for _, r := range b.RefsOut {
			if r.Kind == "cite" && r.Target == "Knuth" {
				host = b
				break
			}
		}
		if host != nil {
			break
		}
	}
	require.NotNil(t, host)
	assert.Equal(t, "Knuth", FirstCiteRef(host))
	// Block with no refs returns "".
	empty := &parser.Block{}
	assert.Equal(t, "", FirstCiteRef(empty))
	assert.Equal(t, "", FirstCiteRef(nil))
}

func TestOpenBibPopup_HitAndMiss(t *testing.T) {
	doc := bibDoc(t)
	// Find the block carrying the cite so we can anchor the cursor there.
	var host *parser.Block
	for _, b := range doc.Blocks {
		if FirstCiteRef(b) == "Knuth" {
			host = b
			break
		}
	}
	require.NotNil(t, host)

	m := New(doc, nil)
	m.CursorBlockID = host.ID
	out := m.OpenBibPopup()
	require.IsType(t, &BibPopup{}, out.Popup)
	pop := out.Popup.(*BibPopup)
	assert.Equal(t, "Knuth", pop.Key)
	require.NotNil(t, pop.Entry)
	assert.Equal(t, "D. E. Knuth", pop.Entry.Authors)
	assert.Empty(t, out.Status)

	// Move cursor to a block without any outgoing cite -> status message,
	// popup stays nil.
	blankID := ""
	for _, b := range doc.Blocks {
		if b.ID == "root" {
			continue
		}
		if FirstCiteRef(b) == "" {
			blankID = b.ID
			break
		}
	}
	require.NotEmpty(t, blankID)
	m.CursorBlockID = blankID
	m.Popup = nil
	out = m.OpenBibPopup()
	assert.Nil(t, out.Popup)
	assert.Contains(t, out.Status, "no cite")
}

func TestOpenBibPopup_MissingBBLEntry(t *testing.T) {
	doc := bibDoc(t)
	delete(doc.BibEntries, "Knuth")
	var host *parser.Block
	for _, b := range doc.Blocks {
		if FirstCiteRef(b) == "Knuth" {
			host = b
			break
		}
	}
	require.NotNil(t, host)
	m := New(doc, nil)
	m.CursorBlockID = host.ID
	out := m.OpenBibPopup()
	require.IsType(t, &BibPopup{}, out.Popup)
	pop := out.Popup.(*BibPopup)
	assert.Equal(t, "Knuth", pop.Key)
	assert.Nil(t, pop.Entry)

	body := RenderBibBody(pop, 80, 20, DefaultStyles())
	assert.Contains(t, body, "Knuth")
	assert.Contains(t, body, "no bibliography entry")
}

func TestRenderBibBody_Content(t *testing.T) {
	p := &BibPopup{
		Key: "Knuth",
		Entry: &parser.BibEntry{
			Key:     "Knuth",
			Authors: "D. E. Knuth",
			Title:   "TAOCP",
			Text:    "D. E. Knuth. TAOCP.",
		},
	}
	body := RenderBibBody(p, 80, 20, DefaultStyles())
	assert.Contains(t, body, "Knuth")
	assert.Contains(t, body, "D. E. Knuth")
	assert.Contains(t, body, "TAOCP")
}
