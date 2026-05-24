package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/jessevdk/go-flags"

	"mreview/pkg/diffreview"
	"mreview/pkg/ui"
)

// diffOpts holds flags for the "mreview diff" subcommand.
type diffOpts struct {
	Base    string `long:"base" description:"compare REV:<path> against the working-tree path"`
	NoBuild bool   `long:"no-build" description:"skip latexmk build for the new endpoint"`
	Draft   bool   `long:"draft" description:"open TUI even when the new build fails"`

	BuildCmd string `long:"build-cmd" description:"override latexmk command for the new endpoint"`
	Sidecar  string `long:"sidecar" description:"path to diff sidecar file"`
	Stdout   string `long:"stdout" default:"md" choice:"md" choice:"json" choice:"none" description:"format for diff annotations emitted on quit"`
	Config   string `long:"config" description:"path to config file"`
	NoConfig bool   `long:"noconfig" description:"ignore config files; use built-in defaults"`

	OpenZed            bool `long:"open-zed" description:"open old and new sources in Zed after startup"`
	AllowModifications bool `long:"allow-modifications" description:"allow e/E edits to the new endpoint when it is a real file"`
}

// diffModel is the temporary diff-mode Bubble Tea model used until pkg/diffui
// lands. Keep the CLI-owned state explicit so tests and the future UI package
// can agree on the contract.
type diffModel struct {
	Review *diffreview.Review
	Config *ui.Config
	Styles ui.Styles

	AllowModifications          bool
	RequestedAllowModifications bool
	NoBuild                     bool
	Draft                       bool
	BuildCmd                    string
	SidecarPath                 string
	StdoutFormat                string
	OpenZed                     bool
	Status                      string
}

func (m diffModel) Init() tea.Cmd { return nil }

func (m diffModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if key, ok := msg.(tea.KeyMsg); ok {
		switch key.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m diffModel) View() string {
	if m.Review == nil {
		return "mreview diff\n"
	}
	return fmt.Sprintf("mreview diff: %d semantic pairs\n", m.Review.Stats.Total)
}

var runDiffTUI = func(model tea.Model, stdout, stderr io.Writer) (tea.Model, error) {
	return runTUI(model, stdout, stderr)
}

// runDiff implements "mreview diff [FLAGS] --base REV paper.tex" and
// "mreview diff [FLAGS] OLD NEW".
func runDiff(args []string, stdout, stderr io.Writer) int {
	var o diffOpts
	p := flags.NewParser(&o, flags.HelpFlag|flags.PassDoubleDash)
	p.Name = "mreview diff"
	p.Usage = "[OPTIONS] --base REV paper.tex | OLD NEW"

	rest, err := p.ParseArgs(args)
	if err != nil {
		var flagsErr *flags.Error
		if errors.As(err, &flagsErr) && flagsErr.Type == flags.ErrHelp {
			_, _ = fmt.Fprintln(stdout, err.Error())
			return 0
		}
		_, _ = fmt.Fprintf(stderr, "mreview diff: %v\n", err)
		return 2
	}

	if usageErr := validateDiffArgs(o, rest); usageErr != "" {
		_, _ = fmt.Fprintf(stderr, "mreview diff: %s\n", usageErr)
		_, _ = fmt.Fprintln(stderr, "usage: mreview diff [OPTIONS] --base REV paper.tex | OLD NEW")
		return 2
	}

	cfg, cfgErr := ui.LoadConfig(o.Config, o.NoConfig)
	if cfgErr != nil {
		_, _ = fmt.Fprintf(stderr, "mreview diff: %v\n", cfgErr)
		return 1
	}
	cfg = ui.ApplyThemeEnv(cfg)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	oldEndpoint, newEndpoint, resolveErr := resolveDiffEndpoints(ctx, o, rest)
	if resolveErr != nil {
		_, _ = fmt.Fprintf(stderr, "mreview diff: %v\n", resolveErr)
		return 1
	}

	review, reviewErr := diffreview.BuildReview(oldEndpoint, newEndpoint)
	if reviewErr != nil {
		_, _ = fmt.Fprintf(stderr, "mreview diff: %v\n", reviewErr)
		return 1
	}

	allowEdits := o.AllowModifications && review.New.Editable
	model := diffModel{
		Review:                      review,
		Config:                      cfg,
		Styles:                      ui.StylesForTheme(cfg.Theme),
		AllowModifications:          allowEdits,
		RequestedAllowModifications: o.AllowModifications,
		NoBuild:                     o.NoBuild,
		Draft:                       o.Draft,
		BuildCmd:                    o.BuildCmd,
		SidecarPath:                 o.Sidecar,
		StdoutFormat:                o.Stdout,
		OpenZed:                     o.OpenZed,
		Status:                      initialDiffStatus(o, review),
	}

	if _, err := runDiffTUI(model, stdout, stderr); err != nil {
		_, _ = fmt.Fprintf(stderr, "mreview diff: tui: %v\n", err)
		return 1
	}
	return 0
}

func validateDiffArgs(o diffOpts, rest []string) string {
	if o.Base != "" {
		switch len(rest) {
		case 0:
			return "--base requires one filesystem path"
		case 1:
			return ""
		default:
			return "--base cannot be combined with explicit OLD NEW endpoints"
		}
	}
	switch len(rest) {
	case 0:
		return "missing endpoints"
	case 1:
		return "missing NEW endpoint"
	case 2:
		return ""
	default:
		return fmt.Sprintf("unexpected extra endpoint %q", rest[2])
	}
}

func resolveDiffEndpoints(ctx context.Context, o diffOpts, rest []string) (diffreview.Endpoint, diffreview.Endpoint, error) {
	resolver := diffreview.Resolver{}
	if o.Base != "" {
		oldEndpoint, newEndpoint, err := resolver.ResolveBase(ctx, o.Base, rest[0])
		if err != nil {
			return diffreview.Endpoint{}, diffreview.Endpoint{}, fmt.Errorf("resolve --base endpoints: %w", err)
		}
		return oldEndpoint, newEndpoint, nil
	}
	oldEndpoint, newEndpoint, err := resolver.ResolveEndpoints(ctx, rest[0], rest[1])
	if err != nil {
		return diffreview.Endpoint{}, diffreview.Endpoint{}, fmt.Errorf("resolve endpoints: %w", err)
	}
	return oldEndpoint, newEndpoint, nil
}

func initialDiffStatus(o diffOpts, review *diffreview.Review) string {
	if review == nil {
		return ""
	}
	if o.AllowModifications && !review.New.Editable {
		return "new endpoint is read-only; run from the branch and use --base REV path.tex"
	}
	return ""
}
