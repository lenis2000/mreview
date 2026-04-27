package pdfreview

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"gopkg.in/yaml.v3"
)

// editorCandidates is the fallback order when $EDITOR isn't set.
var editorCandidates = []string{"nvim", "vim", "vi", "nano"}

// editFinishedMsg is delivered after $EDITOR exits. The model decides
// what to do with the (possibly modified) tmp file path.
type editFinishedMsg struct {
	mode int // 0 = text-only, 1 = yaml
	id   int // comment ID being edited
	path string
	err  error
}

const (
	editText = 0
	editYAML = 1
)

// startEditText opens $EDITOR on a tmp file with the comment's
// original_text; on save, replaces the text and marks status=edited.
func (m Model) startEditText() (tea.Model, tea.Cmd) {
	c := m.currentComment()
	if c == nil {
		return m, nil
	}
	tmp, err := writeEditTextTmp(c)
	if err != nil {
		m.Status = "e: " + err.Error()
		return m, clearStatusAfter(3 * time.Second)
	}
	cmd, err := buildEditorExec(tmp)
	if err != nil {
		m.Status = "e: " + err.Error()
		return m, clearStatusAfter(3 * time.Second)
	}
	id := c.ID
	return m, tea.ExecProcess(cmd.process, func(rerr error) tea.Msg {
		if cmd.tty != nil {
			cmd.tty.Close()
		}
		return editFinishedMsg{mode: editText, id: id, path: tmp, err: rerr}
	})
}

// startEditYAML opens $EDITOR on a YAML doc representing the full
// editable subset; on save, applies (text, page, quote, kind), marks
// edited, and re-validates anchoring.
func (m Model) startEditYAML() (tea.Model, tea.Cmd) {
	c := m.currentComment()
	if c == nil {
		return m, nil
	}
	tmp, err := writeEditYAMLTmp(c)
	if err != nil {
		m.Status = "E: " + err.Error()
		return m, clearStatusAfter(3 * time.Second)
	}
	cmd, err := buildEditorExec(tmp)
	if err != nil {
		m.Status = "E: " + err.Error()
		return m, clearStatusAfter(3 * time.Second)
	}
	id := c.ID
	return m, tea.ExecProcess(cmd.process, func(rerr error) tea.Msg {
		if cmd.tty != nil {
			cmd.tty.Close()
		}
		return editFinishedMsg{mode: editYAML, id: id, path: tmp, err: rerr}
	})
}

// applyEditFinished is called by Update on editFinishedMsg.
func (m Model) applyEditFinished(msg editFinishedMsg) (tea.Model, tea.Cmd) {
	defer os.Remove(msg.path)
	if msg.err != nil {
		m.Status = "edit: editor exited with error: " + msg.err.Error()
		return m, clearStatusAfter(4 * time.Second)
	}
	target := -1
	for i := range m.Comments {
		if m.Comments[i].ID == msg.id {
			target = i
			break
		}
	}
	if target < 0 {
		m.Status = "edit: comment vanished"
		return m, clearStatusAfter(3 * time.Second)
	}
	c := &m.Comments[target]
	switch msg.mode {
	case editText:
		raw, err := os.ReadFile(msg.path)
		if err != nil {
			m.Status = "edit: read tmp: " + err.Error()
			return m, clearStatusAfter(4 * time.Second)
		}
		newText := strings.TrimRight(string(raw), "\n")
		if newText == c.OriginalText {
			m.Status = "edit: no change"
			return m, clearStatusAfter(2 * time.Second)
		}
		c.OriginalText = newText
		c.Status = StatusEdited
		m.Dirty = true
		m.Status = fmt.Sprintf("comment #%d edited", c.ID)
		return m, clearStatusAfter(3 * time.Second)

	case editYAML:
		raw, err := os.ReadFile(msg.path)
		if err != nil {
			m.Status = "edit: read tmp: " + err.Error()
			return m, clearStatusAfter(4 * time.Second)
		}
		var doc editYAMLDoc
		if err := yaml.Unmarshal(raw, &doc); err != nil {
			m.Status = "edit: bad YAML: " + err.Error()
			return m, clearStatusAfter(5 * time.Second)
		}
		if doc.Kind != "" && !ValidKind(doc.Kind) {
			m.Status = "edit: unknown kind: " + doc.Kind
			return m, clearStatusAfter(5 * time.Second)
		}
		if doc.Page < 0 || doc.Page > m.NumPages {
			m.Status = fmt.Sprintf("edit: page %d out of range (0..%d)", doc.Page, m.NumPages)
			return m, clearStatusAfter(5 * time.Second)
		}
		c.OriginalText = strings.TrimRight(doc.Text, "\n")
		if doc.Kind != "" {
			c.Kind = doc.Kind
		}
		c.Page = doc.Page
		c.Quote = doc.Quote
		c.QuoteFocus = doc.QuoteFocus
		c.Status = StatusEdited
		m.Dirty = true
		// Verify the user-supplied quote against the page text. We DON'T
		// silently rewrite — the edit is deliberate. Just warn.
		if c.Quote != "" && c.Page > 0 {
			if rects, ok := m.BBox.FindQuote(c.Page, c.Quote); !ok || len(rects) == 0 {
				m.Status = fmt.Sprintf("comment #%d edited (warning: quote not found on p.%d — highlight will be page-only)", c.ID, c.Page)
				return m, clearStatusAfter(6 * time.Second)
			}
		}
		if c.QuoteFocus != "" && c.Page > 0 {
			if rects, ok := m.BBox.FindQuote(c.Page, c.QuoteFocus); !ok || len(rects) == 0 {
				m.Status = fmt.Sprintf("comment #%d edited (warning: quote_focus not found on p.%d — strong highlight will be skipped)", c.ID, c.Page)
				return m, clearStatusAfter(6 * time.Second)
			}
		}
		m.Status = fmt.Sprintf("comment #%d edited (kind=%s, page=%d)", c.ID, c.Kind, c.Page)
		// If the user changed the page, follow it.
		if c.Page > 0 {
			m.Page = c.Page
		}
		return m, m.schedulePDFRender()
	}
	return m, nil
}

type editYAMLDoc struct {
	Page       int    `yaml:"page"`
	Quote      string `yaml:"quote"`
	QuoteFocus string `yaml:"quote_focus"`
	Kind       string `yaml:"kind"`
	Text       string `yaml:"text"`
}

const editYAMLHeader = `# Edit any field, save & quit ($EDITOR) to apply.
# page:        integer in [0, NumPages]; 0 means unanchored.
# quote:       broad context — verbatim PDF span; "" disables highlighting.
# quote_focus: optional narrow phrase pointing at the precise issue locus
#              (verbatim substring of the page; rendered as a strong
#              highlight on top of the faint quote).
# kind:        comment | minor | framing-intro | framing-outro | meta
# text:        the body text; YAML literal block (preserves newlines).
`

func writeEditYAMLTmp(c *Comment) (string, error) {
	doc := editYAMLDoc{
		Page:       c.Page,
		Quote:      c.Quote,
		QuoteFocus: c.QuoteFocus,
		Kind:       c.Kind,
		Text:       c.OriginalText,
	}
	body, err := yaml.Marshal(doc)
	if err != nil {
		return "", err
	}
	return writeTmp("mreview-pdfreview-yaml-*.yaml",
		[]byte(editYAMLHeader+string(body)))
}

func writeEditTextTmp(c *Comment) (string, error) {
	return writeTmp("mreview-pdfreview-text-*.md",
		[]byte(c.OriginalText+"\n"))
}

func writeTmp(pattern string, data []byte) (string, error) {
	f, err := os.CreateTemp("", pattern)
	if err != nil {
		return "", err
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		os.Remove(f.Name())
		return "", err
	}
	if err := f.Close(); err != nil {
		os.Remove(f.Name())
		return "", err
	}
	return f.Name(), nil
}

// editorExec bundles the configured *exec.Cmd with the /dev/tty handle
// that must outlive it (closed in the bubbletea callback).
type editorExec struct {
	process *exec.Cmd
	tty     *os.File
}

func buildEditorExec(path string) (editorExec, error) {
	bin, args, ok := resolveEditor()
	if !ok {
		return editorExec{}, fmt.Errorf("no editor found (set $EDITOR)")
	}
	args = append(append([]string{}, args...), path)
	cmd := exec.Command(bin, args...)
	out := editorExec{process: cmd}
	if tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0); err == nil {
		out.tty = tty
		cmd.Stdin = tty
		cmd.Stdout = tty
		cmd.Stderr = tty
	} else {
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
	}
	return out, nil
}

func resolveEditor() (string, []string, bool) {
	if v := strings.TrimSpace(os.Getenv("EDITOR")); v != "" {
		toks := tokenize(v)
		if len(toks) > 0 {
			if _, err := exec.LookPath(toks[0]); err == nil {
				return toks[0], toks[1:], true
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

// tokenize is a minimal whitespace splitter for $EDITOR. Quotes are not
// supported — this matches the user's typical usage ("vim -p" etc.) and
// avoids reimplementing pkg/ui/editor.go's parseShellArgs.
func tokenize(s string) []string {
	return strings.Fields(s)
}

// baseTmpPattern is exposed so tests can predict the tmp filenames.
func baseTmpPattern(p string) string { return filepath.Base(p) }
