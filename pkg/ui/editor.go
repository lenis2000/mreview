package ui

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

// editorCandidates is the fallback order when $EDITOR isn't set. Kept in
// the vim family plus nano so the key does something useful on a fresh
// machine without manual config.
var editorCandidates = []string{"nvim", "vim", "vi", "nano"}

// editInExternalEditor suspends mreview and runs $EDITOR on the current
// paper.tex positioned at the cursor's absolute source line. The sidecar
// is flushed before suspending so any in-memory annotation that hasn't
// been written yet survives an unexpected editor crash. On return we
// post a reloadMsg which the Update loop handles via reloadFromDisk.
func (m Model) editInExternalEditor() (tea.Model, tea.Cmd) {
	if m.Doc == nil || m.Doc.File == "" {
		m.Status = "E: no source file"
		return m, nil
	}
	head, userArgs, ok := resolveEditor()
	if !ok {
		m.Status = "E: no editor found (set $EDITOR)"
		return m, nil
	}
	if err := m.saveSidecar(); err != nil {
		m.Status = "E: save sidecar: " + err.Error()
		return m, nil
	}
	lineArgs := buildEditorLineArgs(head, m.Doc.File, absoluteCursorLine(m))
	argv := append(append([]string{}, userArgs...), lineArgs...)
	cmd := exec.Command(head, argv...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return m, tea.ExecProcess(cmd, func(err error) tea.Msg {
		return reloadMsg{err: err}
	})
}

// resolveEditor picks an editor invocation from $EDITOR or the fallback
// candidates. Returns (binary, userArgs, true) where userArgs are any
// flags the user set in $EDITOR ("code --wait" → head="code",
// userArgs=["--wait"]). Flag preservation matters: a blocking wrapper
// like --wait is how tea.ExecProcess knows to wait for the edit to
// finish before reloading.
//
// $EDITOR is split on whitespace via strings.Fields — adequate for the
// common cases (`code --wait`, `emacsclient -c`, `nvim -u NONE`). Shell
// quoting (spaces inside a path) is not parsed; users with such setups
// should point $EDITOR at a wrapper script instead.
func resolveEditor() (string, []string, bool) {
	if v := strings.TrimSpace(os.Getenv("EDITOR")); v != "" {
		tokens := strings.Fields(v)
		if len(tokens) > 0 {
			if _, err := exec.LookPath(tokens[0]); err == nil {
				return tokens[0], tokens[1:], true
			}
		}
	}
	for _, c := range editorCandidates {
		if _, err := exec.LookPath(c); err == nil {
			return c, nil, true
		}
	}
	return "", nil, false
}

// buildEditorLineArgs produces the final argv tail: a jump-to-line flag
// (when the editor family supports one) plus the file path. The head
// binary name is matched loosely so `vim`, `nvim`, `nvim.appimage`, and
// `/opt/vim/bin/vim` all pick up the `+N` flag. VS Code / emacs-client
// accept their own line syntaxes which the user can express via
// $EDITOR flags; we don't try to auto-detect those.
func buildEditorLineArgs(editor, path string, line int) []string {
	if line < 1 {
		line = 1
	}
	name := strings.ToLower(filepath.Base(editor))
	switch {
	case strings.Contains(name, "vim"),
		strings.Contains(name, "vi"),
		strings.Contains(name, "nano"),
		strings.Contains(name, "emacs"):
		return []string{fmt.Sprintf("+%d", line), path}
	default:
		return []string{path}
	}
}

// absoluteCursorLine resolves the current (CursorBlockID, SourceLineCursor)
// pair to a 1-based absolute source line number for the jump-to-line flag.
func absoluteCursorLine(m Model) int {
	if m.Doc == nil || m.CursorBlockID == "" {
		return 1
	}
	b := m.Doc.ByID[m.CursorBlockID]
	if b == nil || b.StartLine == 0 {
		return 1
	}
	line := b.StartLine + m.SourceLineCursor - 1
	if line < 1 {
		line = 1
	}
	return line
}

// --- Inline single-line edit ------------------------------------------------

// LineEditPopup hosts the inline single-line editor (ctrl+e). Scope is
// deliberately one source line so the workflow stays as "minor wording
// fix without leaving mreview" — structural edits still belong in
// $EDITOR. AbsoluteLine is the 1-based line we're rewriting on disk;
// Original holds the pre-edit content so cancel can confirm no change.
type LineEditPopup struct {
	TI           textinput.Model
	AbsoluteLine int
	Original     string
}

func (*LineEditPopup) popup() {}

// StartLineEdit opens the inline editor on the current source line. A
// no-op when the cursor has no resolvable line (pre-doc cursor, block
// with no line range).
func (m Model) StartLineEdit() (tea.Model, tea.Cmd) {
	if m.Doc == nil || m.Doc.File == "" {
		return m, nil
	}
	line := absoluteCursorLine(m)
	if line < 1 {
		return m, nil
	}
	lines := strings.Split(string(m.Doc.Source), "\n")
	if line-1 >= len(lines) {
		return m, nil
	}
	ti := textinput.New()
	ti.SetValue(lines[line-1])
	ti.Prompt = ""
	ti.Width = 120
	ti.CharLimit = 4000
	cmd := ti.Focus()
	m.Popup = &LineEditPopup{
		TI:           ti,
		AbsoluteLine: line,
		Original:     lines[line-1],
	}
	m.CountBuf = ""
	return m, cmd
}

// SubmitLineEdit commits the textinput contents back to the .tex on
// disk, then kicks the reload pipeline so parser/sidecar/PDF catch up.
func (m Model) SubmitLineEdit() (tea.Model, tea.Cmd) {
	p, ok := m.Popup.(*LineEditPopup)
	if !ok {
		return m, nil
	}
	newLine := p.TI.Value()
	m.Popup = nil
	if newLine == p.Original {
		m.Status = "line edit: no change"
		return m, nil
	}
	if err := writeSourceLine(m.Doc.File, p.AbsoluteLine, newLine); err != nil {
		m.Status = "line edit: " + err.Error()
		return m, nil
	}
	m.Status = fmt.Sprintf("line %d updated · rebuilding…", p.AbsoluteLine)
	return m.startReload()
}

// CancelLineEdit dismisses the inline editor without writing anything.
func (m Model) CancelLineEdit() (tea.Model, tea.Cmd) {
	m.Popup = nil
	return m, nil
}

// writeSourceLine rewrites line N (1-based) of path with newContent,
// preserving every other line exactly and keeping the file's trailing
// newline if the original had one. Writes atomically via os.CreateTemp
// + rename so a crash mid-write can't leave a truncated .tex, and
// preserves the original file's mode bits so a `0600` paper doesn't
// get widened to `0644` by the edit.
func writeSourceLine(path string, n int, newContent string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	hadTrailingNL := len(data) > 0 && data[len(data)-1] == '\n'
	lines := strings.Split(string(data), "\n")
	if hadTrailingNL {
		// Split leaves an empty string after the trailing "\n"; drop it so
		// our indexing matches 1-based line numbers.
		lines = lines[:len(lines)-1]
	}
	if n < 1 || n > len(lines) {
		return fmt.Errorf("line %d out of range (1..%d)", n, len(lines))
	}
	lines[n-1] = newContent
	out := strings.Join(lines, "\n")
	if hadTrailingNL {
		out += "\n"
	}

	dir := filepath.Dir(path)
	base := filepath.Base(path)
	f, err := os.CreateTemp(dir, base+".mreview-edit.*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	if _, err := f.Write([]byte(out)); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	mode := os.FileMode(0o644)
	if info, statErr := os.Stat(path); statErr == nil {
		mode = info.Mode().Perm()
	}
	if err := os.Chmod(tmp, mode); err != nil {
		os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}
