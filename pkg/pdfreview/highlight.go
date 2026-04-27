package pdfreview

import (
	"image"
	"image/color"
)

// DefaultHighlight is a translucent yellow that reads on white pages
// without obscuring underlying text.
var DefaultHighlight = color.RGBA{R: 255, G: 230, B: 0, A: 96}

// DrawHighlights alpha-blends a translucent fill into img at each rect.
// Rectangles are in PDF points; dpi controls the conversion to pixels.
// Origin is top-left in both pdftotext-bbox-layout and image.RGBA, so
// no axis flip is needed.
func DrawHighlights(img *image.RGBA, rects []PageRect, dpi float64, fill color.RGBA) {
	if img == nil || len(rects) == 0 || dpi <= 0 {
		return
	}
	scale := dpi / 72.0
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
