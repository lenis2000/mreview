// Package main is the entry point for mreview, a LaTeX-aware math paper review TUI.
package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/jessevdk/go-flags"

	"mreview/pkg/build"
	"mreview/pkg/parser"
	"mreview/pkg/pdf"
	"mreview/pkg/persist"
	"mreview/pkg/synctex"
	"mreview/pkg/ui"
)

// populatePDFRegions fills Block.PDFRegion for every block whose SyncTeX entry
// can be located. Skips the synthetic root and blocks without line ranges.
func populatePDFRegions(doc *parser.Document, idx *synctex.Index) {
	if doc == nil || idx == nil {
		return
	}
	for _, b := range doc.Blocks {
		if b == doc.Root || b.StartLine == 0 {
			continue
		}
		file := b.File
		if file == "" {
			file = doc.File
		}
		r := idx.RegionForLines(file, b.StartLine, b.EndLine)
		if r == nil {
			continue
		}
		b.PDFRegion = &parser.Region{Page: r.Page, X: r.X, Y: r.Y, W: r.W, H: r.H}
	}
}

// runTUI is overridable by tests to bypass tea.NewProgram (which requires a
// real TTY). It returns the final model (so the caller can read the sidecar
// state back out) plus any runtime error.
//
// The TUI draws to /dev/tty rather than the inherited stdout so that
// `mreview paper.tex > review.md` works: TUI escape sequences land on
// the terminal while stdout stays a clean channel for the final
// markdown/JSON emit. stdout is used only when /dev/tty cannot be opened
// (e.g. a pipe without a controlling terminal), in which case the old
// mixed-output behaviour is at least no worse than before.
//
// On exit we emit a kitty-delete APC to the TTY (when we have one and the
// terminal supports kitty) so any lingering PDF image is retired before
// control returns to the user's shell. Without this, kitty keeps painting
// the last crop under the shell prompt until the next TIOCGWINSZ clear.
var runTUI = func(model tea.Model, stdout, stderr io.Writer) (tea.Model, error) {
	opts := []tea.ProgramOption{
		tea.WithAltScreen(),
		tea.WithMouseCellMotion(),
	}
	var ttyFile *os.File
	if tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0); err == nil {
		defer tty.Close()
		ttyFile = tty
		opts = append(opts, tea.WithInput(tty), tea.WithOutput(tty))
	} else {
		opts = append(opts, tea.WithOutput(stdout))
	}
	prog := tea.NewProgram(model, opts...)
	final, runErr := prog.Run()
	if ttyFile != nil && ui.KittyGraphicsAvailable() {
		fmt.Fprint(ttyFile, pdf.KittyDeleteAll)
	}
	return final, runErr
}

// version is the mreview release version. Overridable at build time via -ldflags.
var version = "0.1.0"

// opts holds all command-line options.
type opts struct {
	NoBuild  bool   `long:"no-build" description:"skip latexmk build, use existing outputs"`
	Draft    bool   `long:"draft" description:"open TUI even when the build fails (stale artefacts shown with a warning)"`
	BuildCmd string `long:"build-cmd" description:"override latexmk command"`
	Sidecar  string `long:"sidecar" description:"path to sidecar .mreview.md (default: <paper>.mreview.md)"`
	Stdout   string `long:"stdout" default:"md" choice:"md" choice:"json" choice:"none" description:"format for annotations emitted on quit"`
	Config   string `long:"config" description:"path to config file (default: ~/.config/mreview/config.toml)"`
	Version  bool   `short:"v" long:"version" description:"print version and exit"`

	File string `positional-arg-name:"paper.tex" description:"path to LaTeX paper source"`
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// run parses args, dispatches commands, and returns an exit code.
// 0 = success, 1 = error, 2 = usage error.
func run(args []string, stdout, stderr io.Writer) int {
	var o opts
	flagParser := flags.NewParser(&o, flags.HelpFlag|flags.PassDoubleDash)
	flagParser.Name = "mreview"
	flagParser.Usage = "[OPTIONS] paper.tex"

	rest, err := flagParser.ParseArgs(args)
	if err != nil {
		var flagsErr *flags.Error
		if errors.As(err, &flagsErr) && flagsErr.Type == flags.ErrHelp {
			fmt.Fprintln(stdout, err.Error())
			return 0
		}
		fmt.Fprintf(stderr, "mreview: %v\n", err)
		return 2
	}

	if o.Version {
		fmt.Fprintf(stdout, "mreview %s\n", version)
		return 0
	}

	if len(rest) > 1 {
		fmt.Fprintf(stderr, "mreview: unexpected extra argument %q\n", rest[1])
		fmt.Fprintln(stderr, "usage: mreview [OPTIONS] paper.tex")
		return 2
	}
	if len(rest) == 1 {
		o.File = rest[0]
	}

	if o.File == "" {
		fmt.Fprintln(stderr, "mreview: missing paper argument")
		fmt.Fprintln(stderr, "usage: mreview [OPTIONS] paper.tex")
		return 2
	}

	if _, statErr := os.Stat(o.File); statErr != nil {
		fmt.Fprintf(stderr, "mreview: cannot read %q: %v\n", o.File, statErr)
		return 1
	}

	src, readErr := os.ReadFile(o.File)
	if readErr != nil {
		fmt.Fprintf(stderr, "mreview: read %q: %v\n", o.File, readErr)
		return 1
	}
	doc, parseErr := parser.Parse(src)
	if parseErr != nil {
		fmt.Fprintf(stderr, "mreview: parse %q: %v\n", o.File, parseErr)
		return 1
	}
	doc.File = o.File

	// Load config early so the build step can use cfg.BuildCmd.
	cfg, cfgErr := ui.LoadConfig(o.Config)
	if cfgErr != nil {
		fmt.Fprintf(stderr, "mreview: %v\n", cfgErr)
		return 1
	}
	cfg = ui.ApplyThemeEnv(cfg)

	// Resolve build artefact paths and optionally run latexmk. --no-build
	// just resolves the conventional paths next to <paper>.tex.
	buildRes := build.ResolveBuildOutputs(o.File)
	var buildWarning string
	if !o.NoBuild {
		buildCmd := o.BuildCmd
		if buildCmd == "" {
			buildCmd = cfg.BuildCmd
		}
		res, berr := build.RunWith(build.Options{
			TexPath:  o.File,
			BuildCmd: buildCmd,
			Stderr:   stderr,
		})
		if berr != nil {
			if !o.Draft {
				fmt.Fprintf(stderr, "mreview: %v\n", berr)
				return 1
			}
			fmt.Fprintf(stderr, "mreview: --draft: %v\n", berr)
			buildWarning = shortBuildWarning(berr)
		}
		buildRes = res
	}

	// Enrich the document with .aux (block numbers) and .bbl (bib entries +
	// cite resolution). Missing files are non-fatal — LoadAux/LoadBBL return
	// empty maps/slices so fresh sources that have never been compiled still
	// open cleanly.
	if auxEntries, auxErr := parser.LoadAux(buildRes.AuxPath); auxErr == nil {
		parser.ApplyAux(doc, auxEntries)
	}
	if bibEntries, bibErr := parser.LoadBBL(buildRes.BBLPath); bibErr == nil {
		parser.ApplyBBL(doc, bibEntries)
	}

	sidecarPath := o.Sidecar
	if sidecarPath == "" {
		sidecarPath = o.File + ".mreview.md"
	}
	loaded, sideErr := persist.Load(sidecarPath)
	if sideErr != nil {
		fmt.Fprintf(stderr, "mreview: load sidecar %q: %v\n", sidecarPath, sideErr)
		return 1
	}
	// Remap against the freshly parsed document. Annotations that no longer
	// resolve to any block are preserved in side.Detached so they surface in
	// the outline status line and persist into the next sidecar save.
	// Remap also walks loaded.Detached internally, so blocks that have
	// returned since the last session reattach automatically — the caller
	// must *not* re-append loaded.Detached.
	side, detached := persist.Remap(loaded, doc)
	side.Detached = append(side.Detached, detached...)
	// Refresh UI-derived fields (breadcrumb, quote) against the current
	// document so renamed sections or edited blocks no longer carry stale
	// text through the next save.
	ui.RefreshRemappedAnnotations(doc, side)

	stdoutFmt, fmtErr := persist.ParseStdoutFormat(o.Stdout)
	if fmtErr != nil {
		fmt.Fprintf(stderr, "mreview: %v\n", fmtErr)
		return 2
	}

	model := ui.New(doc, side)
	model.SidecarPath = sidecarPath
	model.Config = cfg
	model.Styles = ui.StylesForTheme(cfg.Theme)
	model.KittyAvailable = ui.KittyGraphicsAvailable()
	if buildWarning != "" {
		model.BuildStale = true
		model.Status = "build: " + buildWarning
	}

	// Best-effort PDF+SyncTeX wire-up. Both are optional at this stage: if
	// either file is missing (e.g. --no-build on a never-built paper) the
	// pane falls back to a placeholder rather than aborting the session.
	if pdfDoc, pdfErr := pdf.Open(buildRes.PDFPath); pdfErr == nil {
		defer pdfDoc.Close()
		model.PDF = pdfDoc
	}
	if idx, idxErr := synctex.Open(buildRes.SyncTeXPath); idxErr == nil {
		model.Synctex = idx
		// Fill in Block.PDFRegion so the outline's ⊘ marker only shows up on
		// blocks that SyncTeX genuinely could not locate.
		populatePDFRegions(doc, idx)
	}

	final, err := runTUI(model, stdout, stderr)
	if err != nil {
		fmt.Fprintf(stderr, "mreview: tui: %v\n", err)
		return 1
	}
	// Prefer the final Model's sidecar — it is the same pointer in practice
	// but the contract matters for test doubles that swap the pointer.
	finalSide := side
	if fm, ok := final.(ui.Model); ok && fm.Sidecar != nil {
		finalSide = fm.Sidecar
		finalSide.Cursor = fm.CursorBlockID
	}
	finalSide.Paper = o.File
	finalSide.PDF = buildRes.PDFPath
	if saveErr := persist.Save(sidecarPath, finalSide); saveErr != nil {
		fmt.Fprintf(stderr, "mreview: save sidecar %q: %v\n", sidecarPath, saveErr)
		return 1
	}
	if emitErr := persist.Emit(stdout, finalSide, stdoutFmt); emitErr != nil {
		fmt.Fprintf(stderr, "mreview: emit: %v\n", emitErr)
		return 1
	}
	return 0
}

// shortBuildWarning extracts a one-line summary from a build error for
// the status bar. Falls back to the full error string when the type
// doesn't carry a structured first-line field.
func shortBuildWarning(err error) string {
	var be *build.BuildError
	if errors.As(err, &be) && be.FirstLine != "" {
		return be.FirstLine
	}
	s := err.Error()
	if i := strings.IndexByte(s, '\n'); i > 0 {
		return s[:i]
	}
	return s
}
