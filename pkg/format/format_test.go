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
		"math.paragraph-suppress",
		"env.spacing",
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

// ---------------------------------------------------------------------------
// math.paragraph-suppress
// ---------------------------------------------------------------------------

func TestMathParagraphSuppress(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		want     string
		wantHits int
	}{
		{
			name: "continuation above (drop)",
			input: "we simply define\n\n\\begin{equation}\nx = 1\n\\end{equation}\nmore text\n",
			want:  "we simply define\n\\begin{equation}\nx = 1\n\\end{equation}\nmore text\n",
			wantHits: 1,
		},
		{
			name: "continuation below (drop)",
			input: "some text\n\\begin{equation}\nx = 1\n\\end{equation}\n\nwhere x is nice\n",
			want:  "some text\n\\begin{equation}\nx = 1\n\\end{equation}\nwhere x is nice\n",
			wantHits: 1,
		},
		{
			name: "default case — no strong signal (drop both)",
			input: "we define\n\n\\begin{equation}\nx = 1\n\\end{equation}\n\nwhere x is\n",
			want:  "we define\n\\begin{equation}\nx = 1\n\\end{equation}\nwhere x is\n",
			wantHits: 2,
		},
		{
			name: "strong paragraph signal (leave alone)",
			input: "This ends a sentence.\n\n\\begin{equation}\nx = 1\n\\end{equation}\n\nThe next paragraph starts.\n",
			want:  "This ends a sentence.\n\n\\begin{equation}\nx = 1\n\\end{equation}\n\nThe next paragraph starts.\n",
			wantHits: 0,
		},
		{
			name: "chain of two equations (collapse all gaps)",
			input: "text\n\n\\begin{equation}\na = 1\n\\end{equation}\n\n\\begin{equation}\nb = 2\n\\end{equation}\n\nmore\n",
			want:  "text\n\\begin{equation}\na = 1\n\\end{equation}\n\\begin{equation}\nb = 2\n\\end{equation}\nmore\n",
			wantHits: 3, // above, inner, below
		},
		{
			name: "chain of three equations (collapse all gaps)",
			input: "text\n\n\\begin{align}\na\n\\end{align}\n\n\\begin{align}\nb\n\\end{align}\n\n\\begin{align}\nc\n\\end{align}\n\nmore\n",
			want:  "text\n\\begin{align}\na\n\\end{align}\n\\begin{align}\nb\n\\end{align}\n\\begin{align}\nc\n\\end{align}\nmore\n",
			wantHits: 4, // above, inner1, inner2, below
		},
		{
			name: "display math followed by section header (no blank below, no-op below)",
			input: "text\n\n\\begin{equation}\nx\n\\end{equation}\n\\section{Next}\n",
			want:  "text\n\\begin{equation}\nx\n\\end{equation}\n\\section{Next}\n",
			wantHits: 1, // only above blank removed
		},
		{
			name: "\\[...\\] form (drop)",
			input: "we have\n\n\\[x = 1\\]\n\nthen\n",
			want:  "we have\n\\[x = 1\\]\nthen\n",
			wantHits: 2,
		},
		{
			name: "starred env align* (drop)",
			input: "satisfies\n\n\\begin{align*}\nx &= 1\n\\end{align*}\n\nwhich gives\n",
			want:  "satisfies\n\\begin{align*}\nx &= 1\n\\end{align*}\nwhich gives\n",
			wantHits: 2,
		},
		{
			name: "inside protected span (no rewrite)",
			input: "\\begin{verbatim}\ntext\n\n\\begin{equation}\nx\n\\end{equation}\n\nmore\n\\end{verbatim}\n",
			want:  "\\begin{verbatim}\ntext\n\n\\begin{equation}\nx\n\\end{equation}\n\nmore\n\\end{verbatim}\n",
			wantHits: 0,
		},
		{
			name: "zero blank lines (no-op)",
			input: "text\n\\begin{equation}\nx\n\\end{equation}\nmore\n",
			want:  "text\n\\begin{equation}\nx\n\\end{equation}\nmore\n",
			wantHits: 0,
		},
		{
			name: "only above blank, no below blank (drop above)",
			input: "the value\n\n\\begin{equation}\nx = 1\n\\end{equation}\nfollows\n",
			want:  "the value\n\\begin{equation}\nx = 1\n\\end{equation}\nfollows\n",
			wantHits: 1,
		},
		{
			name: "half signal: above ends with period but below lowercase (drop both)",
			input: "Sentence ends here.\n\n\\begin{equation}\nx\n\\end{equation}\n\nbut lowercase continues\n",
			want:  "Sentence ends here.\n\\begin{equation}\nx\n\\end{equation}\nbut lowercase continues\n",
			wantHits: 2,
		},
		{
			name: "half signal: below starts uppercase but above no punctuation (drop both)",
			input: "and we have\n\n\\begin{equation}\nx\n\\end{equation}\n\nNow consider\n",
			want:  "and we have\n\\begin{equation}\nx\n\\end{equation}\nNow consider\n",
			wantHits: 2,
		},
		{
			name: "multiple blank lines collapsed (not just one)",
			input: "define\n\n\n\\begin{equation}\nx\n\\end{equation}\n\n\nwhere\n",
			want:  "define\n\\begin{equation}\nx\n\\end{equation}\nwhere\n",
			wantHits: 2,
		},
		{
			name: "gather env (drop)",
			input: "consider\n\n\\begin{gather}\nx\n\\end{gather}\n\nso\n",
			want:  "consider\n\\begin{gather}\nx\n\\end{gather}\nso\n",
			wantHits: 2,
		},
		{
			name: "multline env (drop)",
			input: "we get\n\n\\begin{multline}\nx\n\\end{multline}\n\nhence\n",
			want:  "we get\n\\begin{multline}\nx\n\\end{multline}\nhence\n",
			wantHits: 2,
		},
	}

	opts := Options{Rules: []string{"math.paragraph-suppress"}}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := Apply([]byte(tt.input), opts)
			assert.Equal(t, tt.want, string(res.Src), "source mismatch")
			assert.Equal(t, tt.wantHits, len(res.Hits), "hit count")
			// Verify all hits have ExpectedDiffSourceLines set.
			for _, h := range res.Hits {
				assert.Equal(t, "math.paragraph-suppress", h.RuleID)
				assert.NotNil(t, h.ExpectedDiffSourceLines, "hit should have ExpectedDiffSourceLines")
			}
		})
	}
}

// ---------------------------------------------------------------------------
// env.spacing
// ---------------------------------------------------------------------------

func TestEnvSpacing(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		want     string
		wantHits int
	}{
		{
			name:     "insertion needed before theorem",
			input:    "Some text.\n\\begin{theorem}\nT.\n\\end{theorem}\n",
			want:     "Some text.\n\n\\begin{theorem}\nT.\n\\end{theorem}\n",
			wantHits: 1,
		},
		{
			name:     "insertion needed before figure",
			input:    "Text here.\n\\begin{figure}\nFig.\n\\end{figure}\n",
			want:     "Text here.\n\n\\begin{figure}\nFig.\n\\end{figure}\n",
			wantHits: 1,
		},
		{
			name:     "insertion needed before section",
			input:    "End of old section.\n\\section{New}\n",
			want:     "End of old section.\n\n\\section{New}\n",
			wantHits: 1,
		},
		{
			name:     "insertion needed before subsection",
			input:    "Content.\n\\subsection{Sub}\n",
			want:     "Content.\n\n\\subsection{Sub}\n",
			wantHits: 1,
		},
		{
			name:     "insertion not needed (already has blank)",
			input:    "Some text.\n\n\\begin{theorem}\nT.\n\\end{theorem}\n",
			want:     "Some text.\n\n\\begin{theorem}\nT.\n\\end{theorem}\n",
			wantHits: 0,
		},
		{
			name:     "env in protected span (no-op)",
			input:    "\\begin{verbatim}\ntext\n\\begin{theorem}\n\\end{verbatim}\n",
			want:     "\\begin{verbatim}\ntext\n\\begin{theorem}\n\\end{verbatim}\n",
			wantHits: 0,
		},
		{
			name:     "env at start of file (no-op)",
			input:    "\\begin{theorem}\nT.\n\\end{theorem}\n",
			want:     "\\begin{theorem}\nT.\n\\end{theorem}\n",
			wantHits: 0,
		},
		{
			name:     "consecutive theorem envs (insert before each)",
			input:    "Text.\n\\begin{theorem}\nT1.\n\\end{theorem}\n\\begin{lemma}\nL1.\n\\end{lemma}\n",
			want:     "Text.\n\n\\begin{theorem}\nT1.\n\\end{theorem}\n\n\\begin{lemma}\nL1.\n\\end{lemma}\n",
			wantHits: 2,
		},
		{
			name:     "non-matching env not touched",
			input:    "Text.\n\\begin{itemize}\n\\item A\n\\end{itemize}\n",
			want:     "Text.\n\\begin{itemize}\n\\item A\n\\end{itemize}\n",
			wantHits: 0,
		},
		{
			name:     "definition env",
			input:    "Setup.\n\\begin{definition}\nD.\n\\end{definition}\n",
			want:     "Setup.\n\n\\begin{definition}\nD.\n\\end{definition}\n",
			wantHits: 1,
		},
		{
			name:     "conjecture env",
			input:    "Note.\n\\begin{conjecture}\nC.\n\\end{conjecture}\n",
			want:     "Note.\n\n\\begin{conjecture}\nC.\n\\end{conjecture}\n",
			wantHits: 1,
		},
		{
			name:     "multiple blank lines already present (no-op)",
			input:    "Text.\n\n\n\\begin{theorem}\nT.\n\\end{theorem}\n",
			want:     "Text.\n\n\n\\begin{theorem}\nT.\n\\end{theorem}\n",
			wantHits: 0,
		},
	}

	opts := Options{Rules: []string{"env.spacing"}}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := Apply([]byte(tt.input), opts)
			assert.Equal(t, tt.want, string(res.Src), "source mismatch")
			assert.Equal(t, tt.wantHits, len(res.Hits), "hit count")
			// Verify all hits have ExpectedDiffSourceLines set.
			for _, h := range res.Hits {
				assert.Equal(t, "env.spacing", h.RuleID)
				assert.NotNil(t, h.ExpectedDiffSourceLines, "hit should have ExpectedDiffSourceLines")
			}
		})
	}
}

// TestPdfFixNotRunByDefault verifies Tier-2 rules don't run unless --pdf-fix or --rule is set.
func TestPdfFixNotRunByDefault(t *testing.T) {
	input := "define\n\n\\begin{equation}\nx\n\\end{equation}\n\nwhere\n"
	res := Apply([]byte(input), Options{})
	assert.Equal(t, input, string(res.Src), "Tier-2 rules should not run by default")
}

// TestPdfFixEnabled verifies Tier-2 rules run when PDFFix is enabled.
func TestPdfFixEnabled(t *testing.T) {
	input := "define\n\n\\begin{equation}\nx\n\\end{equation}\n\nwhere\n"
	res := Apply([]byte(input), Options{PDFFix: true})
	assert.NotEqual(t, input, string(res.Src), "Tier-2 rules should run with PDFFix")
	assert.True(t, len(res.Hits) > 0, "should have hits")
}
