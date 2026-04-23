package persist

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadMissingFileReturnsEmpty(t *testing.T) {
	s, err := Load(filepath.Join(t.TempDir(), "does-not-exist.mreview.md"))
	require.NoError(t, err)
	require.NotNil(t, s)
	assert.Empty(t, s.Paper)
	assert.Empty(t, s.Annotations)
	assert.Empty(t, s.Reviewed)
}

func TestMarshalIncludesLineNumbersInHeading(t *testing.T) {
	s := &Sidecar{
		Paper: "main.tex",
		PDF:   "main.pdf",
		Annotations: []Annotation{{
			BlockID:     "thm:main",
			Breadcrumb:  "Theorem 3.2 (Main)",
			File:        "main.tex",
			StartLine:   42,
			EndLine:     58,
			SourceQuote: "\\begin{theorem}\nStatement.\n\\end{theorem}",
			Note:        "check hypothesis",
		}},
	}
	out, err := Marshal(s)
	require.NoError(t, err)
	text := string(out)
	assert.Contains(t, text, "## Theorem 3.2 (Main) — `thm:main` (main.tex:L42-L58)")
	assert.Contains(t, text, "> \\begin{theorem}")
	assert.Contains(t, text, "check hypothesis")
}

func TestRoundTripPreservesAllFields(t *testing.T) {
	in := &Sidecar{
		Paper:    "paper.tex",
		PDF:      "paper.pdf",
		Cursor:   "thm:main",
		Reviewed: []string{"thm:main", "lem:foo"},
		Annotations: []Annotation{
			{
				BlockID:     "thm:main",
				Breadcrumb:  "Theorem 1 (Main)",
				File:        "paper.tex",
				StartLine:   10,
				EndLine:     20,
				SourceQuote: "\\begin{theorem}\\label{thm:main}\nLet $x \\in X$.\n\\end{theorem}",
				Note:        "first annotation\nwith multi-line note",
			},
			{
				BlockID:     "lem:foo",
				Breadcrumb:  "Lemma 2",
				File:        "paper.tex",
				StartLine:   30,
				EndLine:     35,
				SourceQuote: "\\begin{lemma}\\label{lem:foo}\nContent.\n\\end{lemma}",
				Note:        "second note",
			},
		},
	}
	path := filepath.Join(t.TempDir(), "paper.mreview.md")
	require.NoError(t, Save(path, in))
	out, err := Load(path)
	require.NoError(t, err)
	assert.Equal(t, in.Paper, out.Paper)
	assert.Equal(t, in.PDF, out.PDF)
	assert.Equal(t, in.Cursor, out.Cursor)
	assert.Equal(t, in.Reviewed, out.Reviewed)
	require.Len(t, out.Annotations, 2)
	for i := range in.Annotations {
		a, b := in.Annotations[i], out.Annotations[i]
		assert.Equal(t, a.BlockID, b.BlockID)
		assert.Equal(t, a.Breadcrumb, b.Breadcrumb)
		assert.Equal(t, a.File, b.File)
		assert.Equal(t, a.StartLine, b.StartLine)
		assert.Equal(t, a.EndLine, b.EndLine)
		assert.Equal(t, a.SourceQuote, b.SourceQuote)
		assert.Equal(t, a.Note, b.Note)
	}
}

func TestTruncateQuoteHonoursSixLineBudget(t *testing.T) {
	long := strings.Join([]string{"l1", "l2", "l3", "l4", "l5", "l6", "l7", "l8"}, "\n")
	lines := truncateQuote(long)
	assert.Len(t, lines, MaxQuoteLines)
	assert.Equal(t, EllipsisLine, lines[MaxQuoteLines-1-2]) // ellipsis before the last two lines
	assert.Equal(t, "l1", lines[0])
	assert.Equal(t, "l8", lines[len(lines)-1])
}

func TestTruncateQuoteShortInputUnchanged(t *testing.T) {
	src := "a\nb\nc"
	lines := truncateQuote(src)
	assert.Equal(t, []string{"a", "b", "c"}, lines)
}

func TestMarshalTruncatesLongQuote(t *testing.T) {
	long := strings.Join([]string{"a1", "a2", "a3", "a4", "a5", "a6", "a7"}, "\n")
	s := &Sidecar{
		Paper: "x.tex",
		Annotations: []Annotation{{
			BlockID:     "b1",
			Breadcrumb:  "B",
			File:        "x.tex",
			StartLine:   1,
			EndLine:     7,
			SourceQuote: long,
			Note:        "n",
		}},
	}
	out, err := Marshal(s)
	require.NoError(t, err)
	text := string(out)
	assert.Contains(t, text, "> "+EllipsisLine)
	assert.Contains(t, text, "> a1")
	assert.Contains(t, text, "> a7")
	assert.NotContains(t, text, "> a5\n")
}

func TestLoadStandaloneMarkdownWithoutFrontmatter(t *testing.T) {
	body := "## Foo — `b1` (a.tex:L1-L2)\n\n> content\n\nnote\n"
	path := filepath.Join(t.TempDir(), "x.mreview.md")
	require.NoError(t, os.WriteFile(path, []byte(body), 0o644))
	s, err := Load(path)
	require.NoError(t, err)
	require.Len(t, s.Annotations, 1)
	a := s.Annotations[0]
	assert.Equal(t, "b1", a.BlockID)
	assert.Equal(t, "Foo", a.Breadcrumb)
	assert.Equal(t, "a.tex", a.File)
	assert.Equal(t, 1, a.StartLine)
	assert.Equal(t, 2, a.EndLine)
	assert.Equal(t, "content", a.SourceQuote)
	assert.Equal(t, "note", a.Note)
}

func TestLoadRejectsUnterminatedFrontmatter(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bad.mreview.md")
	require.NoError(t, os.WriteFile(path, []byte("---\npaper: x\n"), 0o644))
	_, err := Load(path)
	require.Error(t, err)
}

func TestLoadPreservesMultilineNote(t *testing.T) {
	body := "---\npaper: a.tex\n---\n\n## Sec — `b1` (a.tex:L1-L3)\n\n> src\n\nline1\nline2\n\nline4\n"
	path := filepath.Join(t.TempDir(), "x.mreview.md")
	require.NoError(t, os.WriteFile(path, []byte(body), 0o644))
	s, err := Load(path)
	require.NoError(t, err)
	require.Len(t, s.Annotations, 1)
	assert.Equal(t, "line1\nline2\n\nline4", s.Annotations[0].Note)
}
