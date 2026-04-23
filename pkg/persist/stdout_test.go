package persist

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseStdoutFormat(t *testing.T) {
	cases := map[string]StdoutFormat{
		"":         StdoutMarkdown,
		"md":       StdoutMarkdown,
		"markdown": StdoutMarkdown,
		"MD":       StdoutMarkdown,
		"json":     StdoutJSON,
		"JSON":     StdoutJSON,
		"none":     StdoutNone,
		"off":      StdoutNone,
	}
	for in, want := range cases {
		got, err := ParseStdoutFormat(in)
		require.NoError(t, err, "input=%q", in)
		assert.Equal(t, want, got, "input=%q", in)
	}
	_, err := ParseStdoutFormat("xml")
	assert.Error(t, err)
}

func sampleForStdout() *Sidecar {
	return &Sidecar{
		Paper:  "paper.tex",
		PDF:    "paper.pdf",
		Cursor: "thm:main",
		Annotations: []Annotation{
			{
				BlockID:     "thm:main",
				Breadcrumb:  "Theorem 1",
				File:        "paper.tex",
				StartLine:   10,
				EndLine:     12,
				SourceQuote: "\\begin{theorem}\n...\n\\end{theorem}",
				Note:        "first",
			},
			{
				BlockID:     "prf:main",
				Breadcrumb:  "Proof of Theorem 1",
				File:        "paper.tex",
				StartLine:   15,
				EndLine:     20,
				SourceQuote: "trivial.",
				Note:        "second",
			},
		},
		Detached: []Annotation{
			{
				BlockID:     "gone:1",
				Breadcrumb:  "Deleted lemma",
				File:        "paper.tex",
				StartLine:   1,
				EndLine:     2,
				SourceQuote: "disappeared",
				Note:        "orphan",
			},
		},
	}
}

func TestEmitMarkdownHasNoFrontmatter(t *testing.T) {
	var buf bytes.Buffer
	require.NoError(t, EmitMarkdown(&buf, sampleForStdout()))
	out := buf.String()
	assert.False(t, strings.HasPrefix(out, "---"), "markdown stdout must omit YAML frontmatter")
	assert.Contains(t, out, "## Theorem 1 — `thm:main` (paper.tex:L10-L12)")
	assert.Contains(t, out, "## Proof of Theorem 1 — `prf:main` (paper.tex:L15-L20)")
	assert.Contains(t, out, "first")
	assert.Contains(t, out, "second")
}

func TestEmitMarkdownEmitsDetachedSection(t *testing.T) {
	var buf bytes.Buffer
	require.NoError(t, EmitMarkdown(&buf, sampleForStdout()))
	out := buf.String()
	assert.Contains(t, out, DetachedMarker)
	idx := strings.Index(out, DetachedMarker)
	require.Greater(t, idx, 0, "detached marker should follow attached annotations")
	// The orphan annotation should appear only after the marker.
	firstOrphan := strings.Index(out, "orphan")
	assert.Greater(t, firstOrphan, idx)
}

func TestEmitMarkdownEmptySidecar(t *testing.T) {
	var buf bytes.Buffer
	require.NoError(t, EmitMarkdown(&buf, &Sidecar{}))
	assert.Equal(t, "", buf.String())
}

func TestEmitJSONShape(t *testing.T) {
	var buf bytes.Buffer
	require.NoError(t, EmitJSON(&buf, sampleForStdout()))
	var records []map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &records))
	require.Len(t, records, 3)
	// Attached first, detached last.
	assert.Equal(t, "thm:main", records[0]["block_id"])
	assert.Equal(t, "paper.tex", records[0]["file"])
	assert.Equal(t, float64(10), records[0]["start_line"])
	assert.Equal(t, float64(12), records[0]["end_line"])
	assert.Equal(t, "first", records[0]["note"])
	_, hasDetached := records[0]["detached"]
	assert.False(t, hasDetached, "attached records should omit detached field")
	assert.Equal(t, true, records[2]["detached"])
	assert.Equal(t, "gone:1", records[2]["block_id"])
}

func TestEmitJSONEmpty(t *testing.T) {
	var buf bytes.Buffer
	require.NoError(t, EmitJSON(&buf, &Sidecar{}))
	assert.Equal(t, "[]\n", buf.String())
}

func TestEmitNoneIsNoOp(t *testing.T) {
	var buf bytes.Buffer
	require.NoError(t, Emit(&buf, sampleForStdout(), StdoutNone))
	assert.Equal(t, "", buf.String())
}

func TestEmitDispatches(t *testing.T) {
	var md, js bytes.Buffer
	require.NoError(t, Emit(&md, sampleForStdout(), StdoutMarkdown))
	require.NoError(t, Emit(&js, sampleForStdout(), StdoutJSON))
	assert.Contains(t, md.String(), "## Theorem 1")
	assert.True(t, strings.HasPrefix(strings.TrimSpace(js.String()), "["))
}

func TestEmitMarkdownDoesNotEscapeDetachedLineInNote(t *testing.T) {
	s := &Sidecar{
		Annotations: []Annotation{{
			BlockID:     "thm:main",
			Breadcrumb:  "Theorem 1",
			File:        "paper.tex",
			StartLine:   1,
			EndLine:     2,
			SourceQuote: "src",
			Note:        "before\n## Detached\nafter",
		}},
	}
	var buf bytes.Buffer
	require.NoError(t, EmitMarkdown(&buf, s))
	out := buf.String()
	assert.Contains(t, out, "\n## Detached\n",
		"stdout markdown must preserve the note text verbatim for the LLM")
	assert.NotContains(t, out, `\## Detached`,
		"stdout markdown must not apply the sidecar-only backslash escape")
}

func TestRoundTripPreservesDetached(t *testing.T) {
	in := sampleForStdout()
	out, err := Marshal(in)
	require.NoError(t, err)
	parsed, err := parse(out)
	require.NoError(t, err)
	require.Len(t, parsed.Annotations, 2)
	require.Len(t, parsed.Detached, 1)
	assert.Equal(t, "gone:1", parsed.Detached[0].BlockID)
	assert.Equal(t, "orphan", parsed.Detached[0].Note)
}
