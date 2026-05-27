package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/jessevdk/go-flags"

	"mreview/pkg/pdf"
	"mreview/pkg/pdfreview"
	"mreview/pkg/ui"
)

type pdfReviewOpts struct {
	Out string `long:"out" description:"output letter path (default: <PDF>.review.md, .pdf suffix stripped)"`

	Args struct {
		PDF string `positional-arg-name:"PAPER.pdf" description:"the preprint PDF; the matching .pdf-comments.json must already exist"`
	} `positional-args:"yes" required:"yes"`
}

// runPdfReview opens PAPER.pdf and PAPER.pdf.pdf-comments.json, runs the
// review TUI, and writes PAPER.review.md on a clean exit.
func runPdfReview(args []string, stdout, stderr io.Writer) int {
	var o pdfReviewOpts
	parser := flags.NewParser(&o, flags.HelpFlag|flags.PassDoubleDash)
	parser.Name = "mreview pdf-review"
	parser.Usage = "[OPTIONS]"
	if _, err := parser.ParseArgs(args); err != nil {
		var fe *flags.Error
		if errors.As(err, &fe) && fe.Type == flags.ErrHelp {
			_, _ = fmt.Fprintln(stdout, err.Error())
			return 0
		}
		_, _ = fmt.Fprintf(stderr, "mreview pdf-review: %v\n", err)
		return 2
	}

	pdfPath := o.Args.PDF
	if !strings.EqualFold(filepath.Ext(pdfPath), ".pdf") {
		_, _ = fmt.Fprintf(stderr, "mreview pdf-review: argument must be a .pdf file (got %q)\n", pdfPath)
		return 2
	}
	if _, err := os.Stat(pdfPath); err != nil {
		_, _ = fmt.Fprintf(stderr, "mreview pdf-review: %v\n", err)
		return 1
	}

	jsonPath := pdfreview.ReportPath(pdfPath)
	if _, err := os.Stat(jsonPath); err != nil {
		_, _ = fmt.Fprintf(stderr, "mreview pdf-review: missing %s\n", jsonPath)
		_, _ = fmt.Fprintf(stderr, "  run `mreview pdf-comments REVIEW.md %s` first to anchor your comments.\n", pdfPath)
		return 1
	}
	report, err := pdfreview.LoadReport(jsonPath)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "mreview pdf-review: load %s: %v\n", jsonPath, err)
		return 1
	}

	doc, err := pdf.Open(pdfPath)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "mreview pdf-review: %v\n", err)
		return 1
	}
	defer func() { _ = doc.Close() }()

	letterPath := o.Out
	if letterPath == "" {
		letterPath = pdfreview.LetterPath(pdfPath)
	}

	model := pdfreview.New(pdfPath, jsonPath, letterPath, doc, report)
	model.KittyAvailable = ui.KittyGraphicsAvailable()

	final, err := runReviewTUI(model, stdout, stderr)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "mreview pdf-review: tui: %v\n", err)
		return 1
	}
	if fm, ok := final.(pdfreview.Model); ok {
		// Defensive: if tea exited without explicit q/Q (e.g. ctrl+c), still
		// save the JSON so user edits aren't lost.
		if !fm.Quitting() && fm.Dirty {
			if err := pdfreview.SaveReport(jsonPath, &pdfreview.Report{
				SourceMD:  report.SourceMD,
				SourcePDF: pdfPath,
				Generated: report.Generated,
				Model:     report.Model,
				Comments:  fm.Comments,
			}); err != nil {
				_, _ = fmt.Fprintf(stderr, "mreview pdf-review: save: %v\n", err)
				return 1
			}
		}
	}
	return 0
}

// runReviewTUI is the /dev/tty + kitty cleanup runner, mirroring
// cmd/mreview/main.go's runTUI closure but for the review viewer.
func runReviewTUI(model tea.Model, stdout, stderr io.Writer) (tea.Model, error) {
	opts := []tea.ProgramOption{
		tea.WithAltScreen(),
		tea.WithMouseCellMotion(),
	}
	var ttyFile *os.File
	if tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0); err == nil {
		defer func() { _ = tty.Close() }()
		ttyFile = tty
		opts = append(opts, tea.WithInput(tty), tea.WithOutput(tty))
	} else {
		opts = append(opts, tea.WithOutput(stdout))
	}
	prog := tea.NewProgram(model, opts...)
	final, runErr := prog.Run()
	if ttyFile != nil && ui.KittyGraphicsAvailable() {
		_, _ = fmt.Fprint(ttyFile, pdf.KittyDeleteAll)
	}
	return final, runErr
}
