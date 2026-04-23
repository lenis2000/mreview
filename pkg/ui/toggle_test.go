package ui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"mreview/pkg/persist"
)

// TestToggleManualPDF_PreservesImageWhenBuildStale is the
// review-new.md (pass 5) #2 regression guard for the V key. While
// BuildStale is true, schedulePDFRender returns nil — if the V
// handler also clears m.PDFImage, the pane goes blank, which
// contradicts the "keep the last known-good crop until the next
// successful reload" contract.
func TestToggleManualPDF_PreservesImageWhenBuildStale(t *testing.T) {
	doc := parsedSample(t)
	m := New(doc, &persist.Sidecar{})
	m.Width, m.Height = 120, 40
	m.BuildStale = true
	const sentinel = "\x1b_Gpreserved-crop\x1b\\"
	m.PDFImage = sentinel

	out, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'V'}})
	nm := out.(Model)
	assert.True(t, nm.PDFManual, "V must still flip manual mode")
	assert.Equal(t, sentinel, nm.PDFImage,
		"toggling V while BuildStale must not blank the PDF pane")
}

// TestToggleManualPDF_ClearsImageWhenBuildFresh guards the
// non-stale path so the preservation doesn't accidentally apply
// when a fresh render *would* replace the image.
func TestToggleManualPDF_ClearsImageWhenBuildFresh(t *testing.T) {
	doc := parsedSample(t)
	m := New(doc, &persist.Sidecar{})
	m.Width, m.Height = 120, 40
	m.BuildStale = false
	m.PDFImage = "about-to-be-replaced"

	out, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'V'}})
	nm := out.(Model)
	assert.True(t, nm.PDFManual)
	assert.Empty(t, nm.PDFImage,
		"fresh-build V toggle should clear so the new render replaces cleanly")
}

// TestToggleLayout_PreservesImageWhenBuildStale is the \ equivalent.
// Same reasoning — blanking on layout toggle while stale breaks the
// BuildStale contract.
func TestToggleLayout_PreservesImageWhenBuildStale(t *testing.T) {
	doc := parsedSample(t)
	m := New(doc, &persist.Sidecar{})
	m.Width, m.Height = 120, 40
	m.BuildStale = true
	const sentinel = "\x1b_Gpreserved-crop\x1b\\"
	m.PDFImage = sentinel
	priorCache := m.pdfCache
	require.NotNil(t, priorCache)

	out, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'\\'}})
	nm := out.(Model)
	assert.Equal(t, LayoutStacked, nm.Layout, "layout should still toggle")
	assert.Equal(t, sentinel, nm.PDFImage,
		"toggling \\ while BuildStale must not blank the PDF pane")
	assert.Same(t, priorCache, nm.pdfCache,
		"toggling \\ while BuildStale must not flush the crop cache")
}

// TestToggleLayout_ClearsImageWhenBuildFresh guards the non-stale
// path so a fresh render replaces cleanly at the new geometry.
func TestToggleLayout_ClearsImageWhenBuildFresh(t *testing.T) {
	doc := parsedSample(t)
	m := New(doc, &persist.Sidecar{})
	m.Width, m.Height = 120, 40
	m.BuildStale = false
	m.PDFImage = "old-geometry"

	out, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'\\'}})
	nm := out.(Model)
	assert.Equal(t, LayoutStacked, nm.Layout)
	assert.Empty(t, nm.PDFImage,
		"fresh-build \\ toggle must clear so the re-render at new geometry replaces cleanly")
}
