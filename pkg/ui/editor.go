package ui

import (
	"fmt"
	"os"
	"os/exec"
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
	editor := resolveEditor()
	if editor == "" {
		m.Status = "E: no editor found (set $EDITOR)"
		return m, nil
	}
	if err := m.saveSidecar(); err != nil {
		m.Status = "E: save sidecar: " + err.Error()
		return m, nil
	}
	args := buildEditorArgs(editor, m.Doc.File, absoluteCursorLine(m))
	cmd := exec.Command(editor, args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return m, tea.ExecProcess(cmd, func(err error) tea.Msg {
		return reloadMsg{err: err}
	})
}

// resolveEditor picks an editor binary. $EDITOR wins when its first word
// resolves to an executable on $PATH; otherwise we fall back to the
// editorCandidates list in order.
func resolveEditor() string {
	if v := os.Getenv("EDITOR"); v != "" {
		head := strings.Fields(v)
		if len(head) > 0 {
			if _, err := exec.LookPath(head[0]); err == nil {
				return head[0]
			}
		}
	}
	for _, c := range editorCandidates {
		if _, err := exec.LookPath(c); err == nil {
			return c
		}
	}
	return ""
}

// buildEditorArgs assembles editor arguments. Vim-family editors accept
// `+N` to jump to line N; VS Code / emacs-client use other forms which
// we don't try to auto-detect — $EDITOR can always include its own flags
// and we append the filename last.
func buildEditorArgs(editor, path string, line int) []string {
	if line < 1 {
		line = 1
	}
	switch {
	case strings.Contains(editor, "vim"), strings.Contains(editor, "vi"), strings.Contains(editor, "nano"):
		return []string{fmt.Sprintf("+%d", line), path}
	case strings.Contains(editor, "emacs"):
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
// newline if the original had one. Writes atomically via a temp file +
// rename so a crash mid-write can't leave a truncated .tex.
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
	tmp := path + ".mreview-edit.tmp"
	if err := os.WriteFile(tmp, []byte(out), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
