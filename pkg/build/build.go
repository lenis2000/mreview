// Package build wraps the LaTeX build pipeline (latexmk by default) and
// surfaces a strict pass/fail signal: a build is considered failed not only
// when the tool exits non-zero, but also when the .log contains a TeX-style
// "!" error line or an undefined-reference / undefined-citation warning that
// has survived to the final pass. The latter conditions are what callers care
// about because mreview's review workflow assumes refs and labels resolve.
package build

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Result holds the file paths produced (or expected) by a build.
type Result struct {
	PDFPath     string
	SyncTeXPath string
	AuxPath     string
	BBLPath     string
	LogPath     string
}

// Options controls a single Run invocation. The zero value is valid; Dir
// defaults to the directory of TexPath, BuildCmd defaults to the standard
// latexmk invocation, and Stdout/Stderr default to io.Discard.
type Options struct {
	// TexPath is the path to the .tex file to compile. Required.
	TexPath string
	// BuildCmd, if non-empty, is run via "sh -c" inside Dir. The tex
	// basename (no extension) is exposed as the env var $MREVIEW_BASENAME
	// so custom commands can interpolate it.
	BuildCmd string
	// Dir is the working directory for the command. Defaults to
	// filepath.Dir(TexPath).
	Dir string
	// Stdout and Stderr receive the raw command output. They may be nil.
	Stdout io.Writer
	Stderr io.Writer
	// Ctx is the optional context used for the command.
	Ctx context.Context
}

// Run compiles texPath using buildCmd (or the default latexmk invocation if
// buildCmd is empty) and returns the expected output paths. A non-nil error
// indicates a build failure; the returned *Result is still populated so
// callers can inspect log/aux paths for diagnostics.
func Run(texPath, buildCmd string) (*Result, error) {
	return RunWith(Options{TexPath: texPath, BuildCmd: buildCmd})
}

// RunWith is the explicit-options form of Run.
func RunWith(opts Options) (*Result, error) {
	if opts.TexPath == "" {
		return nil, errors.New("build: empty tex path")
	}
	res := ResolveBuildOutputs(opts.TexPath)
	dir := opts.Dir
	if dir == "" {
		dir = filepath.Dir(opts.TexPath)
	}
	base := strings.TrimSuffix(filepath.Base(opts.TexPath), filepath.Ext(opts.TexPath))

	cmdline := opts.BuildCmd
	if cmdline == "" {
		cmdline = fmt.Sprintf(
			"latexmk -pdf -synctex=1 -interaction=nonstopmode -halt-on-error -file-line-error %s",
			shellQuote(base),
		)
	}

	ctx := opts.Ctx
	if ctx == nil {
		ctx = context.Background()
	}
	cmd := exec.CommandContext(ctx, "sh", "-c", cmdline)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "MREVIEW_BASENAME="+base)

	stdout := opts.Stdout
	if stdout == nil {
		stdout = io.Discard
	}
	stderr := opts.Stderr
	if stderr == nil {
		stderr = io.Discard
	}
	cmd.Stdout = stdout
	cmd.Stderr = stderr

	runErr := cmd.Run()

	logTail, _ := tailLines(res.LogPath, 40)
	logIssue := scanLogForErrors(res.LogPath)

	if runErr != nil {
		return res, wrapBuildErr(opts.TexPath, fmt.Sprintf("command failed: %v", runErr), logIssue, logTail)
	}
	if logIssue != "" {
		return res, wrapBuildErr(opts.TexPath, "log reported issues", logIssue, logTail)
	}
	return res, nil
}

// ResolveBuildOutputs returns the conventional output paths next to texPath,
// without invoking the build. Used by --no-build mode and as a base for Run.
func ResolveBuildOutputs(texPath string) *Result {
	dir := filepath.Dir(texPath)
	base := strings.TrimSuffix(filepath.Base(texPath), filepath.Ext(texPath))
	return &Result{
		PDFPath:     filepath.Join(dir, base+".pdf"),
		SyncTeXPath: filepath.Join(dir, base+".synctex.gz"),
		AuxPath:     filepath.Join(dir, base+".aux"),
		BBLPath:     filepath.Join(dir, base+".bbl"),
		LogPath:     filepath.Join(dir, base+".log"),
	}
}

// scanLogForErrors returns the first offending line in the .log file, or "" if
// the log appears clean. It looks for TeX `!` error lines and undefined-ref /
// undefined-citation warnings (the latter only matter once they have survived
// the final compilation pass — latexmk reruns until they're stable, so any
// surviving warning is final).
func scanLogForErrors(logPath string) string {
	f, err := os.Open(logPath)
	if err != nil {
		return ""
	}
	defer f.Close()
	return scanLogReader(f)
}

// ScanLogBytes runs the same error/warning heuristics as the per-path
// scanner against an in-memory log buffer. Exported so the lmkf reload
// path (which has already read the log to check for the completion
// marker) can reuse the same policy without re-reading the file or
// duplicating the rule set.
func ScanLogBytes(data []byte) string {
	return scanLogReader(bytes.NewReader(data))
}

// scanLogReader is the shared scan loop for both the path-based and
// byte-based entry points. Its rules are the contract: any change to
// what counts as a build failure happens here.
func scanLogReader(r io.Reader) string {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	for sc.Scan() {
		line := sc.Text()
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "!") {
			return trimmed
		}
		if isUndefinedRefWarning(line) {
			return strings.TrimSpace(line)
		}
	}
	return ""
}

// isUndefinedRefWarning matches LaTeX's standard warnings for unresolved
// labels and citations. Examples:
//
//	LaTeX Warning: Reference `foo' on page 1 undefined on input line 12.
//	LaTeX Warning: Citation `bar' on page 1 undefined on input line 13.
//	Package natbib Warning: Citation `baz' on page 1 undefined on input line 14.
func isUndefinedRefWarning(line string) bool {
	if !strings.Contains(line, "undefined") {
		return false
	}
	low := strings.ToLower(line)
	if !strings.Contains(low, "warning") {
		return false
	}
	return strings.Contains(line, "Reference `") ||
		strings.Contains(line, "Citation `") ||
		strings.Contains(low, "reference '") ||
		strings.Contains(low, "citation '")
}

// tailLines reads up to n trailing lines from path. Missing files yield an
// empty slice and a non-nil error which the caller may safely ignore.
func tailLines(path string, n int) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	ring := make([]string, 0, n)
	for sc.Scan() {
		if len(ring) == n {
			ring = ring[1:]
		}
		ring = append(ring, sc.Text())
	}
	return ring, sc.Err()
}

// BuildError is returned when latexmk fails or the .log contains errors. It
// carries the captured tail and the first detected error line so callers can
// surface them directly to the user.
type BuildError struct {
	TexPath   string
	Reason    string
	FirstLine string
	LogTail   []string
}

func (e *BuildError) Error() string {
	var b strings.Builder
	fmt.Fprintf(&b, "build failed for %s: %s", e.TexPath, e.Reason)
	if e.FirstLine != "" {
		fmt.Fprintf(&b, "\n  first error: %s", e.FirstLine)
	}
	if len(e.LogTail) > 0 {
		b.WriteString("\n  log tail:\n")
		for _, line := range e.LogTail {
			b.WriteString("    ")
			b.WriteString(line)
			b.WriteByte('\n')
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

func wrapBuildErr(texPath, reason, first string, tail []string) error {
	return &BuildError{
		TexPath:   texPath,
		Reason:    reason,
		FirstLine: first,
		LogTail:   tail,
	}
}

// shellQuote wraps s in single quotes for safe inclusion in a sh -c command.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
