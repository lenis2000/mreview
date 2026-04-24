package pdf

import (
	"bytes"
	"image/png"
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"mreview/pkg/synctex"
)

func openFixture(t *testing.T) *Doc {
	t.Helper()
	d, err := Open(fixturePath(t, "sample.pdf"))
	require.NoError(t, err)
	return d
}

func TestSuggestDPI_WidthLimitedPickedWhenCropIsWideRelativeToPane(t *testing.T) {
	// Crop is 400×200 pt (aspect 0.5), pane is 600×400 px (aspect 0.67).
	// crop aspect (0.5) < pane aspect (0.67) → width-limited.
	// DPI = 600 × 72 / 400 = 108 → ceil bucket 125.
	dpi := SuggestDPI(400, 200, 600, 400)
	assert.Equal(t, 125.0, dpi)
}

func TestSuggestDPI_HeightLimitedPickedWhenCropIsTallerThanPane(t *testing.T) {
	// Crop 200×600 pt (aspect 3.0), pane 400×600 px (aspect 1.5).
	// crop aspect (3.0) > pane aspect (1.5) → height-limited.
	// DPI = 600 × 72 / 600 = 72 → floored to 100.
	dpi := SuggestDPI(200, 600, 400, 600)
	assert.Equal(t, 100.0, dpi)
}

func TestSuggestDPI_LargePaneRequestsHigherDPI(t *testing.T) {
	// Pane 1500×1500 px on a 612×800 pt crop.
	// cropAspect = 800/612 = 1.307 > paneAspect = 1.0 → height-limited.
	// DPI = 1500 × 72 / 800 = 135 → ceil bucket 150.
	dpi := SuggestDPI(612, 800, 1500, 1500)
	assert.Equal(t, 150.0, dpi)
}

func TestSuggestDPI_CapsAtMaxToProtectMemory(t *testing.T) {
	// Tiny crop, huge pane — would naively want > 300 dpi.
	// 30 pt wide × 30 pt tall, pane 2000×2000 px. DPI = 2000×72/30 = 4800.
	dpi := SuggestDPI(30, 30, 2000, 2000)
	assert.Equal(t, fitMaxDPI, dpi, "DPI cap stops the page bitmap from blowing up")
}

func TestSuggestDPI_FloorsAtMinForFontHinting(t *testing.T) {
	// Huge crop, tiny pane — would naively want very low DPI; floor at 100.
	dpi := SuggestDPI(2000, 2000, 100, 100)
	assert.Equal(t, fitMinDPI, dpi, "DPI floor keeps font hinting acceptable")
}

func TestSuggestDPI_CeilsToBucket(t *testing.T) {
	// Crop 612 × 400 pt, pane 1024 × 600 px.
	// cropAspect = 400/612 = 0.65 > paneAspect = 600/1024 = 0.59
	// → height-limited. DPI = 600×72/400 = 108 → ceil bucket 125.
	dpi := SuggestDPI(612, 400, 1024, 600)
	assert.Equal(t, 125.0, dpi)
}

func TestSuggestDPI_DegenerateInputsReturnDefault(t *testing.T) {
	assert.Equal(t, DefaultCropDPI, SuggestDPI(0, 200, 600, 400))
	assert.Equal(t, DefaultCropDPI, SuggestDPI(400, 200, 0, 400))
	assert.Equal(t, DefaultCropDPI, SuggestDPI(-1, 200, 600, 400))
}

// CropFitted needs a real PDF; the existing fixture (pkg/pdf/doc_test.go's
// helper) lives in the same package so we can lean on it directly.
func TestCropFitted_ProducesDecodablePNGAtAdaptiveDPI(t *testing.T) {
	d := openFixture(t)
	defer d.Close()

	bounds, err := d.Bounds(0)
	require.NoError(t, err)
	r := synctex.Region{
		Page: 1,
		X:    float64(bounds.Dx()) / 4,
		Y:    float64(bounds.Dy()) / 4,
		W:    float64(bounds.Dx()) / 4,
		H:    float64(bounds.Dy()) / 4,
	}
	pngBytes, err := CropFitted(d, r, FitOptions{
		PaneWidthPx:  800,
		PaneHeightPx: 600,
	})
	require.NoError(t, err)
	require.NotEmpty(t, pngBytes)
	img, err := png.Decode(bytes.NewReader(pngBytes))
	require.NoError(t, err)
	assert.Greater(t, img.Bounds().Dx(), 0)
	assert.Greater(t, img.Bounds().Dy(), 0)
}

func TestCropFitted_TightVpadIndependentOfPaneAspect(t *testing.T) {
	d := openFixture(t)
	defer d.Close()

	bounds, err := d.Bounds(0)
	require.NoError(t, err)
	r := synctex.Region{
		Page: 1,
		X:    float64(bounds.Dx()) / 4,
		Y:    float64(bounds.Dy()) / 2,
		W:    float64(bounds.Dx()) / 4,
		H:    15, // ~one line of body text
	}
	// Tall pane and short pane should produce the same (tight) crop
	// height because vpad no longer grows to match pane aspect. This
	// trades "pane fills cleanly" for "crop actually shows the block"
	// — adaptive growth was inflating crops to near-page-size on tiny
	// regions in two-column papers (OCR bug report for scope-2).
	tall, err := CropFitted(d, r, FitOptions{PaneWidthPx: 300, PaneHeightPx: 1800})
	require.NoError(t, err)
	short, err := CropFitted(d, r, FitOptions{PaneWidthPx: 300, PaneHeightPx: 200})
	require.NoError(t, err)
	tallImg, err := png.Decode(bytes.NewReader(tall))
	require.NoError(t, err)
	shortImg, err := png.Decode(bytes.NewReader(short))
	require.NoError(t, err)
	// Accept a small DPI-bucket-driven difference; core property is
	// that the tall-pane crop no longer dwarfs the short-pane crop.
	ratio := float64(tallImg.Bounds().Dy()) / float64(shortImg.Bounds().Dy())
	assert.InDelta(t, 1.0, ratio, 0.35,
		"tight vpad: crop heights should be similar regardless of pane aspect (got ratio %.2f)", ratio)

	// Upper bound on absolute crop height: 2·VpadPt + r.H ≈ 35 pt, times
	// fitMaxDPI scale — plenty of slack for DPI bucketing but still far
	// below the old ~520 pt adaptive ceiling.
	maxHPt := 2*fitVpadDefault + r.H
	maxHPx := int(maxHPt*fitMaxDPI/72.0) + 8
	assert.Less(t, tallImg.Bounds().Dy(), maxHPx,
		"crop height should stay near 2·VpadPt + r.H (got %d px, want < %d px)",
		tallImg.Bounds().Dy(), maxHPx)
}

func TestCropFitted_ColumnModeNarrowsHorizontally(t *testing.T) {
	d := openFixture(t)
	defer d.Close()

	bounds, err := d.Bounds(0)
	require.NoError(t, err)
	r := synctex.Region{
		Page: 1,
		X:    float64(bounds.Dx()) / 4,
		Y:    float64(bounds.Dy()) / 4,
		W:    float64(bounds.Dx()) / 4, // narrow region
		H:    float64(bounds.Dy()) / 4,
	}
	full, err := CropFitted(d, r, FitOptions{
		PaneWidthPx:  800,
		PaneHeightPx: 600,
		MultiColumn:  false,
	})
	require.NoError(t, err)
	col, err := CropFitted(d, r, FitOptions{
		PaneWidthPx:  800,
		PaneHeightPx: 600,
		MultiColumn:  true,
	})
	require.NoError(t, err)

	fullImg, err := png.Decode(bytes.NewReader(full))
	require.NoError(t, err)
	colImg, err := png.Decode(bytes.NewReader(col))
	require.NoError(t, err)

	// Both panes are identical (same PaneWidthPx/PaneHeightPx) — but at the
	// same DPI bucket the column-mode crop covers fewer page pixels in W
	// because we trimmed the page horizontally before rendering. The
	// resulting image should be narrower in pixels.
	assert.Less(t, colImg.Bounds().Dx(), fullImg.Bounds().Dx(),
		"column-mode crop must be narrower than full-width crop")
}

func TestCropFitted_RebalancesClampedTopEdge(t *testing.T) {
	d := openFixture(t)
	defer d.Close()

	bounds, err := d.Bounds(0)
	require.NoError(t, err)

	// Region at the very top of the page — vpad above would clamp.
	rTop := synctex.Region{
		Page: 1,
		X:    float64(bounds.Dx()) / 4,
		Y:    1.0, // near top
		W:    float64(bounds.Dx()) / 4,
		H:    20,
	}
	// Region in the middle — symmetric vpad.
	rMid := synctex.Region{
		Page: 1,
		X:    float64(bounds.Dx()) / 4,
		Y:    float64(bounds.Dy()) / 2,
		W:    float64(bounds.Dx()) / 4,
		H:    20,
	}

	opts := FitOptions{PaneWidthPx: 600, PaneHeightPx: 600}
	top, err := CropFitted(d, rTop, opts)
	require.NoError(t, err)
	mid, err := CropFitted(d, rMid, opts)
	require.NoError(t, err)

	topImg, err := png.Decode(bytes.NewReader(top))
	require.NoError(t, err)
	midImg, err := png.Decode(bytes.NewReader(mid))
	require.NoError(t, err)

	// Without rebalancing, the top crop would be markedly shorter than
	// the middle crop. With rebalancing, the crop heights agree to
	// within a small (sub-pixel rounding) margin.
	diff := math.Abs(float64(topImg.Bounds().Dy() - midImg.Bounds().Dy()))
	assert.Less(t, diff, float64(midImg.Bounds().Dy())*0.10,
		"rebalanced clamp keeps top-edge crop within 10%% of mid-page crop height")
}

func TestCropFitted_DegenerateInputsErrorEarly(t *testing.T) {
	d := openFixture(t)
	defer d.Close()

	_, err := CropFitted(nil, synctex.Region{Page: 1, W: 10, H: 10}, FitOptions{})
	assert.Error(t, err, "nil doc")

	_, err = CropFitted(d, synctex.Region{Page: 1}, FitOptions{})
	assert.Error(t, err, "zero extent")
}
