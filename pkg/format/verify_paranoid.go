//go:build pdfverify

package format

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// ParanoidAvailable reports whether the paranoid verifier (diff-pdf pixel
// comparison) was compiled in. This build has the pdfverify tag.
const ParanoidAvailable = true

// VerifyParanoid runs pixel-level PDF verification via diff-pdf on top of the
// text-layer check. It compares beforePDF and afterPDF using diff-pdf and
// produces a visual diff PDF at diffOutPath.
//
// Prerequisites: diff-pdf and pdfinfo must be on $PATH.
//
// Returns a ParanoidResult describing whether the comparison passed.
func VerifyParanoid(beforePDF, afterPDF string) (*ParanoidResult, error) {
	// Verify diff-pdf is available.
	if _, err := exec.LookPath("diff-pdf"); err != nil {
		return nil, fmt.Errorf("paranoid verify: diff-pdf not found on $PATH: %w", err)
	}

	// Page-count precondition via pdfinfo.
	beforePages, err := pdfPageCount(beforePDF)
	if err != nil {
		return nil, fmt.Errorf("paranoid verify: pdfinfo before: %w", err)
	}
	afterPages, err := pdfPageCount(afterPDF)
	if err != nil {
		return nil, fmt.Errorf("paranoid verify: pdfinfo after: %w", err)
	}
	if beforePages != afterPages {
		return &ParanoidResult{
			OK:      false,
			Message: fmt.Sprintf("page count mismatch: before=%d, after=%d", beforePages, afterPages),
		}, nil
	}

	// Determine diff output path: alongside the after PDF.
	diffDir := filepath.Dir(afterPDF)
	diffOutPath := filepath.Join(diffDir, "diff.pdf")

	// Run diff-pdf with --output-diff to produce a visual diff PDF.
	// diff-pdf returns exit code 0 if PDFs are identical, 1 if they differ.
	cmd := exec.Command("diff-pdf", "--output-diff="+diffOutPath, beforePDF, afterPDF)
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	runErr := cmd.Run()

	if runErr == nil {
		// Exit code 0: PDFs are pixel-identical.
		// Clean up the diff PDF if it was created (it shouldn't be meaningful).
		os.Remove(diffOutPath)
		return &ParanoidResult{
			OK:      true,
			Message: "pixel-identical",
		}, nil
	}

	// Check if it was a real error vs. exit code 1 (differs).
	if exitErr, ok := runErr.(*exec.ExitError); ok {
		if exitErr.ExitCode() == 1 {
			return &ParanoidResult{
				OK:          false,
				Message:     "pixel diff detected",
				DiffPDFPath: diffOutPath,
			}, nil
		}
	}

	return nil, fmt.Errorf("paranoid verify: diff-pdf failed: %w", runErr)
}
