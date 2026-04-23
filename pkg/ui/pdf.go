package ui

import (
	"container/list"
	"fmt"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"mreview/pkg/parser"
	"mreview/pkg/pdf"
	"mreview/pkg/synctex"
)

// pdfRenderDebounce is the delay between a cursor move and the PDF crop/render
// that follows it. Keeps us from rendering for every intermediate block
// traversed by a count-prefixed motion.
const pdfRenderDebounce = 30 * time.Millisecond

// pdfCropCacheMax bounds the UI-level crop cache (keyed by block + geometry).
const pdfCropCacheMax = 64

// pdfRenderMsg is emitted from a debounced Tick. A stale message — one whose
// Generation no longer matches the Model's current pdfGen — is dropped.
type pdfRenderMsg struct {
	Generation int
	Image      string
	Status     string
}

// pdfRenderInputs captures the data a Tick callback needs to compute a crop.
// Held by value so the async goroutine does not race the Update loop.
type pdfRenderInputs struct {
	Doc        *parser.Document
	BlockID    string
	PDF        *pdf.Doc
	Index      *synctex.Index
	WidthCells int
	HeightCells int
}

// schedulePDFRender bumps the generation counter and returns a Tick command
// that will produce a pdfRenderMsg after pdfRenderDebounce. The callback is
// a pure function of its captured inputs — no Model state leaks across
// goroutines.
func (m *Model) schedulePDFRender() tea.Cmd {
	if m.PDF == nil {
		return nil
	}
	if !m.KittyAvailable {
		// Terminal can't render kitty graphics — emitting the APC
		// sequences anyway would paint raw escape garbage on screen.
		// The pane shows a placeholder via pdfPaneBody instead.
		return nil
	}
	if m.BuildStale {
		// The current m.Doc was parsed from a freshly-edited .tex but
		// the rebuild that should have produced matching PDF + SyncTeX
		// failed (or the artefacts couldn't be paired). Auto rendering
		// would feed new line numbers into the old SyncTeX index, and
		// the resulting region is meaningless. Manual mode is page-
		// based and could in principle still render, but suppressing
		// it too keeps the contract simple — "stale build = no new
		// crops until the next successful rebuild".
		return nil
	}
	w, h := pdfPaneCells(m.Width, m.Height, m.Layout)
	if m.PDFManual {
		// Manual mode renders the current page directly — no SyncTeX
		// needed and no debounce, since user-driven page/zoom keys want
		// instant feedback.
		m.pdfGen++
		gen := m.pdfGen
		inputs := manualRenderInputs{
			Doc:         m.PDF,
			Page:        m.ManualPDFPage,
			Zoom:        m.ManualPDFZoom,
			WidthCells:  w,
			HeightCells: h,
			Dual:        m.ManualPDFDual,
			Dark:        m.ManualPDFDark,
			CropT:       m.ManualPDFCropT,
			CropB:       m.ManualPDFCropB,
			CropL:       m.ManualPDFCropL,
			CropR:       m.ManualPDFCropR,
		}
		return func() tea.Msg {
			img, status := renderManualPDF(inputs)
			return pdfRenderMsg{Generation: gen, Image: img, Status: status}
		}
	}
	if m.Synctex == nil {
		return nil
	}
	m.pdfGen++
	gen := m.pdfGen
	inputs := pdfRenderInputs{
		Doc:         m.Doc,
		BlockID:     m.CursorBlockID,
		PDF:         m.PDF,
		Index:       m.Synctex,
		WidthCells:  w,
		HeightCells: h,
	}
	cache := m.pdfCache
	return tea.Tick(pdfRenderDebounce, func(time.Time) tea.Msg {
		image, status := renderPDFForBlock(inputs, cache)
		return pdfRenderMsg{Generation: gen, Image: image, Status: status}
	})
}

// pdfPaneCells returns the inner (width, height) in terminal cells for the
// PDF pane at the given terminal dimensions. Mirrors renderPane's inset math:
// border eats 2 cells each axis, title eats one row, status bar one row.
func pdfPaneCells(termW, termH int, layout LayoutMode) (int, int) {
	if termW <= 0 || termH <= 0 {
		return 0, 0
	}
	paneH := termH - statusBarHeight
	if paneH < 1 {
		paneH = 1
	}
	var paneW, paneRows int
	switch layout {
	case LayoutStacked:
		_, paneW = stackedWidths(termW)
		_, paneRows = stackedHeights(paneH)
	default:
		_, _, paneW = paneWidths(termW)
		paneRows = paneH
	}
	innerW := paneW - 2
	if innerW < 1 {
		innerW = 1
	}
	innerH := paneRows - 2 - 1 // border top/bottom + title
	if innerH < 1 {
		innerH = 1
	}
	return innerW, innerH
}

// renderPDFForBlock does the actual crop+kitty-encode. Returns the escape
// string on success, or a status string on failure/no-region. Results are
// memoised in cache for cheap revisits.
func renderPDFForBlock(in pdfRenderInputs, cache *pdfCropCache) (string, string) {
	block := resolveBlock(in.Doc, in.BlockID)
	if block == nil {
		return "", "(no cursor)"
	}
	if block.StartLine == 0 {
		return "", pdf.NoRegionPlaceholder
	}
	file := block.File
	if file == "" && in.Doc != nil {
		file = in.Doc.File
	}
	region := in.Index.RegionForLines(file, block.StartLine, block.EndLine)
	if region == nil || !pdf.HasExtent(*region) {
		return "", pdf.NoRegionPlaceholder
	}
	key := pdfCropKey{
		BlockID: block.ID,
		Mtime:   in.PDF.Mtime().UnixNano(),
		Width:   in.WidthCells,
		Height:  in.HeightCells,
	}
	if cache != nil {
		if esc, ok := cache.get(key); ok {
			return esc, ""
		}
	}
	// Render the SyncTeX target in the flow of its surroundings instead of a
	// tight box. 80 PDF points is roughly an inch — about a paragraph above
	// and below for typical 11pt body text — which gives the reviewer enough
	// surrounding context to recognise the spot at a glance.
	png, err := pdf.CropWithContext(in.PDF, *region, 80)
	if err != nil {
		return "", fmt.Sprintf("pdf: %v", err)
	}
	esc, err := pdf.RenderKitty(png, in.WidthCells, in.HeightCells)
	if err != nil {
		return "", fmt.Sprintf("pdf: %v", err)
	}
	if cache != nil {
		cache.put(key, esc)
	}
	return esc, ""
}

// resolveBlock is a thin wrapper returning the Block for an ID or nil.
func resolveBlock(doc *parser.Document, id string) *parser.Block {
	if doc == nil || id == "" {
		return nil
	}
	return doc.ByID[id]
}

// pdfCropKey keys the per-block crop memo on (id, file mtime, geometry).
// mtime guarantees an outdated cache is flushed when latexmk rebuilds.
type pdfCropKey struct {
	BlockID string
	Mtime   int64
	Width   int
	Height  int
}

// pdfCropCache is a bounded LRU of rendered kitty escape strings. Concurrent
// tick goroutines (scheduled from rapid cursor moves) may read/write this
// cache in parallel, so the mutex is load-bearing — not just defensive.
type pdfCropCache struct {
	mu    sync.Mutex
	max   int
	ll    *list.List
	index map[pdfCropKey]*list.Element
}

type pdfCropEntry struct {
	key pdfCropKey
	esc string
}

// newPDFCropCache returns a cache bounded to max entries (fallback 1).
func newPDFCropCache(max int) *pdfCropCache {
	if max < 1 {
		max = 1
	}
	return &pdfCropCache{max: max, ll: list.New(), index: map[pdfCropKey]*list.Element{}}
}

func (c *pdfCropCache) get(k pdfCropKey) (string, bool) {
	if c == nil {
		return "", false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if e, ok := c.index[k]; ok {
		c.ll.MoveToFront(e)
		return e.Value.(pdfCropEntry).esc, true
	}
	return "", false
}

func (c *pdfCropCache) put(k pdfCropKey, esc string) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if e, ok := c.index[k]; ok {
		e.Value = pdfCropEntry{key: k, esc: esc}
		c.ll.MoveToFront(e)
		return
	}
	e := c.ll.PushFront(pdfCropEntry{key: k, esc: esc})
	c.index[k] = e
	for c.ll.Len() > c.max {
		tail := c.ll.Back()
		if tail == nil {
			break
		}
		c.ll.Remove(tail)
		delete(c.index, tail.Value.(pdfCropEntry).key)
	}
}

// pdfPaneBody picks what to draw inside the PDF pane: a live kitty escape
// when one is available, a status string when the render produced no image,
// or the fallback placeholder when neither is set yet.
//
// Transitions from image to text prepend a kitty-delete APC so any lingering
// bitmap is retired before the status paints. Without this the kitty plane
// (which is independent of Bubble Tea's text buffer) keeps showing the last
// image even though the rest of the pane now says "(no PDF region)".
func (m Model) pdfPaneBody() string {
	if !m.KittyAvailable {
		return "(PDF pane requires kitty or ghostty terminal)"
	}
	if m.PDFImage != "" {
		return m.PDFImage
	}
	if m.PDFStatus != "" {
		return pdf.KittyDeleteAll + m.PDFStatus
	}
	if m.Doc == nil || m.CursorBlockID == "" {
		return pdf.KittyDeleteAll + "(no PDF region)"
	}
	if m.PDF == nil && m.Synctex == nil {
		return pdf.KittyDeleteAll + "(no PDF loaded)"
	}
	return pdf.KittyDeleteAll + pdf.NoRegionPlaceholder
}
