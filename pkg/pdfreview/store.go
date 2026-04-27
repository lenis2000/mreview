package pdfreview

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// ReportPath returns the conventional location of the anchored-comments
// JSON for a given PDF: <pdf>.pdf-comments.json next to the PDF.
func ReportPath(pdfPath string) string {
	return pdfPath + ".pdf-comments.json"
}

// LoadReport reads a Report from disk. Missing-file errors are returned
// untouched so the caller can offer "run pdf-comments first" guidance.
func LoadReport(path string) (*Report, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var r Report
	if err := json.Unmarshal(b, &r); err != nil {
		return nil, fmt.Errorf("decode %s: %w", path, err)
	}
	// Backfill defaults so older anchoring outputs (without kind/status)
	// load cleanly.
	for i := range r.Comments {
		c := &r.Comments[i]
		if c.Kind == "" {
			c.Kind = KindComment
		}
		if !ValidKind(c.Kind) {
			c.Kind = KindComment
		}
		if c.Status == "" {
			c.Status = StatusPending
		}
	}
	return &r, nil
}

// SaveReport writes a Report to disk atomically (tmp file + rename) so a
// crash mid-write can't leave a truncated JSON.
func SaveReport(path string, r *Report) error {
	enc, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	enc = append(enc, '\n')
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, enc, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("rename %s -> %s: %w", tmp, path, err)
	}
	return nil
}

// LetterPath returns the default location of the rendered letter:
// trims a trailing ".pdf" and appends ".review.md". For a PDF without
// that suffix the suffix is appended verbatim.
func LetterPath(pdfPath string) string {
	if strings.HasSuffix(strings.ToLower(pdfPath), ".pdf") {
		return pdfPath[:len(pdfPath)-4] + ".review.md"
	}
	return pdfPath + ".review.md"
}
