package ui

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"mreview/pkg/parser"
)

func TestRenderSource_IncludesLineNumbers(t *testing.T) {
	doc := outlineDoc(t)
	// Pick the proof block so the source pane has meaningful multi-line body.
	var proof *parser.Block
	for _, b := range doc.Blocks {
		if b.Kind == parser.KindProof {
			proof = b
			break
		}
	}
	require.NotNil(t, proof)

	out := RenderSource(doc, proof.ID, 60, 20, DefaultStyles(), false)
	plain := stripANSI(out)
	assert.Contains(t, plain, "proof") // the \begin{proof} command text
	// The first visible line should start with the proof's StartLine padded
	// with a gutter separator.
	firstLine := strings.SplitN(plain, "\n", 2)[0]
	assert.Regexp(t, `^\s*\d+\s`, firstLine, "first line must carry a line-number gutter")
}

func TestRenderSource_EmptyCursor(t *testing.T) {
	out := RenderSource(nil, "", 40, 5, DefaultStyles(), false)
	assert.Contains(t, stripANSI(out), "no block selected")
}

func TestRenderSource_UnknownCursor(t *testing.T) {
	doc := outlineDoc(t)
	out := RenderSource(doc, "does-not-exist", 40, 5, DefaultStyles(), false)
	assert.Contains(t, stripANSI(out), "unknown block")
}

func TestColorizeLaTeXLine_RecognisesCommandsAndMath(t *testing.T) {
	s := DefaultStyles()
	// Basic command detection — output must contain the backslash intact.
	got := colorizeLaTeXLine(`\begin{proof} and $x^2$ and % comment`, s)
	plain := stripANSI(got)
	assert.Contains(t, plain, `\begin`)
	assert.Contains(t, plain, `$x^2$`)
	assert.Contains(t, plain, `% comment`)
}

func TestColorizeLaTeXLine_CommentTerminatesLine(t *testing.T) {
	s := DefaultStyles()
	got := colorizeLaTeXLine(`abc % rest of line \alpha`, s)
	// `\alpha` is inside the comment, so it must not be treated as a command
	// (no separate styling boundary would leak out as extra ANSI segments).
	plain := stripANSI(got)
	assert.Equal(t, "abc % rest of line \\alpha", plain)
}

func TestColorizeLaTeXLine_DollarDelimiters(t *testing.T) {
	s := DefaultStyles()
	got := colorizeLaTeXLine(`text $$display$$ text`, s)
	assert.Contains(t, stripANSI(got), "$$display$$")
}

func TestStripANSI_RemovesCSISequences(t *testing.T) {
	in := "\x1b[31mhello\x1b[0m world"
	assert.Equal(t, "hello world", stripANSI(in))
}

func TestItoa_Basic(t *testing.T) {
	assert.Equal(t, "0", itoa(0))
	assert.Equal(t, "42", itoa(42))
	assert.Equal(t, "-7", itoa(-7))
}
