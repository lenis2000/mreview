package ui

import (
	"sort"
	"sync"

	"mreview/pkg/parser"
	"mreview/pkg/pdf"
)

// pageLayoutCache memoizes per-page column detection. The decision is
// computed once per page (per PDF mtime) by taking the median width of
// all SyncTeX-mapped block regions on that page; if the median spans
// less than ~55% of the page, the page is treated as multi-column.
//
// The cache lives on the model because it needs both the parsed doc
// (for block iteration) and the open PDF (for page bounds). Concurrent
// reads happen from PDF render goroutines, so the mutex is load-bearing.
type pageLayoutCache struct {
	mu      sync.Mutex
	entries map[pageLayoutKey]bool
}

type pageLayoutKey struct {
	mtime int64
	page  int
}

func newPageLayoutCache() *pageLayoutCache {
	return &pageLayoutCache{entries: map[pageLayoutKey]bool{}}
}

// IsMultiColumn returns true when the page's block widths suggest a
// multi-column layout. Per-region width detection (the previous approach)
// trips on a single-column paper containing a narrow inline equation;
// per-page detection looks at the page's overall structure instead.
//
// Returns false when there's not enough data (< 3 mapped blocks on the
// page) — single-column is the safe default because the column-crop
// horizontal slicing is destructive.
func (c *pageLayoutCache) IsMultiColumn(d *pdf.Doc, doc *parser.Document, page int) bool {
	if c == nil || d == nil || doc == nil || page < 1 {
		return false
	}
	key := pageLayoutKey{mtime: d.Mtime().UnixNano(), page: page}

	c.mu.Lock()
	if v, ok := c.entries[key]; ok {
		c.mu.Unlock()
		return v
	}
	c.mu.Unlock()

	bounds, err := d.Bounds(page - 1)
	if err != nil {
		return false
	}
	pageW := float64(bounds.Dx())
	if pageW < 1 {
		return false
	}

	var widths []float64
	for _, b := range doc.Blocks {
		if b.PDFRegion == nil || b.PDFRegion.Page != page {
			continue
		}
		if b.PDFRegion.W <= 0 {
			continue
		}
		widths = append(widths, b.PDFRegion.W)
	}

	isMulti := false
	if len(widths) >= 3 {
		sort.Float64s(widths)
		median := widths[len(widths)/2]
		// Same threshold as the previous per-region heuristic (region
		// width × 2 < page width × 1.1) so single-column papers don't
		// suddenly start showing column crops.
		isMulti = median*2 < pageW*1.1
	}

	c.mu.Lock()
	c.entries[key] = isMulti
	c.mu.Unlock()
	return isMulti
}

// Invalidate clears the cache; called on reload so a rebuilt PDF
// re-detects layout (mtime change would already trigger a fresh
// computation, but explicit invalidation costs nothing and keeps the
// map from accumulating stale mtimes).
func (c *pageLayoutCache) Invalidate() {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.entries = map[pageLayoutKey]bool{}
	c.mu.Unlock()
}
