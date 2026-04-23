package ui

import (
	"fmt"
	"os"

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
// reparsed document, freshly opened PDF / SyncTeX handles, remapped
// sidecar, restored cursor, and any status line the build step emitted.
// Running the heavy work off the Update goroutine keeps the UI
// responsive — the user sees a "rebuilding…" status instead of a frozen
// pane while latexmk churns.
type reloadResultMsg struct {
	newDoc     *parser.Document
	newSidecar *persist.Sidecar
	newPDF     *pdf.Doc
	newSyncTeX *synctex.Index
	newCursor  string
	status     string
}

// requestReload returns a tea.Cmd that posts a reloadMsg. Used by edit
// paths that don't go through tea.ExecProcess — they still want the same
// post-edit pipeline (reparse + rebuild + remap + cursor restore).
func requestReload(err error) tea.Cmd {
	return func() tea.Msg { return reloadMsg{err: err} }
}

// startReload sets a "rebuilding…" status and returns a tea.Cmd that
// performs the heavy work (parse, latexmk, PDF+SyncTeX reopen, sidecar
// remap) off the Update goroutine. When done it emits a reloadResultMsg
// that the Update handler applies to the model. Keeping the work off
// the main loop is important: latexmk on a real paper takes seconds and
// freezing the TUI that long makes it look like the edit had no effect.
func (m Model) startReload() (Model, tea.Cmd) {
	if m.Doc == nil || m.Doc.File == "" {
		m.Status = "reload: no source file"
		return m, nil
	}
	path := m.Doc.File
	oldSidecar := cloneSidecar(m.Sidecar)
	oldCursor := m.CursorBlockID
	oldPDF := m.PDF
	buildCmd := ""
	if m.Config != nil {
		buildCmd = m.Config.BuildCmd
	}
	m.Status = "rebuilding…"

	cmd := func() tea.Msg {
		return performReload(path, oldSidecar, oldCursor, oldPDF, buildCmd)
	}
	return m, cmd
}

// applyReloadResult installs the outcome of startReload on the model.
// Old PDF handle closure happens inside performReload (well before this
// point) so there's no chance of closing a handle that's about to be
// used by a lingering PDF render goroutine.
func (m Model) applyReloadResult(r reloadResultMsg) (Model, tea.Cmd) {
	if r.newDoc != nil {
		m.Doc = r.newDoc
	}
	if r.newSidecar != nil {
		m.Sidecar = r.newSidecar
	}
	if r.newPDF != nil {
		m.PDF = r.newPDF
	}
	m.Synctex = r.newSyncTeX
	if r.newCursor != "" {
		m.CursorBlockID = r.newCursor
	}
	m.SourceLineCursor = clampLineCursor(m.Doc, m.CursorBlockID, m.SourceLineCursor)
	m.pdfCache = newPDFCropCache(pdfCropCacheMax)
	m.PDFImage = ""
	if r.status != "" {
		m.Status = r.status
	} else if m.Doc != nil {
		m.Status = fmt.Sprintf("reloaded · %d blocks", len(m.Doc.Blocks))
	}
	return m, m.schedulePDFRender()
}

// performReload is the goroutine body launched by startReload. Doing
// the full pipeline here (parse → build → reopen PDF+SyncTeX → remap)
// keeps the Update loop responsive; we only touch the model through
// the reloadResultMsg it returns.
func performReload(path string, oldSidecar *persist.Sidecar, oldCursor string, oldPDF *pdf.Doc, buildCmd string) reloadResultMsg {
	src, err := os.ReadFile(path)
	if err != nil {
		return reloadResultMsg{status: "reload: " + err.Error()}
	}
	newDoc, err := parser.Parse(src)
	if err != nil {
		return reloadResultMsg{status: "reload: parse: " + err.Error()}
	}
	newDoc.File = path

	buildRes := build.ResolveBuildOutputs(path)
	status := ""
	if shouldRebuild(path, buildRes.PDFPath) {
		res, berr := build.RunWith(build.Options{
			TexPath:  path,
			BuildCmd: buildCmd,
		})
		if berr != nil {
			status = "rebuild failed — " + shortBuildErr(berr)
		} else {
			buildRes = res
			status = fmt.Sprintf("rebuilt + reloaded · %d blocks", len(newDoc.Blocks))
		}
	} else {
		status = fmt.Sprintf("reloaded · %d blocks", len(newDoc.Blocks))
	}

	if auxEntries, err := parser.LoadAux(buildRes.AuxPath); err == nil {
		parser.ApplyAux(newDoc, auxEntries)
	}
	if bibEntries, err := parser.LoadBBL(buildRes.BBLPath); err == nil {
		parser.ApplyBBL(newDoc, bibEntries)
	}

	newSidecar, detached := persist.Remap(oldSidecar, newDoc)
	if oldSidecar != nil {
		newSidecar.Detached = append(newSidecar.Detached, oldSidecar.Detached...)
	}
	newSidecar.Detached = append(newSidecar.Detached, detached...)

	var newPDF *pdf.Doc
	if pdfDoc, err := pdf.Open(buildRes.PDFPath); err == nil {
		newPDF = pdfDoc
	}
	// Close the old handle *after* opening the new one so a failure to
	// reopen (e.g. PDF was deleted) doesn't leave us with no pane.
	if oldPDF != nil && oldPDF != newPDF {
		oldPDF.Close()
	}

	var newSyncTeX *synctex.Index
	if idx, err := synctex.Open(buildRes.SyncTeXPath); err == nil {
		newSyncTeX = idx
		populateRegions(newDoc, idx)
	}

	newCursor := oldCursor
	if _, ok := newDoc.ByID[oldCursor]; !ok {
		if b, ok := newDoc.ByLabel[oldCursor]; ok {
			newCursor = b.ID
		} else {
			newCursor = firstContentBlockID(newDoc)
		}
	}

	return reloadResultMsg{
		newDoc:     newDoc,
		newSidecar: newSidecar,
		newPDF:     newPDF,
		newSyncTeX: newSyncTeX,
		newCursor:  newCursor,
		status:     status,
	}
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
