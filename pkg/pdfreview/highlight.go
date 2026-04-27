package pdfreview

import (
	"image"
	"image/color"
)

// DefaultHighlight is the translucent yellow fill drawn over each
// matched word run. Roughly 60% opacity reads strongly on a white page
// while still leaving the glyphs legible underneath — the CLI-search
// "yellow marker" look. Pair with DefaultHighlightBorder for the hard
// outline that makes it pop on dense pages.
var DefaultHighlight = color.RGBA{R: 255, G: 220, B: 0, A: 150}

// DefaultHighlightBorder is the saturated orange/red rectangle border
// drawn around each highlight rect. Solid (alpha 255) and 2 pixels wide,
// it pulls the user's eye to the match the way a search-result box does
// in a CLI viewer or PDF reader.
var DefaultHighlightBorder = color.RGBA{R: 230, G: 60, B: 0, A: 255}

// ContextHighlight is the faint fill drawn over the broader Quote
// when a narrower QuoteFocus is also being highlighted. ~20% opacity so
// it reads as "this is the surrounding context" without competing with
// the strong focus marker on top.
var ContextHighlight = color.RGBA{R: 255, G: 230, B: 0, A: 60}

// DrawHighlightsFill is DrawHighlights without the border — used for
// the faint "context" tier behind a narrower QuoteFocus.
func DrawHighlightsFill(img *image.RGBA, rects []PageRect, dpi float64, fill color.RGBA) {
	if img == nil || len(rects) == 0 || dpi <= 0 {
		return
	}
	scale := dpi / 72.0
	for _, r := range rects {
		px := image.Rect(
			int(r.XMin*scale)-1,
			int(r.YMin*scale)-1,
			int(r.XMax*scale)+1,
			int(r.YMax*scale)+1,
		)
		drawRectAlpha(img, px, fill)
	}
}

// DrawHighlights alpha-blends a translucent fill plus a solid border
// into img at each rect. Rectangles are in PDF points; dpi controls the
// conversion to pixels. Origin is top-left in both pdftotext-bbox-layout
// and image.RGBA, so no axis flip is needed.
func DrawHighlights(img *image.RGBA, rects []PageRect, dpi float64, fill color.RGBA) {
	if img == nil || len(rects) == 0 || dpi <= 0 {
		return
	}
	scale := dpi / 72.0
	// Border thickness scales with DPI so it stays visible on Retina
	// renders without dominating low-DPI ones. 1 px at 100 DPI, 3 px at
	// 300 DPI.
	border := int(scale + 0.5)
	if border < 1 {
		border = 1
	}
	if border > 4 {
		border = 4
	}
	for _, r := range rects {
		// Pad ~1pt around each rect so the highlight sits comfortably above
		// ascenders/descenders rather than clipping them.
		px := image.Rect(
			int(r.XMin*scale)-1,
			int(r.YMin*scale)-1,
			int(r.XMax*scale)+1,
			int(r.YMax*scale)+1,
		)
		drawRectAlpha(img, px, fill)
		drawRectBorder(img, px, border, DefaultHighlightBorder)
	}
}

// drawRectBorder paints a solid border of thickness px on the inside of r
// (so the border doesn't extend outside the highlight bounds).
func drawRectBorder(img *image.RGBA, r image.Rectangle, thickness int, fill color.RGBA) {
	if thickness <= 0 {
		return
	}
	r = r.Intersect(img.Bounds())
	if r.Empty() {
		return
	}
	top := image.Rect(r.Min.X, r.Min.Y, r.Max.X, r.Min.Y+thickness)
	bot := image.Rect(r.Min.X, r.Max.Y-thickness, r.Max.X, r.Max.Y)
	left := image.Rect(r.Min.X, r.Min.Y, r.Min.X+thickness, r.Max.Y)
	right := image.Rect(r.Max.X-thickness, r.Min.Y, r.Max.X, r.Max.Y)
	for _, e := range [...]image.Rectangle{top, bot, left, right} {
		drawRectAlpha(img, e, fill)
	}
}

// drawRectAlpha blends fill (with its alpha as the blend weight) into the
// pixels of img inside r. Straight-alpha src-over compositing.
func drawRectAlpha(img *image.RGBA, r image.Rectangle, fill color.RGBA) {
	r = r.Intersect(img.Bounds())
	if r.Empty() {
		return
	}
	a := uint32(fill.A)
	sr := uint32(fill.R) * a / 255
	sg := uint32(fill.G) * a / 255
	sb := uint32(fill.B) * a / 255
	inv := 255 - a
	stride := img.Stride
	for y := r.Min.Y; y < r.Max.Y; y++ {
		off := (y-img.Rect.Min.Y)*stride + (r.Min.X-img.Rect.Min.X)*4
		for x := r.Min.X; x < r.Max.X; x++ {
			img.Pix[off] = uint8((uint32(img.Pix[off])*inv + sr*255) / 255)
			img.Pix[off+1] = uint8((uint32(img.Pix[off+1])*inv + sg*255) / 255)
			img.Pix[off+2] = uint8((uint32(img.Pix[off+2])*inv + sb*255) / 255)
			off += 4
		}
	}
}
