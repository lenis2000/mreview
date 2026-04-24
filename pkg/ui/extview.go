package ui

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"math"

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
	Doc          *pdf.Doc
	Page         int
	Zoom         int
	WidthCells   int
	HeightCells  int
	CellWidthPx  float64
	CellHeightPx float64
	Dual         string // "" | "horizontal" | "vertical"
	Dark         string // "" | "smart" | "invert"
	CropT        float64
	CropB        float64
	CropL        float64
	CropR        float64
}

// renderManualPDF produces a kitty-graphics escape sequence for the PDF
// pane in manual mode. Applies in order: per-edge crop, centred zoom
// crop, optional dark-mode invert, optional dual-page composition, then
// aspect-fit into the pane cells.
//
// DPI is chosen adaptively (chooseManualDPI) so a high zoom level on a
// large pane gets a crisp render instead of the upscale blur the old
// fixed-200 dpi path produced.
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
	dpi := chooseManualDPI(in)
	primary, err := renderSinglePage(in.Doc, in.Page, in, dpi)
	if err != nil {
		return "", fmt.Sprintf("pdf: %v", err)
	}
	var composed image.Image = primary
	if in.Dual != "" && in.Page+1 < in.Doc.NumPage() {
		second, err := renderSinglePage(in.Doc, in.Page+1, in, dpi)
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

// chooseManualDPI computes the render resolution that best fits the
// pane after per-edge crop, zoom crop, and dual-page composition.
// Falls back to DefaultCropDPI when pane pixel dimensions aren't known.
func chooseManualDPI(in manualRenderInputs) float64 {
	if in.Doc == nil || in.CellWidthPx <= 0 || in.CellHeightPx <= 0 {
		return pdf.DefaultCropDPI
	}
	bounds, err := in.Doc.Bounds(in.Page)
	if err != nil {
		return pdf.DefaultCropDPI
	}
	pageWPt := float64(bounds.Dx())
	pageHPt := float64(bounds.Dy())
	if pageWPt < 1 || pageHPt < 1 {
		return pdf.DefaultCropDPI
	}
	visW := pageWPt * (1 - clampFrac(in.CropL) - clampFrac(in.CropR))
	visH := pageHPt * (1 - clampFrac(in.CropT) - clampFrac(in.CropB))
	if in.Zoom > 0 {
		z := in.Zoom
		if z > pdfManualMaxZoom {
			z = pdfManualMaxZoom
		}
		frac := 1.0 - float64(z)*pdfManualZoomStep
		if frac < 0.1 {
			frac = 0.1
		}
		visW *= frac
		visH *= frac
	}
	finalW, finalH := visW, visH
	switch in.Dual {
	case "horizontal":
		finalW = 2 * visW
	case "vertical":
		finalH = 2 * visH
	}
	paneWPx := int(float64(in.WidthCells) * in.CellWidthPx)
	paneHPx := int(float64(in.HeightCells) * in.CellHeightPx)
	return pdf.SuggestDPI(finalW, finalH, paneWPx, paneHPx)
}

// renderSinglePage rasterises one page at the given DPI then applies
// per-edge crop + zoom + dark-mode invert. The output is already in
// its final pixel dimensions; renderManualPDF only composes and
// encodes from here.
func renderSinglePage(d *pdf.Doc, pageIdx int, in manualRenderInputs, dpi float64) (image.Image, error) {
	img, err := d.Page(pageIdx, dpi)
	if err != nil {
		return nil, err
	}
	cropped := cropEdges(img, in.CropL, in.CropT, in.CropR, in.CropB)
	if in.Zoom > 0 {
		cropped = zoomCrop(cropped, in.Zoom)
	}
	switch in.Dark {
	case "smart":
		cropped = smartInvert(cropped)
	case "invert":
		cropped = invertColors(cropped)
	}
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

// invertColors returns a copy of img with RGB inverted, remapped into a
// dark-gray background range (255→30, 0→255) with alpha preserved.
// Matches docviewer's "D" simple-invert mode.
func invertColors(img image.Image) image.Image {
	b := img.Bounds()
	dst := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			r, g, bl, a := img.At(x, y).RGBA()
			r8 := uint32(r >> 8)
			g8 := uint32(g >> 8)
			b8 := uint32(bl >> 8)
			dst.Set(x-b.Min.X, y-b.Min.Y, color.RGBA{
				R: uint8(30 + (255-r8)*225/255),
				G: uint8(30 + (255-g8)*225/255),
				B: uint8(30 + (255-b8)*225/255),
				A: uint8(a >> 8),
			})
		}
	}
	return dst
}

// smartInvert inverts lightness while preserving hue and saturation, so
// white backgrounds become near-black, dark text becomes light, and tinted
// figures keep their colour identity. Matches docviewer's "i" mode.
func smartInvert(img image.Image) image.Image {
	b := img.Bounds()
	dst := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			r, g, bl, a := img.At(x, y).RGBA()
			r8 := float64(r>>8) / 255.0
			g8 := float64(g>>8) / 255.0
			b8 := float64(bl>>8) / 255.0
			h, s, l := rgbToHSL(r8, g8, b8)
			l = 0.12 + (1.0-l)*0.88
			nr, ng, nb := hslToRGB(h, s, l)
			dst.Set(x-b.Min.X, y-b.Min.Y, color.RGBA{
				R: uint8(nr * 255),
				G: uint8(ng * 255),
				B: uint8(nb * 255),
				A: uint8(a >> 8),
			})
		}
	}
	return dst
}

func rgbToHSL(r, g, b float64) (h, s, l float64) {
	maxv := math.Max(r, math.Max(g, b))
	minv := math.Min(r, math.Min(g, b))
	l = (maxv + minv) / 2
	if maxv == minv {
		return 0, 0, l
	}
	d := maxv - minv
	if l > 0.5 {
		s = d / (2.0 - maxv - minv)
	} else {
		s = d / (maxv + minv)
	}
	switch maxv {
	case r:
		h = (g - b) / d
		if g < b {
			h += 6
		}
	case g:
		h = (b-r)/d + 2
	case b:
		h = (r-g)/d + 4
	}
	h /= 6
	return
}

func hslToRGB(h, s, l float64) (r, g, b float64) {
	if s == 0 {
		return l, l, l
	}
	var q float64
	if l < 0.5 {
		q = l * (1 + s)
	} else {
		q = l + s - l*s
	}
	p := 2*l - q
	r = hueToRGB(p, q, h+1.0/3.0)
	g = hueToRGB(p, q, h)
	b = hueToRGB(p, q, h-1.0/3.0)
	return
}

func hueToRGB(p, q, t float64) float64 {
	if t < 0 {
		t++
	}
	if t > 1 {
		t--
	}
	if t < 1.0/6.0 {
		return p + (q-p)*6*t
	}
	if t < 1.0/2.0 {
		return q
	}
	if t < 2.0/3.0 {
		return p + (q-p)*(2.0/3.0-t)*6
	}
	return p
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
