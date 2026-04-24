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

// TestLineEditWordMotions pins down the vim-style w/b/e semantics
// used by the inline editor's normal mode. The string under test
// exercises the three word classes (alnum / punctuation / space) so
// the class-transition rule — which is what distinguishes `foo.bar`
// as three "words" rather than one — stays tested.
func TestLineEditWordMotions(t *testing.T) {
	runes := []rune("Nevertheless, we present bounds which support a refined conjecture that")

	// w: advance over current run + trailing whitespace.
	assert.Equal(t, 12, motionWordForward(runes, 0), "w from 0 lands on ','")
	assert.Equal(t, 14, motionWordForward(runes, 12), "w from ',' lands on 'we'")
	assert.Equal(t, 17, motionWordForward(runes, 14), "w from 'we' lands on 'present'")

	// b: step back to start of previous word.
	assert.Equal(t, 0, motionWordBackward(runes, 12), "b from ',' returns to 'N'")
	assert.Equal(t, 14, motionWordBackward(runes, 17), "b from 'present' returns to 'we'")
	assert.Equal(t, 0, motionWordBackward(runes, 0), "b at 0 stays at 0")

	// e: land on last rune of current/next word.
	assert.Equal(t, 11, motionWordEnd(runes, 0), "e from 0 lands on last 's' of Nevertheless")
	assert.Equal(t, 12, motionWordEnd(runes, 11), "e from last letter lands on ','")
	assert.Equal(t, 15, motionWordEnd(runes, 12), "e from ',' lands on 'e' of 'we'")

	// Count application: 5b from end-of-string should move back 5 words
	// (that, conjecture, refined, a, support).
	pos := len(runes)
	for i := 0; i < 5; i++ {
		pos = motionWordBackward(runes, pos)
	}
	assert.Equal(t, "support a refined conjecture that", string(runes[pos:]))

	// Empty and boundary cases.
	assert.Equal(t, 0, motionWordForward([]rune(""), 0))
	assert.Equal(t, 0, motionWordBackward([]rune(""), 0))
	assert.Equal(t, 0, motionWordEnd([]rune(""), 0))
}
