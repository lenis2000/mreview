package pdfreview

import (
	"os/exec"
	"strings"
	"testing"
)

func TestBBoxParse_FindsKnownPhrase(t *testing.T) {
	if _, err := exec.LookPath("pdftotext"); err != nil {
		t.Skip("pdftotext not on PATH")
	}
	const pdf = "../../testdata/sample.pdf"
	c := NewBBoxCache(pdf)
	rects, ok := c.FindQuote(1, "SAMPLE PAPER")
	if !ok {
		t.Fatalf("expected to find 'SAMPLE PAPER' in sample.pdf")
	}
	if len(rects) == 0 {
		t.Fatalf("got 0 rects, want >=1")
	}
	r := rects[0]
	// US-letter title block: ~middle horizontally, near the top.
	if r.XMin <= 0 || r.YMin <= 0 || r.XMax <= r.XMin || r.YMax <= r.YMin {
		t.Fatalf("degenerate rect: %+v", r)
	}
	if r.YMin > 250 {
		t.Fatalf("expected title near top of page, got yMin=%v", r.YMin)
	}
}

func TestBBoxParse_MissReturnsFalse(t *testing.T) {
	if _, err := exec.LookPath("pdftotext"); err != nil {
		t.Skip("pdftotext not on PATH")
	}
	c := NewBBoxCache("../../testdata/sample.pdf")
	_, ok := c.FindQuote(1, "this exact phrase will not appear in the sample")
	if ok {
		t.Fatalf("expected miss to return ok=false")
	}
}

func TestNormalizeWS(t *testing.T) {
	cases := []struct{ in, want string }{
		{"  hello   world  ", "hello world"},
		{"a\tb\nc\rd", "a b c d"},
		{"", ""},
		{"   ", ""},
	}
	for _, tc := range cases {
		if got := NormalizeWS(tc.in); got != tc.want {
			t.Errorf("NormalizeWS(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestParseBBoxXML_Tolerant(t *testing.T) {
	// Minimal well-formed sliver — verifies we don't reject the namespace
	// or DOCTYPE preamble pdftotext emits.
	src := `<!DOCTYPE html><html xmlns="http://www.w3.org/1999/xhtml"><body><doc><page width="612" height="792">
<flow><block><line>
  <word xMin="100" yMin="200" xMax="120" yMax="210">Hello</word>
  <word xMin="125" yMin="200" xMax="160" yMax="210">World</word>
</line></block></flow>
</page></doc></body></html>`
	pb, err := parseBBoxXML([]byte(src))
	if err != nil {
		t.Fatalf("parseBBoxXML: %v", err)
	}
	if len(pb.Words) != 2 {
		t.Fatalf("got %d words, want 2 (%v)", len(pb.Words), pb.Words)
	}
	if !strings.Contains(pb.Words[0].Text, "Hello") {
		t.Fatalf("first word text = %q", pb.Words[0].Text)
	}
	if pb.PageW != 612 || pb.PageH != 792 {
		t.Fatalf("page dims = %v x %v", pb.PageW, pb.PageH)
	}
}
