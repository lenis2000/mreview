package ui

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"mreview/pkg/persist"
)

// TestBuildAnnotListItems_IncludesDetached asserts that sidecar.Detached
// entries reach the `@` popup. Before the fix, the popup said
// "no annotations" even when the startup status bar advertised N
// detached — user had no in-app path to clean them up.
func TestBuildAnnotListItems_IncludesDetached(t *testing.T) {
	doc := parsedSample(t)
	require.NotEmpty(t, doc.Blocks)
	live := doc.Blocks[1].ID
	side := &persist.Sidecar{
		Annotations: []persist.Annotation{{BlockID: live, Note: "live"}},
		Detached: []persist.Annotation{
			{BlockID: "ghost-1", Breadcrumb: "Gone", Note: "orphan 1"},
			{BlockID: "ghost-2", Breadcrumb: "Also gone", Note: "orphan 2"},
		},
	}
	items := BuildAnnotListItems(doc, side)
	require.Len(t, items, 3)
	// Live one first, detached at the end.
	assert.False(t, items[0].Detached)
	assert.True(t, items[1].Detached)
	assert.True(t, items[2].Detached)
}

// TestBuildAnnotListItems_ReturnsNonNilForDetachedOnly guards the
// previously-buggy early-return that short-circuited when Annotations
// was empty, hiding detached entries entirely.
func TestBuildAnnotListItems_ReturnsNonNilForDetachedOnly(t *testing.T) {
	doc := parsedSample(t)
	side := &persist.Sidecar{
		Detached: []persist.Annotation{{BlockID: "ghost", Note: "orphan"}},
	}
	items := BuildAnnotListItems(doc, side)
	require.Len(t, items, 1)
	assert.True(t, items[0].Detached)
}

// TestAnnotListItem_Display_PrefixesDetached confirms the row text
// carries a "(detached)" marker so the user can distinguish live
// notes from orphans at a glance.
func TestAnnotListItem_Display_PrefixesDetached(t *testing.T) {
	live := AnnotListItem{Breadcrumb: "Theorem 1", FirstLine: "needs ref"}
	orphan := AnnotListItem{Breadcrumb: "Theorem 1", FirstLine: "needs ref", Detached: true}
	assert.Equal(t, "Theorem 1 — needs ref", live.Display())
	assert.Equal(t, "(detached) Theorem 1 — needs ref", orphan.Display())
}

// TestRemoveDetachedAnnotation_DropsFirstMatch confirms the helper
// only removes one entry per call (so duplicates can be pruned one
// by one) and matches on both blockID and lineOffset so a
// block-level detached annotation isn't removed by a `d` on the
// line-pinned one.
func TestRemoveDetachedAnnotation_DropsFirstMatch(t *testing.T) {
	xs := []persist.Annotation{
		{BlockID: "a", LineOffset: 0, Note: "block-level"},
		{BlockID: "a", LineOffset: 3, Note: "line-pinned"},
		{BlockID: "b", LineOffset: 0, Note: "other block"},
	}
	out := removeDetachedAnnotation(xs, "a", 0)
	require.Len(t, out, 2)
	assert.Equal(t, "line-pinned", out[0].Note)
	assert.Equal(t, "other block", out[1].Note)
}

func TestRemoveDetachedAnnotation_NoMatchReturnsOriginal(t *testing.T) {
	xs := []persist.Annotation{{BlockID: "a", LineOffset: 0}}
	out := removeDetachedAnnotation(xs, "nope", 0)
	assert.Equal(t, xs, out)
}
