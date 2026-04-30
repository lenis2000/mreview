package ui

import (
	"os"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"mreview/pkg/build"
	"mreview/pkg/pdf"
	"mreview/pkg/synctex"
)

// pdfWatchInterval is the cadence of the on-disk PDF/SyncTeX poll. One
// second matches sourceWatchInterval and is plenty fine-grained for an
// external rebuild that already takes seconds to produce new artefacts.
const pdfWatchInterval = 1 * time.Second

// tickPDFWatchMsg fires every pdfWatchInterval to drive the PDF/SyncTeX
// freshness check. Distinct from tickSourceWatchMsg so the two pollers
// can have independent cadences if we tune them later.
type tickPDFWatchMsg struct{}

// tickPDFWatch is the recurring command. The handler reschedules itself
// on every cycle so the watcher self-perpetuates for the lifetime of
// the program.
func tickPDFWatch() tea.Cmd {
	return tea.Tick(pdfWatchInterval, func(time.Time) tea.Msg {
		return tickPDFWatchMsg{}
	})
}

// pdfWatchResultMsg carries the outcome of an asynchronous reopen
// triggered by handlePDFWatch. Mirrors reloadResultMsg but never touches
// Doc — only the PDF + SyncTeX leg refreshes here.
type pdfWatchResultMsg struct {
	// gen carries the pdfWatchGen captured by handlePDFWatch. Stale
	// results (a newer watch tick has already fired) are dropped on
	// apply.
	gen int
	// newPDF / newSyncTeX are nil on open failure; applyPDFWatchResult
	// then leaves the previous handles in place.
	newPDF       *pdf.Doc
	newSyncTeX   *synctex.Index
	syncTeXMtime time.Time
	// oldPDF is the handle that was live when handlePDFWatch fired; the
	// apply step closes it after the new handle is committed onto the
	// model. Without this hand-off, a goroutine closing the old handle
	// while a newer watch is in flight could leave the model pointing
	// at a closed handle.
	oldPDF *pdf.Doc
}

// handlePDFWatch is the body of the pdf-watch tick. Stats the on-disk
// PDF + SyncTeX pair; when either has advanced past the recorded
// baselines (m.PDF.Mtime() / m.SyncTeXMtime) it kicks off an async
// reopen and posts pdfWatchResultMsg. Always reschedules the next tick.
//
// Independent of the source-watch path so an external rebuild that
// happens without a source edit (a clean rebuild, an aux/bib refresh,
// or a lmkf pass that mreview's own log-marker poll missed) still
// lands in the pane.
//
// Conservative early-outs match handleSourceWatch: skip when a popup
// or pending action is open so a mid-keystroke swap doesn't yank the
// cursor's region out from under the user.
func (m Model) handlePDFWatch() (Model, tea.Cmd) {
	if !autoReloadSourceEnabled(m.Config) {
		return m, nil
	}
	next := tickPDFWatch()
	if m.Popup != nil || m.Pending != nil {
		return m, next
	}
	if m.PDF == nil || m.Doc == nil || m.Doc.File == "" {
		return m, next
	}
	buildRes := build.ResolveBuildOutputsOnDisk(m.Doc.File)
	pdfPath := buildRes.PDFPath
	sxPath := buildRes.SyncTeXPath
	pdfStat, err := os.Stat(pdfPath)
	if err != nil {
		return m, next
	}
	sxStat, err := os.Stat(sxPath)
	if err != nil {
		return m, next
	}
	pdfChanged := pdfStat.ModTime().After(m.PDF.Mtime())
	sxChanged := sxStat.ModTime().After(m.SyncTeXMtime)
	if !pdfChanged && !sxChanged {
		return m, next
	}
	// A reload from the source-watch leg is in flight (phase 1 has
	// installed the new doc, phase 2 is still waiting on lmkf). Let
	// applyReloadResult own the artefact swap so we don't fight over
	// the (PDF, SyncTeX) pair. The next pdf-watch tick will re-check
	// once that reload has either landed or failed.
	if m.BuildStale {
		return m, next
	}
	m.pdfWatchGen++
	gen := m.pdfWatchGen
	oldPDF := m.PDF
	return m, tea.Batch(func() tea.Msg {
		return performPDFWatchReopen(pdfPath, sxPath, gen, oldPDF)
	}, next)
}

// performPDFWatchReopen is the goroutine body for handlePDFWatch. Opens
// PDF + SyncTeX as a coherent pair: on partial success the orphan is
// closed and both nils are returned so applyPDFWatchResult keeps the
// previous handles in place. Done off the Update goroutine because
// fitz.New + synctex.Parse can take tens of milliseconds on a real
// paper and freezing the TUI that long is the same UX problem the
// source-watch goroutine solves.
func performPDFWatchReopen(pdfPath, sxPath string, gen int, oldPDF *pdf.Doc) pdfWatchResultMsg {
	newPDF, pdfErr := pdf.Open(pdfPath)
	newSx, sxErr := synctex.Open(sxPath)
	if pdfErr != nil || sxErr != nil {
		if newPDF != nil {
			newPDF.Close()
		}
		// synctex.Index has no Close — GC reclaims it once the local
		// goes out of scope.
		return pdfWatchResultMsg{gen: gen, oldPDF: oldPDF}
	}
	var sxMtime time.Time
	if st, err := os.Stat(sxPath); err == nil {
		sxMtime = st.ModTime()
	}
	return pdfWatchResultMsg{
		gen:          gen,
		newPDF:       newPDF,
		newSyncTeX:   newSx,
		syncTeXMtime: sxMtime,
		oldPDF:       oldPDF,
	}
}

// applyPDFWatchResult swaps the freshly opened PDF + SyncTeX onto the
// live model, repopulates Block.PDFRegion against the new index,
// flushes the crop cache, clears BuildStale (the new pair is by
// construction coherent with m.Doc — phase 2 of any in-flight source
// reload would have set BuildStale=true and we'd have early-outed),
// and schedules a render. Stale messages (gen != m.pdfWatchGen) are
// dropped to handle two pdf-watch reopens running close together.
func (m Model) applyPDFWatchResult(r pdfWatchResultMsg) (Model, tea.Cmd) {
	if r.gen != m.pdfWatchGen {
		// A newer pdf-watch has already fired and committed its
		// result. Close the orphan handle from this stale reopen so
		// it doesn't leak.
		if r.newPDF != nil {
			r.newPDF.Close()
		}
		return m, nil
	}
	if r.newPDF == nil || r.newSyncTeX == nil {
		// Open failed; previous handles stay live. Don't surface a
		// status — a transient half-written PDF (latexmk mid-rename)
		// will resolve on the next tick.
		return m, nil
	}
	if r.oldPDF != nil && r.oldPDF != r.newPDF {
		r.oldPDF.Close()
	}
	m.PDF = r.newPDF
	m.Synctex = r.newSyncTeX
	m.SyncTeXMtime = r.syncTeXMtime
	populateRegions(m.Doc, r.newSyncTeX)
	m.pdfCache = newPDFCropCache(pdfCropCacheMax)
	m.BuildStale = false
	m.Status = "pdf reloaded"
	return m, m.schedulePDFRender()
}
