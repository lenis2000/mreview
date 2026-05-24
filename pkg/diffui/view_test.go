package diffui

import (
	"strings"
	"testing"
)

func TestWideViewUsesSeparateOldAndNewSourcePanes(t *testing.T) {
	m := New(fixtureReview(), Options{})
	m.Width = 160
	m.Height = 24

	view := m.View()
	for _, want := range []string{"Outline", "Old source", "New source", "PDF"} {
		if !strings.Contains(view, want) {
			t.Fatalf("wide view missing %q:\n%s", want, view)
		}
	}
}

func TestPDFPaneDoesNotClipKittyEscapeBody(t *testing.T) {
	m := New(fixtureReview(), Options{KittyAvailable: true})
	m.PDFImage = "\x1b_Ga=T,m=0;" + strings.Repeat("x", 200) + "\x1b\\"

	view := m.renderPDFPane(24, 8)
	if !strings.Contains(view, m.PDFImage) {
		t.Fatalf("PDF pane clipped or rewrote kitty image escape")
	}
}
