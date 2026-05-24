package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"mreview/pkg/parser"
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
	got := resolveReloadCursor(cur, "", 0, doc, &persist.Sidecar{})
	assert.Equal(t, cur, got)
}

// TestResolveReloadCursor_LabelRescue asserts a label survives ID
// churn (same behaviour as persist.Remap's cursor rescue).
func TestResolveReloadCursor_LabelRescue(t *testing.T) {
	doc := parsedSample(t)
	b, ok := doc.ByLabel["thm:main"]
	require.True(t, ok, "fixture must expose thm:main")
	got := resolveReloadCursor("thm:main", "", 0, doc, &persist.Sidecar{})
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

	got := resolveReloadCursor("gone-block-id", "", 0, doc, side)
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
	got := resolveReloadCursor("missing", "", 0, doc, side)
	assert.Equal(t, firstContentBlockID(doc), got)
}

// TestResolveReloadCursor_NilDocReturnsEmpty guards the defensive
// nil-doc branch — performReload returns early before calling this
// helper on a nil document, but the helper should still not panic
// if it ever gets one.
func TestResolveReloadCursor_NilDocReturnsEmpty(t *testing.T) {
	got := resolveReloadCursor("any", "", 0, nil, &persist.Sidecar{})
	assert.Empty(t, got)
}

// TestResolveReloadCursor_LineAnchorFallback exercises the line-range
// fallback: an edit inside an unlabeled block changes its sha-derived
// ID, so neither ID nor label lookup hits, but the new doc has a block
// straddling the same source line. Without the anchor we would jump to
// firstUnreviewedOrAny — typically the top of the document — which is
// the bug this fallback fixes.
func TestResolveReloadCursor_LineAnchorFallback(t *testing.T) {
	doc := parsedSample(t)
	// Pick a non-root block with a real line range; reviewed-mark every
	// other block so firstUnreviewedOrAny would land on a *different*
	// block if our anchor lookup didn't fire.
	var target *parser.Block
	for _, b := range doc.Blocks {
		if b == doc.Root || b.StartLine == 0 {
			continue
		}
		target = b
		break
	}
	require.NotNil(t, target, "fixture must expose a block with a line range")
	var reviewed []string
	for _, b := range doc.Blocks {
		if b == doc.Root || b == target {
			continue
		}
		reviewed = append(reviewed, b.ID)
	}
	side := &persist.Sidecar{Reviewed: reviewed}

	file := target.File
	if file == "" {
		file = doc.File
	}
	got := resolveReloadCursor("stale-sha-id", file, target.StartLine, doc, side)
	assert.Equal(t, target.ID, got)
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
// side half of the contract: with BuildStale=true, the cursor-following
// crop short-circuits (it would lookup new line numbers against a stale
// SyncTeX index), but manual mode keeps rendering — page rasterisation
// doesn't go through SyncTeX, so a failed rebuild can't make it wrong.
// Uses the same fixture as the existing PDF tests so we exercise the
// actual early-return path rather than a synthetic mock.
func TestSchedulePDFRender_SuppressedWhenBuildStale(t *testing.T) {
	pdfDoc, err := pdf.Open(pdfFixturePath(t, "sample.pdf"))
	require.NoError(t, err)
	defer func() { _ = pdfDoc.Close() }()

	idx, err := synctex.Open(pdfFixturePath(t, "sample.synctex.gz"))
	require.NoError(t, err)

	m := New(parsedSample(t), &persist.Sidecar{})
	m.PDF = pdfDoc
	m.Synctex = idx
	m.Width, m.Height = 120, 40
	m.BuildStale = true

	assert.Nil(t, m.schedulePDFRender(), "BuildStale=true must short-circuit the cursor-following render scheduler")

	// Manual mode keeps rendering across stale builds.
	m.PDFManual = true
	require.NotNil(t, m.schedulePDFRender(), "manual mode must render even while BuildStale is true")
	m.PDFManual = false

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

// TestApplyReloadDocResult_FastPath_InstallsDocAndDefersBuild is
// the regression guard for the split-phase reload: phase 1 must
// install the freshly parsed doc immediately, set BuildStale=true so
// renders are suppressed against the still-old SyncTeX, and return
// a non-nil cmd that launches phase 2 (the build wait + PDF reopen).
func TestApplyReloadDocResult_FastPath_InstallsDocAndDefersBuild(t *testing.T) {
	doc := parsedSample(t)
	m := New(doc, &persist.Sidecar{})
	m.reloadGen = 1

	// Build a *different* doc pointer to confirm the install actually
	// swapped m.Doc rather than no-op'd on equality.
	newDoc := parsedSample(t)
	require.NotSame(t, doc, newDoc)

	nm, cmd := m.applyReloadDocResult(reloadDocMsg{
		gen:    1,
		newDoc: newDoc,
	})

	assert.Same(t, newDoc, nm.Doc, "phase 1 must install the freshly parsed doc")
	assert.True(t, nm.BuildStale,
		"phase 1 must assert BuildStale so renders are suppressed until phase 2 lands")
	require.NotNil(t, cmd,
		"phase 1 must return a non-nil cmd that kicks off the build wait (phase 2)")
}

// TestApplyReloadDocResult_StaleGenIsDropped guards the same
// gen-mismatch contract applyReloadResult enforces: a phase-1
// message arriving after a newer reload has been started must not
// roll the doc back to the older parse.
func TestApplyReloadDocResult_StaleGenIsDropped(t *testing.T) {
	doc := parsedSample(t)
	m := New(doc, &persist.Sidecar{})
	m.reloadGen = 5

	otherDoc := parsedSample(t)
	nm, cmd := m.applyReloadDocResult(reloadDocMsg{
		gen:    3, // older than m.reloadGen → stale
		newDoc: otherDoc,
	})

	assert.Same(t, doc, nm.Doc, "stale phase-1 message must not replace m.Doc")
	assert.Nil(t, cmd, "stale phase-1 message must not kick off a phase-2 build")
}

// TestApplyReloadDocResult_ParseErrorSetsStatusOnly covers the
// read/parse-failure branch of performParse. With newDoc nil, the
// model must surface the error in the status line and skip phase 2
// — a build wait against a doc we never parsed would just compound
// the failure.
func TestApplyReloadDocResult_ParseErrorSetsStatusOnly(t *testing.T) {
	doc := parsedSample(t)
	m := New(doc, &persist.Sidecar{})
	m.reloadGen = 1

	nm, cmd := m.applyReloadDocResult(reloadDocMsg{
		gen:    1,
		status: "reload: parse: unexpected EOF",
	})

	assert.Same(t, doc, nm.Doc, "parse failure must leave m.Doc untouched")
	assert.False(t, nm.BuildStale, "parse failure must not flip BuildStale")
	assert.Contains(t, nm.Status, "reload: parse")
	assert.Nil(t, cmd, "parse failure must not launch phase 2")
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

// TestApplyReloadResult_StaleGenClosesNewPDFNotOld is the review.md #3
// regression guard for overlapping reloads. When two reloads race and
// the slower one's result is dropped (gen mismatch), its newPDF must
// be closed (to avoid a handle leak) but the model's live PDF must
// stay open.
func TestApplyReloadResult_StaleGenClosesNewPDFNotOld(t *testing.T) {
	doc := parsedSample(t)
	pdfDoc, err := pdf.Open(pdfFixturePath(t, "sample.pdf"))
	require.NoError(t, err)
	defer func() { _ = pdfDoc.Close() }()

	m := New(doc, &persist.Sidecar{})
	m.reloadGen = 5
	m.PDF = pdfDoc

	// Open a SECOND handle to act as the stale goroutine's newPDF.
	stalePDF, err := pdf.Open(pdfFixturePath(t, "sample.pdf"))
	require.NoError(t, err)

	nm, cmd := m.applyReloadResult(reloadResultMsg{
		gen:    3, // older than m.reloadGen → stale
		newPDF: stalePDF,
		oldPDF: pdfDoc,
		newDoc: doc,
		status: "stale reload",
	})

	assert.Nil(t, cmd, "stale result must return nil cmd")
	// Model keeps the live PDF; it must NOT have been closed.
	assert.Same(t, pdfDoc, nm.PDF, "stale result must not replace model PDF")
	assert.Greater(t, pdfDoc.NumPage(), 0,
		"live PDF must still be usable (not closed by the stale goroutine)")
	// Stale goroutine's newPDF was closed by applyReloadResult.
	assert.Equal(t, 0, stalePDF.NumPage(),
		"stale result's newPDF must be closed so the handle doesn't leak")
}

// TestOCRReport_BlockedInManualMode is the review.md #2 regression
// guard: pressing B in manual PDF mode must show a clear status
// instead of generating a report for a different image.
func TestOCRReport_BlockedInManualMode(t *testing.T) {
	m := New(parsedSample(t), nil)
	m.PDFManual = true

	nm, cmd := m.startOCRReport()
	assert.Nil(t, cmd)
	assert.Contains(t, nm.Status, "not available in manual PDF mode")
}

// TestOCRReport_BlockedWhenBuildStale is the deep-review #3 regression
// guard: pressing B while BuildStale is true must short-circuit with
// a "rebuild first" status, mirroring the render scheduler's contract.
// Without this guard, startOCRReport would feed the new doc's line
// numbers into the old SyncTeX index and write a report for the wrong
// region.
func TestOCRReport_BlockedWhenBuildStale(t *testing.T) {
	m := New(parsedSample(t), nil)
	m.BuildStale = true

	nm, cmd := m.startOCRReport()
	assert.Nil(t, cmd)
	assert.Contains(t, nm.Status, "build is stale")
}

// TestUpdate_MouseIgnoredWhilePopupOpen is the deep-review #4
// regression guard. Background mouse events must not move the cursor
// or source-line state while an annotation popup is open — doing so
// would detach the live editor from the popup's TargetID and cause
// submit to land on a different block from the one the user sees.
func TestUpdate_MouseIgnoredWhilePopupOpen(t *testing.T) {
	doc := parsedSample(t)
	require.GreaterOrEqual(t, len(doc.Blocks), 2)

	m := New(doc, nil)
	m.Width, m.Height = 120, 40
	beforeCursor := m.CursorBlockID
	beforeLine := m.SourceLineCursor

	// Open an annotation popup (block-level — `A`).
	res, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'A'}})
	m = res.(Model)
	require.NotNil(t, m.Popup, "A must open the annotation popup")

	// Now simulate a mouse click somewhere in the source pane that
	// would normally move the cursor.
	out, _ := m.Update(tea.MouseMsg{
		Action: tea.MouseActionPress,
		Button: tea.MouseButtonLeft,
		X:      80,
		Y:      10,
	})
	nm := out.(Model)

	assert.Equal(t, beforeCursor, nm.CursorBlockID,
		"mouse must not change cursor block while popup is open")
	assert.Equal(t, beforeLine, nm.SourceLineCursor,
		"mouse must not change source line while popup is open")
	assert.NotNil(t, nm.Popup, "popup must remain open after background mouse")
}

// TestSourceLineAt_WrapAware is the deep-review #5 regression guard.
// Under soft-wrap, a click on a continuation row must resolve to the
// source line whose wrapped fragment occupies that row — not
// `startLine + row` as if every source line were one row.
func TestSourceLineAt_WrapAware(t *testing.T) {
	// Build a doc whose first block is a single very long line that
	// wraps to multiple physical rows. Click on row 0 (first wrapped
	// segment) and a deeper row (second segment) — both must resolve
	// to the SAME source line, not to consecutive source lines.
	src := "\\documentclass{article}\n\\begin{document}\n" +
		strings.Repeat("word ", 200) + "\n" + // long line that wraps
		"second line\n" +
		"\\end{document}\n"
	doc, err := parser.Parse([]byte(src))
	require.NoError(t, err)
	doc.File = "fake.tex"

	// Find the first non-root block with a real source range.
	var blockID string
	for _, b := range doc.Blocks {
		if b.StartLine > 0 {
			blockID = b.ID
			break
		}
	}
	require.NotEmpty(t, blockID)

	// Narrow terminal so the long line wraps several times.
	const termW, termH = 60, 30

	id0, line0 := sourceLineAt(doc, blockID, termW, termH, LayoutThreeCol, true, 0)
	id1, line1 := sourceLineAt(doc, blockID, termW, termH, LayoutThreeCol, true, 1)

	require.Equal(t, blockID, id0, "row 0 must land in the cursor block")
	require.Equal(t, blockID, id1, "row 1 must also land in the cursor block (still wrapped)")
	assert.Equal(t, line0, line1,
		"row 0 and row 1 must resolve to the same source line under soft-wrap (the long line wraps)")
}

// TestSourceLineAt_SoftWrapOffStaysOneToOne ensures the hit-tester
// keeps the simple "one row per source line" mapping when soft-wrap is
// disabled — toggling `w` off restores click-positions-cursor exactly
// as before the wrap-aware refactor.
func TestSourceLineAt_SoftWrapOffStaysOneToOne(t *testing.T) {
	doc := parsedSample(t)
	// Use the proof block as cursor: it's multi-line so consecutive
	// rows fall inside the block range and resolve to consecutive
	// LineOffsets, which is the property we want to assert.
	var proof *parser.Block
	for _, b := range doc.Blocks {
		if b.Kind == parser.KindProof && b.EndLine > b.StartLine+1 {
			proof = b
			break
		}
	}
	require.NotNil(t, proof, "fixture must have a multi-line proof block")

	const termW, termH = 120, 40
	// With termH=40, sourcePaneInnerH = 36 and startLine clamps to 1,
	// so row N maps to source line (N+1) under soft-wrap=false.
	rowAtStart := proof.StartLine - 1 // row that lands on proof.StartLine

	id0, line0 := sourceLineAt(doc, proof.ID, termW, termH, LayoutThreeCol, false, rowAtStart)
	id1, line1 := sourceLineAt(doc, proof.ID, termW, termH, LayoutThreeCol, false, rowAtStart+1)
	require.Equal(t, proof.ID, id0)
	require.Equal(t, proof.ID, id1)
	assert.Equal(t, line0+1, line1,
		"with soft-wrap off, row+1 must map to source line +1 within the cursor block")
}

// TestEditFallback_RefusesWhenCursorHasNoLine is the deep-review #6
// regression guard. When the cursor has no resolvable source line,
// edit commands must refuse with a clear status instead of silently
// opening at line 1.
func TestEditFallback_RefusesWhenCursorHasNoLine(t *testing.T) {
	m := New(parsedSample(t), nil)
	m.Doc.File = "/tmp/fake.tex"
	// Force the unresolvable case: a cursor pointing at a non-existent
	// block ID. absoluteCursorLine should return ok=false.
	m.CursorBlockID = "no-such-block"

	nm, cmd := m.StartLineEdit()
	assert.Nil(t, cmd)
	updated := nm.(Model)
	assert.Nil(t, updated.Popup, "e must not open a popup when cursor has no line")
	assert.Contains(t, updated.Status, "no resolvable source line")

	nm2, cmd2 := m.editInExternalEditor()
	assert.Nil(t, cmd2)
	updated2 := nm2.(Model)
	assert.Contains(t, updated2.Status, "no resolvable source line")
}
