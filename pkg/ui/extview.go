package ui

import (
	"fmt"
	"os"
	"os/exec"

	tea "github.com/charmbracelet/bubbletea"
)

// defaultPDFViewer is the absolute path to LP's docviewer binary (the
// `dv` shell alias points here). Used as the fallback when the
// MREVIEW_PDF_VIEWER env var is unset and `dv` is not on $PATH (which it
// usually isn't, since dv is just a shell alias).
const defaultPDFViewer = "/Users/leo/local/bin/docviewer"

// openPDFViewer suspends the TUI and execs an external PDF viewer on the
// current paper's PDF. Resolution order:
//  1. $MREVIEW_PDF_VIEWER (single binary path; arguments not supported).
//  2. The `dv` binary on $PATH.
//  3. defaultPDFViewer.
//
// Returns a status-only update if the PDF isn't loaded or the viewer
// can't be located, so the user gets feedback in the status bar instead
// of a silent no-op.
func (m Model) openPDFViewer() (tea.Model, tea.Cmd) {
	if m.PDF == nil {
		m.Status = "V: no PDF loaded"
		return m, nil
	}
	pdfPath := m.PDF.Path()
	if pdfPath == "" {
		m.Status = "V: PDF path unknown"
		return m, nil
	}
	viewer := resolvePDFViewer()
	if viewer == "" {
		m.Status = fmt.Sprintf("V: PDF viewer not found (set $MREVIEW_PDF_VIEWER or install at %s)", defaultPDFViewer)
		return m, nil
	}
	cmd := exec.Command(viewer, pdfPath)
	return m, tea.ExecProcess(cmd, func(err error) tea.Msg {
		if err != nil {
			return statusMsg{text: fmt.Sprintf("V: viewer exited: %v", err)}
		}
		return statusMsg{text: ""}
	})
}

// statusMsg is delivered after the external viewer exits. The Update loop
// applies it to Model.Status so any error message is visible once the TUI
// resumes.
type statusMsg struct{ text string }

// resolvePDFViewer picks the binary to exec, in env / PATH / fallback order.
func resolvePDFViewer() string {
	if v := os.Getenv("MREVIEW_PDF_VIEWER"); v != "" {
		if _, err := os.Stat(v); err == nil {
			return v
		}
		if abs, err := exec.LookPath(v); err == nil {
			return abs
		}
	}
	if abs, err := exec.LookPath("dv"); err == nil {
		return abs
	}
	if _, err := os.Stat(defaultPDFViewer); err == nil {
		return defaultPDFViewer
	}
	return ""
}
