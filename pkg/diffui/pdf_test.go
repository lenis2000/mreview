package diffui

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mreview/pkg/diffreview"
)

func TestPrepareNewPDFUsesNewFilesystemEndpoint(t *testing.T) {
	dir := t.TempDir()
	oldDir := filepath.Join(dir, "old")
	newDir := filepath.Join(dir, "new")
	if err := os.MkdirAll(oldDir, 0o755); err != nil {
		t.Fatalf("mkdir old: %v", err)
	}
	if err := os.MkdirAll(newDir, 0o755); err != nil {
		t.Fatalf("mkdir new: %v", err)
	}
	oldPath := filepath.Join(oldDir, "paper.tex")
	newPath := filepath.Join(newDir, "paper.tex")
	if err := os.WriteFile(oldPath, []byte("\\bye\n"), 0o600); err != nil {
		t.Fatalf("write old: %v", err)
	}
	if err := os.WriteFile(newPath, []byte("\\bye\n"), 0o600); err != nil {
		t.Fatalf("write new: %v", err)
	}
	marker := filepath.Join(dir, "built.txt")
	t.Setenv("MARKER", marker)
	review := &diffreview.Review{
		Old: diffreview.Endpoint{Kind: diffreview.WorkingFile, Path: oldPath},
		New: diffreview.Endpoint{Kind: diffreview.WorkingFile, Path: newPath, Editable: true},
	}

	_, err := PrepareNewPDF(review, PDFOptions{
		BuildCmd: `printf '%s' "$PWD/$MREVIEW_BASENAME" > "$MARKER"`,
	})
	if err != nil {
		t.Fatalf("prepare pdf: %v", err)
	}
	data, err := os.ReadFile(marker)
	if err != nil {
		t.Fatalf("read marker: %v", err)
	}
	want := filepath.Join(newDir, "paper")
	if got := string(data); got != want {
		t.Fatalf("build ran for %q, want new endpoint %q", got, want)
	}
}

func TestPrepareNewPDFNoBuildSkipsBuildCommand(t *testing.T) {
	review, _, newPath := pdfReviewFixture(t)
	marker := filepath.Join(t.TempDir(), "built.txt")
	t.Setenv("MARKER", marker)

	artifacts, err := PrepareNewPDF(review, PDFOptions{
		NoBuild:  true,
		BuildCmd: `printf built > "$MARKER"`,
	})
	if err != nil {
		t.Fatalf("prepare pdf: %v", err)
	}
	if artifacts == nil {
		t.Fatalf("expected artifacts")
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("build command should not have run for %s", newPath)
	}
}

func TestPrepareNewPDFBuildFailureHonorsDraft(t *testing.T) {
	review, _, _ := pdfReviewFixture(t)
	var stderr bytes.Buffer

	_, err := PrepareNewPDF(review, PDFOptions{BuildCmd: "false", Stderr: &stderr})
	if err == nil {
		t.Fatalf("expected non-draft build failure")
	}

	artifacts, err := PrepareNewPDF(review, PDFOptions{BuildCmd: "false", Draft: true})
	if err != nil {
		t.Fatalf("draft build failure should not abort: %v", err)
	}
	if artifacts == nil || !artifacts.BuildStale || !strings.Contains(artifacts.Status, "build:") {
		t.Fatalf("draft artifacts = %#v, want stale build warning", artifacts)
	}
}

func TestPrepareNewPDFSkipsBuildWhenLmkfIsWatching(t *testing.T) {
	review, _, newPath := pdfReviewFixture(t)
	statusFile := writeLmkfStatus(t, newPath)
	t.Cleanup(func() { _ = os.Remove(statusFile) })
	marker := filepath.Join(t.TempDir(), "built.txt")
	t.Setenv("MARKER", marker)

	artifacts, err := PrepareNewPDF(review, PDFOptions{BuildCmd: `printf built > "$MARKER"`})
	if err != nil {
		t.Fatalf("prepare pdf: %v", err)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("build command should not run while lmkf is active")
	}
	if artifacts == nil || !strings.Contains(artifacts.Status, "lmkf is building") {
		t.Fatalf("lmkf status missing from artifacts: %#v", artifacts)
	}
}

func TestNewEndpointBuildPathRejectsGitBlob(t *testing.T) {
	review := &diffreview.Review{
		New: diffreview.Endpoint{Kind: diffreview.GitBlob, Path: "/tmp/materialized.tex"},
	}
	if got, ok := newEndpointBuildPath(review); ok || got != "" {
		t.Fatalf("git blob build path = %q, %v; want no build path", got, ok)
	}
}

func TestPDFPaneDeletedPairShowsPlaceholder(t *testing.T) {
	review := fixtureReview()
	m := New(review, Options{KittyAvailable: true})
	m.Cursor = pairIndexByID(review, "deleted")
	m.PDFImage = "stale-image"

	body := m.pdfPaneBody()
	if !strings.Contains(body, deletedPDFPlaceholder) {
		t.Fatalf("deleted PDF placeholder missing from %q", body)
	}
	if strings.Contains(body, "stale-image") {
		t.Fatalf("deleted pair should clear stale image, got %q", body)
	}
}

func pdfReviewFixture(t *testing.T) (*diffreview.Review, string, string) {
	t.Helper()
	dir := t.TempDir()
	oldPath := filepath.Join(dir, "old", "paper.tex")
	newPath := filepath.Join(dir, "new", "paper.tex")
	if err := os.MkdirAll(filepath.Dir(oldPath), 0o755); err != nil {
		t.Fatalf("mkdir old: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(newPath), 0o755); err != nil {
		t.Fatalf("mkdir new: %v", err)
	}
	if err := os.WriteFile(oldPath, []byte("\\bye\n"), 0o600); err != nil {
		t.Fatalf("write old: %v", err)
	}
	if err := os.WriteFile(newPath, []byte("\\bye\n"), 0o600); err != nil {
		t.Fatalf("write new: %v", err)
	}
	return &diffreview.Review{
		Old: diffreview.Endpoint{Kind: diffreview.WorkingFile, Path: oldPath},
		New: diffreview.Endpoint{Kind: diffreview.WorkingFile, Path: newPath, Editable: true},
	}, oldPath, newPath
}

func writeLmkfStatus(t *testing.T, texPath string) string {
	t.Helper()
	abs, err := filepath.Abs(texPath)
	if err != nil {
		t.Fatalf("abs tex path: %v", err)
	}
	statusDir := filepath.Join(os.TempDir(), "lmkf-status")
	if err := os.MkdirAll(statusDir, 0o755); err != nil {
		t.Fatalf("mkdir lmkf status: %v", err)
	}
	statusFile := filepath.Join(statusDir, filepath.Base(filepath.Dir(abs)))
	logPath := strings.TrimSuffix(abs, filepath.Ext(abs)) + ".log"
	if err := os.WriteFile(statusFile, []byte(logPath), 0o600); err != nil {
		t.Fatalf("write lmkf status: %v", err)
	}
	return statusFile
}
