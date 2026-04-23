package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"mreview/pkg/build"
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
}

// requestReload returns a tea.Cmd that posts a reloadMsg. Used by edit
// paths that don't go through tea.ExecProcess — they still want the same
// post-edit pipeline (reparse + rebuild + remap + cursor restore).
func requestReload(err error) tea.Cmd {
	return func() tea.Msg { return reloadMsg{err: err} }
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
	oldPDF := m.PDF
	buildCmd := ""
	if m.Config != nil {
		buildCmd = m.Config.BuildCmd
	}
	m.reloadGen++
	gen := m.reloadGen
	m.Status = "rebuilding…"

	cmd := func() tea.Msg {
		return performReload(path, gen, oldPDF, buildCmd)
	}
	return m, cmd
}

// applyReloadResult installs the outcome of startReload on the model.
// Old PDF handle closure happens inside performReload (well before this
// point) so there's no chance of closing a handle that's about to be
// used by a lingering PDF render goroutine.
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
		return m, nil
	}
	if r.newDoc != nil {
		m.Doc = r.newDoc
	}
	if r.newPDF != nil {
		m.PDF = r.newPDF
	}
	if r.newSyncTeX != nil {
		m.Synctex = r.newSyncTeX
	}

	// Remap the *live* sidecar against the new doc. Any annotations
	// the user added during the rebuild live in m.Sidecar.Annotations
	// and would not be in a snapshot taken at startReload time.
	if m.Sidecar != nil && m.Doc != nil {
		newSidecar, detached := persist.Remap(m.Sidecar, m.Doc)
		newSidecar.Detached = append(newSidecar.Detached, detached...)
		RefreshRemappedAnnotations(m.Doc, newSidecar)
		m.Sidecar = newSidecar
	}
	// Resolve the cursor against the *live* cursor too — if the user
	// navigated during the rebuild, m.CursorBlockID is what they
	// expect to stay on, not the snapshot.
	m.CursorBlockID = resolveReloadCursor(m.CursorBlockID, m.Doc, m.Sidecar)
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

// performReload is the goroutine body launched by startReload. Doing
// the full pipeline here (parse → build → reopen PDF+SyncTeX) keeps
// the Update loop responsive; we only touch the model through the
// reloadResultMsg it returns. Sidecar remap and cursor resolution are
// intentionally deferred to applyReloadResult so they see live state.
func performReload(path string, gen int, oldPDF *pdf.Doc, buildCmd string) reloadResultMsg {
	src, err := os.ReadFile(path)
	if err != nil {
		return reloadResultMsg{gen: gen, status: "reload: " + err.Error()}
	}
	newDoc, err := parser.Parse(src)
	if err != nil {
		return reloadResultMsg{gen: gen, status: "reload: parse: " + err.Error()}
	}
	newDoc.File = path

	buildRes := build.ResolveBuildOutputs(path)
	status := ""
	// buildStale flips to true whenever the reload can't deliver a
	// coherent (Doc, PDF, SyncTeX) triple back to the model. Sources:
	//
	//   - latexmk reported an error or lmkf returned error/timeout
	//     (the new artefacts on disk do not match newDoc);
	//   - both pdf.Open and synctex.Open are needed for a coherent
	//     pair, and a partial success would leave the model with new
	//     PDF + old SyncTeX or old PDF + new SyncTeX. Both opens must
	//     therefore succeed together; on partial success we close the
	//     orphan, keep the previous handles, and treat the reload as
	//     stale.
	//
	// Aux/bbl loads are gated on the build step alone (rebuildOK below)
	// — they only enrich newDoc and don't participate in the rendering
	// pair, so a mid-pair failure shouldn't suppress them.
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
		logPath, _ := lmkfLogPath(path)
		result, errLine := waitForLmkfComplete(logPath, editTime, 25*time.Second)
		switch result {
		case "ok":
			status = fmt.Sprintf("lmkf rebuild ok · %d blocks", len(newDoc.Blocks))
		case "error":
			status = "lmkf rebuild error — " + errLine
			rebuildOK = false
		default:
			status = "lmkf didn't finish in time (edit saved anyway)"
			rebuildOK = false
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
			status = fmt.Sprintf("rebuilt + reloaded · %d blocks", len(newDoc.Blocks))
		}
	default:
		status = fmt.Sprintf("reloaded · %d blocks", len(newDoc.Blocks))
	}

	if !rebuildOK {
		buildStale = true
	}

	// Aux/bbl enrich newDoc with theorem numbers and citation entries.
	// They're only safe to apply when the build that produced them
	// matches newDoc — i.e. when the current rebuild succeeded. On
	// failure we leave newDoc un-enriched (numbers blank) rather than
	// pulling stale numbers off disk.
	if rebuildOK {
		if auxEntries, err := parser.LoadAux(buildRes.AuxPath); err == nil {
			parser.ApplyAux(newDoc, auxEntries)
		}
		if bibEntries, err := parser.LoadBBL(buildRes.BBLPath); err == nil {
			parser.ApplyBBL(newDoc, bibEntries)
		}
	}

	// PDF + SyncTeX must update as a coherent pair. The render path
	// (renderPDFForBlock) calls Index.RegionForLines(file, doc.StartLine,
	// doc.EndLine) — feeding *new* doc line numbers into an *old* SyncTeX
	// index would silently produce wrong regions, and pairing a new PDF
	// with an old SyncTeX (or vice versa) breaks coherence in subtler
	// ways. So: only adopt the new pair when both opens succeed; on any
	// partial success, close the orphan handle, keep the previous
	// handles in the model (applyReloadResult is nil-preserving), and
	// flag the reload as stale so the next render is suppressed.
	var newPDF *pdf.Doc
	var newSyncTeX *synctex.Index
	if rebuildOK {
		pdfDoc, pdfErr := pdf.Open(buildRes.PDFPath)
		idx, sxErr := synctex.Open(buildRes.SyncTeXPath)
		if pdfErr == nil && sxErr == nil {
			newPDF = pdfDoc
			newSyncTeX = idx
			populateRegions(newDoc, idx)
			// Only close the old handle after the new pair is committed.
			if oldPDF != nil && oldPDF != newPDF {
				oldPDF.Close()
			}
		} else {
			if pdfDoc != nil {
				pdfDoc.Close()
			}
			// synctex.Index has no Close — it's an in-memory parsed
			// struct that the GC will reclaim once the local goes out
			// of scope.
			buildStale = true
		}
	}

	return reloadResultMsg{
		gen:        gen,
		newDoc:     newDoc,
		newPDF:     newPDF,
		newSyncTeX: newSyncTeX,
		status:     status,
		buildStale: buildStale,
	}
}

// resolveReloadCursor picks the cursor block for the post-reload model.
// Preference order:
//  1. The previous cursor if its ID still resolves in newDoc.
//  2. The previous cursor treated as a LaTeX label (remap rescue).
//  3. firstUnreviewedOrAny — mirrors ui.New's fallback so an edit that
//     deletes or splits the cursor block reopens on outstanding work
//     instead of bouncing back to the start of the document.
func resolveReloadCursor(oldCursor string, newDoc *parser.Document, newSidecar *persist.Sidecar) string {
	if newDoc == nil {
		return ""
	}
	if _, ok := newDoc.ByID[oldCursor]; ok {
		return oldCursor
	}
	if b, ok := newDoc.ByLabel[oldCursor]; ok {
		return b.ID
	}
	return firstUnreviewedOrAny(newDoc, newSidecar)
}

// cloneSidecar returns a shallow copy so the reload goroutine can
// operate on a snapshot while the user keeps typing notes in the live
// model. The slices still share backing storage, but we never mutate
// them in performReload.
func cloneSidecar(s *persist.Sidecar) *persist.Sidecar {
	if s == nil {
		return nil
	}
	out := *s
	out.Annotations = append([]persist.Annotation(nil), s.Annotations...)
	out.Detached = append([]persist.Annotation(nil), s.Detached...)
	out.Reviewed = append([]string(nil), s.Reviewed...)
	return &out
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

// lmkfActive reports whether LP's `lmkf` shell function (latexmk -pvc
// wrapper) is watching this particular .tex. lmkf writes
// /tmp/lmkf-status/<project-dirname> containing the absolute path to
// the .log file it's monitoring; we confirm both the file exists and
// the log path inside matches what we'd expect for our tex. This lets
// us skip our own latexmk invocation and instead wait for lmkf's
// continuous-build loop to regenerate the PDF.
func lmkfActive(texPath string) bool {
	_, ok := lmkfLogPath(texPath)
	return ok
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
				if bytes := logContainsMarker(data); bytes {
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
