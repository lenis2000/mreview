package ui

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestParseShellArgs covers the shell-word tokeniser that drives
// resolveEditor. The cases that matter in practice: plain argv,
// GUI-app paths containing literal spaces (single or double quoted),
// flags after a quoted head, backslash-escaped spaces, mixed
// quoting, and graceful behaviour on unterminated quotes.
func TestParseShellArgs(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want []string
	}{
		{"empty", "", nil},
		{"whitespace only", "   \t  ", nil},
		{"single token", "vim", []string{"vim"}},
		{"flags", "code --wait --new-window", []string{"code", "--wait", "--new-window"}},
		{"double-quoted path with space", `"/Applications/My App/bin/edit" --wait`, []string{"/Applications/My App/bin/edit", "--wait"}},
		{"single-quoted path with space", `'/Applications/My App/bin/edit' --wait`, []string{"/Applications/My App/bin/edit", "--wait"}},
		{"backslash-escaped space", `/Applications/My\ App/bin/edit --wait`, []string{"/Applications/My App/bin/edit", "--wait"}},
		{"mixed quoting", `emacsclient -c "--eval=(find-file \"/tmp/x\")"`, []string{"emacsclient", "-c", `--eval=(find-file "/tmp/x")`}},
		{"internal single quotes inside double", `bash -c "echo 'hi'"`, []string{"bash", "-c", "echo 'hi'"}},
		{"adjacent quoted strings glue", `a"b"c`, []string{"abc"}},
		{"collapses extra whitespace", "  vim   --noplugin  ", []string{"vim", "--noplugin"}},
		{"unterminated double quote", `code --wait "unterminated`, []string{"code", "--wait", "unterminated"}},
		{"unterminated single quote", "code 'still open", []string{"code", "still open"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := parseShellArgs(tc.in)
			assert.Equal(t, tc.want, got)
		})
	}
}

// TestBuildEditorLineArgs confirms the jump-to-line flag is only
// emitted for editor families we know accept it, and falls back to
// just the path for anything else (VS Code, Sublime etc — the user
// can supply the editor's own line syntax via $EDITOR flags instead).
func TestBuildEditorLineArgs(t *testing.T) {
	assert.Equal(t, []string{"+42", "/tmp/x.tex"}, buildEditorLineArgs("nvim", "/tmp/x.tex", 42))
	assert.Equal(t, []string{"+42", "/tmp/x.tex"}, buildEditorLineArgs("/opt/vim/bin/vim", "/tmp/x.tex", 42))
	assert.Equal(t, []string{"+1", "/tmp/x.tex"}, buildEditorLineArgs("nano", "/tmp/x.tex", 0))
	assert.Equal(t, []string{"+7", "/tmp/x.tex"}, buildEditorLineArgs("emacsclient", "/tmp/x.tex", 7))
	assert.Equal(t, []string{"/tmp/x.tex"}, buildEditorLineArgs("code", "/tmp/x.tex", 42))
}
