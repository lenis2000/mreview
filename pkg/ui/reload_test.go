package ui

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"mreview/pkg/persist"
)

// TestResolveReloadCursor_KeepsLiveID asserts that a cursor that
// still resolves is kept as-is, even if later blocks would also
// pass firstUnreviewedOrAny.
func TestResolveReloadCursor_KeepsLiveID(t *testing.T) {
	doc := parsedSample(t)
	require.GreaterOrEqual(t, len(doc.Blocks), 3)
	cur := doc.Blocks[2].ID
	got := resolveReloadCursor(cur, doc, &persist.Sidecar{})
	assert.Equal(t, cur, got)
}

// TestResolveReloadCursor_LabelRescue asserts a label survives ID
// churn (same behaviour as persist.Remap's cursor rescue).
func TestResolveReloadCursor_LabelRescue(t *testing.T) {
	doc := parsedSample(t)
	b, ok := doc.ByLabel["thm:main"]
	require.True(t, ok, "fixture must expose thm:main")
	got := resolveReloadCursor("thm:main", doc, &persist.Sidecar{})
	assert.Equal(t, b.ID, got)
}

// TestResolveReloadCursor_FallsBackToFirstUnreviewed is the
// regression guard for the review2.md #3 fix: after an edit wipes
// the current block (no ID, no label), we must land on the first
// *unreviewed* block instead of bouncing to the top of the document.
func TestResolveReloadCursor_FallsBackToFirstUnreviewed(t *testing.T) {
	doc := parsedSample(t)
	// Mark every block up to (and excluding) doc.Blocks[3] as reviewed
	// so the naive firstContentBlockID fallback would land on one of
	// them. firstUnreviewedOrAny should skip past them.
	var reviewed []string
	for _, b := range doc.Blocks {
		if b == doc.Root {
			continue
		}
		reviewed = append(reviewed, b.ID)
		if len(reviewed) >= 3 {
			break
		}
	}
	side := &persist.Sidecar{Reviewed: reviewed}

	got := resolveReloadCursor("gone-block-id", doc, side)
	require.NotEmpty(t, got)
	for _, rid := range reviewed {
		assert.NotEqual(t, rid, got, "fallback must not pick an already-reviewed block")
	}
}

// TestResolveReloadCursor_AllReviewedStillReturnsABlock checks the
// degenerate case: when every block is reviewed, firstUnreviewedOrAny
// falls through to firstContentBlockID so the UI still has a valid
// cursor.
func TestResolveReloadCursor_AllReviewedStillReturnsABlock(t *testing.T) {
	doc := parsedSample(t)
	var all []string
	for _, b := range doc.Blocks {
		if b == doc.Root {
			continue
		}
		all = append(all, b.ID)
	}
	side := &persist.Sidecar{Reviewed: all}
	got := resolveReloadCursor("missing", doc, side)
	assert.Equal(t, firstContentBlockID(doc), got)
}

// TestResolveReloadCursor_NilDocReturnsEmpty guards the defensive
// nil-doc branch — performReload returns early before calling this
// helper on a nil document, but the helper should still not panic
// if it ever gets one.
func TestResolveReloadCursor_NilDocReturnsEmpty(t *testing.T) {
	got := resolveReloadCursor("any", nil, &persist.Sidecar{})
	assert.Empty(t, got)
}

// TestNew_FallsBackToFirstUnreviewed covers the startup-side
// counterpart of resolveReloadCursor: the sidecar cursor is missing
// and some early blocks are already reviewed, so ui.New must skip
// them and land on the first outstanding block.
func TestNew_FallsBackToFirstUnreviewed(t *testing.T) {
	doc := parsedSample(t)
	require.GreaterOrEqual(t, len(doc.Blocks), 4)
	// Mark the first two non-root blocks as reviewed.
	reviewed := []string{}
	for _, b := range doc.Blocks {
		if b == doc.Root {
			continue
		}
		reviewed = append(reviewed, b.ID)
		if len(reviewed) == 2 {
			break
		}
	}
	side := &persist.Sidecar{Reviewed: reviewed}
	m := New(doc, side)
	for _, rid := range reviewed {
		assert.NotEqual(t, rid, m.CursorBlockID, "New must skip reviewed blocks on fallback")
	}
	// Must still be a real block from the parsed doc.
	assert.NotNil(t, doc.ByID[m.CursorBlockID])
}

// TestNew_FallsBackToFirstContentWhenAllReviewed matches the
// degenerate branch: every block is reviewed, so the fallback ends
// up on the first content block rather than an empty string.
func TestNew_FallsBackToFirstContentWhenAllReviewed(t *testing.T) {
	doc := parsedSample(t)
	var all []string
	for _, b := range doc.Blocks {
		if b == doc.Root {
			continue
		}
		all = append(all, b.ID)
	}
	side := &persist.Sidecar{Reviewed: all}
	m := New(doc, side)
	assert.Equal(t, firstContentBlockID(doc), m.CursorBlockID)
}
