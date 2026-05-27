package ui

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"mreview/pkg/pdf"
	"mreview/pkg/persist"
)

// TestPDFPaneBody_ShowsImageVerbatim: when PDFImage is set, the
// body just returns the escape as-is — RenderKitty already prepends
// its own a=d so we must not double-wrap or otherwise transform it.
func TestPDFPaneBody_ShowsImageVerbatim(t *testing.T) {
	m := New(parsedSample(t), &persist.Sidecar{})
	m.KittyAvailable = true
	const img = "\x1b_Ga=d\x1b\\\x1b_Ga=T,...;AAAA\x1b\\"
	m.PDFImage = img
	assert.Equal(t, img, m.pdfPaneBody())
}

// TestPDFPaneBody_StatusPrependsKittyDelete: transitioning from an
// image to text must emit a kitty-delete APC so the previously
// painted bitmap is retired. Without this, the kitty plane keeps
// showing the last crop even though the text buffer now says
// something else.
func TestPDFPaneBody_StatusPrependsKittyDelete(t *testing.T) {
	m := New(parsedSample(t), &persist.Sidecar{})
	m.KittyAvailable = true
	m.PDFStatus = "pdf: no region"

	out := m.pdfPaneBody()
	assert.True(t, strings.HasPrefix(out, pdf.KittyDeleteAll),
		"status transitions must begin with the kitty-delete escape so stale bitmaps don't linger")
	assert.Contains(t, out, "pdf: no region")
}

// TestPDFPaneBody_NoRegionPrependsKittyDelete: same contract for
// the "(no PDF region)" fallback that shows when the block has no
// SyncTeX mapping.
func TestPDFPaneBody_NoRegionPrependsKittyDelete(t *testing.T) {
	m := New(parsedSample(t), &persist.Sidecar{})
	m.KittyAvailable = true
	// Neither PDFImage nor PDFStatus set; fall through to the
	// no-cursor / no-pdf placeholders.
	m.CursorBlockID = ""

	out := m.pdfPaneBody()
	assert.True(t, strings.HasPrefix(out, pdf.KittyDeleteAll),
		"placeholder transitions must also retire the prior image")
}

// TestPDFPaneBody_WithoutKittyShowsPlaceholder: on terminals that
// can't render kitty graphics we must never emit APC sequences,
// even as a delete — unknown terminals echo the escape as literal
// characters. Show a plain placeholder instead.
func TestPDFPaneBody_WithoutKittyShowsPlaceholder(t *testing.T) {
	m := New(parsedSample(t), &persist.Sidecar{})
	m.KittyAvailable = false
	m.PDFImage = "kitty-escape-would-go-here"

	out := m.pdfPaneBody()
	assert.False(t, strings.Contains(out, "\x1b_G"),
		"non-kitty terminals must not see any APC graphics escape, not even a delete")
	assert.Contains(t, out, "kitty", "placeholder should hint at the requirement")
}

// TestSchedulePDFRender_SuppressedWithoutKitty is the render-side
// counterpart: even with a valid *pdf.Doc attached, scheduling a
// render must return nil when KittyAvailable is false so we don't
// spray escape sequences into a terminal that can't decode them.
func TestSchedulePDFRender_SuppressedWithoutKitty(t *testing.T) {
	pdfDoc, err := pdf.Open(pdfFixturePath(t, "sample.pdf"))
	if err != nil {
		t.Fatalf("open fixture pdf: %v", err)
	}
	defer func() { _ = pdfDoc.Close() }()

	m := New(parsedSample(t), &persist.Sidecar{})
	m.PDF = pdfDoc
	m.Width, m.Height = 120, 40
	m.KittyAvailable = false

	assert.Nil(t, m.schedulePDFRender(),
		"no-kitty terminal must not schedule any render")
}
