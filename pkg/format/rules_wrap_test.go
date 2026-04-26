package format

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func runWrap(src, mode string, col int) string {
	res := Apply([]byte(src), Options{
		Wrap: WrapOptions{Mode: mode, Col: col},
	})
	return string(res.Src)
}

func TestWrap_OffIsNoop(t *testing.T) {
	// No trailing whitespace so space.trailing doesn't fire.
	src := strings.Repeat("a ", 199) + "a\n"
	got := runWrap(src, "off", 100)
	assert.Equal(t, src, got)
}

func TestWrap_SentenceBoundary(t *testing.T) {
	src := "First sentence. Second sentence. Third sentence.\n"
	got := runWrap(src, "sentence", 100)
	want := "First sentence.\nSecond sentence.\nThird sentence.\n"
	assert.Equal(t, want, got)
}

func TestWrap_PreservesIndent(t *testing.T) {
	// Use spaces, not a tab — space.tabs runs before us and would expand a tab.
	src := "    First sentence. Second sentence.\n"
	got := runWrap(src, "sentence", 100)
	want := "    First sentence.\n    Second sentence.\n"
	assert.Equal(t, want, got)
}

func TestWrap_AbbreviationsKeepSentence(t *testing.T) {
	// "et al.", "e.g.", "i.e." must NOT split.
	src := "Smith et al. showed that e.g. this works. Therefore we proceed.\n"
	got := runWrap(src, "sentence", 200)
	want := "Smith et al. showed that e.g. this works.\nTherefore we proceed.\n"
	assert.Equal(t, want, got)
}

func TestWrap_DoesNotBreakInsideMath(t *testing.T) {
	// The period inside $...$ must not start a new sentence.
	src := `Equation $a.b$. Next sentence.` + "\n"
	got := runWrap(src, "sentence", 200)
	want := "Equation $a.b$.\nNext sentence.\n"
	assert.Equal(t, want, got)
}

func TestWrap_DoesNotBreakInsideRefCommands(t *testing.T) {
	// `\eqref{eq.1}.` followed by space + capital should split AFTER \eqref{}.
	src := `See \eqref{eq:1}. Next sentence.` + "\n"
	got := runWrap(src, "sentence", 200)
	want := "See \\eqref{eq:1}.\nNext sentence.\n"
	assert.Equal(t, want, got)
}

func TestWrap_ColumnGreedy(t *testing.T) {
	// "alpha bravo charlie delta echo foxtrot golf" with col=20 should
	// break at the rightmost space ≤ 20 columns.
	src := "alpha bravo charlie delta echo foxtrot golf\n"
	got := runWrap(src, "column", 20)
	// First piece must end at or before col=20.
	first := strings.SplitN(got, "\n", 2)[0]
	if visualWidth(first) > 20 {
		t.Fatalf("first piece exceeds col=20: %q", first)
	}
}

func TestWrap_SentencePlusColumn(t *testing.T) {
	// Single long sentence — sentence-only would not split it; sentence+column does.
	long := strings.Repeat("word ", 30) + "end."
	src := long + "\n"
	got := runWrap(src, "sentence+column", 60)
	if !strings.Contains(got, "\n") {
		t.Fatalf("sentence+column must wrap a too-long sentence: %q", got)
	}
}

func TestWrap_VerbatimNotTouched(t *testing.T) {
	src := strings.Join([]string{
		"\\begin{verbatim}",
		"This is one long verbatim line that exceeds the column limit easily.",
		"\\end{verbatim}",
		"",
	}, "\n")
	got := runWrap(src, "sentence+column", 30)
	assert.Equal(t, src, got, "verbatim content must not be wrapped")
}

func TestWrap_SkipDirective(t *testing.T) {
	src := "Long sentence that exceeds the limit. Another sentence. % mreview-fmt: skip\n"
	got := runWrap(src, "sentence", 200)
	assert.Equal(t, src, got, "skipped lines must not be wrapped")
}

func TestWrap_TrailingCommentSticksToLastPiece(t *testing.T) {
	// Trailing comment on a multi-sentence line: comment must end up on
	// the line of the last wrapped piece (not get duplicated or re-anchored).
	src := "First sentence. Second sentence. % side note\n"
	got := runWrap(src, "sentence", 200)
	// Should split into two prose lines; the comment stays on the second.
	assert.Equal(t, "First sentence.\nSecond sentence. % side note\n", got)
}

func TestWrap_Idempotent(t *testing.T) {
	src := "First sentence. Second sentence. Third sentence.\n"
	once := runWrap(src, "sentence", 100)
	twice := runWrap(once, "sentence", 100)
	assert.Equal(t, once, twice, "wrap must be idempotent")
}
