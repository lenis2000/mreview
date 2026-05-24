package ui

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/BurntSushi/toml"
	"github.com/stretchr/testify/assert"
)

// withFracs runs f with the package-level fractions saved/restored so a
// resize test can't bleed state into unrelated tests. Tests in this file
// share package globals, so anything that mutates outlineFrac/pdfFrac/
// stackedTopFrac must wrap in this helper.
func withFracs(t *testing.T, f func()) {
	t.Helper()
	o, p, s := outlineFrac, pdfFrac, stackedTopFrac
	defer func() { outlineFrac, pdfFrac, stackedTopFrac = o, p, s }()
	f()
}

func TestResizeFocusedPane_OutlineGrowShrink(t *testing.T) {
	withFracs(t, func() {
		outlineFrac, pdfFrac = 0.22, 0.28
		moved := resizeFocusedPane(PaneOutline, LayoutThreeCol, +1)
		assert.True(t, moved)
		assert.InDelta(t, 0.24, outlineFrac, 1e-9)
		assert.InDelta(t, 0.28, pdfFrac, 1e-9, "pdf frac unchanged when outline focused")

		moved = resizeFocusedPane(PaneOutline, LayoutThreeCol, -1)
		assert.True(t, moved)
		assert.InDelta(t, 0.22, outlineFrac, 1e-9)
	})
}

func TestResizeFocusedPane_PDFGrowShrink(t *testing.T) {
	withFracs(t, func() {
		outlineFrac, pdfFrac = 0.22, 0.28
		resizeFocusedPane(PanePDF, LayoutThreeCol, +1)
		assert.InDelta(t, 0.30, pdfFrac, 1e-9)
		assert.InDelta(t, 0.22, outlineFrac, 1e-9)
	})
}

func TestResizeFocusedPane_SourceGrowsFromBoth(t *testing.T) {
	withFracs(t, func() {
		outlineFrac, pdfFrac = 0.22, 0.28
		resizeFocusedPane(PaneSource, LayoutThreeCol, +1)
		// `>` while focused on source = grow source = shrink both
		// neighbours by half a step each.
		assert.InDelta(t, 0.21, outlineFrac, 1e-9)
		assert.InDelta(t, 0.27, pdfFrac, 1e-9)
	})
}

func TestResizeFocusedPane_StackedSourceMovesVerticalSplit(t *testing.T) {
	withFracs(t, func() {
		stackedTopFrac = 0.50
		resizeFocusedPane(PaneSource, LayoutStacked, +1)
		assert.InDelta(t, 0.52, stackedTopFrac, 1e-9)
		resizeFocusedPane(PanePDF, LayoutStacked, +1)
		// PDF growing in stacked = top frac shrinks.
		assert.InDelta(t, 0.50, stackedTopFrac, 1e-9)
	})
}

func TestResizeFocusedPane_BoundsClamp(t *testing.T) {
	withFracs(t, func() {
		// Push outline to its lower bound and confirm further shrinks
		// no-op (resizeFocusedPane returns false when nothing moved).
		outlineFrac = minOutline
		moved := resizeFocusedPane(PaneOutline, LayoutThreeCol, -1)
		assert.False(t, moved)
		assert.InDelta(t, minOutline, outlineFrac, 1e-9)

		// Same at the upper bound.
		outlineFrac = maxOutline
		pdfFrac = minPDF
		moved = resizeFocusedPane(PaneOutline, LayoutThreeCol, +1)
		assert.False(t, moved)
		assert.InDelta(t, maxOutline, outlineFrac, 1e-9)
	})
}

func TestResizeFocusedPane_SourceFloorStealsFromOtherNeighbour(t *testing.T) {
	withFracs(t, func() {
		// Source already at floor (= minSource). Growing outline
		// should still succeed by stealing from PDF rather than
		// collapsing source. clampLayoutFracs handles that handoff.
		outlineFrac = 0.40
		pdfFrac = 0.50 // source = 0.10 == minSource
		moved := resizeFocusedPane(PaneOutline, LayoutThreeCol, +1)
		assert.True(t, moved)
		assert.InDelta(t, 0.42, outlineFrac, 1e-9)
		// PDF gave back the 0.02 instead of source dropping below floor.
		assert.InDelta(t, 0.48, pdfFrac, 1e-9)
		assert.InDelta(t, minSource, 1.0-outlineFrac-pdfFrac, 1e-9)
	})
}

func TestSaveLoadLayoutFracs_RoundTrip(t *testing.T) {
	withFracs(t, func() {
		dir := t.TempDir()
		t.Setenv("HOME", dir)
		outlineFrac, pdfFrac, stackedTopFrac = 0.30, 0.20, 0.40
		saveLayoutFracs()

		// Confirm the file landed where LoadLayoutFracs will find it.
		path := filepath.Join(dir, ".config", "mreview", "layout.toml")
		_, err := os.Stat(path)
		assert.NoError(t, err)

		// Reset and reload — the values should come back exactly.
		outlineFrac, pdfFrac, stackedTopFrac = 0.22, 0.28, 0.50
		LoadLayoutFracs()
		assert.InDelta(t, 0.30, outlineFrac, 1e-9)
		assert.InDelta(t, 0.20, pdfFrac, 1e-9)
		assert.InDelta(t, 0.40, stackedTopFrac, 1e-9)
	})
}

func TestLoadLayoutFracs_BadValuesClamped(t *testing.T) {
	withFracs(t, func() {
		dir := t.TempDir()
		t.Setenv("HOME", dir)
		path := filepath.Join(dir, ".config", "mreview", "layout.toml")
		assert.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		f, err := os.Create(path)
		assert.NoError(t, err)
		assert.NoError(t, toml.NewEncoder(f).Encode(LayoutFracs{
			Outline:    0.95, // out-of-range high
			PDF:        0.01, // out-of-range low
			StackedTop: 0.50,
		}))
		_ = f.Close()

		LoadLayoutFracs()
		assert.LessOrEqual(t, outlineFrac, maxOutline)
		assert.GreaterOrEqual(t, pdfFrac, minPDF)
	})
}
