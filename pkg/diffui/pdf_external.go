package diffui

import (
	"errors"
	"fmt"
	"os"
	"os/exec"

	tea "github.com/charmbracelet/bubbletea"

	"mreview/pkg/build"
)

var runDiffPDFOpenProcess = func(cmd *exec.Cmd) tea.Cmd {
	return func() tea.Msg {
		if err := cmd.Start(); err != nil {
			return diffPDFOpenFinishedMsg{err: err}
		}
		go func() { _ = cmd.Wait() }()
		return diffPDFOpenFinishedMsg{}
	}
}

type diffPDFOpenFinishedMsg struct {
	err error
}

func (m Model) openPreviewPDF() (tea.Model, tea.Cmd) {
	path, err := m.newPDFPath()
	if err != nil {
		m.Status = "P: " + err.Error()
		return m, nil
	}
	m.Status = "opening new PDF in Preview"
	return m, runDiffPDFOpenProcess(exec.Command("open", "-a", "Preview", path))
}

func (m Model) applyPDFOpenFinished(msg diffPDFOpenFinishedMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.Status = "P: " + msg.err.Error()
		return m, nil
	}
	m.Status = "opened new PDF in Preview"
	return m, nil
}

func (m Model) newPDFPath() (string, error) {
	if m.PDF != nil && m.PDF.Path() != "" {
		return m.PDF.Path(), nil
	}
	texPath, ok := newEndpointBuildPath(m.Review)
	if !ok {
		return "", errors.New("new endpoint is not a filesystem file")
	}
	res := build.ResolveBuildOutputsOnDisk(texPath)
	if res == nil || res.PDFPath == "" {
		return "", errors.New("new PDF path unavailable")
	}
	if _, err := os.Stat(res.PDFPath); err != nil {
		return "", fmt.Errorf("new PDF not found: %w", err)
	}
	return res.PDFPath, nil
}
