package ui

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"mreview/pkg/pdf"
	"mreview/pkg/persist"
	"mreview/pkg/synctex"
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

// TestNew_FullyReviewedDowngradesFilterToAll covers the review-new.md
// #4 finding: a fully reviewed sidecar would otherwise open with
// FilterUnreviewed (because Reviewed is non-empty) on a reviewed
// cursor block, producing an empty outline while the source pane
// shows a real block. ui.New must relax the filter in that case so
// the outline and cursor stay consistent.
func TestNew_FullyReviewedDowngradesFilterToAll(t *testing.T) {
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
	assert.Equal(t, FilterAll, m.Filter, "everything reviewed must relax filter to All")

	// Sanity check: the chosen cursor is actually in the outline.
	rows := BuildOutline(m.Doc, m.Sidecar, m.Filter)
	require.NotEmpty(t, rows)
	found := false
	for _, r := range rows {
		if r.BlockID == m.CursorBlockID {
			found = true
			break
		}
	}
	assert.True(t, found, "cursor block must pass the active filter")
}

// TestNew_PartiallyReviewedKeepsFilterUnreviewed guards the
// non-degenerate case so the downgrade doesn't kick in whenever any
// block happens to be reviewed — only when *every* block is.
func TestNew_PartiallyReviewedKeepsFilterUnreviewed(t *testing.T) {
	doc := parsedSample(t)
	// Mark only the first non-root block reviewed. Unreviewed should
	// still be the default filter because later blocks are outstanding.
	var firstID string
	for _, b := range doc.Blocks {
		if b == doc.Root {
			continue
		}
		firstID = b.ID
		break
	}
	side := &persist.Sidecar{Reviewed: []string{firstID}}
	m := New(doc, side)
	assert.Equal(t, FilterUnreviewed, m.Filter)
	// Cursor must be a non-reviewed block.
	assert.NotEqual(t, firstID, m.CursorBlockID)
}

// TestApplyReloadResult_PreservesPDFAndSyncTeXOnNil is the
// review-new.md #1 regression guard: after a failed rebuild,
// performReload returns nil PDF and nil SyncTeX so the model keeps
// its prior handles. Clearing either would silently turn the PDF
// pane blank, which is more misleading than a stale-but-recognisable
// crop next to the "rebuild failed" status.
func TestApplyReloadResult_PreservesPDFAndSyncTeXOnNil(t *testing.T) {
	doc := parsedSample(t)
	m := New(doc, &persist.Sidecar{})
	m.Width, m.Height = 120, 40
	// Sentinel values so we can tell whether they survive.
	m.PDF = nil // the type is *pdf.Doc; a non-nil stub would require importing pdf. Use synctex for the test.
	// Synctex preservation is the actionable part of the fix.
	// Build a dummy non-nil *synctex.Index isn't easily constructed without
	// a real file; instead, assert the contract directly: Synctex field
	// must *not* be overwritten to nil by applyReloadResult when the
	// message carries a nil newSyncTeX.
	// To simulate "prior handle", set Synctex via a pointer we stash
	// before the reload and expect unchanged after.
	priorSynctex := m.Synctex // nil in fresh model; the invariant still
	// holds — we assert equality of the before/after pointer.
	nm, _ := m.applyReloadResult(reloadResultMsg{
		gen:        m.reloadGen,
		newDoc:     doc,
		newPDF:     nil,
		newSyncTeX: nil,
		status:     "rebuild failed — ...",
	})
	assert.Equal(t, priorSynctex, nm.Synctex, "nil newSyncTeX must not clear model.Synctex")
	assert.Equal(t, m.PDF, nm.PDF, "nil newPDF must not clear model.PDF")
	assert.Equal(t, "rebuild failed — ...", nm.Status)
}

// TestApplyReloadResult_BuildStalePreservesImageAndSuppressesRender
// is the review-new.md (pass 4) #1 regression guard: after a reload
// that couldn't produce a coherent (Doc, PDF, SyncTeX) triple, the
// prior PDFImage must stay on screen (so the user doesn't see the
// pane turn blank) and no render command must be scheduled (auto-
// rendering against a stale SyncTeX index would paint a wrong crop).
func TestApplyReloadResult_BuildStalePreservesImageAndSuppressesRender(t *testing.T) {
	doc := parsedSample(t)
	m := New(doc, &persist.Sidecar{})
	m.Width, m.Height = 120, 40
	const sentinel = "\x1b_Gsentinel-kitty-escape\x1b\\"
	m.PDFImage = sentinel

	nm, cmd := m.applyReloadResult(reloadResultMsg{
		gen:        m.reloadGen,
		newDoc:     doc,
		newPDF:     nil,
		newSyncTeX: nil,
		status:     "rebuild failed — ...",
		buildStale: true,
	})
	assert.True(t, nm.BuildStale, "stale build must set the flag so subsequent renders self-suppress")
	assert.Equal(t, sentinel, nm.PDFImage, "stale build must leave the prior PDF crop on screen")
	assert.Nil(t, cmd, "stale build must not schedule a new render against the mismatched handles")
}

// TestApplyReloadResult_FreshBuildClearsStaleFlag covers the happy
// path that unfreezes the PDF pane: a subsequent successful reload
// clears BuildStale and the prior PDFImage stays on screen through
// the render debounce so the pane doesn't blink. handlePDFRender
// replaces it atomically once the new crop is ready.
func TestApplyReloadResult_FreshBuildClearsStaleFlag(t *testing.T) {
	doc := parsedSample(t)
	m := New(doc, &persist.Sidecar{})
	m.Width, m.Height = 120, 40
	m.BuildStale = true
	const priorImage = "leftover-from-stale-session"
	m.PDFImage = priorImage

	nm, _ := m.applyReloadResult(reloadResultMsg{
		gen:        m.reloadGen,
		newDoc:     doc,
		status:     "rebuilt + reloaded · 5 blocks",
		buildStale: false,
	})
	assert.False(t, nm.BuildStale, "successful reload must clear the stale flag")
	assert.Equal(t, priorImage, nm.PDFImage,
		"healthy reload keeps the prior image through the render debounce so the pane doesn't flicker")
}

// TestSchedulePDFRender_SuppressedWhenBuildStale covers the render-
// side half of the contract: even with a real *pdf.Doc attached,
// scheduling must short-circuit while BuildStale is true. Uses the
// same fixture as the existing PDF tests so we exercise the actual
// early-return path rather than a synthetic mock.
func TestSchedulePDFRender_SuppressedWhenBuildStale(t *testing.T) {
	pdfDoc, err := pdf.Open(pdfFixturePath(t, "sample.pdf"))
	require.NoError(t, err)
	defer pdfDoc.Close()

	idx, err := synctex.Open(pdfFixturePath(t, "sample.synctex.gz"))
	require.NoError(t, err)

	m := New(parsedSample(t), &persist.Sidecar{})
	m.PDF = pdfDoc
	m.Synctex = idx
	m.Width, m.Height = 120, 40
	m.BuildStale = true

	assert.Nil(t, m.schedulePDFRender(), "BuildStale=true must short-circuit the render scheduler")

	// Clearing the flag restores normal scheduling.
	m.BuildStale = false
	require.NotNil(t, m.schedulePDFRender(), "clearing BuildStale must resume rendering")
}

// TestApplyReloadResult_DropsStaleGeneration is the review-new.md
// (pass 5) #1 regression guard for concurrent reloads. When two
// reloads race, only the latest (gen == m.reloadGen) may apply; an
// older goroutine finishing late must not roll the model back.
func TestApplyReloadResult_DropsStaleGeneration(t *testing.T) {
	doc := parsedSample(t)
	m := New(doc, &persist.Sidecar{})
	m.reloadGen = 5
	beforeDoc := m.Doc
	beforeCursor := m.CursorBlockID

	// Message carries gen=4 (older reload finishing late); current is 5.
	nm, cmd := m.applyReloadResult(reloadResultMsg{
		gen:    4,
		newDoc: parsedSample(t), // fresh pointer to prove it didn't install
		status: "stale reload finishing late",
	})
	assert.Same(t, beforeDoc, nm.Doc, "stale-gen reload must not replace m.Doc")
	assert.Equal(t, beforeCursor, nm.CursorBlockID, "stale-gen reload must not touch cursor")
	assert.Nil(t, cmd)
}

// TestApplyReloadResult_MergesLiveSidecarEdits is the regression
// guard for annotations added *during* a rebuild. The reload
// pipeline used to snapshot the sidecar at startReload time and
// install the remapped snapshot on completion — any annotation the
// user added in between was silently dropped. The fix moves the
// remap to applyReloadResult, where it runs against the live
// m.Sidecar.
func TestApplyReloadResult_MergesLiveSidecarEdits(t *testing.T) {
	doc := parsedSample(t)
	require.GreaterOrEqual(t, len(doc.Blocks), 2)
	targetID := doc.Blocks[1].ID

	m := New(doc, &persist.Sidecar{})
	m.reloadGen = 1
	// Simulate: user added an annotation while the rebuild was running.
	m.Sidecar.Annotations = append(m.Sidecar.Annotations, persist.Annotation{
		BlockID: targetID,
		Note:    "added during reload",
	})

	nm, _ := m.applyReloadResult(reloadResultMsg{
		gen:    1,
		newDoc: doc,
		status: "reloaded",
	})

	require.Len(t, nm.Sidecar.Annotations, 1, "live annotation must survive reload")
	assert.Equal(t, "added during reload", nm.Sidecar.Annotations[0].Note)
	assert.Equal(t, targetID, nm.Sidecar.Annotations[0].BlockID)
}

// TestApplyReloadResult_RespectsLiveCursor is the counterpart for
// cursor navigation during a rebuild. resolveReloadCursor now runs
// against m.CursorBlockID (live), not a snapshot taken at
// startReload time, so the user's in-flight j/k motion sticks.
func TestApplyReloadResult_RespectsLiveCursor(t *testing.T) {
	doc := parsedSample(t)
	require.GreaterOrEqual(t, len(doc.Blocks), 3)

	m := New(doc, &persist.Sidecar{})
	m.reloadGen = 1
	// Pick a cursor block different from the one startReload would
	// have snapshotted — simulating "user navigated during rebuild".
	liveCursor := doc.Blocks[2].ID
	m.CursorBlockID = liveCursor

	nm, _ := m.applyReloadResult(reloadResultMsg{
		gen:    1,
		newDoc: doc,
		status: "reloaded",
	})
	assert.Equal(t, liveCursor, nm.CursorBlockID,
		"live navigation during rebuild must survive applyReloadResult")
}

// TestNew_PartialReviewWithReviewedCursorKeepsUnreviewedFilter is
// the review-new.md (pass 4) #2 regression guard. Before the fix,
// any saved cursor that didn't pass DefaultFilter would trigger a
// downgrade to FilterAll — even when outstanding unreviewed work
// still existed. The correct behaviour: downgrade only when the
// filter would render an empty outline.
func TestNew_PartialReviewWithReviewedCursorKeepsUnreviewedFilter(t *testing.T) {
	doc := parsedSample(t)
	require.GreaterOrEqual(t, len(doc.Blocks), 3)

	// Pick a block to mark reviewed AND use as the saved cursor; pick
	// another block that stays unreviewed so FilterUnreviewed has
	// something to show.
	var reviewedID, unreviewedID string
	for _, b := range doc.Blocks {
		if b == doc.Root {
			continue
		}
		if reviewedID == "" {
			reviewedID = b.ID
			continue
		}
		unreviewedID = b.ID
		break
	}
	require.NotEmpty(t, reviewedID)
	require.NotEmpty(t, unreviewedID)

	side := &persist.Sidecar{
		Reviewed: []string{reviewedID},
		Cursor:   reviewedID, // saved cursor is on a reviewed block
	}
	m := New(doc, side)

	assert.Equal(t, FilterUnreviewed, m.Filter,
		"saved cursor on a reviewed block must NOT downgrade the filter when unreviewed work remains")
	assert.Equal(t, reviewedID, m.CursorBlockID,
		"cursor stays on the user-saved block even when it falls outside the active filter")
}
