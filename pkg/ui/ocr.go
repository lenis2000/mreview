package ui

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
	"unicode"

	tea "github.com/charmbracelet/bubbletea"

	"mreview/pkg/pdf"
	"mreview/pkg/synctex"
)

const ocrReportDir = ".mreview-ocr-reports"

type ocrReportMsg struct {
	status string
}

// startOCRReport kicks off an OCR bug report for the current PDF crop.
// Re-runs the crop pipeline to get PNG bytes (the kitty escape threw
// them away), runs tesseract, compares to the cursor block's source,
// and writes a report file to .mreview-ocr-reports/.
func (m Model) startOCRReport() (Model, tea.Cmd) {
	if m.Doc == nil || m.CursorBlockID == "" {
		m.Status = "B: no block selected"
		return m, nil
	}
	if m.PDF == nil || m.Synctex == nil {
		m.Status = "B: no PDF/SyncTeX loaded"
		return m, nil
	}
	block := m.Doc.ByID[m.CursorBlockID]
	if block == nil || block.StartLine == 0 {
		m.Status = "B: block has no source range"
		return m, nil
	}

	file := block.File
	if file == "" {
		file = m.Doc.File
	}
	region := m.Synctex.RegionForLines(file, block.StartLine, block.EndLine)
	if region == nil || !pdf.HasExtent(*region) {
		m.Status = "B: block has no PDF region"
		return m, nil
	}

	cellW, cellH := pdf.DetectCellPixelSize()
	w, h := pdfPaneCells(m.Width, m.Height, m.Layout)
	paneWPx := int(float64(w) * cellW)
	paneHPx := int(float64(h) * cellH)

	multi := false
	if m.pageLayout != nil {
		multi = m.pageLayout.IsMultiColumn(m.PDF, m.Doc, region.Page)
	}

	pngBytes, err := pdf.CropFitted(m.PDF, *region, pdf.FitOptions{
		PaneWidthPx:  paneWPx,
		PaneHeightPx: paneHPx,
		MultiColumn:  multi,
	})
	if err != nil {
		m.Status = "B: crop failed — " + err.Error()
		return m, nil
	}

	blockSource := block.Source
	blockID := block.ID
	breadcrumb := block.Title
	if breadcrumb == "" {
		breadcrumb = block.Kind.String()
	}
	startLine := block.StartLine
	endLine := block.EndLine
	docFile := m.Doc.File
	regCopy := *region
	paneCells := fmt.Sprintf("%dx%d", w, h)

	m.Status = "B: running OCR…"
	return m, func() tea.Msg {
		report, err := buildOCRReport(pngBytes, blockSource, blockID, breadcrumb, docFile, startLine, endLine, regCopy, paneCells)
		if err != nil {
			return ocrReportMsg{status: "B: " + err.Error()}
		}
		return ocrReportMsg{status: report}
	}
}

func buildOCRReport(pngBytes []byte, blockSource, blockID, breadcrumb, docFile string, startLine, endLine int, region synctex.Region, paneCells string) (string, error) {
	ocrText, ocrErr := runTesseract(pngBytes)
	if ocrErr != nil {
		ocrText = "(tesseract not available: " + ocrErr.Error() + ")"
	}

	sim := ocrSimilarity(blockSource, ocrText)

	ts := time.Now().Format("20060102-150405")
	slug := sanitizeFilename(blockID)
	if len(slug) > 40 {
		slug = slug[:40]
	}
	base := fmt.Sprintf("%s-%s", ts, slug)

	dir := ocrReportDir
	if docFile != "" {
		dir = filepath.Join(filepath.Dir(docFile), ocrReportDir)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("mkdir %s: %w", dir, err)
	}

	pngPath := filepath.Join(dir, base+".png")
	if err := os.WriteFile(pngPath, pngBytes, 0o644); err != nil {
		return "", fmt.Errorf("write png: %w", err)
	}

	mdPath := filepath.Join(dir, base+".md")
	var buf bytes.Buffer
	fmt.Fprintf(&buf, "# OCR Bug Report — %s\n\n", ts)
	fmt.Fprintf(&buf, "**Block:** `%s`\n", blockID)
	fmt.Fprintf(&buf, "**Breadcrumb:** %s\n", breadcrumb)
	fmt.Fprintf(&buf, "**File:** %s:L%d-L%d\n", docFile, startLine, endLine)
	fmt.Fprintf(&buf, "**Pane:** %s cells\n", paneCells)
	fmt.Fprintf(&buf, "**Region:** page %d (x=%.1f y=%.1f w=%.1f h=%.1f pt)\n",
		region.Page, region.X, region.Y, region.W, region.H)
	fmt.Fprintf(&buf, "**OCR Similarity:** %.2f\n\n", sim)
	fmt.Fprintf(&buf, "**Crop:** [%s.png](%s.png)\n\n", base, base)
	fmt.Fprintf(&buf, "## Source (LaTeX)\n\n```latex\n%s\n```\n\n", blockSource)
	fmt.Fprintf(&buf, "## OCR Output\n\n```\n%s\n```\n\n", strings.TrimSpace(ocrText))

	if sim < 0.3 {
		fmt.Fprintf(&buf, "## Auto-notes\n\n- Similarity very low (%.2f) — likely wrong page/region or OCR failed on math-heavy content.\n", sim)
	} else if sim < 0.6 {
		fmt.Fprintf(&buf, "## Auto-notes\n\n- Moderate similarity (%.2f) — may indicate a partial mismatch or heavy math that OCR couldn't parse.\n", sim)
	}

	if err := os.WriteFile(mdPath, buf.Bytes(), 0o644); err != nil {
		return "", fmt.Errorf("write report: %w", err)
	}

	return fmt.Sprintf("OCR report saved · sim=%.2f · %s", sim, filepath.Base(mdPath)), nil
}

func runTesseract(pngBytes []byte) (string, error) {
	bin, err := exec.LookPath("tesseract")
	if err != nil {
		return "", fmt.Errorf("tesseract not installed (brew install tesseract)")
	}
	cmd := exec.Command(bin, "stdin", "stdout", "-l", "eng", "--psm", "6")
	cmd.Stdin = bytes.NewReader(pngBytes)
	var out, errBuf bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errBuf
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("tesseract: %v (%s)", err, strings.TrimSpace(errBuf.String()))
	}
	return out.String(), nil
}

// ocrSimilarity computes a loose similarity between LaTeX source and
// OCR output: strips non-alphanumeric characters and whitespace, then
// does a character-level Jaccard on bigrams. Not meant to be precise —
// it's a triage signal for the report.
func ocrSimilarity(latexSource, ocrText string) float64 {
	norm := func(s string) string {
		var b strings.Builder
		for _, r := range strings.ToLower(s) {
			if unicode.IsLetter(r) || unicode.IsDigit(r) {
				b.WriteRune(r)
			}
		}
		return b.String()
	}
	a := norm(latexSource)
	b := norm(ocrText)
	if len(a) < 2 || len(b) < 2 {
		return 0
	}
	bigrams := func(s string) map[string]int {
		m := map[string]int{}
		for i := 0; i < len(s)-1; i++ {
			m[s[i:i+2]]++
		}
		return m
	}
	ba := bigrams(a)
	bb := bigrams(b)
	var inter, union int
	for k, va := range ba {
		vb := bb[k]
		if va < vb {
			inter += va
		} else {
			inter += vb
		}
		if va > vb {
			union += va
		} else {
			union += vb
		}
	}
	for k, vb := range bb {
		if _, ok := ba[k]; !ok {
			union += vb
		}
	}
	if union == 0 {
		return 0
	}
	return float64(inter) / float64(union)
}

func sanitizeFilename(s string) string {
	var b strings.Builder
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '-' || r == '_' || r == '.' {
			b.WriteRune(r)
		} else {
			b.WriteRune('_')
		}
	}
	return b.String()
}
