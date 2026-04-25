package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// writeFmtFixture writes a .tex file with trailing whitespace so the
// Tier-1 space.trailing rule has something to rewrite.
func writeFmtFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	paper := filepath.Join(dir, "paper.tex")
	// Trailing spaces on "hi  " line trigger space.trailing.
	body := "\\documentclass{amsart}\n\\begin{document}\nhi  \n\\end{document}\n"
	if err := os.WriteFile(paper, []byte(body), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	return paper
}

// writeFmtFixtureClean writes a .tex file that requires no formatting changes.
func writeFmtFixtureClean(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	paper := filepath.Join(dir, "paper.tex")
	body := "\\documentclass{amsart}\n\\begin{document}\nhi\n\\end{document}\n"
	if err := os.WriteFile(paper, []byte(body), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	return paper
}

func TestFmt_MissingArg(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"fmt"}, &stdout, &stderr)
	if code != 2 {
		t.Fatalf("expected exit 2, got %d (stderr=%q)", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "missing paper") {
		t.Fatalf("expected missing-paper error, got %q", stderr.String())
	}
}

func TestFmt_ExtraArg(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"fmt", "a.tex", "b.tex"}, &stdout, &stderr)
	if code != 2 {
		t.Fatalf("expected exit 2, got %d", code)
	}
	if !strings.Contains(stderr.String(), "unexpected extra argument") {
		t.Fatalf("expected extra-arg error, got %q", stderr.String())
	}
}

func TestFmt_MissingFile(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"fmt", "/nonexistent/paper.tex"}, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("expected exit 1, got %d", code)
	}
	if !strings.Contains(stderr.String(), "cannot read") {
		t.Fatalf("expected cannot-read error, got %q", stderr.String())
	}
}

func TestFmt_HelpExitsZero(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"fmt", "--help"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("expected exit 0 for --help, got %d (stderr=%q)", code, stderr.String())
	}
	out := stdout.String()
	if !strings.Contains(out, "--diff") {
		t.Fatalf("expected help to mention --diff, got %q", out)
	}
}

func TestFmt_UnknownFlag(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"fmt", "--bad-flag"}, &stdout, &stderr)
	if code != 2 {
		t.Fatalf("expected exit 2, got %d", code)
	}
}

func TestFmt_Diff(t *testing.T) {
	paper := writeFmtFixture(t)

	var stdout, stderr bytes.Buffer
	code := run([]string{"fmt", "--diff", paper}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("expected exit 0, got %d (stderr=%q)", code, stderr.String())
	}

	diff := stdout.String()
	if !strings.Contains(diff, "---") {
		t.Fatalf("expected unified diff header, got %q", diff)
	}
	if !strings.Contains(diff, "+++") {
		t.Fatalf("expected unified diff header, got %q", diff)
	}
	// The diff should show the trailing-space removal.
	if !strings.Contains(diff, "-hi  ") {
		t.Fatalf("expected removed trailing-space line in diff, got %q", diff)
	}
	if !strings.Contains(diff, "+hi") {
		t.Fatalf("expected clean line in diff, got %q", diff)
	}

	// File should NOT be modified (--diff is read-only).
	content, _ := os.ReadFile(paper)
	if !strings.Contains(string(content), "hi  ") {
		t.Fatalf("--diff should not modify file")
	}
}

func TestFmt_DiffNoChanges(t *testing.T) {
	paper := writeFmtFixtureClean(t)

	var stdout, stderr bytes.Buffer
	code := run([]string{"fmt", "--diff", paper}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("expected exit 0, got %d", code)
	}
	if !strings.Contains(stderr.String(), "no changes") {
		t.Fatalf("expected no-changes message, got %q", stderr.String())
	}
}

func TestFmt_Check_ChangesNeeded(t *testing.T) {
	paper := writeFmtFixture(t)

	var stdout, stderr bytes.Buffer
	code := run([]string{"fmt", "--check", paper}, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("expected exit 1 when changes needed, got %d", code)
	}
}

func TestFmt_Check_NoChanges(t *testing.T) {
	paper := writeFmtFixtureClean(t)

	var stdout, stderr bytes.Buffer
	code := run([]string{"fmt", "--check", paper}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("expected exit 0 when no changes needed, got %d", code)
	}
}

func TestFmt_WriteAllowDirty(t *testing.T) {
	paper := writeFmtFixture(t)

	var stdout, stderr bytes.Buffer
	// File is in a temp dir (not a git repo), so dirty check would error.
	// --allow-dirty skips the check.
	code := run([]string{"fmt", "--allow-dirty", "--no-verify", paper}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("expected exit 0, got %d (stderr=%q)", code, stderr.String())
	}

	// File should be modified: trailing whitespace removed.
	content, _ := os.ReadFile(paper)
	if strings.Contains(string(content), "hi  ") {
		t.Fatalf("expected trailing whitespace to be removed")
	}
	if !strings.Contains(string(content), "hi\n") {
		t.Fatalf("expected clean 'hi' line, got %q", string(content))
	}
}

func TestFmt_WriteNoChanges(t *testing.T) {
	paper := writeFmtFixtureClean(t)

	var stdout, stderr bytes.Buffer
	code := run([]string{"fmt", "--allow-dirty", paper}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("expected exit 0, got %d", code)
	}
	if !strings.Contains(stderr.String(), "no changes") {
		t.Fatalf("expected no-changes message, got %q", stderr.String())
	}
}

func TestFmt_DirtyTreeRefused(t *testing.T) {
	// Set up a git repo with a dirty file.
	dir := t.TempDir()
	paper := filepath.Join(dir, "paper.tex")
	body := "\\documentclass{amsart}\n\\begin{document}\nhi  \n\\end{document}\n"
	if err := os.WriteFile(paper, []byte(body), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	// Initialize a git repo, add and commit the file, then modify it.
	mustGit(t, dir, "init")
	mustGit(t, dir, "config", "user.email", "test@test.com")
	mustGit(t, dir, "config", "user.name", "Test")
	mustGit(t, dir, "add", "paper.tex")
	mustGit(t, dir, "commit", "-m", "init")

	// Modify the file so git status shows it as dirty.
	if err := os.WriteFile(paper, []byte(body+"% extra\n"), 0o644); err != nil {
		t.Fatalf("re-write fixture: %v", err)
	}

	var stdout, stderr bytes.Buffer
	code := run([]string{"fmt", paper}, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("expected exit 1 for dirty tree, got %d (stderr=%q)", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "uncommitted changes") {
		t.Fatalf("expected dirty-tree refusal message, got %q", stderr.String())
	}
	if !strings.Contains(stderr.String(), "--allow-dirty") {
		t.Fatalf("expected hint about --allow-dirty, got %q", stderr.String())
	}
}

func TestFmt_CleanGitTreeWritesOK(t *testing.T) {
	// Set up a git repo with a committed (clean) file that needs formatting.
	dir := t.TempDir()
	paper := filepath.Join(dir, "paper.tex")
	body := "\\documentclass{amsart}\n\\begin{document}\nhi  \n\\end{document}\n"
	if err := os.WriteFile(paper, []byte(body), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	mustGit(t, dir, "init")
	mustGit(t, dir, "config", "user.email", "test@test.com")
	mustGit(t, dir, "config", "user.name", "Test")
	mustGit(t, dir, "add", "paper.tex")
	mustGit(t, dir, "commit", "-m", "init")

	var stdout, stderr bytes.Buffer
	code := run([]string{"fmt", "--no-verify", paper}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("expected exit 0, got %d (stderr=%q)", code, stderr.String())
	}
	// File should be modified.
	content, _ := os.ReadFile(paper)
	if strings.Contains(string(content), "hi  ") {
		t.Fatalf("expected trailing whitespace to be removed")
	}
}

func TestFmt_RuleFilter(t *testing.T) {
	dir := t.TempDir()
	paper := filepath.Join(dir, "paper.tex")
	// Input with both trailing whitespace AND tabs.
	body := "\\documentclass{amsart}\n\\begin{document}\n\thi  \n\\end{document}\n"
	if err := os.WriteFile(paper, []byte(body), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	var stdout, stderr bytes.Buffer
	// Only run space.trailing — tabs should survive.
	code := run([]string{"fmt", "--diff", "--rule=space.trailing", paper}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("expected exit 0, got %d (stderr=%q)", code, stderr.String())
	}

	diff := stdout.String()
	// Trailing whitespace should be in the diff.
	if !strings.Contains(diff, "-\thi  ") {
		t.Fatalf("expected trailing-space removal in diff, got %q", diff)
	}
	// But the tab should remain (not replaced with spaces).
	if !strings.Contains(diff, "+\thi") {
		t.Fatalf("expected tab to remain when only space.trailing is selected, got %q", diff)
	}
}

func TestFmt_WriteSummary(t *testing.T) {
	paper := writeFmtFixture(t)

	var stdout, stderr bytes.Buffer
	code := run([]string{"fmt", "--allow-dirty", "--no-verify", paper}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("expected exit 0, got %d (stderr=%q)", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "wrote") {
		t.Fatalf("expected write summary on stderr, got %q", stderr.String())
	}
	if !strings.Contains(stderr.String(), "rewrite") {
		t.Fatalf("expected rewrite count in summary, got %q", stderr.String())
	}
}

func TestFmt_NonGitDirWarnAndProceed(t *testing.T) {
	// In a non-git temp dir, without --allow-dirty, should warn but still write.
	paper := writeFmtFixture(t)

	var stdout, stderr bytes.Buffer
	code := run([]string{"fmt", "--no-verify", paper}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("expected exit 0 (non-git dir should warn, not refuse), got %d (stderr=%q)", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "cannot check git status") {
		t.Fatalf("expected git-status warning, got %q", stderr.String())
	}
	// File should be modified.
	content, _ := os.ReadFile(paper)
	if strings.Contains(string(content), "hi  ") {
		t.Fatalf("expected trailing whitespace to be removed")
	}
}

// mustGit runs a git command in dir, failing the test on error.
func mustGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}
