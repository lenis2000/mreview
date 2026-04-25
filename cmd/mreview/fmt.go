package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/jessevdk/go-flags"
	"github.com/pmezard/go-difflib/difflib"

	"mreview/pkg/format"
)

// fmtOpts holds flags for the "mreview fmt" subcommand.
type fmtOpts struct {
	Diff         bool     `long:"diff" description:"show unified diff to stdout, do not write"`
	Check        bool     `long:"check" description:"exit 1 if changes needed (CI / pre-commit)"`
	PDFFix       bool     `long:"pdf-fix" description:"enable Tier-2 PDF-fixing rules"`
	Rule         []string `long:"rule" description:"restrict to these rule IDs (repeatable)"`
	AllowDirty   bool     `long:"allow-dirty" description:"skip dirty-tree check before writing"`
	NoVerify     bool     `long:"no-verify" description:"skip PDF verification"`
	VerifyPDF    string   `long:"verify-pdf" choice:"text" choice:"visual" description:"verifier mode"`
	Report       bool     `long:"report" description:"write paper.tex.fmt-report.md"`
	CleanTempdir bool     `long:"clean-tempdir" description:"remove all mr-fmt-* verification tempdirs"`
}

// runFmt implements "mreview fmt [FLAGS] paper.tex".
func runFmt(args []string, stdout, stderr io.Writer) int {
	var o fmtOpts
	p := flags.NewParser(&o, flags.HelpFlag|flags.PassDoubleDash)
	p.Name = "mreview fmt"
	p.Usage = "[OPTIONS] paper.tex"

	rest, err := p.ParseArgs(args)
	if err != nil {
		var flagsErr *flags.Error
		if errors.As(err, &flagsErr) && flagsErr.Type == flags.ErrHelp {
			fmt.Fprintln(stdout, err.Error())
			return 0
		}
		fmt.Fprintf(stderr, "mreview fmt: %v\n", err)
		return 2
	}

	// --clean-tempdir: remove all verification tempdirs and exit.
	if o.CleanTempdir {
		if err := format.CleanTempDirs(); err != nil {
			fmt.Fprintf(stderr, "mreview fmt: clean tempdirs: %v\n", err)
			return 1
		}
		fmt.Fprintln(stderr, "mreview fmt: cleaned verification tempdirs")
		return 0
	}

	if len(rest) == 0 {
		fmt.Fprintln(stderr, "mreview fmt: missing paper argument")
		fmt.Fprintln(stderr, "usage: mreview fmt [OPTIONS] paper.tex")
		return 2
	}
	if len(rest) > 1 {
		fmt.Fprintf(stderr, "mreview fmt: unexpected extra argument %q\n", rest[1])
		fmt.Fprintln(stderr, "usage: mreview fmt [OPTIONS] paper.tex")
		return 2
	}

	paperPath := rest[0]

	fileInfo, statErr := os.Stat(paperPath)
	if statErr != nil {
		fmt.Fprintf(stderr, "mreview fmt: cannot read %q: %v\n", paperPath, statErr)
		return 1
	}

	src, readErr := os.ReadFile(paperPath)
	if readErr != nil {
		fmt.Fprintf(stderr, "mreview fmt: read %q: %v\n", paperPath, readErr)
		return 1
	}

	// Build pipeline options.
	opts := format.Options{
		PDFFix: o.PDFFix,
		Rules:  o.Rule,
		Diag:   o.Report, // enable diagnostics when --report is set
	}

	result := format.Apply(src, opts)

	// Write report early — both --check and no-changes paths benefit.
	writeReportIfNeeded := func(verifyResult *format.VerifyResult) {
		if !o.Report || len(result.Diags) == 0 && len(result.Hits) == 0 {
			return
		}
		rpt := format.BuildReport(filepath.Base(paperPath), opts, result, verifyResult)
		reportPath := format.ReportPath(paperPath)
		if rptErr := format.WriteReport(reportPath, rpt); rptErr != nil {
			fmt.Fprintf(stderr, "mreview fmt: write report: %v\n", rptErr)
			return
		}
		fmt.Fprintf(stderr, "mreview fmt: wrote %s\n", filepath.Base(reportPath))
	}

	// No changes?
	if string(result.Src) == string(src) {
		writeReportIfNeeded(nil)
		if o.Check {
			return 0
		}
		if !o.Report || len(result.Diags) == 0 {
			fmt.Fprintln(stderr, "mreview fmt: no changes")
		}
		return 0
	}

	// --check: exit 1 if changes are needed.
	if o.Check {
		writeReportIfNeeded(nil)
		return 1
	}

	// --diff: print unified diff to stdout, no write.
	if o.Diff {
		writeReportIfNeeded(nil)
		return printDiff(stdout, paperPath, src, result.Src)
	}

	// Write mode: check dirty tree unless --allow-dirty.
	if !o.AllowDirty {
		dirty, dirtyErr := isGitDirty(paperPath)
		if dirtyErr != nil {
			// Not a git repo or git not available — proceed with a warning.
			fmt.Fprintf(stderr, "mreview fmt: warning: cannot check git status: %v\n", dirtyErr)
		} else if dirty {
			fmt.Fprintf(stderr, "mreview fmt: %s has uncommitted changes; refusing to overwrite\n", filepath.Base(paperPath))
			fmt.Fprintln(stderr, "hint: commit or stash first, or pass --allow-dirty")
			return 1
		}
	}

	// Verify: build before/after PDFs and compare text layer.
	var verifyResult *format.VerifyResult
	if !o.NoVerify {
		tree, treeErr := format.DiscoverTree(paperPath)
		if treeErr != nil {
			fmt.Fprintf(stderr, "mreview fmt: discover build inputs: %v\n", treeErr)
			return 1
		}

		fmt.Fprintln(stderr, "mreview fmt: verifying PDF text layer...")
		vr, verifyErr := format.Verify(*tree, src, result.Src, result.Hits)
		if verifyErr != nil {
			fmt.Fprintf(stderr, "mreview fmt: verification error: %v\n", verifyErr)
			fmt.Fprintf(stderr, "hint: pass --no-verify to skip, or inspect %s\n", format.LastTempDir())
			return 1
		}
		if !vr.OK {
			fmt.Fprintln(stderr, "mreview fmt: verification FAILED — unexpected PDF text diffs:")
			format.FormatDiffs(stderr, vr.Unexpected)
			fmt.Fprintf(stderr, "tempdir preserved at %s for inspection\n", format.LastTempDir())
			fmt.Fprintln(stderr, "hint: pass --no-verify to skip verification")
			return 1
		}
		for _, w := range vr.Warnings {
			fmt.Fprintf(stderr, "mreview fmt: warning: %s\n", w)
		}
		fmt.Fprintln(stderr, "mreview fmt: verification ok (text layer)")
		verifyResult = vr

		// Paranoid mode: pixel-level diff-pdf comparison.
		if o.VerifyPDF == "visual" {
			if !format.ParanoidAvailable {
				fmt.Fprintln(stderr, "mreview fmt: paranoid verifier not available — rebuild with -tags=pdfverify")
				return 1
			}
			fmt.Fprintln(stderr, "mreview fmt: running paranoid pixel-level verification...")
			pr, prErr := format.VerifyParanoid(vr.BeforePDF, vr.AfterPDF)
			if prErr != nil {
				fmt.Fprintf(stderr, "mreview fmt: paranoid verification error: %v\n", prErr)
				return 1
			}
			if !pr.OK {
				fmt.Fprintf(stderr, "mreview fmt: paranoid verification FAILED — %s\n", pr.Message)
				if pr.DiffPDFPath != "" {
					fmt.Fprintf(stderr, "diff PDF saved to %s\n", pr.DiffPDFPath)
				}
				fmt.Fprintf(stderr, "tempdir preserved at %s for inspection\n", format.LastTempDir())
				return 1
			}
			fmt.Fprintln(stderr, "mreview fmt: paranoid verification ok (pixel-identical)")
		}
	}

	// Write the rewritten source, preserving original file permissions.
	if writeErr := os.WriteFile(paperPath, result.Src, fileInfo.Mode().Perm()); writeErr != nil {
		fmt.Fprintf(stderr, "mreview fmt: write %q: %v\n", paperPath, writeErr)
		return 1
	}

	// Write report if --report is set (with verifier result).
	writeReportIfNeeded(verifyResult)

	// Summary.
	nHits := len(result.Hits)
	if nHits == 1 {
		fmt.Fprintf(stderr, "mreview fmt: wrote %s (1 rewrite)\n", filepath.Base(paperPath))
	} else {
		fmt.Fprintf(stderr, "mreview fmt: wrote %s (%d rewrites)\n", filepath.Base(paperPath), nHits)
	}

	return 0
}

// printDiff writes a unified diff of before/after to w, returning exit code.
func printDiff(w io.Writer, path string, before, after []byte) int {
	diff := difflib.UnifiedDiff{
		A:        difflib.SplitLines(string(before)),
		B:        difflib.SplitLines(string(after)),
		FromFile: "a/" + filepath.Base(path),
		ToFile:   "b/" + filepath.Base(path),
		Context:  3,
	}
	text, err := difflib.GetUnifiedDiffString(diff)
	if err != nil {
		return 1
	}
	fmt.Fprint(w, text)
	return 0
}

// isGitDirty reports whether path has uncommitted changes in git.
// Returns an error if git is not available or path is not in a git repo.
func isGitDirty(path string) (bool, error) {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return false, err
	}
	dir := filepath.Dir(absPath)
	base := filepath.Base(absPath)

	cmd := exec.Command("git", "status", "--porcelain", "--", base)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return false, fmt.Errorf("git status: %w", err)
	}
	return len(strings.TrimSpace(string(out))) > 0, nil
}
