// Package pdfreview implements the side-by-side TUI viewer for anchored
// PDF review comments — the second stage of the pdf-comments / pdf-review
// pipeline. It loads a PAPER.pdf.pdf-comments.json, opens PAPER.pdf, lets
// the user triage each comment with the relevant span highlighted in the
// rendered page, and exports a clean PAPER.review.md letter.
package pdfreview

import (
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strconv"
	"strings"
	"sync"
)

// WordBox is one word on a PDF page with its bounding box in PDF points
// (1pt = 1/72in), top-left origin per `pdftotext -bbox-layout`.
type WordBox struct {
	Text                   string
	XMin, YMin, XMax, YMax float64
}

// PageBoxes holds all words on a single page plus the page's pt dimensions.
type PageBoxes struct {
	Words        []WordBox
	PageW, PageH float64
}

// PageRect is a highlight rectangle on a page in PDF points.
type PageRect struct {
	XMin, YMin, XMax, YMax float64
}

// BBoxCache lazily extracts and caches per-page word boxes for one PDF.
// Failures are cached separately so a transient pdftotext crash on one
// page (poppler hits font edge cases on some PDFs) doesn't poison
// subsequent successful pages.
type BBoxCache struct {
	pdfPath string
	mu      sync.Mutex
	pages   map[int]*PageBoxes
	failed  map[int]error
}

// NewBBoxCache returns an empty cache bound to pdfPath. Pages are extracted
// on first request.
func NewBBoxCache(pdfPath string) *BBoxCache {
	return &BBoxCache{
		pdfPath: pdfPath,
		pages:   map[int]*PageBoxes{},
		failed:  map[int]error{},
	}
}

// Page returns word boxes for page (1-indexed).
func (c *BBoxCache) Page(page int) (*PageBoxes, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if pb, ok := c.pages[page]; ok {
		return pb, nil
	}
	if err, ok := c.failed[page]; ok {
		return nil, err
	}
	pb, err := extractPageBoxes(c.pdfPath, page)
	if err != nil {
		c.failed[page] = err
		return nil, err
	}
	c.pages[page] = pb
	return pb, nil
}

// FindQuote locates a contiguous run of words on page (1-indexed) whose
// joined whitespace-normalized text contains the (likewise normalized)
// quote. Returns one rect per page-line covering the run; ok=false on
// miss (caller falls back to page-only highlighting).
func (c *BBoxCache) FindQuote(page int, quote string) ([]PageRect, bool) {
	if strings.TrimSpace(quote) == "" {
		return nil, false
	}
	pb, err := c.Page(page)
	if err != nil || pb == nil || len(pb.Words) == 0 {
		return nil, false
	}
	target := normalizeWS(quote)
	if target == "" {
		return nil, false
	}
	var sb strings.Builder
	starts := make([]int, len(pb.Words))
	ends := make([]int, len(pb.Words))
	for i, w := range pb.Words {
		if sb.Len() > 0 {
			sb.WriteByte(' ')
		}
		starts[i] = sb.Len()
		sb.WriteString(normalizeWS(w.Text))
		ends[i] = sb.Len()
	}
	concat := sb.String()
	idx := strings.Index(concat, target)
	if idx < 0 {
		return nil, false
	}
	end := idx + len(target)
	first, last := -1, -1
	for i := range pb.Words {
		if first < 0 && ends[i] > idx {
			first = i
		}
		if starts[i] < end {
			last = i
		}
	}
	if first < 0 || last < 0 || last < first {
		return nil, false
	}
	// Group consecutive words by baseline (yMin within a small tolerance);
	// each group becomes one rect — multi-line quotes therefore produce
	// multi-line highlights instead of one giant box spanning whitespace.
	const yTol = 2.0
	groups := [][]int{{first}}
	for i := first + 1; i <= last; i++ {
		prev := pb.Words[groups[len(groups)-1][len(groups[len(groups)-1])-1]].YMin
		if absf(pb.Words[i].YMin-prev) <= yTol {
			groups[len(groups)-1] = append(groups[len(groups)-1], i)
		} else {
			groups = append(groups, []int{i})
		}
	}
	rects := make([]PageRect, 0, len(groups))
	for _, g := range groups {
		rects = append(rects, wordsBBox(pb.Words, g))
	}
	return rects, true
}

func wordsBBox(words []WordBox, idxs []int) PageRect {
	w := words[idxs[0]]
	r := PageRect{XMin: w.XMin, YMin: w.YMin, XMax: w.XMax, YMax: w.YMax}
	for _, i := range idxs[1:] {
		w := words[i]
		if w.XMin < r.XMin {
			r.XMin = w.XMin
		}
		if w.YMin < r.YMin {
			r.YMin = w.YMin
		}
		if w.XMax > r.XMax {
			r.XMax = w.XMax
		}
		if w.YMax > r.YMax {
			r.YMax = w.YMax
		}
	}
	return r
}

func normalizeWS(s string) string {
	var b strings.Builder
	prevSpace := true
	for _, r := range s {
		if r == ' ' || r == '\t' || r == '\n' || r == '\r' || r == '\f' {
			if !prevSpace {
				b.WriteByte(' ')
				prevSpace = true
			}
		} else {
			b.WriteRune(r)
			prevSpace = false
		}
	}
	return strings.TrimSpace(b.String())
}

func absf(f float64) float64 {
	if f < 0 {
		return -f
	}
	return f
}

func extractPageBoxes(pdfPath string, page int) (*PageBoxes, error) {
	if page < 1 {
		return nil, fmt.Errorf("bbox: page %d invalid", page)
	}
	cmd := exec.Command("pdftotext",
		"-bbox-layout",
		"-f", strconv.Itoa(page),
		"-l", strconv.Itoa(page),
		pdfPath, "-",
	)
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("pdftotext -bbox-layout p.%d: %w", page, err)
	}
	return parseBBoxXML(out)
}

// parseBBoxXML walks the XHTML output of `pdftotext -bbox-layout` and
// returns the words on the (sole, expected) <page> element. Tolerant of
// poppler's occasional malformed output: we accept a partial parse if at
// least some words came through, and we treat any decoder error after
// progress as end-of-stream rather than failure.
func parseBBoxXML(data []byte) (*PageBoxes, error) {
	dec := xml.NewDecoder(strings.NewReader(string(data)))
	dec.Strict = false
	dec.AutoClose = xml.HTMLAutoClose
	dec.Entity = xml.HTMLEntity

	pb := &PageBoxes{}
	var cur *WordBox
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			break
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "page":
				if pb.PageW == 0 {
					pb.PageW = readFloatAttr(t, "width")
					pb.PageH = readFloatAttr(t, "height")
				}
			case "word":
				cur = &WordBox{
					XMin: readFloatAttr(t, "xMin"),
					YMin: readFloatAttr(t, "yMin"),
					XMax: readFloatAttr(t, "xMax"),
					YMax: readFloatAttr(t, "yMax"),
				}
			}
		case xml.CharData:
			if cur != nil {
				cur.Text += string(t)
			}
		case xml.EndElement:
			if t.Name.Local == "word" && cur != nil {
				cur.Text = strings.TrimSpace(cur.Text)
				if cur.Text != "" {
					pb.Words = append(pb.Words, *cur)
				}
				cur = nil
			}
		}
	}
	if len(pb.Words) == 0 {
		return nil, errors.New("bbox: no words extracted")
	}
	return pb, nil
}

func readFloatAttr(e xml.StartElement, name string) float64 {
	for _, a := range e.Attr {
		if a.Name.Local == name {
			if v, err := strconv.ParseFloat(a.Value, 64); err == nil {
				return v
			}
		}
	}
	return 0
}
