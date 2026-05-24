package diffui

import (
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
