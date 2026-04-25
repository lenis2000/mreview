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
	Diff       bool     `long:"diff" description:"show unified diff to stdout, do not write"`
	Check      bool     `long:"check" description:"exit 1 if changes needed (CI / pre-commit)"`
	PDFFix     bool     `long:"pdf-fix" description:"enable Tier-2 PDF-fixing rules"`
	Rule       []string `long:"rule" description:"restrict to these rule IDs (repeatable)"`
	AllowDirty bool     `long:"allow-dirty" description:"skip dirty-tree check before writing"`
	NoVerify   bool     `long:"no-verify" description:"skip PDF verification"`
	VerifyPDF  string   `long:"verify-pdf" choice:"text" choice:"visual" description:"verifier mode"`
	Report     bool     `long:"report" description:"write paper.tex.fmt-report.md"`
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

	if _, statErr := os.Stat(paperPath); statErr != nil {
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
	}

	result := format.Apply(src, opts)

	// No changes?
	if string(result.Src) == string(src) {
		if o.Check {
			return 0
		}
		fmt.Fprintln(stderr, "mreview fmt: no changes")
		return 0
	}

	// --check: exit 1 if changes are needed.
	if o.Check {
		return 1
	}

	// --diff: print unified diff to stdout, no write.
	if o.Diff {
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

	// Verifier not built yet (Task 4). Warn that verification was skipped.
	if !o.NoVerify {
		fmt.Fprintln(stderr, "mreview fmt: warning: PDF verification not yet implemented; writing without verify")
	}

	// Write the rewritten source.
	if writeErr := os.WriteFile(paperPath, result.Src, 0o644); writeErr != nil {
		fmt.Fprintf(stderr, "mreview fmt: write %q: %v\n", paperPath, writeErr)
		return 1
	}

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
