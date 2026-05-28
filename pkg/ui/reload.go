package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"mreview/pkg/build"
	"mreview/pkg/format"
	"mreview/pkg/parser"
	"mreview/pkg/pdf"
	"mreview/pkg/persist"
	"mreview/pkg/synctex"
)

// reloadMsg is delivered after an edit path (external editor or inline
// line edit) completes; `err` carries any failure from the edit step so
// the reload path can surface it in the status line instead of pretending
// nothing happened.
type reloadMsg struct {
	err error
}

// reloadDocMsg carries the parse-only fast leg of a reload: just the
// freshly parsed document, lands in milliseconds, lets the source pane
// and outline refresh while the parallel build cmd is still waiting on
// lmkf / latexmk. PDF + SyncTeX stay on the previous handles until
// reloadResultMsg arrives; BuildStale gates the render path during the
// gap so we never paint new doc line numbers through an old SyncTeX
// index.
type reloadDocMsg struct {
	gen    int
	newDoc *parser.Document // nil on read/parse error — caller treats status as the error string
	status string           // empty on success; phase 2 owns the final status
}

// reloadResultMsg carries the outcome of an asynchronous reload: the
// reparsed document plus freshly opened PDF / SyncTeX handles. Sidecar
// remap and cursor resolution happen in applyReloadResult against the
// *live* model so user edits made while the rebuild was running (new
// annotations, reviewed toggles, navigation) survive instead of being
// clobbered by snapshots taken at startReload time.
//
// Running the heavy work off the Update goroutine keeps the UI
// responsive — the user sees a "rebuilding…" status instead of a frozen
// pane while latexmk churns.
//
// In the live pipeline newDoc is usually nil — reloadDocMsg already
// installed the doc through applyReloadDocResult. Tests still construct
// reloadResultMsg with newDoc set, and applyReloadResult is nil-
// preserving so the same code path works for both.
type reloadResultMsg struct {
	// gen carries the reloadGen captured by startReload. Out-of-order
	// or superseded reloads (gen != m.reloadGen at apply time) are
	// dropped so the model can never be rolled back to an earlier
	// snapshot by a slow goroutine finishing late.
	gen        int
	newDoc     *parser.Document
	newPDF     *pdf.Doc
	newSyncTeX *synctex.Index
	status     string
	// buildStale is true when this reload could not produce a coherent
	// (doc, PDF, SyncTeX) triple — either the rebuild failed or one of
	// the artefact reopens failed. applyReloadResult uses it to keep
	// the prior PDFImage on screen and skip scheduling a render that
	// would lookup new line numbers in an old SyncTeX index.
	buildStale bool
	// oldPDF is the handle that was live when this reload started,
	// passed through so applyReloadResult can close it only after the
	// new handle is installed. Without this, a goroutine closing oldPDF
	// while a newer reload is still in flight would leave the model
	// pointing at a closed handle until the newer reload finishes.
	oldPDF *pdf.Doc
}

// startReload sets a "rebuilding…" status and returns a tea.Cmd that
// performs the heavy work (parse, latexmk, PDF+SyncTeX reopen) off the
// Update goroutine. The result message also carries the reload
// generation so applyReloadResult can drop superseded reloads. Sidecar
// remap and cursor resolution are deferred to apply time so they run
// against the live model — preserving any annotations / reviewed
// toggles / navigation the user did while the rebuild was running.
//
// Keeping the work off the main loop is important: latexmk on a real
// paper takes seconds and freezing the TUI that long makes it look
// like the edit had no effect.
func (m Model) startReload() (Model, tea.Cmd) {
	if m.Doc == nil || m.Doc.File == "" {
		m.Status = "reload: no source file"
		return m, nil
	}
	path := m.Doc.File
	m.reloadGen++
	gen := m.reloadGen
	m.Status = "rebuilding…"

	// Two-phase reload, sequenced through Update:
	//   phase 1 (parseCmd) reads + parses the .tex (~ms) and posts
	//     reloadDocMsg so the source pane / outline / annotation
	//     remap refresh immediately, without waiting for lmkf.
	//   phase 2 is launched by applyReloadDocResult once phase 1 has
	//     installed the new doc; it waits for lmkf (or runs latexmk),
	//     reopens PDF + SyncTeX, posts reloadResultMsg.
	// Sequencing through Update (rather than tea.Batch) guarantees
	// phase 2 sees the new doc when it applies — Batch's message
	// order is unspecified, and out-of-order delivery would leave
	// BuildStale set when phase 1 lands after phase 2's success.
	return m, func() tea.Msg { return performParse(path, gen) }
}

// performParse is the goroutine body for the fast leg of startReload.
// It reads + parses the .tex and best-effort enriches the doc with
// whatever aux/bbl is on disk now (possibly slightly stale — phase 2
// will re-apply after the build finishes if it succeeds). Bailout on
// I/O or parse error sets `status` and leaves newDoc nil; applyReloadDocResult
// surfaces the status without touching the model.
func performParse(path string, gen int) reloadDocMsg {
	src, err := os.ReadFile(path)
	if err != nil {
		return reloadDocMsg{gen: gen, status: "reload: " + err.Error()}
	}
	newDoc, err := parser.Parse(src)
	if err != nil {
		return reloadDocMsg{gen: gen, status: "reload: parse: " + err.Error()}
	}
	newDoc.File = path
	// Best-effort aux/bbl from disk-as-of-now. If the user just edited
	// the .tex but lmkf hasn't finished its rebuild, these are pre-edit
	// numbers — better than rendering "??" until phase 2 lands. Phase 2
	// re-applies the post-build aux/bbl on success so the displayed
	// numbers self-correct once lmkf finishes.
	buildRes := build.ResolveBuildOutputsOnDisk(path)
	if auxEntries, err := parser.LoadAux(buildRes.AuxPath); err == nil {
		parser.ApplyAux(newDoc, auxEntries)
	}
	if bibEntries, err := parser.LoadBBL(buildRes.BBLPath); err == nil {
		parser.ApplyBBL(newDoc, bibEntries)
	}
	return reloadDocMsg{gen: gen, newDoc: newDoc}
}

// applyReloadDocResult installs the freshly parsed doc immediately so
// the source pane / outline / annotation remap reflect external edits
// within milliseconds, without waiting for the build cmd's lmkf or
// latexmk wait. PDF + SyncTeX stay on the previous handles; BuildStale
// is asserted so any cursor-driven render is suppressed until the
// parallel reloadResultMsg lands a coherent triple.
//
// Stale messages (gen != m.reloadGen) are dropped so an older reload
// finishing after a newer one cannot roll the doc back.
func (m Model) applyReloadDocResult(r reloadDocMsg) (Model, tea.Cmd) {
	if r.gen != m.reloadGen {
		return m, nil
	}
	if r.newDoc == nil {
		if r.status != "" {
			m.Status = r.status
		}
		return m, nil
	}
	// Snapshot the cursor's anchor (file + line) against the OLD doc
	// before we swap it out, so resolveReloadCursor can fall back to
	// "the new block at the same source line" when content edits have
	// shifted the sha-derived ID of the cursor block.
	oldFile, oldLine := cursorAnchor(m.Doc, m.CursorBlockID)
	m.Doc = r.newDoc
	if m.Sidecar != nil {
		newSidecar, detached := persist.Remap(m.Sidecar, m.Doc)
		newSidecar.Detached = append(newSidecar.Detached, detached...)
		RefreshRemappedAnnotations(m.Doc, newSidecar)
		m.Sidecar = newSidecar
		m.SidecarBase = SnapshotSidecar(m.Sidecar)
	}
	if m.Doc.File != "" {
		reportPath := format.ReportPath(m.Doc.File)
		if ext, extErr := LoadExternalIssues(reportPath, m.Doc); extErr == nil {
			m.ExternalIssues = ext
		}
	}
	m.CursorBlockID = resolveReloadCursor(m.CursorBlockID, oldFile, oldLine, m.Doc, m.Sidecar)
	if paths := m.sourceWatchPaths(); len(paths) > 0 {
		if newest := newestSourceMtime(paths); !newest.IsZero() {
			m.SourceMtime = newest
		}
	}
	m.SourceLineCursor = clampLineCursor(m.Doc, m.CursorBlockID, m.SourceLineCursor)
	// Phase 1 has installed the new doc but PDF / SyncTeX are still the
	// previous build's. A render at this point would feed new line
	// numbers into an old SyncTeX index → wrong crops, so we suppress
	// the render path until phase 2 either lands a coherent pair
	// (BuildStale=false) or confirms a build failure (BuildStale=true
	// kept). Phase 2 always overwrites this field on apply.
	m.BuildStale = true
	if r.status != "" {
		m.Status = r.status
	}
	// Kick off phase 2 (build wait + PDF/SyncTeX reopen). Captures
	// oldPDF *now* — at the point we know phase 1 installed the doc —
	// so the eventual close in applyReloadResult retires the right
	// handle even if the user navigates between phase 1 and phase 2.
	path := m.Doc.File
	oldPDF := m.PDF
	buildCmd := ""
	if m.Config != nil {
		buildCmd = m.Config.BuildCmd
	}
	gen := r.gen
	return m, func() tea.Msg {
		return performBuildAndReopen(path, gen, oldPDF, buildCmd)
	}
}

// applyReloadResult installs the outcome of startReload on the model.
// Old PDF handle closure happens here — the goroutine passes oldPDF
// through the result message so the handle is only closed when the
// winning reload is actually installed (not earlier, which would
// leave the model pointing at a closed handle until a newer reload
// arrives).
//
// Sidecar remap and cursor resolution run *here* (not in the
// goroutine) against m.Sidecar / m.CursorBlockID, so any user edits
// made during the rebuild — new annotations, reviewed toggles,
// navigation — survive the reload. Snapshotting at startReload time
// would silently overwrite them.
//
// PDF and SyncTeX are both nil-preserving: a reload that didn't
// reproduce them (e.g. the rebuild failed) leaves the previous handles
// in place. When r.buildStale is true the model also keeps its prior
// PDFImage and skips the render schedule — the alternative (running
// the render anyway) would feed new doc line numbers into the old
// SyncTeX index and paint a wrong crop on top of a "rebuild failed"
// status bar.
//
// Stale messages (gen != m.reloadGen) are dropped: when two reloads
// run close together, the slower older one finishing last must not
// roll the model back to its older artefacts.
func (m Model) applyReloadResult(r reloadResultMsg) (Model, tea.Cmd) {
	if r.gen != m.reloadGen {
		// Stale reload — discard. If this goroutine opened a new PDF
		// handle, close it so it doesn't leak; oldPDF stays live for
		// the winning reload or the model.
		if r.newPDF != nil {
			_ = r.newPDF.Close()
		}
		return m, nil
	}
	if r.newDoc != nil {
		m.Doc = r.newDoc
	}
	if r.newPDF != nil {
		// Close the previous handle now that we're committing the new
		// one onto the model. Safe: no other goroutine still references
		// oldPDF because startReload captured it by value.
		if r.oldPDF != nil && r.oldPDF != r.newPDF {
			_ = r.oldPDF.Close()
		}
		m.PDF = r.newPDF
	}
	if r.newSyncTeX != nil {
		m.Synctex = r.newSyncTeX
		// Populate PDFRegion on m.Doc's blocks against the freshly
		// opened SyncTeX index. Phase 2's goroutine no longer carries
		// a doc handle — phase 1 owns the doc install — so this
		// (cheap) loop runs at apply time on the main goroutine.
		populateRegions(m.Doc, r.newSyncTeX)
		// Refresh the pdf-watch baseline so its tick only fires when
		// a *later* external rebuild advances the synctex.gz mtime
		// past the file phase 2 just opened.
		if m.Doc != nil && m.Doc.File != "" {
			sxPath := build.ResolveBuildOutputsOnDisk(m.Doc.File).SyncTeXPath
			if info, statErr := os.Stat(sxPath); statErr == nil {
				m.SyncTeXMtime = info.ModTime()
			}
		}
	}

	// On a successful build, re-apply aux/bbl from disk so theorem
	// numbers and citation entries reflect the post-build artefacts.
	// Phase 1 already applied a best-effort enrichment from disk-as-
	// of-parse-time; redoing it here picks up any numbering shifts
	// the rebuild introduced (added theorems, new \cite keys, etc.).
	if !r.buildStale && m.Doc != nil && m.Doc.File != "" {
		buildRes := build.ResolveBuildOutputsOnDisk(m.Doc.File)
		if auxEntries, err := parser.LoadAux(buildRes.AuxPath); err == nil {
			parser.ApplyAux(m.Doc, auxEntries)
		}
		if bibEntries, err := parser.LoadBBL(buildRes.BBLPath); err == nil {
			parser.ApplyBBL(m.Doc, bibEntries)
		}
	}

	// Remap the *live* sidecar against the new doc. Any annotations
	// the user added during the rebuild live in m.Sidecar.Annotations
	// and would not be in a snapshot taken at startReload time.
	if m.Sidecar != nil && m.Doc != nil {
		newSidecar, detached := persist.Remap(m.Sidecar, m.Doc)
		newSidecar.Detached = append(newSidecar.Detached, detached...)
		RefreshRemappedAnnotations(m.Doc, newSidecar)
		m.Sidecar = newSidecar
		// Refresh the merge baseline: the post-remap sidecar now
		// represents what the next save should be diffed against.
		// Without this, a save after a reload would treat every
		// remap-induced BlockID change as a user delta and overwrite
		// the agent's deletions on disk.
		m.SidecarBase = SnapshotSidecar(m.Sidecar)
	}
	// Refresh external issues (fmt-report diagnostics) against the new
	// doc so the issues filter stays current after edits.
	if m.Doc != nil && m.Doc.File != "" {
		reportPath := format.ReportPath(m.Doc.File)
		if ext, extErr := LoadExternalIssues(reportPath, m.Doc); extErr == nil {
			m.ExternalIssues = ext
		}
	}
	// Resolve the cursor against the *live* cursor too — if the user
	// navigated during the rebuild, m.CursorBlockID is what they
	// expect to stay on, not the snapshot. Phase 2 runs after phase 1
	// already swapped m.Doc, so the live doc is the post-reload doc;
	// look up the anchor against it (a no-op for ID-stable cursors,
	// load-bearing only when the user navigated to a block whose
	// content also drifted across this same reload).
	liveFile, liveLine := cursorAnchor(m.Doc, m.CursorBlockID)
	m.CursorBlockID = resolveReloadCursor(m.CursorBlockID, liveFile, liveLine, m.Doc, m.Sidecar)
	// Refresh the source-watch baseline against the current state of
	// disk: another external edit between our startReload and now must
	// trigger again on the next tick rather than getting absorbed into
	// the just-applied result.
	if paths := m.sourceWatchPaths(); len(paths) > 0 {
		if newest := newestSourceMtime(paths); !newest.IsZero() {
			m.SourceMtime = newest
		}
	}
	m.SourceLineCursor = clampLineCursor(m.Doc, m.CursorBlockID, m.SourceLineCursor)
	m.BuildStale = r.buildStale
	if r.status != "" {
		m.Status = r.status
	} else if m.Doc != nil {
		m.Status = fmt.Sprintf("reloaded · %d blocks", len(m.Doc.Blocks))
	}
	if m.BuildStale {
		// Keep the prior PDF crop visible and don't schedule a fresh
		// render. The cache stays warm so a subsequent successful
		// reload can flush it cleanly.
		return m, nil
	}
	// Healthy reload: flush the crop cache because mtime / block IDs
	// may have shifted, but keep m.PDFImage on screen through the
	// render debounce so the pane doesn't blink. handlePDFRender
	// atomically replaces it once the new crop is ready; if the new
	// render produces a status instead of an image, pdfPaneBody's
	// kitty-delete prefix retires the stale bitmap.
	m.pdfCache = newPDFCropCache(pdfCropCacheMax)
	return m, m.schedulePDFRender()
}

// performBuildAndReopen is the goroutine body for the slow leg of
// startReload. By the time this runs, applyReloadDocResult has already
// installed the new doc on the live model — this leg's only job is to
// wait for the build (lmkf log marker, or run latexmk) and reopen the
// PDF + SyncTeX pair. Sidecar remap, cursor resolution, populateRegions,
// and aux/bbl re-application happen at apply time against the live
// model so they don't race with phase 1.
func performBuildAndReopen(path string, gen int, oldPDF *pdf.Doc, buildCmd string) reloadResultMsg {
	buildRes := build.ResolveBuildOutputsOnDisk(path)
	status := ""
	// buildStale flips to true whenever the reload can't deliver a
	// coherent (Doc, PDF, SyncTeX) triple back to the model. Sources:
	//
	//   - latexmk reported an error or lmkf returned error/timeout
	//     (the new artefacts on disk do not match the doc that
	//     phase 1 already installed);
	//   - both pdf.Open and synctex.Open are needed for a coherent
	//     pair, and a partial success would leave the model with new
	//     PDF + old SyncTeX or old PDF + new SyncTeX. Both opens must
	//     therefore succeed together; on partial success we close the
	//     orphan, keep the previous handles, and treat the reload as
	//     stale.
	rebuildOK := true
	buildStale := false
	editTime := time.Now()
	if st, err := os.Stat(path); err == nil {
		// Use the tex's mtime as the "edit time" yardstick so we don't
		// race against a second-level clock skew.
		editTime = st.ModTime()
	}
	switch {
	case func() bool { _, ok := lmkfLogPath(path); return ok }():
		// lmkf (latexmk -pvc wrapper) is already watching this paper.
		// Skip our own latexmk and poll the log file for the pass-
		// completion marker triggered by our edit.
		//
		// Timeout is set generously: a heavy paper rebuild (50+ pages,
		// many figures) routinely takes 30-60s, and timing out before
		// lmkf finishes leaves the model with a stale PDF + new doc
		// pair, which the user reads as "PDF didn't reload after vim".
		// Two minutes is long enough for any paper that builds at all.
		if res, lmkf := AwaitLmkfRebuild(path, editTime, 2*time.Minute); res != nil {
			buildRes = res
			switch lmkf.Status {
			case LmkfRebuildOK:
				status = "lmkf rebuild ok"
			case LmkfRebuildError:
				status = "lmkf rebuild error — " + lmkf.ErrorLine
				rebuildOK = false
			default:
				status = "lmkf didn't finish in time (edit saved anyway)"
				rebuildOK = false
			}
		}
	case shouldRebuild(path, buildRes.PDFPath):
		res, berr := build.RunWith(build.Options{
			TexPath:  path,
			BuildCmd: buildCmd,
		})
		if berr != nil {
			status = "rebuild failed — " + shortBuildErr(berr)
			rebuildOK = false
		} else {
			buildRes = res
			status = "rebuilt + reloaded"
		}
	default:
		status = ""
	}

	if !rebuildOK {
		buildStale = true
	}

	// PDF + SyncTeX must update as a coherent pair. The render path
	// (renderPDFForBlock) calls Index.RegionForLines(file, doc.StartLine,
	// doc.EndLine) — pairing a new PDF with an old SyncTeX (or vice
	// versa) breaks coherence. So: only adopt the new pair when both
	// opens succeed; on any partial success, close the orphan handle,
	// keep the previous handles in the model (applyReloadResult is
	// nil-preserving), and flag the reload as stale so the next render
	// is suppressed.
	var newPDF *pdf.Doc
	var newSyncTeX *synctex.Index
	if rebuildOK {
		pdfDoc, pdfErr := pdf.Open(buildRes.PDFPath)
		idx, sxErr := synctex.Open(buildRes.SyncTeXPath)
		if pdfErr == nil && sxErr == nil {
			newPDF = pdfDoc
			newSyncTeX = idx
			// populateRegions runs at apply time against the live
			// m.Doc — phase 2 doesn't carry a doc handle.
			// Don't close oldPDF here — applyReloadResult owns that
			// decision. A goroutine closing oldPDF while a newer reload
			// is in flight would leave the live model pointing at a
			// closed handle until the newer result arrives.
		} else {
			if pdfDoc != nil {
				_ = pdfDoc.Close()
			}
			// synctex.Index has no Close — it's an in-memory parsed
			// struct that the GC will reclaim once the local goes out
			// of scope.
			buildStale = true
		}
	}

	return reloadResultMsg{
		gen:        gen,
		newPDF:     newPDF,
		newSyncTeX: newSyncTeX,
		status:     status,
		buildStale: buildStale,
		oldPDF:     oldPDF,
	}
}

// resolveReloadCursor picks the cursor block for the post-reload model.
// Preference order:
//  1. The previous cursor if its ID still resolves in newDoc.
//  2. The previous cursor treated as a LaTeX label (remap rescue).
//  3. The deepest block in newDoc whose [StartLine, EndLine] contains
//     oldLine (matching oldFile). Catches the common case where an
//     unlabeled block (proof step, paragraph, inline math) keeps the
//     same source position but its sha-derived ID drifted because the
//     cursor block's first 40 chars changed — without this fallback
//     every save inside the cursor block bounced the cursor to the
//     first unreviewed item near the top of the document.
//  4. firstUnreviewedOrAny — mirrors ui.New's fallback so an edit that
//     deletes the cursor block entirely (no line-range hit either)
//     reopens on outstanding work instead of returning a dead ID.
func resolveReloadCursor(oldCursor, oldFile string, oldLine int, newDoc *parser.Document, newSidecar *persist.Sidecar) string {
	if newDoc == nil {
		return ""
	}
	if _, ok := newDoc.ByID[oldCursor]; ok {
		return oldCursor
	}
	if b, ok := newDoc.ByLabel[oldCursor]; ok {
		return b.ID
	}
	if id := blockAtLine(newDoc, oldFile, oldLine); id != "" {
		return id
	}
	return firstUnreviewedOrAny(newDoc, newSidecar)
}

// cursorAnchor returns (file, startLine) for the block id'd by `cursor`
// in `doc`. Returns ("", 0) on miss so callers can pass these straight
// through to resolveReloadCursor without a separate ok check —
// blockAtLine treats a zero line as "no anchor available" and skips the
// line-range fallback.
func cursorAnchor(doc *parser.Document, cursor string) (string, int) {
	if doc == nil || cursor == "" {
		return "", 0
	}
	b := doc.ByID[cursor]
	if b == nil {
		return "", 0
	}
	file := b.File
	if file == "" {
		file = doc.File
	}
	return file, b.StartLine
}

// blockAtLine returns the ID of the deepest block in doc whose
// [StartLine, EndLine] contains line, restricted to blocks whose File
// matches (or whose File is empty when the cursor was anchored in the
// root file). "Deepest" = smallest line span among the candidates,
// which picks a proof step over its enclosing proof over its enclosing
// theorem when all three contain the line. Returns "" when the anchor
// is unset or no block straddles the line.
func blockAtLine(doc *parser.Document, file string, line int) string {
	if doc == nil || line <= 0 {
		return ""
	}
	rootFile := doc.File
	bestID := ""
	bestSpan := 0
	for _, b := range doc.Blocks {
		if b == nil || b == doc.Root {
			continue
		}
		if b.StartLine == 0 || b.EndLine < b.StartLine {
			continue
		}
		bf := b.File
		if bf == "" {
			bf = rootFile
		}
		if file != "" && bf != "" && bf != file {
			continue
		}
		if line < b.StartLine || line > b.EndLine {
			continue
		}
		span := b.EndLine - b.StartLine
		if bestID == "" || span < bestSpan {
			bestID = b.ID
			bestSpan = span
		}
	}
	return bestID
}

// shouldRebuild reports whether tex mtime is newer than pdf mtime. A
// missing PDF also triggers a build (the user might have edited and
// deleted the stale output).
func shouldRebuild(texPath, pdfPath string) bool {
	ti, err := os.Stat(texPath)
	if err != nil {
		return false
	}
	pi, err := os.Stat(pdfPath)
	if err != nil {
		return true
	}
	return ti.ModTime().After(pi.ModTime())
}

// shortBuildErr trims a build.BuildError to its first-line message so
// the status bar doesn't overflow with log tail spam on rebuild failure.
func shortBuildErr(err error) string {
	msg := err.Error()
	for i, c := range msg {
		if c == '\n' {
			return msg[:i]
		}
	}
	return msg
}

// LmkfRebuildStatus is the outcome of waiting for LP's lmkf wrapper
// to finish the latexmk pass triggered by a source edit.
type LmkfRebuildStatus int

const (
	LmkfRebuildNotWatching LmkfRebuildStatus = iota
	LmkfRebuildOK
	LmkfRebuildError
	LmkfRebuildTimeout
)

// LmkfRebuildResult records the lmkf log-path handshake outcome.
type LmkfRebuildResult struct {
	Status    LmkfRebuildStatus
	LogPath   string
	ErrorLine string
}

// AwaitLmkfRebuild waits for lmkf's latexmk -pvc pass to finish and
// returns freshly rediscovered build artefact paths. It uses the same
// log-marker and error scanning policy as normal review reloads, and on
// success waits briefly for the PDF/SyncTeX files to become visible so
// callers do not open a stale pre-edit pair.
func AwaitLmkfRebuild(texPath string, editTime time.Time, timeout time.Duration) (*build.Result, LmkfRebuildResult) {
	res := build.ResolveBuildOutputsOnDisk(texPath)
	logPath, ok := lmkfLogPath(texPath)
	if !ok {
		return res, LmkfRebuildResult{Status: LmkfRebuildNotWatching}
	}
	result, errLine := waitForLmkfComplete(logPath, editTime, timeout)
	switch result {
	case "ok":
		res = build.ResolveBuildOutputsOnDisk(texPath)
		waitForArtefactsFresh(res.PDFPath, res.SyncTeXPath, editTime, 5*time.Second)
		res = build.ResolveBuildOutputsOnDisk(texPath)
		return res, LmkfRebuildResult{Status: LmkfRebuildOK, LogPath: logPath}
	case "error":
		res = build.ResolveBuildOutputsOnDisk(texPath)
		return res, LmkfRebuildResult{Status: LmkfRebuildError, LogPath: logPath, ErrorLine: errLine}
	default:
		res = build.ResolveBuildOutputsOnDisk(texPath)
		return res, LmkfRebuildResult{Status: LmkfRebuildTimeout, LogPath: logPath}
	}
}

// lmkfLogPath returns the absolute .log path lmkf is watching for
// this .tex, or ok=false if no matching status file exists.
func lmkfLogPath(texPath string) (string, bool) {
	abs, err := filepath.Abs(texPath)
	if err != nil {
		return "", false
	}
	projectDir := filepath.Dir(abs)
	statusFile := filepath.Join("/tmp/lmkf-status", filepath.Base(projectDir))
	data, err := os.ReadFile(statusFile)
	if err != nil {
		return "", false
	}
	want := strings.TrimSuffix(abs, filepath.Ext(abs)) + ".log"
	got := strings.TrimSpace(string(data))
	if got != want {
		return "", false
	}
	return got, true
}

// LmkfWatching reports whether LP's lmkf shell function is already
// running latexmk -pvc on this .tex. When true, callers should skip
// invoking their own build — lmkf is already producing artefacts and
// a parallel latexmk would race on the build directory. Wraps
// lmkfLogPath so the cmd/ entry point can consult the same wire
// protocol the in-mreview reload pipeline uses.
func LmkfWatching(texPath string) bool {
	_, ok := lmkfLogPath(texPath)
	return ok
}

// latexmkCompleteMarker is the line latexmk prints at the end of every
// successful pdflatex pass. The menubar plugin at
// /Users/leo/menubar-plugins/lmkf-status.100ms.sh uses the same marker
// to distinguish "still compiling" from "finished".
const latexmkCompleteMarker = "Here is how much of TeX"

// waitForLmkfComplete polls the .log file until lmkf finishes a pass
// triggered by an edit made after `editTime`. Returns ("ok", "") on
// success, ("error", firstErrorLine) when the completed pass has a
// LaTeX error, or ("timeout", "") if the deadline expired (lmkf maybe
// not running fast enough, or not running at all). Polling the log is
// more reliable than comparing .pdf mtime — the PDF only updates on
// success, so failures would otherwise look like "stuck compiling"
// forever.
func waitForLmkfComplete(logPath string, editTime time.Time, timeout time.Duration) (string, string) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		st, err := os.Stat(logPath)
		if err == nil && !st.ModTime().Before(editTime) {
			if data, err := os.ReadFile(logPath); err == nil {
				if found := logContainsMarker(data); found {
					if errLine := firstLogError(data); errLine != "" {
						return "error", errLine
					}
					return "ok", ""
				}
			}
		}
		time.Sleep(300 * time.Millisecond)
	}
	return "timeout", ""
}

// logContainsMarker reports whether the latexmk completion marker
// appears in the last 8 KiB of the log. latexmk appends on each pass,
// so checking the tail keeps the read cheap even for multi-thousand-
// line logs.
func logContainsMarker(data []byte) bool {
	const tailBytes = 8 * 1024
	if len(data) > tailBytes {
		data = data[len(data)-tailBytes:]
	}
	return strings.Contains(string(data), latexmkCompleteMarker)
}

// waitForArtefactsFresh polls pdfPath and synctexPath until each one's
// mtime is at least as recent as editTime, or the timeout elapses. The
// log-marker poll proves lmkf finished a pass; the artefact poll proves
// the resulting files are visible to the next pdf.Open / synctex.Open.
// Falls through silently on timeout — the caller has its own staleness
// fallback when the opens don't produce a coherent pair.
func waitForArtefactsFresh(pdfPath, synctexPath string, editTime time.Time, timeout time.Duration) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		ps, perr := os.Stat(pdfPath)
		ss, serr := os.Stat(synctexPath)
		if perr == nil && serr == nil &&
			!ps.ModTime().Before(editTime) && !ss.ModTime().Before(editTime) {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// firstLogError surfaces the first TeX error or undefined-ref/citation
// warning from the log, delegating to build.ScanLogBytes so the lmkf
// path applies the same error policy as a direct build.RunWith call.
// Keeping the two scanners in sync matters because a "lmkf rebuild ok"
// message should never be shown for a state a manual rebuild would
// have rejected.
func firstLogError(data []byte) string {
	return build.ScanLogBytes(data)
}

// populateRegions mirrors cmd/mreview/main.go's populatePDFRegions but
// scoped to the ui package so reloadFromDisk doesn't reach into main.
// Fills Block.PDFRegion for every block whose SyncTeX entry can be
// located; skips the synthetic root and blocks without line ranges.
func populateRegions(doc *parser.Document, idx *synctex.Index) {
	if doc == nil || idx == nil {
		return
	}
	for _, b := range doc.Blocks {
		if b == doc.Root || b.StartLine == 0 {
			continue
		}
		file := b.File
		if file == "" {
			file = doc.File
		}
		r := idx.RegionForLines(file, b.StartLine, b.EndLine)
		if r == nil {
			continue
		}
		b.PDFRegion = &parser.Region{Page: r.Page, X: r.X, Y: r.Y, W: r.W, H: r.H}
	}
}
