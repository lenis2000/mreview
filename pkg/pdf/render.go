package pdf

import (
	"bytes"
	"fmt"
	"image"
	"image/draw"
	"image/png"

	"mreview/pkg/synctex"
)

// DefaultCropDPI is the render resolution used for crops. 200 strikes a
// reasonable balance between sharpness in the PDF pane and render latency.
const DefaultCropDPI = 200.0

// Crop renders the page containing r and returns PNG bytes for the bounded
// sub-rectangle inflated by pad PDF points on every side. A zero-valued
// region (all fields 0) returns an error; use HasExtent to guard callers.
func Crop(d *Doc, r synctex.Region, pad float64) ([]byte, error) {
	return CropAtDPI(d, r, pad, DefaultCropDPI)
}

// CropAtDPI is Crop with an explicit render DPI — handy for tests.
func CropAtDPI(d *Doc, r synctex.Region, pad, dpi float64) ([]byte, error) {
	if d == nil {
		return nil, fmt.Errorf("pdf: nil doc")
	}
	if !HasExtent(r) {
		return nil, fmt.Errorf("pdf: region has zero extent")
	}
	if dpi <= 0 {
		dpi = DefaultCropDPI
	}
	pageIdx := r.Page - 1
	img, err := d.Page(pageIdx, dpi)
	if err != nil {
		return nil, err
	}
	bounds := img.Bounds()
	scale := dpi / 72.0
	x0 := int((r.X - pad) * scale)
	y0 := int((r.Y - pad) * scale)
	x1 := int((r.X + r.W + pad) * scale)
	y1 := int((r.Y + r.H + pad) * scale)
	if x0 < bounds.Min.X {
		x0 = bounds.Min.X
	}
	if y0 < bounds.Min.Y {
		y0 = bounds.Min.Y
	}
	if x1 > bounds.Max.X {
		x1 = bounds.Max.X
	}
	if y1 > bounds.Max.Y {
		y1 = bounds.Max.Y
	}
	if x1 <= x0 || y1 <= y0 {
		return nil, fmt.Errorf("pdf: crop rect degenerate after clamp")
	}
	rect := image.Rect(x0, y0, x1, y1)
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
		return nil, fmt.Errorf("pdf: encode png: %w", err)
	}
	return buf.Bytes(), nil
}

// HasExtent reports whether r has a non-zero width and height. SyncTeX
// records without dimensions (glue, kern) only set X/Y; those are unusable
// for cropping so callers filter on HasExtent before rendering.
func HasExtent(r synctex.Region) bool {
	return r.Page > 0 && r.W > 0 && r.H > 0
}
