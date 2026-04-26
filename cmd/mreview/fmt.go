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
	"mreview/pkg/ui"
)

// fmtOpts holds flags for the "mreview fmt" subcommand.
//
// Defaults are aggressive: Tier-2 PDF-fix rules on, paranoid pixel-level
// verification on, and a fmt-report.md emitted next to the paper. Use the
// --no-* flags or --verify-pdf=text to opt out.
type fmtOpts struct {
	Diff         bool     `long:"diff" description:"show unified diff to stdout, do not write"`
	Print        bool     `long:"print" short:"p" description:"print formatted source to stdout, do not write"`
	Check        bool     `long:"check" description:"exit 1 if changes needed (CI / pre-commit)"`
	NoPDFFix     bool     `long:"no-pdf-fix" description:"disable Tier-2 PDF-fixing rules (Tier-1 only)"`
	Rule         []string `long:"rule" description:"restrict to these rule IDs (repeatable)"`
	AllowDirty   bool     `long:"allow-dirty" description:"skip dirty-tree check before writing"`
	NoVerify     bool     `long:"no-verify" description:"skip PDF verification entirely"`
	VerifyPDF    string   `long:"verify-pdf" choice:"text" choice:"visual" description:"verifier mode (default: visual)"`
	NoReport     bool     `long:"no-report" description:"do not write paper.tex.fmt-report.md"`
	CleanTempdir bool     `long:"clean-tempdir" description:"remove all mr-fmt-* verification tempdirs"`
	Config       string   `long:"config" description:"path to config file"`
	NoConfig     bool     `long:"noconfig" description:"ignore config files; use built-in defaults"`
	NoIndent     bool     `long:"no-indent" description:"disable env-aware reindentation"`
	Wrap         string   `long:"wrap" description:"wrap mode: off | column | sentence | sentence+column"`
	WrapCol      int      `long:"wrap-col" description:"target column for wrapping (default: 80)"`
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

	// --print is mutually exclusive with --diff and --check.
	if (o.Print && o.Diff) || (o.Print && o.Check) || (o.Diff && o.Check) {
		fmt.Fprintln(stderr, "mreview fmt: --diff, --print, and --check are mutually exclusive")
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
		fmt.Fprintln(stderr, "usage: mreview fmt [OPTIONS] paper.tex [paper.tex...]")
		return 2
	}

	// --print / --diff / --check have no sensible interpretation across many
	// files: refuse so output isn't accidentally interleaved.
	if (o.Print || o.Diff || o.Check) && len(rest) > 1 {
		fmt.Fprintln(stderr, "mreview fmt: --print, --diff, and --check accept only one file")
		return 2
	}

	// Validate --rule IDs once before opening any file.
	if err := format.ValidateRuleIDs(o.Rule); err != nil {
		fmt.Fprintf(stderr, "mreview fmt: %v\n", err)
		return 2
	}

	// Load config once; defaults are shared across all files.
	cfg, cfgErr := ui.LoadConfig(o.Config, o.NoConfig)
	if cfgErr != nil {
		fmt.Fprintf(stderr, "mreview fmt: %v\n", cfgErr)
		return 1
	}

	// Resolve aggressive defaults. Flag (when set) overrides config; config
	// (when set) overrides built-in default. Built-in default is "aggressive":
	// pdf-fix on, verify=visual, report on.
	pdfFix := !resolveBool(o.NoPDFFix, cfg.Fmt.NoPDFFix, false)
	noVerify := resolveBool(o.NoVerify, cfg.Fmt.NoVerify, false)
	wantReport := !resolveBool(o.NoReport, cfg.Fmt.NoReport, false)
	verifyMode := o.VerifyPDF
	if verifyMode == "" {
		verifyMode = cfg.Fmt.VerifyPDF
	}
	if verifyMode == "" {
		verifyMode = "visual"
	}

	// Resolve indent options. Default ON; flag (--no-indent) wins, then
	// config (`[fmt] indent`), else the built-in default (true).
	indentEnabled := !o.NoIndent
	if !o.NoIndent && cfg.Fmt.Indent != nil {
		indentEnabled = *cfg.Fmt.Indent
	}
	indentChar := cfg.Fmt.IndentChar
	if indentChar == "" {
		indentChar = "tab"
	}
	indentSize := cfg.Fmt.IndentSize
	if indentSize <= 0 {
		if indentChar == "tab" {
			indentSize = 1
		} else {
			indentSize = 2
		}
	}
	indentOpts := format.IndentOptions{
		Enabled: indentEnabled,
		UseTab:  indentChar == "tab",
		Size:    indentSize,
	}

	// Resolve wrap options. Default mode is "sentence+column"; flag wins,
	// then config, else built-in default.
	wrapMode := o.Wrap
	if wrapMode == "" {
		wrapMode = cfg.Fmt.Wrap
	}
	if wrapMode == "" {
		wrapMode = "sentence+column"
	}
	wrapCol := o.WrapCol
	if wrapCol <= 0 {
		wrapCol = cfg.Fmt.WrapCol
	}
	if wrapCol <= 0 {
		wrapCol = 80
	}
	wrapOpts := format.WrapOptions{
		Mode: wrapMode,
		Col:  wrapCol,
	}

	// Loop the per-file work; aggregate exit codes.
	worst := 0
	for i, paperPath := range rest {
		if len(rest) > 1 {
			fmt.Fprintf(stderr, "mreview fmt: [%d/%d] %s\n", i+1, len(rest), filepath.Base(paperPath))
		}
		code := runFmtOne(paperPath, &o, cfg, pdfFix, noVerify, wantReport, verifyMode, indentOpts, wrapOpts, stdout, stderr)
		if code > worst {
			worst = code
		}
	}
	return worst
}

// runFmtOne runs the format pipeline for a single .tex file. Returns 0 on
// success, 1 on per-file errors, 2 on usage errors. Caller pre-validates
// shared inputs (--rule, config) and resolves the aggressive defaults.
func runFmtOne(
	paperPath string,
	o *fmtOpts,
	cfg *ui.Config,
	pdfFix, noVerify, wantReport bool,
	verifyMode string,
	indentOpts format.IndentOptions,
	wrapOpts format.WrapOptions,
	stdout, stderr io.Writer,
) int {
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
		PDFFix:       pdfFix,
		Rules:        o.Rule,
		Diag:         wantReport, // enable diagnostics when a report will be written
		VerbatimEnvs: cfg.Fmt.VerbatimEnvs,
		Indent:       indentOpts,
		Wrap:         wrapOpts,
	}

	result := format.Apply(src, opts)

	// Write report early — both --check and no-changes paths benefit.
	writeReportIfNeeded := func(verifyResult *format.VerifyResult) {
		if !wantReport {
			return
		}
		reportPath := format.ReportPath(paperPath)
		if len(result.Diags) == 0 && len(result.Hits) == 0 {
			// Clean up stale report so the UI doesn't show outdated diagnostics.
			if rmErr := os.Remove(reportPath); rmErr != nil && !os.IsNotExist(rmErr) {
				fmt.Fprintf(stderr, "mreview fmt: remove stale report: %v\n", rmErr)
			}
			return
		}
		rpt := format.BuildReport(filepath.Base(paperPath), opts, result, verifyResult)
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
		if o.Print {
			if _, werr := stdout.Write(result.Src); werr != nil {
				fmt.Fprintf(stderr, "mreview fmt: write stdout: %v\n", werr)
				return 1
			}
			return 0
		}
		if !wantReport || len(result.Diags) == 0 {
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

	// --print: write formatted source to stdout, no file write, no verify, no report.
	if o.Print {
		if _, werr := stdout.Write(result.Src); werr != nil {
			fmt.Fprintf(stderr, "mreview fmt: write stdout: %v\n", werr)
			return 1
		}
		return 0
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
	if !noVerify {
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

		// Paranoid mode: pixel-level diff-pdf comparison. Default; opt out
		// with --verify-pdf=text.
		if verifyMode == "visual" {
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

// resolveBool returns the effective bool from (flag, config, default).
//
// Flag wins when true (go-flags can't distinguish "passed false" from "not
// passed"). Otherwise config wins when explicitly set. Otherwise the built-in
// default is used.
func resolveBool(flag bool, cfg *bool, def bool) bool {
	if flag {
		return true
	}
	if cfg != nil {
		return *cfg
	}
	return def
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
