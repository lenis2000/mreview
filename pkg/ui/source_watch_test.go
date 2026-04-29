package ui

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"mreview/pkg/parser"
)

// writeTeXFile writes the sampleTeX content to a temp .tex file and
// returns its path. Used by source-watch tests that need a real on-disk
// source for stat() to inspect.
func writeTeXFile(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "paper.tex")
	require.NoError(t, os.WriteFile(path, []byte(sampleTeX), 0o644))
	return path
}

// modelForSourceWatch builds a Model whose Doc.File points at a real
// temp source. Returns the model and the source path so tests can
// stat/touch it.
func modelForSourceWatch(t *testing.T) (Model, string) {
	t.Helper()
	path := writeTeXFile(t)
	doc, err := parser.Parse([]byte(sampleTeX))
	require.NoError(t, err)
	doc.File = path
	for _, b := range doc.Blocks {
		if b != nil {
			b.File = path
		}
	}
	m := New(doc, nil)
	info, err := os.Stat(path)
	require.NoError(t, err)
	m.SourceMtime = info.ModTime()
	return m, path
}

func TestHandleSourceWatch_NoChange_DoesNotFire(t *testing.T) {
	m, _ := modelForSourceWatch(t)
	statusBefore := m.Status

	out, cmd := m.handleSourceWatch()

	assert.NotNil(t, cmd, "must reschedule next tick")
	assert.Equal(t, statusBefore, out.Status, "no change → no status update")
}

func TestHandleSourceWatch_FileAdvanced_TriggersReload(t *testing.T) {
	m, path := modelForSourceWatch(t)
	// Bump the source mtime past the seeded baseline. Use Chtimes
	// directly so the test isn't sensitive to filesystem timestamp
	// granularity.
	future := m.SourceMtime.Add(2 * time.Second)
	require.NoError(t, os.Chtimes(path, future, future))

	out, cmd := m.handleSourceWatch()

	assert.NotNil(t, cmd, "must reschedule next tick after firing")
	assert.True(t, out.SourceMtime.After(m.SourceMtime),
		"baseline must advance to the newly observed mtime")
	assert.Contains(t, out.Status, "auto-reload",
		"status should surface the auto-reload trigger, got %q", out.Status)
}

func TestHandleSourceWatch_FileAdvanced_PopupOpen_DoesNotFire(t *testing.T) {
	m, path := modelForSourceWatch(t)
	future := m.SourceMtime.Add(2 * time.Second)
	require.NoError(t, os.Chtimes(path, future, future))
	// Simulate an open annotation popup: the user is mid-typing.
	m.Popup = &AnnotationPopup{}

	out, cmd := m.handleSourceWatch()

	assert.NotNil(t, cmd, "tick keeps rescheduling so the change is picked up post-popup")
	assert.NotContains(t, out.Status, "auto-reload",
		"open popup must suppress the trigger, got status %q", out.Status)
	assert.Equal(t, m.SourceMtime, out.SourceMtime,
		"baseline must NOT advance while suppressed (so the next tick can still fire)")
}

func TestHandleSourceWatch_PendingDeleteSuppresses(t *testing.T) {
	m, path := modelForSourceWatch(t)
	future := m.SourceMtime.Add(2 * time.Second)
	require.NoError(t, os.Chtimes(path, future, future))
	m.Pending = &PendingDelete{}

	out, _ := m.handleSourceWatch()

	assert.NotContains(t, out.Status, "auto-reload",
		"y/N delete confirm must suppress the trigger, got %q", out.Status)
}

func TestHandleSourceWatch_DisabledByConfig(t *testing.T) {
	m, path := modelForSourceWatch(t)
	off := false
	m.Config.AutoReloadSource = &off
	future := m.SourceMtime.Add(2 * time.Second)
	require.NoError(t, os.Chtimes(path, future, future))

	out, cmd := m.handleSourceWatch()

	assert.Nil(t, cmd, "disabled watcher must not reschedule")
	assert.Equal(t, m.SourceMtime, out.SourceMtime,
		"disabled watcher must not advance the baseline either")
}

func TestHandleSourceWatch_FirstTickSeedsBaselineSilently(t *testing.T) {
	// modelForSourceWatch seeds SourceMtime, so to test the "first tick"
	// path we explicitly clear it.
	m, _ := modelForSourceWatch(t)
	m.SourceMtime = time.Time{}

	out, cmd := m.handleSourceWatch()

	assert.NotNil(t, cmd, "must reschedule")
	assert.False(t, out.SourceMtime.IsZero(),
		"first tick must seed the baseline from disk")
	assert.NotContains(t, out.Status, "auto-reload",
		"seeding must not trigger a reload, got %q", out.Status)
}

func TestSourceWatchPaths_DedupesAndIncludesAllBlockFiles(t *testing.T) {
	doc, err := parser.Parse([]byte(sampleTeX))
	require.NoError(t, err)
	doc.File = "main.tex"
	require.GreaterOrEqual(t, len(doc.Blocks), 2)
	doc.Blocks[0].File = "main.tex"
	doc.Blocks[1].File = "section1.tex"

	m := New(doc, nil)
	got := m.sourceWatchPaths()

	assert.Contains(t, got, "main.tex")
	assert.Contains(t, got, "section1.tex")
	// Dedup: main.tex should appear only once even though doc.File and
	// blocks[0].File both name it.
	count := 0
	for _, p := range got {
		if p == "main.tex" {
			count++
		}
	}
	assert.Equal(t, 1, count, "duplicate main.tex must dedup to one entry")
}
