package format

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// space.trailing
// ---------------------------------------------------------------------------

func TestSpaceTrailing(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		want     string
		wantHits int
	}{
		{
			name:     "nominal trailing spaces",
			input:    "hello   \nworld  \n",
			want:     "hello\nworld\n",
			wantHits: 2,
		},
		{
			name:     "trailing tabs",
			input:    "hello\t\nworld\t\t\n",
			want:     "hello\nworld\n",
			wantHits: 2,
		},
		{
			name:     "no trailing whitespace (no-op)",
			input:    "hello\nworld\n",
			want:     "hello\nworld\n",
			wantHits: 0,
		},
		{
			name:     "blank lines preserved",
			input:    "hello\n\nworld\n",
			want:     "hello\n\nworld\n",
			wantHits: 0,
		},
		{
			name:     "inside verbatim (no rewrite)",
			input:    "\\begin{verbatim}\nhello   \n\\end{verbatim}\n",
			want:     "\\begin{verbatim}\nhello   \n\\end{verbatim}\n",
			wantHits: 0,
		},
		{
			name:     "verb inline content preserved, trailing trimmed",
			input:    "see \\verb|hello   | ok  \n",
			want:     "see \\verb|hello   | ok\n",
			wantHits: 1, // trailing spaces after verb are trimmed; verb content preserved
		},
		{
			name:     "inside comment line",
			input:    "% comment with trailing   \nhello  \n",
			want:     "% comment with trailing   \nhello\n",
			wantHits: 1, // comment line is protected; only "hello  " is trimmed
		},
		{
			name:     "inside lstlisting (no rewrite)",
			input:    "\\begin{lstlisting}\ncode   \n\\end{lstlisting}\n",
			want:     "\\begin{lstlisting}\ncode   \n\\end{lstlisting}\n",
			wantHits: 0,
		},
		{
			name:     "mixed protected and unprotected",
			input:    "a   \n\\begin{verbatim}\nb   \n\\end{verbatim}\nc   \n",
			want:     "a\n\\begin{verbatim}\nb   \n\\end{verbatim}\nc\n",
			wantHits: 2,
		},
	}

	opts := Options{Rules: []string{"space.trailing"}}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := Apply([]byte(tt.input), opts)
			assert.Equal(t, tt.want, string(res.Src))
			assert.Equal(t, tt.wantHits, len(res.Hits), "hit count")
		})
	}
}

// ---------------------------------------------------------------------------
// space.blank-runs
// ---------------------------------------------------------------------------

func TestSpaceBlankRuns(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		want     string
		wantHits int
	}{
		{
			name:     "triple newline collapsed",
			input:    "a\n\n\nb\n",
			want:     "a\n\nb\n",
			wantHits: 1,
		},
		{
			name:     "quadruple newline collapsed",
			input:    "a\n\n\n\nb\n",
			want:     "a\n\nb\n",
			wantHits: 1,
		},
		{
			name:     "double newline preserved (no-op)",
			input:    "a\n\nb\n",
			want:     "a\n\nb\n",
			wantHits: 0,
		},
		{
			name:     "single newline preserved (no-op)",
			input:    "a\nb\n",
			want:     "a\nb\n",
			wantHits: 0,
		},
		{
			name:     "multiple runs collapsed",
			input:    "a\n\n\nb\n\n\n\nc\n",
			want:     "a\n\nb\n\nc\n",
			wantHits: 2,
		},
		{
			name:     "inside verbatim (no rewrite)",
			input:    "\\begin{verbatim}\na\n\n\nb\n\\end{verbatim}\n",
			want:     "\\begin{verbatim}\na\n\n\nb\n\\end{verbatim}\n",
			wantHits: 0,
		},
		{
			name:     "inside lstlisting (no rewrite)",
			input:    "\\begin{lstlisting}\na\n\n\nb\n\\end{lstlisting}\n",
			want:     "\\begin{lstlisting}\na\n\n\nb\n\\end{lstlisting}\n",
			wantHits: 0,
		},
		{
			name:     "no extra blanks (no-op)",
			input:    "hello\nworld\n",
			want:     "hello\nworld\n",
			wantHits: 0,
		},
	}

	opts := Options{Rules: []string{"space.blank-runs"}}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := Apply([]byte(tt.input), opts)
			assert.Equal(t, tt.want, string(res.Src))
			assert.Equal(t, tt.wantHits, len(res.Hits), "hit count")
		})
	}
}

// ---------------------------------------------------------------------------
// space.tabs
// ---------------------------------------------------------------------------

func TestSpaceTabs(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		want     string
		wantHits int
	}{
		{
			name:     "tab at start of line",
			input:    "\thello\n",
			want:     "    hello\n",
			wantHits: 1,
		},
		{
			name:     "multiple tabs",
			input:    "\t\thello\n",
			want:     "        hello\n",
			wantHits: 1, // deduplicated: same line
		},
		{
			name:     "tab in middle of line",
			input:    "hello\tworld\n",
			want:     "hello    world\n",
			wantHits: 1,
		},
		{
			name:     "no tabs (no-op)",
			input:    "hello world\n",
			want:     "hello world\n",
			wantHits: 0,
		},
		{
			name:     "inside verbatim (no rewrite)",
			input:    "\\begin{verbatim}\n\thello\n\\end{verbatim}\n",
			want:     "\\begin{verbatim}\n\thello\n\\end{verbatim}\n",
			wantHits: 0,
		},
		{
			name:     "inside verb inline (no rewrite)",
			input:    "see \\verb|\thello|\n",
			want:     "see \\verb|\thello|\n",
			wantHits: 0,
		},
		{
			name:     "inside comment line (no rewrite)",
			input:    "% comment\twith tab\n",
			want:     "% comment\twith tab\n",
			wantHits: 0,
		},
		{
			name:     "inside lstlisting (no rewrite)",
			input:    "\\begin{lstlisting}\n\tcode\n\\end{lstlisting}\n",
			want:     "\\begin{lstlisting}\n\tcode\n\\end{lstlisting}\n",
			wantHits: 0,
		},
		{
			name:     "tabs on multiple lines",
			input:    "\ta\n\tb\n",
			want:     "    a\n    b\n",
			wantHits: 2,
		},
	}

	opts := Options{Rules: []string{"space.tabs"}}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := Apply([]byte(tt.input), opts)
			assert.Equal(t, tt.want, string(res.Src))
			assert.Equal(t, tt.wantHits, len(res.Hits), "hit count")
		})
	}
}

// ---------------------------------------------------------------------------
// display.style
// ---------------------------------------------------------------------------

func TestDisplayStyle(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		want     string
		wantHits int
	}{
		{
			name:     "simple $$...$$ on one line",
			input:    "$$x+y$$\n",
			want:     "\\[x+y\\]\n",
			wantHits: 1,
		},
		{
			name:     "multiline $$...$$",
			input:    "text\n$$\nx+y\n$$\nmore\n",
			want:     "text\n\\[\nx+y\n\\]\nmore\n",
			wantHits: 1,
		},
		{
			name:     "no $$ (no-op)",
			input:    "hello world\n",
			want:     "hello world\n",
			wantHits: 0,
		},
		{
			name:     "single $ not touched",
			input:    "$x+y$\n",
			want:     "$x+y$\n",
			wantHits: 0,
		},
		{
			name:     "already using \\[...\\] (no-op)",
			input:    "\\[x+y\\]\n",
			want:     "\\[x+y\\]\n",
			wantHits: 0,
		},
		{
			name:     "inside verbatim (no rewrite)",
			input:    "\\begin{verbatim}\n$$x$$\n\\end{verbatim}\n",
			want:     "\\begin{verbatim}\n$$x$$\n\\end{verbatim}\n",
			wantHits: 0,
		},
		{
			name:     "inside verb inline (no rewrite)",
			input:    "\\verb|$$x$$|\n",
			want:     "\\verb|$$x$$|\n",
			wantHits: 0,
		},
		{
			name:     "inside comment line (no rewrite)",
			input:    "% $$x$$\n",
			want:     "% $$x$$\n",
			wantHits: 0,
		},
		{
			name:     "inside lstlisting (no rewrite)",
			input:    "\\begin{lstlisting}\n$$x$$\n\\end{lstlisting}\n",
			want:     "\\begin{lstlisting}\n$$x$$\n\\end{lstlisting}\n",
			wantHits: 0,
		},
		{
			name:     "two $$ pairs",
			input:    "$$a$$\n$$b$$\n",
			want:     "\\[a\\]\n\\[b\\]\n",
			wantHits: 2,
		},
		{
			name:     "content with commands",
			input:    "$$\\frac{a}{b}$$\n",
			want:     "\\[\\frac{a}{b}\\]\n",
			wantHits: 1,
		},
	}

	opts := Options{Rules: []string{"display.style"}}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := Apply([]byte(tt.input), opts)
			assert.Equal(t, tt.want, string(res.Src))
			assert.Equal(t, tt.wantHits, len(res.Hits), "hit count")
		})
	}
}

// ---------------------------------------------------------------------------
// Pipeline stale-state recompute test
// ---------------------------------------------------------------------------

// TestPipelineStaleStateRecompute verifies that when space.blank-runs changes
// newline counts, subsequent rules (display.style) see correct line numbers.
func TestPipelineStaleStateRecompute(t *testing.T) {
	// Input has 3+ blank lines before a $$...$$ block. space.blank-runs will
	// collapse the blank lines (changing newline count), then display.style
	// should still correctly identify and replace the $$.
	input := "text\n\n\n\n$$x+y$$\n"
	//        line1 \n \n \n \n line5
	// After blank-runs: "text\n\n$$x+y$$\n"  (3 newlines collapsed to 2)
	// After display.style: "text\n\n\\[x+y\\]\n"

	// Run with default Safe rules (all 4).
	res := Apply([]byte(input), Options{})

	// display.style should have fired.
	assert.Equal(t, "text\n\n\\[x+y\\]\n", string(res.Src))

	// Check that we got hits from both rules.
	blankHits := 0
	displayHits := 0
	for _, h := range res.Hits {
		switch h.RuleID {
		case "space.blank-runs":
			blankHits++
		case "display.style":
			displayHits++
			// After reindex, display.style should see the $$ on line 3
			// (text=1, blank=2, $$=3).
			assert.Equal(t, 3, h.Line, "display.style should see correct line after reindex")
		}
	}
	assert.Equal(t, 1, blankHits, "space.blank-runs should fire once")
	assert.Equal(t, 1, displayHits, "display.style should fire once")
}

// TestPipelineAllSafeRules runs all four safe rules together on a mixed input.
func TestPipelineAllSafeRules(t *testing.T) {
	input := "\thello   \n\n\n\n$$x$$\nworld  \n"
	// Expected after all rules:
	// space.trailing: "\thello\n\n\n\n$$x$$\nworld\n"
	// space.blank-runs: "\thello\n\n$$x$$\nworld\n"
	// space.tabs: "    hello\n\n$$x$$\nworld\n"
	// display.style: "    hello\n\n\\[x\\]\nworld\n"

	res := Apply([]byte(input), Options{})
	assert.Equal(t, "    hello\n\n\\[x\\]\nworld\n", string(res.Src))
	require.True(t, len(res.Hits) > 0, "should have hits")
}

// TestDefaultOptionsSafeOnly verifies that only Safe rules run by default.
func TestDefaultOptionsSafeOnly(t *testing.T) {
	rules := enabledRules(Options{})
	for _, r := range rules {
		assert.Equal(t, Safe, r.Tier, "default should only enable Safe rules")
	}
	// We have exactly 4 safe rules.
	assert.Equal(t, 4, len(rules))
}

// TestRegistryOrder verifies rules are in the expected order.
func TestRegistryOrder(t *testing.T) {
	ids := make([]string, len(Registry))
	for i, r := range Registry {
		ids[i] = r.ID
	}
	assert.Equal(t, []string{
		"space.trailing",
		"space.blank-runs",
		"space.tabs",
		"display.style",
	}, ids)
}

// ---------------------------------------------------------------------------
// Helper: lineAt
// ---------------------------------------------------------------------------

func TestLineAt(t *testing.T) {
	// "abc\ndef\nghi\n"
	// Lines: [0, 0, 4, 8]  (sentinel, line1=0, line2=4, line3=8)
	lines := []int{0, 0, 4, 8}
	assert.Equal(t, 1, lineAt(lines, 0))  // 'a'
	assert.Equal(t, 1, lineAt(lines, 2))  // 'c'
	assert.Equal(t, 1, lineAt(lines, 3))  // '\n' at end of line 1
	assert.Equal(t, 2, lineAt(lines, 4))  // 'd'
	assert.Equal(t, 2, lineAt(lines, 7))  // '\n' at end of line 2
	assert.Equal(t, 3, lineAt(lines, 8))  // 'g'
	assert.Equal(t, 3, lineAt(lines, 11)) // '\n' at end of line 3
}

// ---------------------------------------------------------------------------
// Edge cases for display.style
// ---------------------------------------------------------------------------

func TestDisplayStyleEdgeCases(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "unclosed $$ at EOF",
			input: "$$x\n",
			want:  "$$x\n", // no match, no rewrite
		},
		{
			name:  "empty $$$$",
			input: "$$$$\n",
			want:  "\\[\\]\n",
		},
		{
			name:  "$$ with nested single $",
			input: "$$x + $y$ + z$$\n",
			want:  "\\[x + $y$ + z\\]\n",
		},
	}

	opts := Options{Rules: []string{"display.style"}}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := Apply([]byte(tt.input), opts)
			assert.Equal(t, tt.want, string(res.Src))
		})
	}
}

// TestSpaceBlankRunsProtectedComment verifies blank runs inside a comment
// environment are not collapsed.
func TestSpaceBlankRunsProtectedComment(t *testing.T) {
	input := "\\begin{comment}\na\n\n\nb\n\\end{comment}\n"
	opts := Options{Rules: []string{"space.blank-runs"}}
	res := Apply([]byte(input), opts)
	assert.Equal(t, input, string(res.Src))
}

// TestNoopInput verifies no change on already-clean input.
func TestNoopInput(t *testing.T) {
	input := "Hello world.\n\nAnother paragraph.\n"
	res := Apply([]byte(input), Options{})
	assert.True(t, bytes.Equal([]byte(input), res.Src), "should be identical")
	assert.Empty(t, res.Hits)
}
