package main

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// withStubTUI swaps runTUI for a no-op for the duration of t. The stub
// records the model it received so tests can assert on it.
func withStubTUI(t *testing.T) *tea.Model {
	t.Helper()
	saved := runTUI
	var captured tea.Model
	runTUI = func(model tea.Model, _, _ io.Writer) error {
		captured = model
		return nil
	}
	t.Cleanup(func() { runTUI = saved })
	return &captured
}

func TestRun_Version(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"--version"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("expected exit 0, got %d (stderr=%q)", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "mreview ") {
		t.Fatalf("expected version line on stdout, got %q", stdout.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("expected empty stderr, got %q", stderr.String())
	}
}

func TestRun_ShortVersion(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"-v"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("expected exit 0, got %d (stderr=%q)", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "mreview ") {
		t.Fatalf("expected version line on stdout, got %q", stdout.String())
	}
}

func TestRun_MissingArg(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{}, &stdout, &stderr)
	if code != 2 {
		t.Fatalf("expected exit 2, got %d", code)
	}
	if !strings.Contains(stderr.String(), "missing paper") {
		t.Fatalf("expected missing-paper message on stderr, got %q", stderr.String())
	}
	if !strings.Contains(stderr.String(), "usage:") {
		t.Fatalf("expected usage hint on stderr, got %q", stderr.String())
	}
}

func TestRun_MissingFile(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"/nonexistent/path/to/paper.tex"}, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("expected exit 1, got %d", code)
	}
	if !strings.Contains(stderr.String(), "cannot read") {
		t.Fatalf("expected cannot-read message on stderr, got %q", stderr.String())
	}
}

func TestRun_ExistingFileLaunchesTUI(t *testing.T) {
	dir := t.TempDir()
	paper := filepath.Join(dir, "paper.tex")
	if err := os.WriteFile(paper, []byte("\\documentclass{amsart}\n\\begin{document}hi\\end{document}\n"), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	captured := withStubTUI(t)

	var stdout, stderr bytes.Buffer
	code := run([]string{paper}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("expected exit 0, got %d (stderr=%q)", code, stderr.String())
	}
	if *captured == nil {
		t.Fatalf("expected runTUI to be invoked")
	}
}

func TestRun_UnknownFlag(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"--does-not-exist"}, &stdout, &stderr)
	if code != 2 {
		t.Fatalf("expected exit 2 on unknown flag, got %d", code)
	}
	if stderr.Len() == 0 {
		t.Fatalf("expected error on stderr, got empty")
	}
}

func TestRun_HelpExitsZero(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"--help"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("expected exit 0 for --help, got %d", code)
	}
}
