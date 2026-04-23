// Package main is the entry point for mreview, a LaTeX-aware math paper review TUI.
package main

import (
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/jessevdk/go-flags"
)

// version is the mreview release version. Overridable at build time via -ldflags.
var version = "dev"

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
	parser := flags.NewParser(&o, flags.HelpFlag|flags.PassDoubleDash)
	parser.Name = "mreview"
	parser.Usage = "[OPTIONS] paper.tex"

	rest, err := parser.ParseArgs(args)
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

	fmt.Fprintln(stderr, "mreview: not implemented yet")
	return 1
}
