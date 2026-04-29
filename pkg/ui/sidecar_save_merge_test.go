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

// TestSaveSidecar_MtimeGuard_MergesAgentDeletes proves the end-to-end
// "agent edits the sidecar while user keeps reviewing" workflow:
//
//  1. The user opens mreview with two annotations (a, b) loaded.
//  2. An agent process rewrites the sidecar on disk, dropping b.
//  3. The user adds a new annotation c in the live session and saves.
//
// Without the mtime guard, step 3 would clobber the agent's deletion of
// b — the saved file would contain (a, b, c). With the guard, disk's
// view (post-deletion) is taken as the base and only the user's delta
// (the addition of c) is applied on top. Saved file: (a, c).
func TestSaveSidecar_MtimeGuard_MergesAgentDeletes(t *testing.T) {
	doc := annotDoc(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "side.mreview.md")

	// Two annotations on real blocks so persist.Remap reattaches them
	// instead of demoting either to Detached during a reload.
	idA := firstBlockOfKind(doc, parser.KindTheoremLike)
	idB := secondBlockOfKind(doc, parser.KindTheoremLike)
	require.NotEmpty(t, idA)
	require.NotEmpty(t, idB)

	initial := &persist.Sidecar{
		Annotations: []persist.Annotation{
			fakeAnnotForBlock(doc, idA, "user note A"),
			fakeAnnotForBlock(doc, idB, "user note B"),
		},
	}
	require.NoError(t, persist.Save(path, initial))

	// Construct a Model in the same configuration main.go uses on
	// startup: load → remap → seed SidecarMtime + SidecarBase.
	loaded, err := persist.Load(path)
	require.NoError(t, err)
	side, _ := persist.Remap(loaded, doc)
	RefreshRemappedAnnotations(doc, side)

	m := New(doc, side)
	m.SaveFn = nil
	m.SidecarPath = path
	info, err := os.Stat(path)
	require.NoError(t, err)
	m.SidecarMtime = info.ModTime()
	m.SidecarBase = SnapshotSidecar(side)

	// Agent rewrites the sidecar with b dropped. Bump mtime explicitly so
	// the test isn't at the mercy of filesystem timestamp granularity.
	external := &persist.Sidecar{
		Annotations: []persist.Annotation{fakeAnnotForBlock(doc, idA, "user note A")},
	}
	require.NoError(t, persist.Save(path, external))
	future := info.ModTime().Add(2 * time.Second)
	require.NoError(t, os.Chtimes(path, future, future))

	// User adds annotation c in the live session.
	idC := firstBlockOfKind(doc, parser.KindProofStep)
	require.NotEmpty(t, idC)
	m.Sidecar.Annotations = upsertAnnotation(m.Sidecar.Annotations,
		fakeAnnotForBlock(doc, idC, "user note C"))

	require.NoError(t, m.saveSidecar())

	got, err := persist.Load(path)
	require.NoError(t, err)

	gotIDs := blockIDSet(got.Annotations)
	assert.Contains(t, gotIDs, idA, "a survives — agent didn't touch")
	assert.Contains(t, gotIDs, idC, "c survives — user added during agent run")
	assert.NotContains(t, gotIDs, idB, "b stays deleted — agent's edit not clobbered")
	assert.Contains(t, m.Status, "merged", "status surfaces the merge: %q", m.Status)
}

// TestSaveSidecar_NoExternalChange_PlainSave verifies the fast path:
// when the sidecar mtime hasn't advanced, saveSidecar does a plain
// write without invoking the merge.
func TestSaveSidecar_NoExternalChange_PlainSave(t *testing.T) {
	doc := annotDoc(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "side.mreview.md")

	idA := firstBlockOfKind(doc, parser.KindTheoremLike)
	require.NotEmpty(t, idA)

	side := &persist.Sidecar{
		Annotations: []persist.Annotation{fakeAnnotForBlock(doc, idA, "n1")},
	}
	require.NoError(t, persist.Save(path, side))

	m := New(doc, side)
	m.SaveFn = nil
	m.SidecarPath = path
	info, err := os.Stat(path)
	require.NoError(t, err)
	m.SidecarMtime = info.ModTime()
	m.SidecarBase = SnapshotSidecar(side)

	// No external edit; user adds an annotation on a different block.
	idB := secondBlockOfKind(doc, parser.KindTheoremLike)
	require.NotEmpty(t, idB)
	m.Sidecar.Annotations = upsertAnnotation(m.Sidecar.Annotations,
		fakeAnnotForBlock(doc, idB, "n2"))

	require.NoError(t, m.saveSidecar())

	got, err := persist.Load(path)
	require.NoError(t, err)
	require.Len(t, got.Annotations, 2)
	assert.NotContains(t, m.Status, "merged",
		"plain save shouldn't surface a merge: %q", m.Status)
}

func secondBlockOfKind(doc *parser.Document, k parser.Kind) string {
	seen := false
	for _, b := range doc.Blocks {
		if b != nil && b.Kind == k {
			if seen {
				return b.ID
			}
			seen = true
		}
	}
	return ""
}

func fakeAnnotForBlock(doc *parser.Document, id, note string) persist.Annotation {
	b := doc.ByID[id]
	if b == nil {
		return persist.Annotation{BlockID: id, Note: note}
	}
	file := b.File
	if file == "" {
		file = doc.File
	}
	if file == "" {
		file = "-"
	}
	return persist.Annotation{
		BlockID:    id,
		Breadcrumb: id,
		File:       file,
		StartLine:  b.StartLine,
		EndLine:    b.EndLine,
		Note:       note,
	}
}

func blockIDSet(xs []persist.Annotation) []string {
	out := make([]string, 0, len(xs))
	for _, a := range xs {
		out = append(out, a.BlockID)
	}
	return out
}
