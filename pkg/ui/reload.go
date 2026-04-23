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

// requestReload returns a tea.Cmd that posts a reloadMsg. Used by edit
// paths that don't go through tea.ExecProcess — they still want the same
// post-edit pipeline (reparse + rebuild + remap + cursor restore).
func requestReload(err error) tea.Cmd {
	return func() tea.Msg { return reloadMsg{err: err} }
}

// reloadFromDisk runs the full post-edit pipeline:
//  1. Re-read the .tex from disk.
//  2. Parse — the parser's segmentation passes run automatically.
//  3. Rebuild (latexmk) only when the .tex is newer than the .pdf, so
//     editing prose without touching anything that changes layout doesn't
//     pay the rebuild cost unnecessarily. Build failures post a status
//     line but don't block the reload — we can still render the stale
//     PDF and the new source.
//  4. Re-load .aux + .bbl, reopen PDF + SyncTeX, re-populate block
//     regions so cursor-following PDF crops follow the new line
//     numbers.
//  5. Remap the in-memory sidecar by block ID so annotations track the
//     edit (falling back to block-level when a line-pinned offset no
//     longer fits).
//  6. Restore the cursor to the same block ID when it still exists in
//     the reparsed doc; otherwise fall back to the first content block.
func (m Model) reloadFromDisk() (Model, tea.Cmd) {
	if m.Doc == nil || m.Doc.File == "" {
		m.Status = "reload: no source file"
		return m, nil
	}
	src, err := os.ReadFile(m.Doc.File)
	if err != nil {
		m.Status = "reload: " + err.Error()
		return m, nil
	}
	newDoc, err := parser.Parse(src)
	if err != nil {
		m.Status = "reload: parse: " + err.Error()
		return m, nil
	}
	newDoc.File = m.Doc.File

	buildRes := build.ResolveBuildOutputs(m.Doc.File)
	if shouldRebuild(m.Doc.File, buildRes.PDFPath) {
		buildCmd := ""
		if m.Config != nil {
			buildCmd = m.Config.BuildCmd
		}
		res, berr := build.RunWith(build.Options{
			TexPath:  m.Doc.File,
			BuildCmd: buildCmd,
		})
		if berr != nil {
			m.Status = "reload: build failed — " + shortBuildErr(berr)
		} else {
			buildRes = res
		}
	}

	if auxEntries, err := parser.LoadAux(buildRes.AuxPath); err == nil {
		parser.ApplyAux(newDoc, auxEntries)
	}
	if bibEntries, err := parser.LoadBBL(buildRes.BBLPath); err == nil {
		parser.ApplyBBL(newDoc, bibEntries)
	}

	// Remap sidecar against the new document. Detached annotations
	// accumulate across reloads so the user can recover anything that
	// drifted off-anchor.
	newSidecar, detached := persist.Remap(m.Sidecar, newDoc)
	newSidecar.Detached = append(newSidecar.Detached, m.Sidecar.Detached...)
	newSidecar.Detached = append(newSidecar.Detached, detached...)

	if m.PDF != nil {
		m.PDF.Close()
		m.PDF = nil
	}
	if pdfDoc, err := pdf.Open(buildRes.PDFPath); err == nil {
		m.PDF = pdfDoc
	}
	if idx, err := synctex.Open(buildRes.SyncTeXPath); err == nil {
		m.Synctex = idx
		populateRegions(newDoc, idx)
	} else {
		m.Synctex = nil
	}

	oldCursor := m.CursorBlockID
	if _, ok := newDoc.ByID[oldCursor]; !ok {
		if b, ok := newDoc.ByLabel[oldCursor]; ok {
			m.CursorBlockID = b.ID
		} else {
			m.CursorBlockID = firstContentBlockID(newDoc)
		}
	}

	m.Doc = newDoc
	m.Sidecar = newSidecar
	m.SourceLineCursor = clampLineCursor(m.Doc, m.CursorBlockID, m.SourceLineCursor)
	m.pdfCache = newPDFCropCache(pdfCropCacheMax)
	m.PDFImage = ""
	if m.Status == "" {
		m.Status = fmt.Sprintf("reloaded · %d blocks", len(newDoc.Blocks))
	}
	return m, m.schedulePDFRender()
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
