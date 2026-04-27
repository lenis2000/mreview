package pdfreview

import (
	"bytes"
	"fmt"
	"image"
	"image/png"

	"mreview/pkg/pdf"
)

// renderPageEscape produces a kitty escape string for the given page with
// a translucent yellow overlay covering each rect. paneWcells/paneHcells
// are the target terminal cell counts. Caller is expected to memoise.
//
// Two-tier highlighting: when focus is non-empty AND found on the page,
// the broad quote (if any) is drawn with a faint context fill and the
// focus run is drawn with the strong fill+border. When only quote is
// present, it gets the strong treatment by itself.
//
// On bbox failure (pdftotext crash, no match for the quote) the page is
// rendered without overlay; the caller decides whether to surface a
// "anchor approximate" warning. We never fail the render itself for a
// missing highlight — page-only navigation is the floor.
func renderPageEscape(doc *pdf.Doc, bbox *BBoxCache, page int, quote, focus string, dpi float64, paneWcells, paneHcells int) (string, bool, error) {
	if page < 1 || page > doc.NumPage() {
		return "", false, fmt.Errorf("page %d out of range (1..%d)", page, doc.NumPage())
	}
	src, err := doc.Page(page-1, dpi)
	if err != nil {
		return "", false, err
	}
	// Defensive copy: doc.Page returns a cached pixmap; mutating it would
	// poison the cache for subsequent renders.
	out := image.NewRGBA(src.Rect)
	copy(out.Pix, src.Pix)

	highlighted := false
	var quoteRects, focusRects []PageRect
	if quote != "" && bbox != nil {
		if rs, ok := bbox.FindQuote(page, quote); ok {
			quoteRects = rs
		}
	}
	if focus != "" && bbox != nil {
		if rs, ok := bbox.FindQuote(page, focus); ok {
			focusRects = rs
		}
	}
	switch {
	case len(focusRects) > 0 && len(quoteRects) > 0:
		// Faint context behind, strong focus on top.
		DrawHighlightsFill(out, quoteRects, dpi, ContextHighlight)
		DrawHighlights(out, focusRects, dpi, DefaultHighlight)
		highlighted = true
	case len(focusRects) > 0:
		DrawHighlights(out, focusRects, dpi, DefaultHighlight)
		highlighted = true
	case len(quoteRects) > 0:
		DrawHighlights(out, quoteRects, dpi, DefaultHighlight)
		highlighted = true
	}

	var buf bytes.Buffer
	if err := png.Encode(&buf, out); err != nil {
		return "", false, fmt.Errorf("png encode: %w", err)
	}
	esc, err := pdf.RenderKitty(buf.Bytes(), paneWcells, paneHcells)
	if err != nil {
		return "", false, fmt.Errorf("render kitty: %w", err)
	}
	return esc, highlighted, nil
}
