package ui

import (
	"errors"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

// LayoutFracs is the on-disk shape of the persisted pane fractions. Stored
// at ~/.config/mreview/layout.toml so resize tweaks persist across runs and
// across projects (a global preference, not per-paper).
type LayoutFracs struct {
	Outline    float64 `toml:"outline"`
	PDF        float64 `toml:"pdf"`
	StackedTop float64 `toml:"stacked_top"`
}

// resizeStep is the increment applied to a fraction per `<` / `>` key press.
const resizeStep = 0.02

// Per-fraction bounds. These are deliberately conservative — pushing
// outline below 10% makes the section list unreadable, pushing PDF above
// 60% leaves the source pane too narrow to render real prose. The source
// fraction is the residual (1 - outline - pdf) and must stay ≥ minSource.
const (
	minOutline = 0.10
	maxOutline = 0.50
	minPDF     = 0.10
	maxPDF     = 0.60
	minSource  = 0.10

	minStackedTop = 0.10
	maxStackedTop = 0.90
)

// layoutConfigPath returns ~/.config/mreview/layout.toml. Returns "" when
// the home directory is unknown so callers can short-circuit silently
// rather than scattering an error path through resize key handlers.
func layoutConfigPath() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	return filepath.Join(home, ".config", "mreview", "layout.toml")
}

// LoadLayoutFracs reads the saved fractions and applies them to the
// package-level vars. Missing file is not an error — the built-in
// defaults already sit in those vars from package init. Bad values are
// clamped to the valid range; an unreadable or malformed file is a
// silent no-op (the user gets defaults, not a crash).
func LoadLayoutFracs() {
	path := layoutConfigPath()
	if path == "" {
		return
	}
	if _, err := os.Stat(path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return
		}
		return
	}
	var lf LayoutFracs
	if _, err := toml.DecodeFile(path, &lf); err != nil {
		return
	}
	if lf.Outline > 0 {
		outlineFrac = lf.Outline
	}
	if lf.PDF > 0 {
		pdfFrac = lf.PDF
	}
	if lf.StackedTop > 0 {
		stackedTopFrac = lf.StackedTop
	}
	clampLayoutFracs()
}

// saveLayoutFracs writes the current vars to the layout config. Errors
// are swallowed — a failed save is annoying but not worth interrupting
// the user's review flow with a status-line error.
func saveLayoutFracs() {
	path := layoutConfigPath()
	if path == "" {
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return
	}
	tmp := path + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return
	}
	enc := toml.NewEncoder(f)
	if err := enc.Encode(LayoutFracs{
		Outline:    outlineFrac,
		PDF:        pdfFrac,
		StackedTop: stackedTopFrac,
	}); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmp)
		return
	}
	_ = os.Rename(tmp, path)
}

// clampLayoutFracs forces the package-level fractions back into their
// valid ranges. Called after every adjustment and after loading from
// disk so a hand-edited config can never put the layout into a state
// that renders zero-width panes.
func clampLayoutFracs() {
	if outlineFrac < minOutline {
		outlineFrac = minOutline
	}
	if outlineFrac > maxOutline {
		outlineFrac = maxOutline
	}
	if pdfFrac < minPDF {
		pdfFrac = minPDF
	}
	if pdfFrac > maxPDF {
		pdfFrac = maxPDF
	}
	if 1.0-outlineFrac-pdfFrac < minSource {
		// Source would collapse — give it back from whichever
		// neighbour has the most slack.
		need := minSource - (1.0 - outlineFrac - pdfFrac)
		if pdfFrac-need >= minPDF {
			pdfFrac -= need
		} else if outlineFrac-need >= minOutline {
			outlineFrac -= need
		} else {
			pdfFrac = minPDF
			outlineFrac = 1.0 - minPDF - minSource
		}
	}
	if stackedTopFrac < minStackedTop {
		stackedTopFrac = minStackedTop
	}
	if stackedTopFrac > maxStackedTop {
		stackedTopFrac = maxStackedTop
	}
}

// resizeFocusedPane adjusts the package-level fractions in response to a
// `<` (delta=-1) or `>` (delta=+1) keypress. The focused pane grows or
// shrinks; the source pane absorbs the change in 3-col layout (since it
// sits in the middle and is the natural sink), and the PDF pane absorbs
// the change in stacked layout for source/pdf focus (vertical resize).
//
// Returns true when something actually moved — the caller uses that to
// decide whether to re-schedule a PDF render and persist to disk.
func resizeFocusedPane(focus Pane, layout LayoutMode, delta int) bool {
	if delta == 0 {
		return false
	}
	step := resizeStep * float64(delta)
	before := LayoutFracs{Outline: outlineFrac, PDF: pdfFrac, StackedTop: stackedTopFrac}
	switch layout {
	case LayoutStacked:
		switch focus {
		case PaneOutline:
			outlineFrac += step
		case PaneSource:
			stackedTopFrac += step
		case PanePDF:
			stackedTopFrac -= step
		}
	default:
		switch focus {
		case PaneOutline:
			outlineFrac += step
		case PanePDF:
			pdfFrac += step
		case PaneSource:
			// Grow source by stealing equally from both neighbours.
			outlineFrac -= step / 2
			pdfFrac -= step / 2
		}
	}
	clampLayoutFracs()
	return outlineFrac != before.Outline || pdfFrac != before.PDF || stackedTopFrac != before.StackedTop
}
