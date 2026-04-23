package ui

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"mreview/pkg/parser"
	"mreview/pkg/pdf"
	"mreview/pkg/synctex"
)

// pdfFixturePath locates testdata/<name> relative to the repo root.
func pdfFixturePath(t *testing.T, name string) string {
	t.Helper()
	p, err := filepath.Abs(filepath.Join("..", "..", "testdata", name))
	require.NoError(t, err)
	return p
}

// waitForCmd invokes a tea.Cmd (or its nested Ticks) and returns the first
// message. Returns nil if no message arrives within timeout.
func waitForCmd(t *testing.T, cmd tea.Cmd, timeout time.Duration) tea.Msg {
	t.Helper()
	if cmd == nil {
		return nil
	}
	ch := make(chan tea.Msg, 1)
	go func() { ch <- cmd() }()
	select {
	case msg := <-ch:
		return msg
	case <-time.After(timeout):
		return nil
	}
}

func TestPDFPaneBody_NoPDFLoaded(t *testing.T) {
	m := New(parsedSample(t), nil)
	m.Width = 120
	m.Height = 30
	body := m.pdfPaneBody()
	assert.Equal(t, "(no PDF loaded)", body)
}

func TestPDFPaneBody_ShowsImageAndStatus(t *testing.T) {
	m := New(parsedSample(t), nil)
	m.PDFImage = "ESCAPE"
	assert.Equal(t, "ESCAPE", m.pdfPaneBody())
	m.PDFImage = ""
	m.PDFStatus = "(no region — block outside PDF)"
	assert.Equal(t, "(no region — block outside PDF)", m.pdfPaneBody())
}

func TestSchedulePDFRender_NilWithoutPDF(t *testing.T) {
	m := New(parsedSample(t), nil)
	assert.Nil(t, m.schedulePDFRender())
}

func TestHandlePDFRender_DropsStaleGeneration(t *testing.T) {
	m := New(parsedSample(t), nil)
	m.pdfGen = 5
	m.PDFImage = "ORIG"
	out, _ := m.handlePDFRender(pdfRenderMsg{Generation: 3, Image: "STALE"})
	assert.Equal(t, "ORIG", out.(Model).PDFImage, "stale generation ignored")
}

func TestHandlePDFRender_AppliesMatchingGeneration(t *testing.T) {
	m := New(parsedSample(t), nil)
	m.pdfGen = 7
	out, _ := m.handlePDFRender(pdfRenderMsg{Generation: 7, Image: "NEW", Status: ""})
	nm := out.(Model)
	assert.Equal(t, "NEW", nm.PDFImage)
	assert.Equal(t, "", nm.PDFStatus)
}

func TestUpdate_CursorMoveSchedulesRender(t *testing.T) {
	// Build a minimal pipeline: parse fixture, open the fixture PDF and
	// its synctex index, feed both into the Model, then simulate a
	// key-triggered cursor move and assert a tick cmd fires.
	texPath := pdfFixturePath(t, "sample.tex")
	pdfPath := pdfFixturePath(t, "sample.pdf")
	synctexPath := pdfFixturePath(t, "sample.synctex.gz")
	src, err := readFile(texPath)
	require.NoError(t, err)
	doc, err := parser.Parse(src)
	require.NoError(t, err)
	doc.File = texPath

	pdfDoc, err := pdf.Open(pdfPath)
	require.NoError(t, err)
	defer pdfDoc.Close()

	idx, err := synctex.Open(synctexPath)
	require.NoError(t, err)

	m := New(doc, nil)
	m.PDF = pdfDoc
	m.Synctex = idx
	m.Width = 120
	m.Height = 40

	// j = next sibling. Must pick a starting block that has a next.
	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}}
	out, cmd := m.Update(msg)
	nm := out.(Model)
	assert.GreaterOrEqual(t, nm.pdfGen, 1, "scheduling bumps generation")
	require.NotNil(t, cmd, "cursor change must schedule a PDF render")

	// Run the tick to completion and confirm we receive a pdfRenderMsg.
	got := waitForCmd(t, cmd, 500*time.Millisecond)
	require.NotNil(t, got)
	rm, ok := got.(pdfRenderMsg)
	require.True(t, ok, "expected pdfRenderMsg, got %T", got)
	assert.Equal(t, nm.pdfGen, rm.Generation)
	// Either an escape string or a status message — both are valid outputs.
	if rm.Image == "" {
		assert.NotEmpty(t, rm.Status, "empty image must carry a status")
	}
}

func TestUpdate_WindowSizeSchedulesRenderWhenPDFReady(t *testing.T) {
	m := New(parsedSample(t), nil)
	// Fake a PDF+synctex wire-up: nil causes the scheduler to return nil
	// instead of firing a command. Assert that behaviour explicitly.
	updated, cmd := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	assert.Nil(t, cmd, "no command fires without a PDF")
	nm := updated.(Model)
	assert.Equal(t, 120, nm.Width)
}

func TestPDFCropCache_LRU(t *testing.T) {
	c := newPDFCropCache(2)
	k1 := pdfCropKey{BlockID: "a"}
	k2 := pdfCropKey{BlockID: "b"}
	k3 := pdfCropKey{BlockID: "c"}
	c.put(k1, "esc-a")
	c.put(k2, "esc-b")
	c.put(k3, "esc-c")

	_, ok := c.get(k1)
	assert.False(t, ok, "oldest entry evicted")
	esc, ok := c.get(k2)
	assert.True(t, ok)
	assert.Equal(t, "esc-b", esc)
	esc, ok = c.get(k3)
	assert.True(t, ok)
	assert.Equal(t, "esc-c", esc)
}

func TestPDFCropCache_UpdateExisting(t *testing.T) {
	c := newPDFCropCache(4)
	k := pdfCropKey{BlockID: "a"}
	c.put(k, "v1")
	c.put(k, "v2")
	esc, ok := c.get(k)
	require.True(t, ok)
	assert.Equal(t, "v2", esc, "re-put replaces value")
	assert.Equal(t, 1, c.ll.Len())
}

func TestRenderPDFForBlock_CachesResult(t *testing.T) {
	texPath := pdfFixturePath(t, "sample.tex")
	src, err := readFile(texPath)
	require.NoError(t, err)
	doc, err := parser.Parse(src)
	require.NoError(t, err)
	doc.File = texPath

	pdfDoc, err := pdf.Open(pdfFixturePath(t, "sample.pdf"))
	require.NoError(t, err)
	defer pdfDoc.Close()

	idx, err := synctex.Open(pdfFixturePath(t, "sample.synctex.gz"))
	require.NoError(t, err)

	// First non-root block will have a start/end line range that should
	// usually resolve via synctex; if not, the status is the placeholder.
	require.NotEmpty(t, doc.Root.ChildIDs)
	blockID := doc.Root.ChildIDs[0]

	cache := newPDFCropCache(8)
	inputs := pdfRenderInputs{
		Doc:         doc,
		BlockID:     blockID,
		PDF:         pdfDoc,
		Index:       idx,
		WidthCells:  40,
		HeightCells: 30,
	}
	img1, status1 := renderPDFForBlock(inputs, cache)
	img2, status2 := renderPDFForBlock(inputs, cache)
	assert.Equal(t, img1, img2)
	assert.Equal(t, status1, status2)
	if img1 != "" {
		// Cached — the second lookup should not add a new entry.
		assert.Equal(t, 1, cache.ll.Len())
	}
}

func TestPDFPaneCells_Inset(t *testing.T) {
	w, h := pdfPaneCells(120, 40)
	assert.Greater(t, w, 0)
	assert.Greater(t, h, 0)
	// Inner width should be a bit less than 35% of 120 minus border cells.
	assert.Less(t, w, 42)
	// Height minus status(1) minus border(2) minus title(1) = 36.
	assert.Equal(t, 40-1-2-1, h)
}

func TestPDFPaneCells_Degenerate(t *testing.T) {
	w, h := pdfPaneCells(0, 0)
	assert.Equal(t, 0, w)
	assert.Equal(t, 0, h)
	w, h = pdfPaneCells(2, 2)
	assert.GreaterOrEqual(t, w, 1)
	assert.GreaterOrEqual(t, h, 1)
}

// readFile is a tiny os.ReadFile wrapper used across a few tests here.
func readFile(path string) ([]byte, error) {
	return os.ReadFile(path)
}
