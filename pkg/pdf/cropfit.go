package pdf

import (
	"bytes"
	"fmt"
	"image"
	"image/draw"
	"image/png"
	"math"

	"mreview/pkg/synctex"
)

// FitOptions describes the target pane that a crop should fill. CropFitted
// uses these to choose render DPI, vertical context (vpad), and horizontal
// crop bounds so the output PNG matches the pane's pixel dimensions and
// aspect — kitty then aspect-fits without significant up- or downscale.
type FitOptions struct {
	// PaneWidthPx, PaneHeightPx are the pane's inner pixel dimensions
	// (cells × cell pixel size). When either is non-positive CropFitted
	// falls back to a sensible default (DefaultCropDPI, 80pt vpad,
	// full-width).
	PaneWidthPx  int
	PaneHeightPx int

	// MultiColumn = true crops horizontally to the region's column plus
	// a small horizontal padding. For single-column pages, leave false
	// to render the full page width.
	MultiColumn bool

	// MinVpadPt and MaxVpadPt bound the adaptive vertical padding. The
	// algorithm aims to grow vpad until the crop's aspect matches the
	// pane's, but caps to avoid wasting the pane on whitespace (Max) or
	// presenting a region with no surrounding context (Min). Defaults
	// applied when zero: Min = 20pt, Max = 250pt.
	MinVpadPt float64
	MaxVpadPt float64

	// HpadPt is the horizontal padding around a column-cropped region.
	// Default 20pt when zero.
	HpadPt float64
}

const (
	// fitMinVpadDefault keeps a small breathing room around the SyncTeX
	// region even when the pane is very tight; below this the cursor
	// block ends up flush against the pane border.
	fitMinVpadDefault = 20.0
	// fitMaxVpadDefault stops adaptive vpad from giving an entire pane
	// to whitespace when the region is short and the pane is tall.
	fitMaxVpadDefault = 250.0
	// fitHpadDefault is the column-crop horizontal margin — wide enough
	// to keep `\item` bullets and inline-math overhang inside the crop.
	fitHpadDefault = 20.0
	// fitMinDPI floors the chosen DPI; below ~100 fitz's font hinting
	// produces visibly blurry text even when the pane is tiny.
	fitMinDPI = 100.0
	// fitMaxDPI caps the chosen DPI; a letter page at 300 dpi is
	// ~22 MB in RGBA, multiplied by the page LRU cache. Anything
	// higher costs memory faster than it improves visible sharpness.
	fitMaxDPI = 300.0
	// fitDPIBucket rounds the chosen DPI so cache keys (pageKey.dpi)
	// dedupe near-identical requests.
	fitDPIBucket = 25.0
)

// SuggestDPI returns the render DPI that would make a (cropWPt × cropHPt)
// PDF region fill a (paneWPx × paneHPx) terminal pane after aspect-fit.
// The choice depends on which axis is the limit:
//
//   - aspectCrop > aspectPane (image taller-than-pane shape) → height
//     limited; DPI = paneHPx × 72 / cropHPt;
//   - otherwise → width limited; DPI = paneWPx × 72 / cropWPt.
//
// Result is clamped to [fitMinDPI, fitMaxDPI] and rounded to the nearest
// fitDPIBucket so the page pixmap LRU dedupes near-identical requests.
func SuggestDPI(cropWPt, cropHPt float64, paneWPx, paneHPx int) float64 {
	if cropWPt <= 0 || cropHPt <= 0 || paneWPx <= 0 || paneHPx <= 0 {
		return DefaultCropDPI
	}
	cropAspect := cropHPt / cropWPt
	paneAspect := float64(paneHPx) / float64(paneWPx)
	var dpi float64
	if cropAspect > paneAspect {
		dpi = float64(paneHPx) * 72.0 / cropHPt
	} else {
		dpi = float64(paneWPx) * 72.0 / cropWPt
	}
	if dpi < fitMinDPI {
		dpi = fitMinDPI
	}
	if dpi > fitMaxDPI {
		dpi = fitMaxDPI
	}
	return math.Ceil(dpi/fitDPIBucket) * fitDPIBucket
}

// CropFitted produces a PNG sized to fill (or come close to filling) the
// given pane. Three improvements over CropWithContext:
//
//  1. Adaptive DPI — the page is rendered at the lowest DPI that produces
//     a crop ≥ pane size in the limiting dimension, so kitty doesn't
//     upscale (blur) on big panes or waste memory on small ones.
//  2. Adaptive vpad — vertical context grows or shrinks so the crop's
//     aspect matches the pane's. Wide panes get more context above/
//     below; tall panes get tighter framing. Bounded by MinVpadPt /
//     MaxVpadPt.
//  3. Clamp rebalancing — when the region is near a page edge and one
//     side of the crop would clip, the lost padding is added to the
//     opposite side so the total context window stays roughly constant.
//
// Column awareness is delegated to the caller via MultiColumn — page-
// level layout detection (e.g. median region width) belongs in the UI
// layer that owns the doc/synctex pair.
func CropFitted(d *Doc, r synctex.Region, opts FitOptions) ([]byte, error) {
	if d == nil {
		return nil, fmt.Errorf("pdf: nil doc")
	}
	if !HasExtent(r) {
		return nil, fmt.Errorf("pdf: region has zero extent")
	}
	if opts.MinVpadPt <= 0 {
		opts.MinVpadPt = fitMinVpadDefault
	}
	if opts.MaxVpadPt <= 0 {
		opts.MaxVpadPt = fitMaxVpadDefault
	}
	if opts.HpadPt <= 0 {
		opts.HpadPt = fitHpadDefault
	}

	// Page bounds in points (= big points; PDF user space).
	bounds, err := d.Bounds(r.Page - 1)
	if err != nil {
		return nil, err
	}
	pageWPt := float64(bounds.Dx())
	pageHPt := float64(bounds.Dy())
	if pageWPt < 1 || pageHPt < 1 {
		return nil, fmt.Errorf("pdf: page %d has zero bounds", r.Page)
	}

	// Horizontal extent in points: full page or column slice.
	cropX0Pt, cropX1Pt := 0.0, pageWPt
	if opts.MultiColumn {
		cropX0Pt = r.X - opts.HpadPt
		cropX1Pt = r.X + r.W + opts.HpadPt
		if cropX0Pt < 0 {
			cropX0Pt = 0
		}
		if cropX1Pt > pageWPt {
			cropX1Pt = pageWPt
		}
	}
	cropWPt := cropX1Pt - cropX0Pt
	if cropWPt < 1 {
		return nil, fmt.Errorf("pdf: degenerate horizontal crop (%.2f pt)", cropWPt)
	}

	// Adaptive vpad — aim for crop aspect = pane aspect so the pane
	// fills cleanly. When PaneWidthPx/PaneHeightPx are unset, fall back
	// to a fixed 80 pt context (matches the legacy CropWithContext
	// default so nothing regresses for callers who haven't plumbed
	// pane dimensions yet).
	vpad := 80.0
	if opts.PaneWidthPx > 0 && opts.PaneHeightPx > 0 {
		paneAspect := float64(opts.PaneHeightPx) / float64(opts.PaneWidthPx)
		desiredCropHPt := cropWPt * paneAspect
		vpad = (desiredCropHPt - r.H) / 2.0
		if vpad < opts.MinVpadPt {
			vpad = opts.MinVpadPt
		}
		if vpad > opts.MaxVpadPt {
			vpad = opts.MaxVpadPt
		}
	}

	// Vertical extent in points, with clamp rebalancing.
	cropY0Pt := r.Y - vpad
	cropY1Pt := r.Y + r.H + vpad
	if cropY0Pt < 0 {
		// Region too close to the page top — recover the lost padding
		// on the bottom so total crop height stays consistent.
		cropY1Pt += -cropY0Pt
		cropY0Pt = 0
	}
	if cropY1Pt > pageHPt {
		excess := cropY1Pt - pageHPt
		cropY0Pt -= excess
		cropY1Pt = pageHPt
	}
	if cropY0Pt < 0 {
		cropY0Pt = 0
	}
	cropHPt := cropY1Pt - cropY0Pt
	if cropHPt < 1 {
		return nil, fmt.Errorf("pdf: degenerate vertical crop (%.2f pt)", cropHPt)
	}

	// Choose render DPI for pane size.
	dpi := DefaultCropDPI
	if opts.PaneWidthPx > 0 && opts.PaneHeightPx > 0 {
		dpi = SuggestDPI(cropWPt, cropHPt, opts.PaneWidthPx, opts.PaneHeightPx)
	}

	img, err := d.Page(r.Page-1, dpi)
	if err != nil {
		return nil, err
	}
	imgBounds := img.Bounds()
	scale := dpi / 72.0

	// Convert crop bounds to pixels, anchored to the rendered image's
	// origin (which need not be (0,0)).
	px0 := imgBounds.Min.X + int(cropX0Pt*scale)
	py0 := imgBounds.Min.Y + int(cropY0Pt*scale)
	px1 := imgBounds.Min.X + int(cropX1Pt*scale)
	py1 := imgBounds.Min.Y + int(cropY1Pt*scale)
	if px0 < imgBounds.Min.X {
		px0 = imgBounds.Min.X
	}
	if py0 < imgBounds.Min.Y {
		py0 = imgBounds.Min.Y
	}
	if px1 > imgBounds.Max.X {
		px1 = imgBounds.Max.X
	}
	if py1 > imgBounds.Max.Y {
		py1 = imgBounds.Max.Y
	}
	if px1 <= px0 || py1 <= py0 {
		return nil, fmt.Errorf("pdf: degenerate pixel crop after clamp")
	}

	cropped := subImage(img, image.Rect(px0, py0, px1, py1))
	var buf bytes.Buffer
	if err := png.Encode(&buf, cropped); err != nil {
		return nil, fmt.Errorf("pdf: encode png: %w", err)
	}
	return buf.Bytes(), nil
}

// subImage returns img cropped to rect, preferring the image type's own
// SubImage method (zero-copy for *image.RGBA from fitz) and falling back
// to a copy when the type doesn't expose one.
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
