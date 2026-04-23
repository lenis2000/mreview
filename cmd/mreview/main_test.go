package main

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"mreview/pkg/persist"
	"mreview/pkg/ui"
)

// withStubTUI swaps runTUI for a no-op for the duration of t. The stub
// records the model it received so tests can assert on it.
func withStubTUI(t *testing.T) *tea.Model {
	t.Helper()
	saved := runTUI
	var captured tea.Model
	runTUI = func(model tea.Model, _, _ io.Writer) (tea.Model, error) {
		captured = model
		return model, nil
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

// writeFixturePaper writes a minimal .tex file and returns its path.
func writeFixturePaper(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	paper := filepath.Join(dir, "paper.tex")
	body := "\\documentclass{amsart}\n" +
		"\\newtheorem{theorem}{Theorem}\n" +
		"\\begin{document}\n" +
		"\\section{Intro}\\label{sec:intro}\n" +
		"Some paragraph.\n" +
		"\\begin{theorem}\\label{thm:main}\nA statement.\n\\end{theorem}\n" +
		"\\end{document}\n"
	if err := os.WriteFile(paper, []byte(body), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	return paper
}

func TestRun_EmitMarkdownOnQuit(t *testing.T) {
	paper := writeFixturePaper(t)
	sidecarPath := paper + ".mreview.md"

	// Seed a sidecar with one annotation so there is something to emit.
	seed := &persist.Sidecar{
		Paper: paper,
		Annotations: []persist.Annotation{{
			BlockID:     "thm:main",
			Breadcrumb:  "Theorem 1",
			File:        paper,
			StartLine:   6,
			EndLine:     8,
			SourceQuote: "\\begin{theorem}\\label{thm:main}\nA statement.\n\\end{theorem}",
			Note:        "check statement",
		}},
	}
	if err := persist.Save(sidecarPath, seed); err != nil {
		t.Fatalf("seed sidecar: %v", err)
	}

	withStubTUI(t)

	var stdout, stderr bytes.Buffer
	code := run([]string{"--stdout=md", paper}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("expected exit 0, got %d (stderr=%q)", code, stderr.String())
	}
	out := stdout.String()
	if !strings.Contains(out, "check statement") {
		t.Fatalf("expected note in markdown stdout; got %q", out)
	}
	if strings.HasPrefix(strings.TrimLeft(out, "\n"), "---") {
		t.Fatalf("markdown stdout must omit YAML frontmatter; got %q", out)
	}
	// Sidecar file must still exist and parse cleanly — i.e. save ran.
	side, err := persist.Load(sidecarPath)
	if err != nil {
		t.Fatalf("load sidecar after quit: %v", err)
	}
	if len(side.Annotations) != 1 {
		t.Fatalf("expected 1 annotation post-save, got %d", len(side.Annotations))
	}
}

func TestRun_EmitJSONOnQuit(t *testing.T) {
	paper := writeFixturePaper(t)
	sidecarPath := paper + ".mreview.md"
	seed := &persist.Sidecar{
		Annotations: []persist.Annotation{{
			BlockID:     "thm:main",
			Breadcrumb:  "Theorem 1",
			File:        paper,
			StartLine:   6,
			EndLine:     8,
			SourceQuote: "stmt",
			Note:        "check",
		}},
	}
	if err := persist.Save(sidecarPath, seed); err != nil {
		t.Fatalf("seed sidecar: %v", err)
	}

	withStubTUI(t)

	var stdout, stderr bytes.Buffer
	code := run([]string{"--stdout=json", paper}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit %d, stderr=%q", code, stderr.String())
	}
	var records []map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &records); err != nil {
		t.Fatalf("stdout not valid JSON: %v\n%s", err, stdout.String())
	}
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	if records[0]["block_id"] != "thm:main" {
		t.Fatalf("expected block_id=thm:main, got %v", records[0]["block_id"])
	}
	if records[0]["note"] != "check" {
		t.Fatalf("expected note=check, got %v", records[0]["note"])
	}
}

func TestRun_EmitNoneSkipsStdout(t *testing.T) {
	paper := writeFixturePaper(t)
	sidecarPath := paper + ".mreview.md"
	seed := &persist.Sidecar{
		Annotations: []persist.Annotation{{
			BlockID: "thm:main", Breadcrumb: "Theorem 1",
			File: paper, StartLine: 6, EndLine: 8,
			SourceQuote: "stmt", Note: "note",
		}},
	}
	if err := persist.Save(sidecarPath, seed); err != nil {
		t.Fatalf("seed: %v", err)
	}

	withStubTUI(t)
	var stdout, stderr bytes.Buffer
	code := run([]string{"--stdout=none", paper}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit %d stderr=%q", code, stderr.String())
	}
	if stdout.Len() != 0 {
		t.Fatalf("--stdout=none should not emit anything; got %q", stdout.String())
	}
}

func TestRun_DetachedAnnotationsSurvive(t *testing.T) {
	paper := writeFixturePaper(t)
	sidecarPath := paper + ".mreview.md"

	// Seed a sidecar whose annotation references a block that is NOT in the
	// current paper. Remap must divert it to Detached, and run() must save
	// the detached list (and emit it to stdout on quit).
	seed := &persist.Sidecar{
		Annotations: []persist.Annotation{{
			BlockID:     "gone:block",
			Breadcrumb:  "Vanished",
			File:        paper,
			StartLine:   99,
			EndLine:     100,
			SourceQuote: "this source does not match any block in the fresh parse",
			Note:        "orphan note",
		}},
	}
	if err := persist.Save(sidecarPath, seed); err != nil {
		t.Fatalf("seed: %v", err)
	}

	withStubTUI(t)
	var stdout, stderr bytes.Buffer
	code := run([]string{"--stdout=md", paper}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit %d stderr=%q", code, stderr.String())
	}

	// The saved sidecar should carry the annotation in the Detached section.
	side, err := persist.Load(sidecarPath)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(side.Annotations) != 0 {
		t.Fatalf("expected 0 attached post-remap, got %d", len(side.Annotations))
	}
	if len(side.Detached) != 1 {
		t.Fatalf("expected 1 detached, got %d", len(side.Detached))
	}
	if side.Detached[0].BlockID != "gone:block" {
		t.Fatalf("unexpected detached block id: %q", side.Detached[0].BlockID)
	}

	// stdout should include the ## Detached marker and the orphan note.
	out := stdout.String()
	if !strings.Contains(out, persist.DetachedMarker) {
		t.Fatalf("stdout missing detached marker:\n%s", out)
	}
	if !strings.Contains(out, "orphan note") {
		t.Fatalf("stdout missing orphan note:\n%s", out)
	}
}

func TestRun_DetachedShowsStatusCountInModel(t *testing.T) {
	paper := writeFixturePaper(t)
	sidecarPath := paper + ".mreview.md"
	seed := &persist.Sidecar{
		Annotations: []persist.Annotation{{
			BlockID:     "gone:block",
			Breadcrumb:  "Vanished",
			File:        paper,
			StartLine:   99,
			EndLine:     100,
			SourceQuote: "wholly unrelated text",
			Note:        "orphan",
		}},
	}
	if err := persist.Save(sidecarPath, seed); err != nil {
		t.Fatalf("seed: %v", err)
	}

	captured := withStubTUI(t)
	var stdout, stderr bytes.Buffer
	code := run([]string{"--stdout=none", paper}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit %d stderr=%q", code, stderr.String())
	}
	if *captured == nil {
		t.Fatalf("runTUI not invoked")
	}
	m, ok := (*captured).(ui.Model)
	if !ok {
		t.Fatalf("unexpected model type %T", *captured)
	}
	if !strings.Contains(m.Status, "detached") {
		t.Fatalf("expected status to mention detached, got %q", m.Status)
	}
}

func TestRun_UnknownStdoutFormatRejected(t *testing.T) {
	paper := writeFixturePaper(t)
	// Go-flags enforces the allowed choices at parse time, so an invalid
	// value is a usage error (exit 2). The check confirms that guard.
	var stdout, stderr bytes.Buffer
	code := run([]string{"--stdout=xml", paper}, &stdout, &stderr)
	if code != 2 {
		t.Fatalf("expected exit 2 for invalid --stdout, got %d (stderr=%q)", code, stderr.String())
	}
}
