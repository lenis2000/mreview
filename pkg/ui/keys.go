// Package ui implements the bubbletea TUI for mreview: a three-pane layout
// (outline / source / PDF crop) plus a bottom status line. Task 9 wires the
// skeleton; later tasks layer on outline rendering, navigation, annotation,
// and PDF rendering.
package ui

import tea "github.com/charmbracelet/bubbletea"

// Keymap centralises the small set of key bindings the TUI recognises. Fields
// hold the bubbletea `KeyMsg.String()` forms so tests can construct synthetic
// events without a real reader. Later tasks extend the struct rather than
// scatter string literals across handlers.
type Keymap struct {
	Quit        []string
	ForceQuit   []string
	CycleFilter []string

	// Navigation (Task 11).
	NavNextOuter []string // j — next outer sibling
	NavPrevOuter []string // k — prev outer sibling
	NavNextInner []string // J — next in DFS (includes proof-steps, etc.)
	NavPrevInner []string // K — prev in DFS
	NavNextSec   []string // } — next section
	NavPrevSec   []string // { — prev section
	NavLast      []string // G — last visible
	NavPrefixG   []string // g — prefix for gg / go / gu

	JumpBack    []string // ctrl+o
	JumpForward []string // ctrl+i, tab

	// Annotation. `a` is now a *line* annotation pinned to the source line
	// cursor; `A` is a block annotation on the current cursor block. The old
	// "annotate enclosing env" feature folded into `A` since the cursor block
	// is already what the reviewer means most of the time.
	Annotate         []string // a — line annotation at SourceLineCursor
	AnnotateEnv      []string // A — block annotation
	EditAnnotation   []string // ctrl+a — edit existing annotation at cursor
	DeleteAnnotation []string // d — delete with [y/N] confirm
	ToggleReviewed   []string // space — toggle reviewed state

	// Popups (Task 13).
	OpenSearch    []string // / — fuzzy search
	OpenAnnotList []string // @ — annotation list

	// Help overlay (Task 16).
	OpenHelp []string // ? — keybinding table overlay

	// Layout (\) cycles between 3-column and outline+stacked layouts.
	// Wrap (w) toggles soft-wrap for the source pane.
	// PDFManual (V) toggles the PDF pane between cursor-following crops
	// and a full-page manual mode (n/p = page nav, +/- = zoom).
	ToggleLayout []string
	ToggleWrap   []string
	// Manual PDF mode (V). These bindings are *only* consulted when
	// m.PDFManual is true, so they're free to overload keys that do
	// something else in normal mode. That keeps the manual UX close to
	// LP's docviewer CLI without adding clashes outside manual mode.
	PDFManual    []string
	PDFNextPage  []string // n / j / space
	PDFPrevPage  []string // p / k
	PDFZoomIn    []string // + / =
	PDFZoomOut   []string // -
	PDFDualPage  []string // 2 — off / vertical / horizontal
	PDFDarkMode  []string // i
	PDFGotoStart []string // 0 — first page / reset zoom

	// Source-line cursor — moves the per-block 1-based line marker that the
	// `a` (line annotation) key operates on. Independent of pane focus so
	// keyboard users can drive line nav without leaving the outline pane.
	SourceLineUp   []string // [ — previous source line (spills into prev block at boundary)
	SourceLineDown []string // ] — next source line (spills into next block at boundary)

	// Edit-in-place. E suspends mreview and runs $EDITOR on paper.tex
	// positioned at the cursor's absolute source line; on return the
	// reload pipeline re-parses, rebuilds if necessary, and remaps
	// annotations. e is the lightweight inline-edit mode for one-line
	// wording fixes.
	ExternalEdit []string
	InlineEdit   []string

	// OCR debug — renders the current PDF crop, runs tesseract, and
	// saves a bug report (PNG + similarity score + source vs OCR text)
	// to .mreview-ocr-reports/ for later investigation.
	OCRReport []string

	// Pane focus — step left / right through the visual pane order
	// (outline → source → PDF). Keyboard alternative to clicking a pane.
	FocusOutline []string // h — focus one pane left
	FocusSource  []string // l — focus one pane right

	// Open the current PDF externally in Skim.app, jumping to the line
	// under the cursor via Skim's `displayline` SyncTeX helper.
	OpenInSkim []string // S
}

// DefaultKeymap returns the built-in bindings.
func DefaultKeymap() Keymap {
	return Keymap{
		Quit:         []string{"q"},
		ForceQuit:    []string{"ctrl+c"},
		CycleFilter:  []string{"f"},
		NavNextOuter: []string{"j", "down"},
		NavPrevOuter: []string{"k", "up"},
		NavNextInner: []string{"J"},
		NavPrevInner: []string{"K"},
		NavNextSec:   []string{"}"},
		NavPrevSec:   []string{"{"},
		NavLast:      []string{"G"},
		NavPrefixG:   []string{"g"},
		JumpBack:     []string{"ctrl+o"},
		JumpForward:  []string{"ctrl+i", "tab"},

		Annotate:         []string{"a"},
		AnnotateEnv:      []string{"A"},
		EditAnnotation:   []string{"ctrl+a"},
		DeleteAnnotation: []string{"d"},
		ToggleReviewed:   []string{" ", "space"},

		OpenSearch:    []string{"/"},
		OpenAnnotList: []string{"@"},

		OpenHelp: []string{"?"},

		ToggleLayout: []string{"\\"},
		ToggleWrap:   []string{"w"},
		PDFManual:    []string{"V"},
		PDFNextPage:  []string{"n", "j", " ", "space", "down", "right", "."},
		PDFPrevPage:  []string{"p", "k", "up", "left", ","},
		PDFZoomIn:    []string{"+", "="},
		PDFZoomOut:   []string{"-", "_"},
		PDFDualPage:  []string{"2"},
		PDFDarkMode:  []string{"i"},
		PDFGotoStart: []string{"0"},

		SourceLineUp:   []string{"["},
		SourceLineDown: []string{"]"},

		ExternalEdit: []string{"E"},
		InlineEdit:   []string{"e"},
		OCRReport:    []string{"B"},

		FocusOutline: []string{"h", "left"},
		FocusSource:  []string{"l", "right"},

		OpenInSkim: []string{"S"},
	}
}

// matches reports whether the given key string is bound to any of the listed
// actions.
func matches(key string, bindings []string) bool {
	for _, b := range bindings {
		if b == key {
			return true
		}
	}
	return false
}

// isQuitKey reports whether the key event should terminate the program.
func (k Keymap) isQuitKey(msg tea.KeyMsg) bool {
	s := msg.String()
	return matches(s, k.Quit) || matches(s, k.ForceQuit)
}

// isFilterKey reports whether the key event should cycle the outline filter.
func (k Keymap) isFilterKey(msg tea.KeyMsg) bool {
	return matches(msg.String(), k.CycleFilter)
}
