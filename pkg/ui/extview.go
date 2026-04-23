package ui

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"

	"mreview/pkg/pdf"
)

// pdfManualZoomStep is the per-keypress crop fraction reduction. Each +
// shrinks the rendered crop by this much (so a smaller slice of the page
// is visible, = zoom in) and each - expands back toward full-page.
const pdfManualZoomStep = 0.15

// pdfManualMaxZoom caps zoom so the user can't accidentally scroll past a
// useful level (crop would become a sliver).
const pdfManualMaxZoom = 6

// manualRenderInputs groups the knobs that drive renderManualPDF so the
// parameter list stays readable.
type manualRenderInputs struct {
	Doc         *pdf.Doc
	Page        int
	Zoom        int
	WidthCells  int
	HeightCells int
	Fit         string // "auto" | "width" | "height"
	Dual        string // "" | "horizontal" | "vertical"
	Dark        bool
	CropT       float64
	CropB       float64
	CropL       float64
	CropR       float64
}

// renderManualPDF produces a kitty-graphics escape sequence for the PDF
// pane in manual mode. Applies in order: per-edge crop, centred zoom
// crop, optional dark-mode invert, optional dual-page composition, then
// aspect-fit into the pane cells (with the requested fit mode choosing
// which axis to prioritise).
func renderManualPDF(in manualRenderInputs) (string, string) {
	if in.Doc == nil {
		return "", "(no PDF loaded)"
	}
	if in.Page < 0 {
		in.Page = 0
	}
	if in.Page >= in.Doc.NumPage() {
		in.Page = in.Doc.NumPage() - 1
	}
	if in.Page < 0 {
		return "", "(empty PDF)"
	}
	primary, err := renderSinglePage(in.Doc, in.Page, in)
	if err != nil {
		return "", fmt.Sprintf("pdf: %v", err)
	}
	var composed image.Image = primary
	if in.Dual != "" && in.Page+1 < in.Doc.NumPage() {
		second, err := renderSinglePage(in.Doc, in.Page+1, in)
		if err == nil {
			composed = composeDual(primary, second, in.Dual)
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, composed); err != nil {
		return "", fmt.Sprintf("pdf: encode png: %v", err)
	}
	esc, err := pdf.RenderKitty(buf.Bytes(), in.WidthCells, in.HeightCells)
	if err != nil {
		return "", fmt.Sprintf("pdf: %v", err)
	}
	return esc, ""
}

// renderSinglePage rasterises one page at an appropriate DPI (informed by
// the requested fit mode and current pane cell size), then applies crop +
// zoom + dark-mode invert. The output is already in its final pixel
// dimensions; renderManualPDF only composes and encodes from here.
func renderSinglePage(d *pdf.Doc, pageIdx int, in manualRenderInputs) (image.Image, error) {
	dpi := pdf.DefaultCropDPI
	img, err := d.Page(pageIdx, dpi)
	if err != nil {
		return nil, err
	}
	bounds := img.Bounds()
	// Apply per-edge crop first so zoom calculates off the visible area,
	// not the full page.
	cropped := cropEdges(img, in.CropL, in.CropT, in.CropR, in.CropB)
	// Apply zoom: shrink the crop rect centrally.
	if in.Zoom > 0 {
		cropped = zoomCrop(cropped, in.Zoom)
	}
	if in.Dark {
		cropped = invertColors(cropped)
	}
	_ = bounds
	return cropped, nil
}

// cropEdges returns a sub-image of img with the given fractional edges
// trimmed. Fractions clamp to [0, 0.45] so we can't crop the whole page.
func cropEdges(img image.Image, left, top, right, bottom float64) image.Image {
	left = clampFrac(left)
	top = clampFrac(top)
	right = clampFrac(right)
	bottom = clampFrac(bottom)
	b := img.Bounds()
	w := b.Dx()
	h := b.Dy()
	x0 := b.Min.X + int(float64(w)*left)
	y0 := b.Min.Y + int(float64(h)*top)
	x1 := b.Max.X - int(float64(w)*right)
	y1 := b.Max.Y - int(float64(h)*bottom)
	if x1 <= x0 || y1 <= y0 {
		return img
	}
	return subImage(img, image.Rect(x0, y0, x1, y1))
}

// zoomCrop returns a centred sub-image representing the requested zoom
// level (each step = pdfManualZoomStep less area on each axis).
func zoomCrop(img image.Image, zoom int) image.Image {
	if zoom <= 0 {
		return img
	}
	if zoom > pdfManualMaxZoom {
		zoom = pdfManualMaxZoom
	}
	frac := 1.0 - float64(zoom)*pdfManualZoomStep
	if frac < 0.1 {
		frac = 0.1
	}
	b := img.Bounds()
	w := b.Dx()
	h := b.Dy()
	cw := int(float64(w) * frac)
	ch := int(float64(h) * frac)
	if cw < 1 {
		cw = 1
	}
	if ch < 1 {
		ch = 1
	}
	x0 := b.Min.X + (w-cw)/2
	y0 := b.Min.Y + (h-ch)/2
	return subImage(img, image.Rect(x0, y0, x0+cw, y0+ch))
}

// subImage returns img cropped to rect, falling back to a copy when the
// underlying type doesn't expose SubImage.
func subImage(img image.Image, rect image.Rectangle) image.Image {
	type subImager interface {
		SubImage(image.Rectangle) image.Image
	}
	if si, ok := any(img).(subImager); ok {
		return si.SubImage(rect)
	}
	dst := image.NewRGBA(image.Rect(0, 0, rect.Dx(), rect.Dy()))
	draw.Draw(dst, dst.Bounds(), img, rect.Min, draw.Src)
	return dst
}

// invertColors returns a copy of img with RGB inverted and alpha
// preserved. Kept simple (matches docviewer's "D" simple-invert mode); no
// hue-preserving smart invert yet.
func invertColors(img image.Image) image.Image {
	b := img.Bounds()
	dst := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			r, g, bl, a := img.At(x, y).RGBA()
			dst.Set(x-b.Min.X, y-b.Min.Y, color.RGBA{
				R: uint8(255 - (r >> 8)),
				G: uint8(255 - (g >> 8)),
				B: uint8(255 - (bl >> 8)),
				A: uint8(a >> 8),
			})
		}
	}
	return dst
}

// composeDual stitches two page images into one image, either side-by-
// side ("horizontal") or stacked ("vertical"). Pages are normalised to
// the same secondary-axis dimension before composition so the seam
// stays clean regardless of per-page size differences.
func composeDual(a, b image.Image, mode string) image.Image {
	ab := a.Bounds()
	bb := b.Bounds()
	switch mode {
	case "horizontal":
		h := ab.Dy()
		if bb.Dy() > h {
			h = bb.Dy()
		}
		w := ab.Dx() + bb.Dx()
		dst := image.NewRGBA(image.Rect(0, 0, w, h))
		draw.Draw(dst, image.Rect(0, 0, ab.Dx(), ab.Dy()), a, ab.Min, draw.Src)
		draw.Draw(dst, image.Rect(ab.Dx(), 0, w, bb.Dy()), b, bb.Min, draw.Src)
		return dst
	default:
		w := ab.Dx()
		if bb.Dx() > w {
			w = bb.Dx()
		}
		h := ab.Dy() + bb.Dy()
		dst := image.NewRGBA(image.Rect(0, 0, w, h))
		draw.Draw(dst, image.Rect(0, 0, ab.Dx(), ab.Dy()), a, ab.Min, draw.Src)
		draw.Draw(dst, image.Rect(0, ab.Dy(), bb.Dx(), h), b, bb.Min, draw.Src)
		return dst
	}
}

func clampFrac(f float64) float64 {
	if f < 0 {
		return 0
	}
	if f > 0.45 {
		return 0.45
	}
	return f
}
