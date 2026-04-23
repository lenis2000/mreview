package ui

import (
	"bytes"
	"fmt"
	"image"
	"image/draw"
	"image/png"

	"mreview/pkg/pdf"
)

// pdfManualZoomStep is the per-keypress crop fraction reduction. Each +
// shrinks the rendered crop by this much (so 1/(1-z*step) of the page is
// visible) and each - expands it back toward full-page.
const pdfManualZoomStep = 0.15

// pdfManualMaxZoom caps the zoom level at ~85% crop so the user can't
// accidentally zoom past a useful level.
const pdfManualMaxZoom = 5

// renderManualPDF produces a kitty-graphics escape sequence for the PDF
// pane in manual mode. It crops the current page to a centred sub-rectangle
// determined by zoom, then aspect-fits the result into the pane cells via
// pdf.RenderKitty (which handles cell sizing + the C=1 cursor pin).
func renderManualPDF(d *pdf.Doc, pageIdx, zoom, widthCells, heightCells int) (string, string) {
	if d == nil {
		return "", "(no PDF loaded)"
	}
	if pageIdx < 0 {
		pageIdx = 0
	}
	if pageIdx >= d.NumPage() {
		pageIdx = d.NumPage() - 1
	}
	if pageIdx < 0 {
		return "", "(empty PDF)"
	}
	img, err := d.Page(pageIdx, pdf.DefaultCropDPI)
	if err != nil {
		return "", fmt.Sprintf("pdf: %v", err)
	}
	bounds := img.Bounds()
	pageW := bounds.Dx()
	pageH := bounds.Dy()
	if pageW < 1 || pageH < 1 {
		return "", "(empty page)"
	}
	if zoom < 0 {
		zoom = 0
	}
	if zoom > pdfManualMaxZoom {
		zoom = pdfManualMaxZoom
	}
	frac := 1.0 - float64(zoom)*pdfManualZoomStep
	if frac < 0.1 {
		frac = 0.1
	}
	cropW := int(float64(pageW) * frac)
	cropH := int(float64(pageH) * frac)
	if cropW < 1 {
		cropW = 1
	}
	if cropH < 1 {
		cropH = 1
	}
	x0 := bounds.Min.X + (pageW-cropW)/2
	y0 := bounds.Min.Y + (pageH-cropH)/2
	rect := image.Rect(x0, y0, x0+cropW, y0+cropH)
	var cropped image.Image
	type subImager interface {
		SubImage(image.Rectangle) image.Image
	}
	if si, ok := any(img).(subImager); ok {
		cropped = si.SubImage(rect)
	} else {
		dst := image.NewRGBA(image.Rect(0, 0, rect.Dx(), rect.Dy()))
		draw.Draw(dst, dst.Bounds(), img, rect.Min, draw.Src)
		cropped = dst
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, cropped); err != nil {
		return "", fmt.Sprintf("pdf: encode png: %v", err)
	}
	esc, err := pdf.RenderKitty(buf.Bytes(), widthCells, heightCells)
	if err != nil {
		return "", fmt.Sprintf("pdf: %v", err)
	}
	return esc, ""
}
