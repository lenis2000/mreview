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

	"mreview/pkg/parser"
	"mreview/pkg/pdf"
	"mreview/pkg/persist"
	"mreview/pkg/synctex"
	"mreview/pkg/ui"
)

// runTUI is overridable by tests to bypass tea.NewProgram (which requires a
// real TTY). It returns the final model (so the caller can read the sidecar
// state back out) plus any runtime error.
var runTUI = func(model tea.Model, stdout, stderr io.Writer) (tea.Model, error) {
	prog := tea.NewProgram(model, tea.WithAltScreen(), tea.WithOutput(stdout))
	return prog.Run()
}

// version is the mreview release version. Overridable at build time via -ldflags.
var version = "0.1.0"

// opts holds all command-line options.
type opts struct {
	NoBuild  bool   `long:"no-build" description:"skip latexmk build, use existing outputs"`
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

	if len(rest) > 0 {
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
	side, detached := persist.Remap(loaded, doc)
	side.Detached = append(side.Detached, loaded.Detached...)
	side.Detached = append(side.Detached, detached...)

	stdoutFmt, fmtErr := persist.ParseStdoutFormat(o.Stdout)
	if fmtErr != nil {
		fmt.Fprintf(stderr, "mreview: %v\n", fmtErr)
		return 2
	}

	cfg, cfgErr := ui.LoadConfig(o.Config)
	if cfgErr != nil {
		fmt.Fprintf(stderr, "mreview: %v\n", cfgErr)
		return 1
	}
	cfg = ui.ApplyThemeEnv(cfg)

	model := ui.New(doc, side)
	model.SidecarPath = sidecarPath
	model.Config = cfg
	model.Styles = ui.StylesForTheme(cfg.Theme)

	// Best-effort PDF+SyncTeX wire-up. Both are optional at this stage: if
	// either file is missing (e.g. latexmk hasn't run yet) the pane falls
	// back to a placeholder rather than aborting the session.
	stem := strings.TrimSuffix(o.File, ".tex")
	pdfPath := stem + ".pdf"
	synctexPath := stem + ".synctex.gz"
	if pdfDoc, pdfErr := pdf.Open(pdfPath); pdfErr == nil {
		defer pdfDoc.Close()
		model.PDF = pdfDoc
	}
	if idx, idxErr := synctex.Open(synctexPath); idxErr == nil {
		model.Synctex = idx
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
	}
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
