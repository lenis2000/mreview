package main

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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
	// Run from an empty temp dir so the lone-.tex auto-pick can't kick in.
	chdir(t, t.TempDir())

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

func TestRun_NoArgPicksLoneTex(t *testing.T) {
	dir := t.TempDir()
	paper := filepath.Join(dir, "paper.tex")
	if err := os.WriteFile(paper, []byte("\\documentclass{amsart}\n\\begin{document}\nhi\n\\end{document}\n"), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	chdir(t, dir)
	captured := withStubTUI(t)

	var stdout, stderr bytes.Buffer
	code := run([]string{"--no-build"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("expected exit 0, got %d (stderr=%q)", code, stderr.String())
	}
	if *captured == nil {
		t.Fatalf("expected runTUI to be invoked")
	}
}

func TestRun_NoArgMultipleTexErrors(t *testing.T) {
	dir := t.TempDir()
	for _, n := range []string{"a.tex", "b.tex"} {
		if err := os.WriteFile(filepath.Join(dir, n), []byte("\\documentclass{amsart}\n\\begin{document}\nhi\n\\end{document}\n"), 0o600); err != nil {
			t.Fatalf("write fixture: %v", err)
		}
	}
	chdir(t, dir)

	var stdout, stderr bytes.Buffer
	code := run([]string{}, &stdout, &stderr)
	if code != 2 {
		t.Fatalf("expected exit 2, got %d (stderr=%q)", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "missing paper") {
		t.Fatalf("expected missing-paper message, got %q", stderr.String())
	}
}

// chdir changes the working directory for the duration of t, restoring
// the original cwd in t.Cleanup.
func chdir(t *testing.T, dir string) {
	t.Helper()
	prev, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir %q: %v", dir, err)
	}
	t.Cleanup(func() { _ = os.Chdir(prev) })
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
	if err := os.WriteFile(paper, []byte("\\documentclass{amsart}\n\\begin{document}\n\\section{Intro}\nhi\n\\end{document}\n"), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	captured := withStubTUI(t)

	var stdout, stderr bytes.Buffer
	code := run([]string{"--no-build", paper}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("expected exit 0, got %d (stderr=%q)", code, stderr.String())
	}
	if *captured == nil {
		t.Fatalf("expected runTUI to be invoked")
	}
}

func TestRun_ProseOnlyPaperLaunchesTUI(t *testing.T) {
	// Pure prose (e.g. an opinion letter) should still open: the parser
	// segments the body into paragraph blocks so the TUI has something to
	// attach annotations to.
	dir := t.TempDir()
	paper := filepath.Join(dir, "paper.tex")
	if err := os.WriteFile(paper, []byte("\\documentclass{amsart}\n\\begin{document}\nFirst paragraph.\n\nSecond paragraph.\n\\end{document}\n"), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	captured := withStubTUI(t)

	var stdout, stderr bytes.Buffer
	code := run([]string{"--no-build", paper}, &stdout, &stderr)
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

func TestRun_TypoSubcommandSuggestsFix(t *testing.T) {
	cases := []struct {
		typo string
		want string
	}{
		{"ftm", "fmt"},
		{"fnt", "fmt"},
		{"confg", "config"},
		{"conifg", "config"},
	}
	for _, tc := range cases {
		var stdout, stderr bytes.Buffer
		code := run([]string{tc.typo}, &stdout, &stderr)
		if code != 2 {
			t.Errorf("typo %q: expected exit 2, got %d (stderr=%q)", tc.typo, code, stderr.String())
			continue
		}
		out := stderr.String()
		if !strings.Contains(out, "unknown subcommand") || !strings.Contains(out, tc.want) {
			t.Errorf("typo %q: stderr=%q; want hint mentioning %q", tc.typo, out, tc.want)
		}
	}
}

func TestRun_NonSubcommandTypoFallsThrough(t *testing.T) {
	// A first arg that isn't close to any known subcommand must NOT be
	// reported as a typo — it should fall through to the normal missing-file
	// path so users with arbitrary positional args still get a useful error.
	var stdout, stderr bytes.Buffer
	code := run([]string{"completely-unrelated-token"}, &stdout, &stderr)
	if code == 0 {
		t.Fatalf("expected non-zero exit for unknown token")
	}
	if strings.Contains(stderr.String(), "unknown subcommand") {
		t.Fatalf("did not expect typo hint for distant token; stderr=%q", stderr.String())
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
	code := run([]string{"--no-build", "--stdout=md", paper}, &stdout, &stderr)
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
	code := run([]string{"--no-build", "--stdout=json", paper}, &stdout, &stderr)
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
	code := run([]string{"--no-build", "--stdout=none", paper}, &stdout, &stderr)
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
	code := run([]string{"--no-build", "--stdout=md", paper}, &stdout, &stderr)
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
	code := run([]string{"--no-build", "--stdout=none", paper}, &stdout, &stderr)
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

func TestRun_ConfigFlagLoaded(t *testing.T) {
	paper := writeFixturePaper(t)
	dir := filepath.Dir(paper)
	configPath := filepath.Join(dir, "mreview.toml")
	if err := os.WriteFile(configPath, []byte("theme = \"light\"\nbuild_cmd = \"pdflatex\"\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	captured := withStubTUI(t)
	var stdout, stderr bytes.Buffer
	code := run([]string{"--no-build", "--config", configPath, "--stdout=none", paper}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit %d stderr=%q", code, stderr.String())
	}
	m, ok := (*captured).(ui.Model)
	if !ok {
		t.Fatalf("unexpected model type %T", *captured)
	}
	if m.Config == nil {
		t.Fatalf("expected model.Config to be set")
	}
	if m.Config.Theme != "light" {
		t.Fatalf("expected light theme, got %q", m.Config.Theme)
	}
	if m.Config.BuildCmd != "pdflatex" {
		t.Fatalf("expected BuildCmd=pdflatex, got %q", m.Config.BuildCmd)
	}
}

func TestRun_ConfigFlagMissingFileIsError(t *testing.T) {
	paper := writeFixturePaper(t)
	withStubTUI(t)
	var stdout, stderr bytes.Buffer
	code := run([]string{"--config", filepath.Join(t.TempDir(), "nope.toml"), paper}, &stdout, &stderr)
	if code == 0 {
		t.Fatalf("expected non-zero exit on missing explicit config")
	}
}

func TestRun_ThemeEnvApplied(t *testing.T) {
	paper := writeFixturePaper(t)
	t.Setenv("MREVIEW_THEME", "light")
	captured := withStubTUI(t)
	var stdout, stderr bytes.Buffer
	code := run([]string{"--no-build", "--stdout=none", paper}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit %d stderr=%q", code, stderr.String())
	}
	m, ok := (*captured).(ui.Model)
	if !ok {
		t.Fatalf("unexpected model type %T", *captured)
	}
	if m.Config.Theme != "light" {
		t.Fatalf("expected env-derived theme=light, got %q", m.Config.Theme)
	}
}

func TestRun_VersionString(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"--version"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit %d stderr=%q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "0.1.0") {
		t.Fatalf("expected version 0.1.0 on stdout, got %q", stdout.String())
	}
}

// TestRun_AuxAndBBLLoaded verifies that --no-build still loads the .aux and
// .bbl files sitting next to paper.tex and enriches the document with block
// numbers and resolved cite refs.
func TestRun_AuxAndBBLLoaded(t *testing.T) {
	paper := writeFixturePaper(t)
	dir := filepath.Dir(paper)
	// writeFixturePaper produces paper.tex; siblings must share the stem.
	stem := strings.TrimSuffix(filepath.Base(paper), ".tex")
	auxPath := filepath.Join(dir, stem+".aux")
	bblPath := filepath.Join(dir, stem+".bbl")
	auxContent := `\newlabel{thm:main}{{3.2}{5}}` + "\n"
	bblContent := "\\begin{thebibliography}{1}\n\\bibitem{smith2020} Smith, Paper.\n\\end{thebibliography}\n"
	if err := os.WriteFile(auxPath, []byte(auxContent), 0o600); err != nil {
		t.Fatalf("write aux: %v", err)
	}
	if err := os.WriteFile(bblPath, []byte(bblContent), 0o600); err != nil {
		t.Fatalf("write bbl: %v", err)
	}

	captured := withStubTUI(t)
	var stdout, stderr bytes.Buffer
	code := run([]string{"--no-build", "--stdout=none", paper}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit %d stderr=%q", code, stderr.String())
	}
	m, ok := (*captured).(ui.Model)
	if !ok {
		t.Fatalf("unexpected model type %T", *captured)
	}
	thm := m.Doc.ByLabel["thm:main"]
	if thm == nil {
		t.Fatalf("expected block labelled thm:main")
	}
	if thm.Number != "3.2" {
		t.Fatalf("expected Number=3.2 after aux load, got %q", thm.Number)
	}
	if m.Doc.BibEntries["smith2020"] == nil {
		t.Fatalf("expected bib entry for smith2020 after bbl load")
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

// TestStartupArtefactsStale covers the four mtime cases the --draft
// startup decision depends on. The function returns true (suppress
// rendering) when the .tex is newer than either artefact OR when both
// artefacts are missing; false only when artefacts exist and are at
// least as new as the .tex.
func TestStartupArtefactsStale(t *testing.T) {
	dir := t.TempDir()
	tex := filepath.Join(dir, "paper.tex")
	pdf := filepath.Join(dir, "paper.pdf")
	syn := filepath.Join(dir, "paper.synctex.gz")

	if err := os.WriteFile(tex, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	texMtime := time.Now()

	t.Run("artefacts-missing reports stale", func(t *testing.T) {
		if !startupArtefactsStale(tex, pdf, syn) {
			t.Errorf("missing artefacts should report stale")
		}
	})

	// Create artefacts NEWER than tex — should be coherent.
	if err := os.WriteFile(pdf, []byte("p"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(syn, []byte("s"), 0o600); err != nil {
		t.Fatal(err)
	}
	newer := texMtime.Add(2 * time.Second)
	if err := os.Chtimes(pdf, newer, newer); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(syn, newer, newer); err != nil {
		t.Fatal(err)
	}
	t.Run("artefacts newer than tex are coherent", func(t *testing.T) {
		if startupArtefactsStale(tex, pdf, syn) {
			t.Errorf("newer artefacts should not report stale")
		}
	})

	// Bump tex mtime so it's now newer than the PDF — should report stale.
	older := newer.Add(-3 * time.Second)
	if err := os.Chtimes(pdf, older, older); err != nil {
		t.Fatal(err)
	}
	t.Run("tex newer than PDF reports stale", func(t *testing.T) {
		if !startupArtefactsStale(tex, pdf, syn) {
			t.Errorf("PDF older than tex should report stale")
		}
	})

	// Restore PDF, age the synctex instead.
	if err := os.Chtimes(pdf, newer, newer); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(syn, older, older); err != nil {
		t.Fatal(err)
	}
	t.Run("tex newer than synctex reports stale", func(t *testing.T) {
		if !startupArtefactsStale(tex, pdf, syn) {
			t.Errorf("synctex older than tex should report stale")
		}
	})

	t.Run("missing tex reports stale", func(t *testing.T) {
		if !startupArtefactsStale(filepath.Join(dir, "nope.tex"), pdf, syn) {
			t.Errorf("missing tex should report stale (defensive)")
		}
	})
}
