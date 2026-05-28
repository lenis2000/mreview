package diffui

import (
	"strings"
	"testing"

	"mreview/pkg/diffreview"
	"mreview/pkg/pdf"
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

func TestNoPDFLayoutHidesPDFPaneOnly(t *testing.T) {
	m := New(fixtureReview(), Options{KittyAvailable: true})
	m.Width = 160
	m.Height = 24
	m.Layout = LayoutNoPDF

	view := m.View()
	if !strings.HasPrefix(view, pdf.KittyDeleteAll) {
		t.Fatalf("no-PDF layout should clear any stale kitty image")
	}
	if strings.Contains(view, "PDF") {
		t.Fatalf("no-PDF layout should omit PDF pane:\n%s", view)
	}
	for _, want := range []string{"Outline", "Old source", "New source"} {
		if !strings.Contains(view, want) {
			t.Fatalf("no-PDF view missing %q:\n%s", want, view)
		}
	}
}

func TestSplitSourcePanesUseSideSpecificAnchors(t *testing.T) {
	review := &diffreview.Review{Pairs: []diffreview.Pair{{
		ID:     "insert-before",
		Status: diffreview.Changed,
		Old:    fixtureBlock("old-insert-before", 10, "before\nafter"),
		New:    fixtureBlock("new-insert-before", 100, "inserted\nbefore\nafter"),
	}}}
	m := New(review, Options{})
	m.Focus = PaneOldSource
	m.SourceLineCursor = 1

	view := m.renderComparisonArea(100, 5)
	if !strings.Contains(view, ">  10 before") {
		t.Fatalf("old pane should anchor on old line, not new-only placeholder:\n%s", view)
	}
}

func TestSplitSourcePaneBordersStayAlignedWhenContentHeightsDiffer(t *testing.T) {
	review := &diffreview.Review{Pairs: []diffreview.Pair{{
		ID:     "uneven",
		Status: diffreview.Changed,
		Old:    fixtureBlock("old-uneven", 1, "old one"),
		New:    fixtureBlock("new-uneven", 1, strings.Join([]string{"new one", "new two", "new three", "new four", "new five", "new six"}, "\n")),
	}}}
	m := New(review, Options{})
	view := m.renderComparisonArea(100, 8)
	if got := strings.Count(view, "\n") + 1; got != 8 {
		t.Fatalf("comparison area height = %d, want 8; borders may drift:\n%s", got, view)
	}
}

func TestPDFPaneDoesNotClipKittyEscapeBody(t *testing.T) {
	m := New(fixtureReview(), Options{KittyAvailable: true})
	m.PDFImage = "\x1b_Ga=T,m=0;" + strings.Repeat("x", 200) + "\x1b\\"

	view := m.renderPDFPane(24, 8, false)
	if !strings.Contains(view, m.PDFImage) {
		t.Fatalf("PDF pane clipped or rewrote kitty image escape")
	}
}
