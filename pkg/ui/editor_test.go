package ui

import (
	"testing"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
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
			got := ParseShellArgs(tc.in)
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

// newLineEditPopupForTest gives the chord tests a fully-initialised
// popup in normal mode with the cursor at the requested position.
func newLineEditPopupForTest(value string, cursor int) *LineEditPopup {
	ti := textinput.New()
	ti.SetValue(value)
	ti.SetCursor(cursor)
	return &LineEditPopup{TI: ti, NormalMode: true}
}

// rkeyStr is a multi-rune sibling of rkey from annotation_test.go;
// most chord steps need only a single rune but using a helper keeps
// the test bodies tight.
func rkeyStr(s string) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

// TestLineEditDeleteWordChord locks in `dw` / `dW` operator-motion
// behaviour. Counts compose vim-style (`2dw`, `d2w`, and `2d2w`),
// `dW` treats whitespace-separated runs as a single WORD, an unknown
// motion after `d` cancels the operator silently, and the deletion
// pushes a snapshot onto the popup's undo stack so `u` can revert it.
func TestLineEditDeleteWordChord(t *testing.T) {
	dispatch := func(p *LineEditPopup, keys ...tea.KeyMsg) {
		t.Helper()
		var m Model
		for _, k := range keys {
			res, _ := m.updateLineEditNormal(p, k)
			m = res.(Model)
		}
	}

	t.Run("dw deletes one word and trailing space", func(t *testing.T) {
		p := newLineEditPopupForTest("foo bar baz", 0)
		dispatch(p, rkeyStr("d"), rkeyStr("w"))
		assert.Equal(t, "bar baz", p.TI.Value())
		assert.Equal(t, 0, p.TI.Position())
		assert.Empty(t, p.Pending)
	})

	t.Run("d2w deletes two words", func(t *testing.T) {
		p := newLineEditPopupForTest("foo bar baz qux", 0)
		dispatch(p, rkeyStr("d"), rkeyStr("2"), rkeyStr("w"))
		assert.Equal(t, "baz qux", p.TI.Value())
	})

	t.Run("2dw deletes two words", func(t *testing.T) {
		p := newLineEditPopupForTest("foo bar baz qux", 0)
		dispatch(p, rkeyStr("2"), rkeyStr("d"), rkeyStr("w"))
		assert.Equal(t, "baz qux", p.TI.Value())
	})

	t.Run("2d2w multiplies counts (4 words)", func(t *testing.T) {
		p := newLineEditPopupForTest("a b c d e f g", 0)
		dispatch(p, rkeyStr("2"), rkeyStr("d"), rkeyStr("2"), rkeyStr("w"))
		assert.Equal(t, "e f g", p.TI.Value())
	})

	t.Run("dw treats foo.bar as separate words", func(t *testing.T) {
		p := newLineEditPopupForTest("foo.bar baz", 0)
		dispatch(p, rkeyStr("d"), rkeyStr("w"))
		assert.Equal(t, ".bar baz", p.TI.Value())
	})

	t.Run("dW deletes a whole WORD across punctuation", func(t *testing.T) {
		p := newLineEditPopupForTest(`\cite{foo} bar`, 0)
		dispatch(p, rkeyStr("d"), rkeyStr("W"))
		assert.Equal(t, "bar", p.TI.Value())
	})

	t.Run("dx clears pending operator and falls through", func(t *testing.T) {
		p := newLineEditPopupForTest("abcd", 1)
		dispatch(p, rkeyStr("d"), rkeyStr("x"))
		assert.Equal(t, "acd", p.TI.Value(), "x should run after d is cancelled")
		assert.Empty(t, p.Pending)
	})

	t.Run("dw is undoable via u", func(t *testing.T) {
		p := newLineEditPopupForTest("foo bar", 0)
		dispatch(p, rkeyStr("d"), rkeyStr("w"))
		assert.Equal(t, "bar", p.TI.Value())
		dispatch(p, rkeyStr("u"))
		assert.Equal(t, "foo bar", p.TI.Value())
	})
}
