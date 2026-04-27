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
	line, ok := absoluteCursorLine(m)
	if !ok {
		m.Status = "E: cursor has no resolvable source line"
		return m, nil
	}
	if err := m.saveSidecar(); err != nil {
		m.Status = "E: save sidecar: " + err.Error()
		return m, nil
	}
	if err := (&m).pushEditSnapshot("external editor"); err != nil {
		m.Status = "E: snapshot: " + err.Error()
		return m, nil
	}
	lineArgs := buildEditorLineArgs(head, m.Doc.File, line)
	argv := append(append([]string{}, userArgs...), lineArgs...)
	cmd := exec.Command(head, argv...)
	// Route the editor's IO through /dev/tty to mirror what runTUI does
	// for the main program. Without this, `mreview paper.tex > review.md`
	// would give $EDITOR an os.Stdout pointed at review.md — full-screen
	// control sequences would land in the redirected file and the editor
	// would have no interactive terminal to paint on. The tty handle
	// must outlive the exec (which happens inside tea.ExecProcess, not
	// inside this function), so the close is deferred into the
	// completion callback.
	var ttyFile *os.File
	if tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0); err == nil {
		ttyFile = tty
		cmd.Stdin = tty
		cmd.Stdout = tty
		cmd.Stderr = tty
	} else {
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
	}
	return m, tea.ExecProcess(cmd, func(err error) tea.Msg {
		if ttyFile != nil {
			ttyFile.Close()
		}
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
// $EDITOR is tokenised with ParseShellArgs, which understands single
// and double quotes and backslash escapes — enough for GUI-app paths
// containing spaces (`EDITOR="/Applications/My App/bin/edit" --wait`)
// without pulling in a full shell parser. Variable expansion and
// command substitution are intentionally not supported; a user who
// needs those should point $EDITOR at a wrapper script.
func resolveEditor() (string, []string, bool) {
	if v := strings.TrimSpace(os.Getenv("EDITOR")); v != "" {
		tokens := ParseShellArgs(v)
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

// ParseShellArgs tokenises an $EDITOR-style string the way a POSIX
// shell would for simple commands: whitespace separates tokens;
// single quotes take their contents literally; double quotes take
// contents literally except that a backslash before `"` or `\`
// is an escape; outside quotes a backslash escapes the next rune
// (so `\ ` produces a literal space). Unterminated quotes fall
// through as a single token ending at EOL — good enough for
// `EDITOR` strings, which are never interactively composed.
func ParseShellArgs(s string) []string {
	const (
		stateNormal = iota
		stateSingle
		stateDouble
	)
	var out []string
	var buf []rune
	state := stateNormal
	inToken := false
	flush := func() {
		if inToken {
			out = append(out, string(buf))
			buf = buf[:0]
			inToken = false
		}
	}
	runes := []rune(s)
	for i := 0; i < len(runes); i++ {
		r := runes[i]
		switch state {
		case stateNormal:
			switch {
			case r == ' ' || r == '\t':
				flush()
			case r == '\'':
				state = stateSingle
				inToken = true
			case r == '"':
				state = stateDouble
				inToken = true
			case r == '\\' && i+1 < len(runes):
				i++
				buf = append(buf, runes[i])
				inToken = true
			default:
				buf = append(buf, r)
				inToken = true
			}
		case stateSingle:
			if r == '\'' {
				state = stateNormal
			} else {
				buf = append(buf, r)
			}
		case stateDouble:
			if r == '"' {
				state = stateNormal
			} else if r == '\\' && i+1 < len(runes) &&
				(runes[i+1] == '"' || runes[i+1] == '\\') {
				i++
				buf = append(buf, runes[i])
			} else {
				buf = append(buf, r)
			}
		}
	}
	flush()
	return out
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
// pair to a 1-based absolute source line number. Returns ok=false when
// the cursor doesn't anchor to a real line (no doc, missing block, or
// block has no source range) — callers must refuse the action instead
// of falling open to line 1, which is rarely what the user meant.
func absoluteCursorLine(m Model) (int, bool) {
	if m.Doc == nil || m.CursorBlockID == "" {
		return 0, false
	}
	b := m.Doc.ByID[m.CursorBlockID]
	if b == nil || b.StartLine == 0 {
		return 0, false
	}
	line := b.StartLine + m.SourceLineCursor - 1
	if line < 1 {
		return 0, false
	}
	return line, true
}

// --- Inline single-line edit ------------------------------------------------

// LineEditPopup hosts the inline single-line editor (e). Scope is
// deliberately one source line so the workflow stays as "minor wording
// fix without leaving mreview" — structural edits still belong in
// $EDITOR. AbsoluteLine is the 1-based line we're rewriting on disk;
// Original holds the pre-edit content so cancel can confirm no change.
//
// NormalMode toggles a minimal vim-style modal editor on top of the
// textinput: false means insert (default, keys type through), true
// means normal (w/b/e/h/l/0/$ with count prefixes like 5b). Count
// accumulates digit presses in normal mode until a motion consumes
// them.
type LineEditPopup struct {
	TI           textinput.Model
	AbsoluteLine int
	Original     string
	NormalMode   bool
	Count        string
	// Indent holds the leading whitespace (tabs and/or spaces) of the
	// original line. bubbles textinput sanitises tabs to spaces on
	// SetValue, so indenting characters can't live inside the
	// textinput; we strip them at start, hide them from the editor,
	// and re-prepend them on submit so the .tex stays byte-faithful
	// outside the body of the line.
	Indent string
	// History stacks the textinput value just before each mutation so
	// `u` in normal mode can step a typo back. Capped by
	// maxLineEditHistory; older states drop off the bottom.
	History []string
	// Redo is the inverse stack: each `u` pop captures the
	// pre-restore value here so Ctrl-R can replay it. A fresh
	// mutation (typing, x) clears Redo — same vim-style branch
	// abandonment as the file-level redo.
	Redo []string
}

const maxLineEditHistory = 500

// pushHistory records prev as the pre-mutation value if it differs
// from the current textinput value (i.e. a real change happened).
// Idempotent for no-op key presses. Mutating the buffer abandons
// the redo branch.
func (p *LineEditPopup) pushHistory(prev string) {
	if p.TI.Value() == prev {
		return
	}
	p.History = append(p.History, prev)
	if len(p.History) > maxLineEditHistory {
		p.History = p.History[len(p.History)-maxLineEditHistory:]
	}
	p.Redo = nil
}

// popHistory pops the most recent pre-mutation value and pushes the
// *current* value onto Redo so a follow-up Ctrl-R can replay it.
// Returns ("", false) when the undo stack is empty.
func (p *LineEditPopup) popHistory() (string, bool) {
	n := len(p.History)
	if n == 0 {
		return "", false
	}
	v := p.History[n-1]
	p.History = p.History[:n-1]
	p.Redo = append(p.Redo, p.TI.Value())
	if len(p.Redo) > maxLineEditHistory {
		p.Redo = p.Redo[len(p.Redo)-maxLineEditHistory:]
	}
	return v, true
}

// popRedo is the symmetric counterpart for Ctrl-R: pop the most
// recent post-undo value and push the current value back onto
// History so a follow-up `u` can walk back through the redone edit.
func (p *LineEditPopup) popRedo() (string, bool) {
	n := len(p.Redo)
	if n == 0 {
		return "", false
	}
	v := p.Redo[n-1]
	p.Redo = p.Redo[:n-1]
	p.History = append(p.History, p.TI.Value())
	if len(p.History) > maxLineEditHistory {
		p.History = p.History[len(p.History)-maxLineEditHistory:]
	}
	return v, true
}

func (*LineEditPopup) popup() {}

// StartLineEdit opens the inline editor on the current source line. A
// no-op when the cursor has no resolvable line (pre-doc cursor, block
// with no line range).
func (m Model) StartLineEdit() (tea.Model, tea.Cmd) {
	if m.Doc == nil || m.Doc.File == "" {
		return m, nil
	}
	line, ok := absoluteCursorLine(m)
	if !ok {
		m.Status = "e: cursor has no resolvable source line"
		return m, nil
	}
	lines := strings.Split(string(m.Doc.Source), "\n")
	if line-1 >= len(lines) {
		return m, nil
	}
	full := lines[line-1]
	indent, body := splitLeadingIndent(full)
	ti := textinput.New()
	ti.SetValue(body)
	ti.Prompt = ""
	ti.Width = 120
	ti.CharLimit = 4000
	cmd := ti.Focus()
	m.Popup = &LineEditPopup{
		TI:           ti,
		AbsoluteLine: line,
		Original:     full,
		Indent:       indent,
	}
	m.CountBuf = ""
	return m, cmd
}

// splitLeadingIndent slices off the leading run of tab/space bytes so
// the inline editor can keep them invisible to bubbles' textinput
// (which collapses tabs to spaces). Returns (indent, rest).
func splitLeadingIndent(s string) (string, string) {
	i := 0
	for i < len(s) && (s[i] == ' ' || s[i] == '\t') {
		i++
	}
	return s[:i], s[i:]
}

// SubmitLineEdit commits the textinput contents back to the .tex on
// disk, then kicks the reload pipeline so parser/sidecar/PDF catch up.
func (m Model) SubmitLineEdit() (tea.Model, tea.Cmd) {
	p, ok := m.Popup.(*LineEditPopup)
	if !ok {
		return m, nil
	}
	newLine := p.Indent + p.TI.Value()
	m.Popup = nil
	if newLine == p.Original {
		m.Status = "line edit: no change"
		return m, nil
	}
	if err := (&m).pushEditSnapshot(fmt.Sprintf("line %d", p.AbsoluteLine)); err != nil {
		m.Status = "line edit: snapshot: " + err.Error()
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

// --- Undo for in-place edits ------------------------------------------------

// pushEditSnapshot reads the current paper.tex and pushes its bytes
// onto the in-memory undo stack so a later `u` can restore it. Called
// from SubmitLineEdit and editInExternalEditor *before* they write the
// new contents. Pointer receiver so the slice mutation is visible to
// the caller's Model copy.
//
// Also clears the redo stack: a fresh edit after an undo means the
// user is moving forward on a new branch; replaying the abandoned
// branch would no longer make sense.
func (m *Model) pushEditSnapshot(label string) error {
	if m.Doc == nil || m.Doc.File == "" {
		return fmt.Errorf("no source file")
	}
	data, err := os.ReadFile(m.Doc.File)
	if err != nil {
		return err
	}
	m.EditUndo = append(m.EditUndo, EditSnapshot{
		Path:  m.Doc.File,
		Bytes: data,
		Label: label,
	})
	if len(m.EditUndo) > maxEditUndo {
		m.EditUndo = m.EditUndo[len(m.EditUndo)-maxEditUndo:]
	}
	m.EditRedo = nil
	return nil
}

// UndoEdit pops the most recent edit snapshot, writes it back to disk,
// and kicks the reload pipeline so parser/sidecar/PDF catch up. Empty
// stack is a no-op with a status hint. Annotations live in the sidecar
// (untouched) and get remapped onto the restored source by the normal
// reload — this only reverts the .tex.
//
// Before restoring, the *current* file contents are captured onto
// EditRedo so Ctrl-R can replay the edit. A failed undo leaves both
// stacks unchanged.
func (m Model) UndoEdit() (tea.Model, tea.Cmd) {
	if len(m.EditUndo) == 0 {
		m.Status = "u: nothing to undo"
		return m, nil
	}
	snap := m.EditUndo[len(m.EditUndo)-1]
	current, err := os.ReadFile(snap.Path)
	if err != nil {
		m.Status = "u: " + err.Error()
		return m, nil
	}
	if err := writeFileAtomic(snap.Path, snap.Bytes); err != nil {
		m.Status = "u: " + err.Error()
		return m, nil
	}
	m.EditUndo = m.EditUndo[:len(m.EditUndo)-1]
	m.EditRedo = append(m.EditRedo, EditSnapshot{
		Path:  snap.Path,
		Bytes: current,
		Label: snap.Label,
	})
	if len(m.EditRedo) > maxEditUndo {
		m.EditRedo = m.EditRedo[len(m.EditRedo)-maxEditUndo:]
	}
	m.Status = fmt.Sprintf("undid %s · rebuilding…", snap.Label)
	return m.startReload()
}

// RedoEdit reverses the most recent UndoEdit by restoring the
// post-edit bytes captured at undo time. The corresponding undo
// snapshot is pushed back onto EditUndo so a follow-up `u` can walk
// back again. Empty redo stack is a no-op with a status hint.
func (m Model) RedoEdit() (tea.Model, tea.Cmd) {
	if len(m.EditRedo) == 0 {
		m.Status = "ctrl-r: nothing to redo"
		return m, nil
	}
	snap := m.EditRedo[len(m.EditRedo)-1]
	current, err := os.ReadFile(snap.Path)
	if err != nil {
		m.Status = "ctrl-r: " + err.Error()
		return m, nil
	}
	if err := writeFileAtomic(snap.Path, snap.Bytes); err != nil {
		m.Status = "ctrl-r: " + err.Error()
		return m, nil
	}
	m.EditRedo = m.EditRedo[:len(m.EditRedo)-1]
	m.EditUndo = append(m.EditUndo, EditSnapshot{
		Path:  snap.Path,
		Bytes: current,
		Label: snap.Label,
	})
	if len(m.EditUndo) > maxEditUndo {
		m.EditUndo = m.EditUndo[len(m.EditUndo)-maxEditUndo:]
	}
	m.Status = fmt.Sprintf("redid %s · rebuilding…", snap.Label)
	return m.startReload()
}

// writeSourceLine rewrites line N (1-based) of path with newContent,
// preserving every other line exactly and keeping the file's trailing
// newline if the original had one. Delegates the actual disk write to
// writeFileAtomic so the temp-file + chmod + rename dance is shared
// with the undo path.
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
	return writeFileAtomic(path, []byte(out))
}

// writeFileAtomic writes data to path via os.CreateTemp + rename so a
// crash mid-write can't leave a truncated file, and preserves the
// original file's mode bits so a `0600` paper doesn't get widened to
// `0644` by the edit. Used by both the inline-edit writer and the
// undo restore.
func writeFileAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	base := filepath.Base(path)
	f, err := os.CreateTemp(dir, base+".mreview-edit.*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	if _, err := f.Write(data); err != nil {
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

// isWordRune mirrors vim's default `iskeyword` for the word motions
// (w/b/e): letters, digits, and underscore count as word characters;
// everything else (punctuation, backslash, braces, whitespace) is a
// separator. LaTeX-aware tweaks (treating `\foo` as one word) are
// deliberately skipped — the stock vim rule is what muscle memory
// expects.
func isWordRune(r rune) bool {
	switch {
	case r >= 'a' && r <= 'z':
		return true
	case r >= 'A' && r <= 'Z':
		return true
	case r >= '0' && r <= '9':
		return true
	case r == '_':
		return true
	}
	return false
}

// wordClass returns 0 for whitespace, 1 for word runes, 2 for other
// (punctuation). Vim's w/b/e treat a transition between classes 1 and
// 2 as a word boundary, which is why `foo.bar` has three "words".
func wordClass(r rune) int {
	switch {
	case r == ' ' || r == '\t':
		return 0
	case isWordRune(r):
		return 1
	default:
		return 2
	}
}

// motionWordForward advances one vim-`w`: from the current position,
// skip the rest of the current run, then skip any whitespace, landing
// on the first rune of the next word. Returns len(runes) if there is
// no next word.
func motionWordForward(runes []rune, pos int) int {
	n := len(runes)
	if pos >= n {
		return n
	}
	start := wordClass(runes[pos])
	i := pos
	if start != 0 {
		for i < n && wordClass(runes[i]) == start {
			i++
		}
	}
	for i < n && wordClass(runes[i]) == 0 {
		i++
	}
	if i == pos {
		i++
	}
	return i
}

// motionWordBackward steps one vim-`b`: from pos, skip whitespace
// leftward, then step back to the start of the current run. Returns 0
// when already at or before the first word.
func motionWordBackward(runes []rune, pos int) int {
	if pos <= 0 {
		return 0
	}
	i := pos - 1
	for i > 0 && wordClass(runes[i]) == 0 {
		i--
	}
	cls := wordClass(runes[i])
	if cls == 0 {
		return 0
	}
	for i > 0 && wordClass(runes[i-1]) == cls {
		i--
	}
	return i
}

// motionWORDForward advances one vim-`W`: a WORD is any run of
// non-whitespace, so punctuation-boundary transitions are ignored.
// Useful for stepping over `\command{arg}` as a single unit.
func motionWORDForward(runes []rune, pos int) int {
	n := len(runes)
	if pos >= n {
		return n
	}
	i := pos
	if !isSpace(runes[i]) {
		for i < n && !isSpace(runes[i]) {
			i++
		}
	}
	for i < n && isSpace(runes[i]) {
		i++
	}
	if i == pos {
		i++
	}
	return i
}

// motionWORDBackward steps one vim-`B`: skip trailing whitespace,
// then walk to the start of the current non-whitespace run.
func motionWORDBackward(runes []rune, pos int) int {
	if pos <= 0 {
		return 0
	}
	i := pos - 1
	for i > 0 && isSpace(runes[i]) {
		i--
	}
	if isSpace(runes[i]) {
		return 0
	}
	for i > 0 && !isSpace(runes[i-1]) {
		i--
	}
	return i
}

// motionWORDEnd advances one vim-`E`: land on the last rune of the
// current WORD, or of the next WORD if already there.
func motionWORDEnd(runes []rune, pos int) int {
	n := len(runes)
	if pos >= n-1 {
		if n == 0 {
			return 0
		}
		return n - 1
	}
	i := pos + 1
	for i < n && isSpace(runes[i]) {
		i++
	}
	if i >= n {
		return n - 1
	}
	for i+1 < n && !isSpace(runes[i+1]) {
		i++
	}
	return i
}

// isSpace is the WORD-motion separator: only ASCII whitespace counts,
// mirroring vim's default behaviour for W/B/E.
func isSpace(r rune) bool {
	return r == ' ' || r == '\t'
}

// motionWordEnd advances one vim-`e`: land on the last rune of the
// current word, or of the next word if already there. Returns the
// last index (n-1) when there is no further word end.
func motionWordEnd(runes []rune, pos int) int {
	n := len(runes)
	if pos >= n-1 {
		if n == 0 {
			return 0
		}
		return n - 1
	}
	i := pos + 1
	for i < n && wordClass(runes[i]) == 0 {
		i++
	}
	if i >= n {
		return n - 1
	}
	cls := wordClass(runes[i])
	for i+1 < n && wordClass(runes[i+1]) == cls {
		i++
	}
	return i
}
