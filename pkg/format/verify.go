package format

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	"mreview/pkg/build"
	"mreview/pkg/synctex"
)

// Tree describes the set of build inputs needed to compile a paper.
// The verifier copies all listed files into an isolated tempdir so
// latexmk runs do not collide with the user's live build.
type Tree struct {
	// Root is the directory containing the paper and its build inputs.
	Root string
	// Paper is the basename of the main .tex file (e.g. "paper.tex").
	Paper string
	// Files lists all build-input paths relative to Root: the main .tex,
	// latexmkrc, .cls, .sty, .bib/.bbl, figures, \input children, etc.
	Files []string
}

// Diff records a single mismatched line between before/after pdftotext output.
type Diff struct {
	Page       int
	LineInPage int // 0-based line within the page's pdftotext output
	Before     string
	After      string
}

// VerifyResult holds the outcome of a verification run.
type VerifyResult struct {
	OK         bool
	Unexpected []Diff
	Warnings   []string
	// BeforePDF and AfterPDF are the paths to the PDFs built by the verifier
	// in the tempdir. Available for paranoid (pixel-level) verification.
	BeforePDF string
	AfterPDF  string
}

// Verify builds before/after PDFs in isolated tempdirs, extracts text via
// pdftotext, and compares them. Tier-1 rules expect byte-identical normalised
// text. Tier-2 rules declare ExpectedDiffSourceLines in their Hits; those
// source lines are mapped to PDF text lines via synctex and whitelisted.
//
// Returns ok=true if all diffs are whitelisted. Callers should refuse to write
// when ok=false.
func Verify(tree Tree, beforeSrc, afterSrc []byte, hits []Hit) (*VerifyResult, error) {
	// Create isolated tempdirs. setLastTmpDir cleans the previous run's
	// tempdir so /tmp/mr-fmt-* doesn't accumulate unboundedly.
	tmpBase, err := os.MkdirTemp("", "mr-fmt-")
	if err != nil {
		return nil, fmt.Errorf("verify: create tempdir: %w", err)
	}
	setLastTmpDir(tmpBase)
	beforeDir := filepath.Join(tmpBase, "before")
	afterDir := filepath.Join(tmpBase, "after")
	if err := os.MkdirAll(beforeDir, 0o755); err != nil {
		return nil, fmt.Errorf("verify: mkdir before: %w", err)
	}
	if err := os.MkdirAll(afterDir, 0o755); err != nil {
		return nil, fmt.Errorf("verify: mkdir after: %w", err)
	}

	// Copy all build inputs to both tempdirs.
	for _, rel := range tree.Files {
		// Defense-in-depth: reject paths that would escape the root or tempdir.
		// Use ".." + separator prefix (not bare "..") to avoid false positives
		// on valid filenames like "..paper.tex".
		cleaned := filepath.Clean(rel)
		if filepath.IsAbs(rel) || cleaned == ".." || strings.HasPrefix(cleaned, ".."+string(filepath.Separator)) {
			return nil, fmt.Errorf("verify: rejecting out-of-tree path %q", rel)
		}
		srcPath := filepath.Join(tree.Root, rel)
		data, readErr := os.ReadFile(srcPath)
		if readErr != nil {
			return nil, fmt.Errorf("verify: read %s: %w", rel, readErr)
		}
		for _, dir := range []string{beforeDir, afterDir} {
			dst := filepath.Join(dir, rel)
			if mkErr := os.MkdirAll(filepath.Dir(dst), 0o755); mkErr != nil {
				return nil, fmt.Errorf("verify: mkdir for %s: %w", rel, mkErr)
			}
			if wErr := os.WriteFile(dst, data, 0o644); wErr != nil {
				return nil, fmt.Errorf("verify: write %s: %w", rel, wErr)
			}
		}
	}

	// Overwrite paper.tex in each tempdir with the correct version.
	paperRel := tree.Paper
	cleanedPaper := filepath.Clean(paperRel)
	if filepath.IsAbs(paperRel) || cleanedPaper == ".." || strings.HasPrefix(cleanedPaper, ".."+string(filepath.Separator)) {
		return nil, fmt.Errorf("verify: rejecting out-of-tree paper path %q", paperRel)
	}
	if err := os.WriteFile(filepath.Join(beforeDir, paperRel), beforeSrc, 0o644); err != nil {
		return nil, fmt.Errorf("verify: write before paper: %w", err)
	}
	if err := os.WriteFile(filepath.Join(afterDir, paperRel), afterSrc, 0o644); err != nil {
		return nil, fmt.Errorf("verify: write after paper: %w", err)
	}

	// Build both.
	beforeRes, err := buildInDir(beforeDir, paperRel)
	if err != nil {
		return nil, fmt.Errorf("verify: build before: %w", err)
	}
	afterRes, err := buildInDir(afterDir, paperRel)
	if err != nil {
		return nil, fmt.Errorf("verify: build after: %w", err)
	}

	// Page-count precondition via pdfinfo.
	beforePages, err := pdfPageCount(beforeRes.PDFPath)
	if err != nil {
		return nil, fmt.Errorf("verify: pdfinfo before: %w", err)
	}
	afterPages, err := pdfPageCount(afterRes.PDFPath)
	if err != nil {
		return nil, fmt.Errorf("verify: pdfinfo after: %w", err)
	}
	if beforePages != afterPages {
		return &VerifyResult{
			OK: false,
			Unexpected: []Diff{{
				Before: fmt.Sprintf("page count: %d", beforePages),
				After:  fmt.Sprintf("page count: %d", afterPages),
			}},
		}, nil
	}

	// Extract text via pdftotext (default mode, NOT -layout).
	beforeText, err := runPdftotext(beforeRes.PDFPath)
	if err != nil {
		return nil, fmt.Errorf("verify: pdftotext before: %w", err)
	}
	afterText, err := runPdftotext(afterRes.PDFPath)
	if err != nil {
		return nil, fmt.Errorf("verify: pdftotext after: %w", err)
	}

	// Whitespace-normalize each line.
	beforeNorm := normalizeTextLines(beforeText)
	afterNorm := normalizeTextLines(afterText)

	// Split into per-page sections. pdftotext uses form-feed (\f) as page separator.
	beforePages2 := splitPages(beforeNorm)
	afterPages2 := splitPages(afterNorm)

	// Build the whitelist from synctex + hits. We use the AFTER synctex because
	// Hits' ExpectedDiffSourceLines are computed from the post-modification source
	// (Tier-1 rules may have shifted line numbers before Tier-2 rules ran).
	whitelist, syncErr := buildWhitelist(afterRes.SyncTeXPath, tree.Paper, hits)
	if syncErr != nil {
		// If synctex fails and there are Tier-2 hits, that's an error — the
		// whitelist is load-bearing. If only Tier-1 hits, no whitelist needed.
		hasTier2 := false
		for _, h := range hits {
			if h.ExpectedDiffSourceLines != nil {
				hasTier2 = true
				break
			}
		}
		if hasTier2 {
			return nil, fmt.Errorf("verify: synctex required for Tier-2 whitelist: %w", syncErr)
		}
		// Tier-1 only: whitelist is empty, proceed.
		whitelist = nil
	}

	// Diff line by line across pages.
	var unexpected []Diff
	maxPages := len(beforePages2)
	if len(afterPages2) > maxPages {
		maxPages = len(afterPages2)
	}

	for p := 0; p < maxPages; p++ {
		var bLines, aLines []string
		if p < len(beforePages2) {
			bLines = beforePages2[p]
		}
		if p < len(afterPages2) {
			aLines = afterPages2[p]
		}
		maxLines := len(bLines)
		if len(aLines) > maxLines {
			maxLines = len(aLines)
		}
		for li := 0; li < maxLines; li++ {
			var bl, al string
			if li < len(bLines) {
				bl = bLines[li]
			}
			if li < len(aLines) {
				al = aLines[li]
			}
			if bl == al {
				continue
			}
			// Check whitelist.
			pageNum := p + 1 // 1-based
			if isWhitelisted(whitelist, pageNum, li) {
				continue
			}
			unexpected = append(unexpected, Diff{
				Page:       pageNum,
				LineInPage: li,
				Before:     bl,
				After:      al,
			})
		}
	}

	// No-op detection: check if Tier-2 expected-diff regions saw zero actual diff.
	var warnings []string
	if whitelist != nil {
		warnings = detectNoOps(whitelist, beforePages2, afterPages2, hits)
	}

	return &VerifyResult{
		OK:         len(unexpected) == 0,
		Unexpected: unexpected,
		Warnings:   warnings,
		BeforePDF:  beforeRes.PDFPath,
		AfterPDF:   afterRes.PDFPath,
	}, nil
}

// whitelistEntry maps a source line to its PDF text position.
type whitelistEntry struct {
	Page       int
	LineInPage int // 0-based line within the page's pdftotext output
	SourceLine int // original source line that declared this whitelist
	RuleID     string
}

// buildWhitelist uses synctex to map each Hit's ExpectedDiffSourceLines to
// pdftotext line positions. Returns nil if no Tier-2 hits exist.
func buildWhitelist(synctexPath, paperBasename string, hits []Hit) ([]whitelistEntry, error) {
	// Check if any hits declare expected diffs.
	hasTier2 := false
	for _, h := range hits {
		if h.ExpectedDiffSourceLines != nil {
			hasTier2 = true
			break
		}
	}
	if !hasTier2 {
		return nil, nil
	}

	idx, err := synctex.Open(synctexPath)
	if err != nil {
		return nil, fmt.Errorf("open synctex: %w", err)
	}

	var entries []whitelistEntry
	for _, h := range hits {
		if h.ExpectedDiffSourceLines == nil {
			continue
		}
		for _, srcLine := range h.ExpectedDiffSourceLines {
			reg := idx.RegionForLines(paperBasename, srcLine, srcLine)
			if reg == nil {
				// Source line not found in synctex — may be a blank line or
				// outside the main document. Skip silently.
				continue
			}
			// Map the region's Y position to a pdftotext line index.
			// pdftotext outputs ~45 lines per page for a standard article;
			// we estimate the line index from the Y position. This is
			// approximate but sufficient for whitelist matching — we whitelist
			// a +-2 line window around the estimated position.
			estimatedLine := estimatePdftextLine(reg.Y, reg.H)
			entries = append(entries, whitelistEntry{
				Page:       reg.Page,
				LineInPage: estimatedLine,
				SourceLine: srcLine,
				RuleID:     h.RuleID,
			})
		}
	}
	return entries, nil
}

// estimatePdftextLine converts a Y coordinate (PDF big points from top) to an
// approximate 0-based line index in pdftotext output. Standard letter paper is
// 792bp tall; a typical text region spans ~650bp with ~45 lines, giving ~14.4bp
// per line. We use 14 as a reasonable estimate.
func estimatePdftextLine(y, h float64) int {
	// Approximate: assume top margin ~72bp, line height ~14bp.
	const topMargin = 72.0
	const lineHeight = 14.0
	line := int((y - topMargin) / lineHeight)
	if line < 0 {
		line = 0
	}
	return line
}

// isWhitelisted checks if a diff at (page, lineInPage) is covered by any
// whitelist entry. Uses a +-2 line tolerance window.
func isWhitelisted(entries []whitelistEntry, page, lineInPage int) bool {
	const tolerance = 2
	for _, e := range entries {
		if e.Page != page {
			continue
		}
		if lineInPage >= e.LineInPage-tolerance && lineInPage <= e.LineInPage+tolerance {
			return true
		}
	}
	return false
}

// detectNoOps checks whether any Tier-2 rule's expected-diff region actually
// had zero diff. Returns warnings for those cases.
func detectNoOps(entries []whitelistEntry, beforePages, afterPages [][]string, hits []Hit) []string {
	// Group whitelist entries by (ruleID, sourceLine).
	type key struct {
		ruleID     string
		sourceLine int
	}
	seen := map[key]bool{}
	entryMap := map[key][]whitelistEntry{}
	for _, e := range entries {
		k := key{e.RuleID, e.SourceLine}
		entryMap[k] = append(entryMap[k], e)
		seen[k] = false // initially no diff seen
	}

	// Check each entry for actual diffs.
	const tolerance = 2
	for k, ents := range entryMap {
		for _, e := range ents {
			p := e.Page - 1 // 0-based
			if p < 0 || p >= len(beforePages) || p >= len(afterPages) {
				continue
			}
			bLines := beforePages[p]
			aLines := afterPages[p]
			for li := e.LineInPage - tolerance; li <= e.LineInPage+tolerance; li++ {
				if li < 0 {
					continue
				}
				var bl, al string
				if li < len(bLines) {
					bl = bLines[li]
				}
				if li < len(aLines) {
					al = aLines[li]
				}
				if bl != al {
					seen[k] = true
					break
				}
			}
			if seen[k] {
				break
			}
		}
		_ = k // suppress
	}

	var warnings []string
	for k, diffSeen := range seen {
		if !diffSeen {
			warnings = append(warnings, fmt.Sprintf(
				"%s hit at L%d produced no PDF change — heuristic may be too aggressive",
				k.ruleID, k.sourceLine,
			))
		}
	}
	return warnings
}

// buildInDir runs latexmk in dir on the given paper file.
func buildInDir(dir, paper string) (*build.Result, error) {
	texPath := filepath.Join(dir, paper)
	return build.RunWith(build.Options{
		TexPath: texPath,
		Dir:     dir,
	})
}

// pdfPageCount returns the number of pages in a PDF using pdfinfo.
func pdfPageCount(pdfPath string) (int, error) {
	out, err := exec.Command("pdfinfo", pdfPath).Output()
	if err != nil {
		return 0, fmt.Errorf("pdfinfo: %w", err)
	}
	for _, line := range strings.Split(string(out), "\n") {
		if strings.HasPrefix(line, "Pages:") {
			field := strings.TrimSpace(strings.TrimPrefix(line, "Pages:"))
			var n int
			if _, scanErr := fmt.Sscanf(field, "%d", &n); scanErr == nil {
				return n, nil
			}
		}
	}
	return 0, fmt.Errorf("pdfinfo: no Pages: field in output")
}

// runPdftotext runs pdftotext (default mode, NOT -layout) and returns stdout.
func runPdftotext(pdfPath string) ([]byte, error) {
	cmd := exec.Command("pdftotext", pdfPath, "-")
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("pdftotext: %w", err)
	}
	return out, nil
}

// normalizeTextLines whitespace-normalizes the output of pdftotext:
// collapse internal runs of whitespace to single space, strip trailing
// whitespace per line. Returns the normalized text as a single string.
func normalizeTextLines(raw []byte) string {
	var buf strings.Builder
	sc := bufio.NewScanner(bytes.NewReader(raw))
	first := true
	for sc.Scan() {
		line := sc.Text()
		norm := collapseSpaces(strings.TrimRight(line, " \t"))
		if !first {
			buf.WriteByte('\n')
		}
		buf.WriteString(norm)
		first = false
	}
	return buf.String()
}

var multiSpaceRe = regexp.MustCompile(`[ \t\r]+`)

// collapseSpaces replaces runs of horizontal whitespace (spaces, tabs,
// carriage returns) with a single space. Form-feed characters are preserved
// because splitPages uses them as page separators.
func collapseSpaces(s string) string {
	return multiSpaceRe.ReplaceAllString(s, " ")
}

// splitPages splits normalised pdftotext output by form-feed characters into
// per-page line slices.
func splitPages(norm string) [][]string {
	pages := strings.Split(norm, "\f")
	result := make([][]string, len(pages))
	for i, page := range pages {
		page = strings.TrimRight(page, "\n")
		if page == "" {
			result[i] = nil
		} else {
			result[i] = strings.Split(page, "\n")
		}
	}
	return result
}

// DiscoverTree walks a paper's directory to find all build inputs needed
// for an isolated build. It includes the main .tex, plus any .cls, .sty,
// .bib, .bbl, .bst, latexmkrc, figures, and \input children it can find.
func DiscoverTree(paperPath string) (*Tree, error) {
	absPath, err := filepath.Abs(paperPath)
	if err != nil {
		return nil, err
	}
	dir := filepath.Dir(absPath)
	paper := filepath.Base(absPath)

	var files []string
	// Walk the directory and collect relevant files.
	err = filepath.Walk(dir, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return nil // skip errors
		}
		if info.IsDir() {
			base := filepath.Base(path)
			// Skip hidden dirs and common build artifacts.
			if base != "." && strings.HasPrefix(base, ".") {
				return filepath.SkipDir
			}
			if base == "auto" || base == "_minted" || base == "__pycache__" {
				return filepath.SkipDir
			}
			return nil
		}
		rel, relErr := filepath.Rel(dir, path)
		if relErr != nil {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(rel))
		base := filepath.Base(rel)

		// Include: .tex, .cls, .sty, .bib, .bbl, .bst, latexmkrc,
		// common figure formats, .def, .cfg, .fd files.
		switch {
		case ext == ".tex":
			files = append(files, rel)
		case ext == ".cls" || ext == ".sty" || ext == ".bst":
			files = append(files, rel)
		case ext == ".bib" || ext == ".bbl":
			files = append(files, rel)
		case ext == ".def" || ext == ".cfg" || ext == ".fd":
			files = append(files, rel)
		case ext == ".pdf" || ext == ".png" || ext == ".jpg" ||
			ext == ".jpeg" || ext == ".eps" || ext == ".svg":
			files = append(files, rel)
		case base == "latexmkrc" || base == ".latexmkrc":
			files = append(files, rel)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("discover tree: %w", err)
	}

	return &Tree{
		Root:  dir,
		Paper: paper,
		Files: files,
	}, nil
}

// lastTmpDir holds the path of the last verifier tempdir, for inspection
// on failure. Set by Verify; guarded by lastTmpMu.
var (
	lastTmpDir string
	lastTmpMu  sync.Mutex
)

// LastTempDir returns the path of the most recent verification tempdir.
func LastTempDir() string {
	lastTmpMu.Lock()
	defer lastTmpMu.Unlock()
	return lastTmpDir
}

// setLastTmpDir atomically updates the last tempdir, cleaning the
// previous one if it exists. The new tempdir is preserved for
// inspection until the next Verify call replaces it.
func setLastTmpDir(dir string) {
	lastTmpMu.Lock()
	defer lastTmpMu.Unlock()
	if lastTmpDir != "" && lastTmpDir != dir {
		os.RemoveAll(lastTmpDir)
	}
	lastTmpDir = dir
}

// CleanTempDirs removes all mr-fmt-* tempdirs in os.TempDir.
func CleanTempDirs() error {
	pattern := filepath.Join(os.TempDir(), "mr-fmt-*")
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return err
	}
	for _, m := range matches {
		os.RemoveAll(m)
	}
	return nil
}

// FormatDiffs formats unexpected diffs for display on stderr.
func FormatDiffs(w io.Writer, diffs []Diff) {
	for _, d := range diffs {
		if d.Page == 0 && d.LineInPage == 0 {
			// Page count mismatch or other structural diff.
			fmt.Fprintf(w, "  %s → %s\n", d.Before, d.After)
			continue
		}
		fmt.Fprintf(w, "  page %d, line %d:\n", d.Page, d.LineInPage)
		fmt.Fprintf(w, "    before: %s\n", truncExcerpt(d.Before))
		fmt.Fprintf(w, "    after:  %s\n", truncExcerpt(d.After))
	}
}
