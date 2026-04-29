package ui

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"mreview/pkg/parser"
	"mreview/pkg/persist"
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

// modelForSidecarPoll builds a Model with a real on-disk sidecar that
// already contains two annotations (idA, idB), and seeds SidecarPath /
// SidecarMtime / SidecarBase the way main.go does on startup. Returns
// the model, the sidecar path, the two annotation block IDs, and the
// initial mtime so tests can stage external edits relative to it.
func modelForSidecarPoll(t *testing.T) (m Model, path, idA, idB string, initialMtime time.Time) {
	t.Helper()
	doc := annotDoc(t)
	dir := t.TempDir()
	path = filepath.Join(dir, "side.mreview.md")

	idA = firstBlockOfKind(doc, parser.KindTheoremLike)
	idB = secondBlockOfKind(doc, parser.KindTheoremLike)
	require.NotEmpty(t, idA)
	require.NotEmpty(t, idB)

	initial := &persist.Sidecar{
		Annotations: []persist.Annotation{
			fakeAnnotForBlock(doc, idA, "note A"),
			fakeAnnotForBlock(doc, idB, "note B"),
		},
	}
	require.NoError(t, persist.Save(path, initial))

	loaded, err := persist.Load(path)
	require.NoError(t, err)
	side, _ := persist.Remap(loaded, doc)
	RefreshRemappedAnnotations(doc, side)

	m = New(doc, side)
	m.SaveFn = nil
	m.SidecarPath = path
	info, err := os.Stat(path)
	require.NoError(t, err)
	m.SidecarMtime = info.ModTime()
	m.SidecarBase = SnapshotSidecar(side)
	return m, path, idA, idB, info.ModTime()
}

// TestPollSidecar_AgentDeletes_MergedIntoMemory proves the watch-tick
// equivalent of the saveSidecar mtime guard: when an external editor
// (an agent) drops an annotation while mreview is running, the next
// pollSidecar tick reloads disk, runs the 3-way merge, and updates
// m.Sidecar in place — no save event needed.
func TestPollSidecar_AgentDeletes_MergedIntoMemory(t *testing.T) {
	m, path, idA, idB, initialMtime := modelForSidecarPoll(t)

	// Agent rewrites the sidecar with b dropped, bumping mtime past
	// our snapshot.
	external := &persist.Sidecar{
		Annotations: []persist.Annotation{fakeAnnotForBlock(m.Doc, idA, "note A")},
	}
	require.NoError(t, persist.Save(path, external))
	future := initialMtime.Add(2 * time.Second)
	require.NoError(t, os.Chtimes(path, future, future))

	out := m.pollSidecar()

	gotIDs := blockIDSet(out.Sidecar.Annotations)
	assert.Contains(t, gotIDs, idA, "a survives — agent didn't drop it")
	assert.NotContains(t, gotIDs, idB, "b is gone — agent's deletion picked up by poll")
	assert.True(t, out.SidecarMtime.After(m.SidecarMtime),
		"baseline must advance after a successful merge")
	assert.Contains(t, out.Status, "auto-reload",
		"status should surface the auto-reload trigger, got %q", out.Status)
}

// TestPollSidecar_NoChange_NoOp covers the fast path: an unchanged
// sidecar mtime must leave Status, baseline, and Sidecar untouched.
func TestPollSidecar_NoChange_NoOp(t *testing.T) {
	m, _, _, _, _ := modelForSidecarPoll(t)
	statusBefore := m.Status
	mtimeBefore := m.SidecarMtime
	annosBefore := len(m.Sidecar.Annotations)

	out := m.pollSidecar()

	assert.Equal(t, statusBefore, out.Status, "no change → no status update")
	assert.Equal(t, mtimeBefore, out.SidecarMtime, "no change → no baseline advance")
	assert.Equal(t, annosBefore, len(out.Sidecar.Annotations))
}

// TestPollSidecar_PopupOpen_Suppresses mirrors the source-watch
// suppression rule: a mid-keystroke popup should never have its
// underlying state silently reloaded.
func TestPollSidecar_PopupOpen_Suppresses(t *testing.T) {
	m, path, idA, _, initialMtime := modelForSidecarPoll(t)
	external := &persist.Sidecar{
		Annotations: []persist.Annotation{fakeAnnotForBlock(m.Doc, idA, "note A")},
	}
	require.NoError(t, persist.Save(path, external))
	future := initialMtime.Add(2 * time.Second)
	require.NoError(t, os.Chtimes(path, future, future))
	m.Popup = &AnnotationPopup{}

	out := m.pollSidecar()

	assert.NotContains(t, out.Status, "auto-reload",
		"popup must suppress the trigger, got %q", out.Status)
	assert.Equal(t, m.SidecarMtime, out.SidecarMtime,
		"baseline must NOT advance while suppressed (so the next tick can fire)")
	assert.Equal(t, 2, len(out.Sidecar.Annotations),
		"suppressed merge must not mutate Sidecar")
}

// TestPollSidecar_FirstTick_ZeroMtime_NoOp verifies the same first-tick
// invariant as the source watcher: an empty SidecarMtime baseline means
// "we haven't synced yet", and the poll must not run a merge until
// startup / saveSidecar has seeded the baseline.
func TestPollSidecar_FirstTick_ZeroMtime_NoOp(t *testing.T) {
	m, path, idA, _, initialMtime := modelForSidecarPoll(t)
	external := &persist.Sidecar{
		Annotations: []persist.Annotation{fakeAnnotForBlock(m.Doc, idA, "note A")},
	}
	require.NoError(t, persist.Save(path, external))
	future := initialMtime.Add(2 * time.Second)
	require.NoError(t, os.Chtimes(path, future, future))
	m.SidecarMtime = time.Time{}

	out := m.pollSidecar()

	assert.True(t, out.SidecarMtime.IsZero(),
		"baseline stays zero until something explicitly seeds it")
	assert.Equal(t, 2, len(out.Sidecar.Annotations),
		"first tick must not mutate Sidecar")
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
