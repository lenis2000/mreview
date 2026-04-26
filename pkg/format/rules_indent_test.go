package format

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func runIndent(src string, useTab bool, size int) string {
	res := Apply([]byte(src), Options{
		Indent: IndentOptions{Enabled: true, UseTab: useTab, Size: size},
	})
	return string(res.Src)
}

func TestIndent_DisabledIsNoop(t *testing.T) {
	src := "\\begin{document}\nhello\n\\end{document}\n"
	res := Apply([]byte(src), Options{}) // Enabled=false
	assert.Equal(t, src, string(res.Src))
}

func TestIndent_DocumentEnvIsNoIndentEnv(t *testing.T) {
	// Body of `document` must NOT be indented (no_indent_envs).
	src := "\\begin{document}\nhello\n\\end{document}\n"
	got := runIndent(src, true, 1)
	assert.Equal(t, src, got, "document body must remain at depth 0")
}

func TestIndent_TheoremEnvIsIndented(t *testing.T) {
	src := strings.Join([]string{
		"\\begin{document}",
		"\\begin{theorem}",
		"hello",
		"\\end{theorem}",
		"\\end{document}",
		"",
	}, "\n")
	got := runIndent(src, true, 1)
	want := strings.Join([]string{
		"\\begin{document}",
		"\\begin{theorem}",
		"\thello",
		"\\end{theorem}",
		"\\end{document}",
		"",
	}, "\n")
	assert.Equal(t, want, got)
}

func TestIndent_NestedEnvs(t *testing.T) {
	src := strings.Join([]string{
		"\\begin{document}",
		"\\begin{outer}",
		"a",
		"\\begin{inner}",
		"b",
		"\\end{inner}",
		"c",
		"\\end{outer}",
		"\\end{document}",
		"",
	}, "\n")
	got := runIndent(src, true, 1)
	want := strings.Join([]string{
		"\\begin{document}",
		"\\begin{outer}",
		"\ta",
		"\t\\begin{inner}",
		"\t\tb",
		"\t\\end{inner}",
		"\tc",
		"\\end{outer}",
		"\\end{document}",
		"",
	}, "\n")
	assert.Equal(t, want, got)
}

func TestIndent_Idempotent(t *testing.T) {
	src := strings.Join([]string{
		"\\begin{document}",
		"\\begin{theorem}",
		"a",
		"\\begin{proof}",
		"b",
		"\\end{proof}",
		"\\end{theorem}",
		"\\end{document}",
		"",
	}, "\n")
	once := runIndent(src, true, 1)
	twice := runIndent(once, true, 1)
	assert.Equal(t, once, twice, "indent must be idempotent")
}

func TestIndent_SkipDirectivePreservesLine(t *testing.T) {
	src := strings.Join([]string{
		"\\begin{document}",
		"\\begin{theorem}",
		"        hand-indented % mreview-fmt: skip",
		"\\end{theorem}",
		"\\end{document}",
		"",
	}, "\n")
	got := runIndent(src, true, 1)
	assert.Contains(t, got, "        hand-indented % mreview-fmt: skip", "skipped lines must keep their original leading whitespace")
}

func TestIndent_VerbatimContentsLeftAlone(t *testing.T) {
	src := strings.Join([]string{
		"\\begin{document}",
		"\\begin{verbatim}",
		"  preserve  whitespace  ",
		"\\end{verbatim}",
		"\\end{document}",
		"",
	}, "\n")
	got := runIndent(src, true, 1)
	assert.Contains(t, got, "  preserve  whitespace  ")
}

func TestIndent_BlankLinesNotIndented(t *testing.T) {
	src := strings.Join([]string{
		"\\begin{document}",
		"\\begin{theorem}",
		"hello",
		"",
		"world",
		"\\end{theorem}",
		"\\end{document}",
		"",
	}, "\n")
	got := runIndent(src, true, 1)
	// Blank line stays blank.
	assert.Contains(t, got, "\thello\n\n\tworld")
}

func TestIndent_SpacesMode(t *testing.T) {
	src := strings.Join([]string{
		"\\begin{document}",
		"\\begin{theorem}",
		"a",
		"\\end{theorem}",
		"\\end{document}",
		"",
	}, "\n")
	got := runIndent(src, false, 2) // 2 spaces per level
	assert.Contains(t, got, "  a")
	assert.NotContains(t, got, "\t")
}

func TestIndent_TabsModeSuppressesSpaceTabs(t *testing.T) {
	// With UseTab=true, the existing space.tabs rule must NOT eat the tabs
	// that space.indent writes — otherwise we get a tabs↔spaces ping-pong.
	src := strings.Join([]string{
		"\\begin{document}",
		"\\begin{theorem}",
		"a",
		"\\end{theorem}",
		"\\end{document}",
		"",
	}, "\n")
	res := Apply([]byte(src), Options{
		Indent: IndentOptions{Enabled: true, UseTab: true, Size: 1},
	})
	assert.Contains(t, string(res.Src), "\ta", "tabs mode must keep its tabs")
}
