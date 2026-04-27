package ui

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"

	"mreview/pkg/format"
	"mreview/pkg/parser"
	"mreview/pkg/pdf"
	"mreview/pkg/persist"
	"mreview/pkg/synctex"
)

// Filter selects which outline rows are visible. The skeleton only stores the
// current value; cycling and filtering logic land in Task 10.
type Filter int

const (
	FilterAll Filter = iota
	FilterUnreviewed
	FilterAnnotated
	FilterIssues
)

// String returns the filter's status-line label.
func (f Filter) String() string {
	switch f {
	case FilterAll:
		return "all"
	case FilterUnreviewed:
		return "unreviewed"
	case FilterAnnotated:
		return "annotated"
	case FilterIssues:
		return "issues"
	}
	return "all"
}

// Pane identifies one of the three top-level panes.
type Pane int

const (
	PaneOutline Pane = iota
	PaneSource
	PanePDF
)

// LayoutMode picks how the three panes are arranged.
//   - LayoutThreeCol: outline | source | pdf (the default).
//   - LayoutStacked:  outline | (source on top, pdf on bottom).
type LayoutMode int

const (
	LayoutThreeCol LayoutMode = iota
	LayoutStacked
)

// Popup is a placeholder for the modal overlays Task 11+ introduce
// (annotation textarea, search, ref list). The skeleton stores none, but
// reserving the field keeps the Update signature stable.
type Popup interface {
	popup()
}

// EditSnapshot captures the full pre-edit contents of paper.tex so an
// in-place edit (E or e) can be reverted from inside the TUI. Full-file
// rather than per-line because $EDITOR can rewrite arbitrary regions;
// papers are small enough that the bytes-cost is negligible.
type EditSnapshot struct {
	Path  string
	Bytes []byte
	Label string
}

// maxEditUndo bounds the in-memory undo stack. Generous because a
// .tex snapshot is tiny (a few hundred KB at most) and a long review
// session can rack up many small wording fixes the user might want to
// walk back through. Older snapshots drop off the bottom.
const maxEditUndo = 500

// Model is the bubbletea root model. The fields named in the Task 9 spec
// are populated; later tasks fill `JumpStack`, `Popup`, and the not-yet-
// existent navigation state.
type Model struct {
	Doc           *parser.Document
	Sidecar       *persist.Sidecar
	CursorBlockID string
	Filter        Filter
	Width, Height int
	Status        string
	JumpStack     JumpStack
	Popup         Popup

	Focus  Pane
	Keymap Keymap
	Styles Styles

	// SidecarPath is the on-disk path written by saveSidecar. Empty in tests
	// that exercise model logic without touching disk.
	SidecarPath string
	// SaveFn, when non-nil, replaces the default persist.Save path. Tests
	// override it to record calls and inspect the in-memory sidecar.
	SaveFn func(*persist.Sidecar) error

	// Pending holds the target of an in-flight `d` delete awaiting [y/N]
	// confirmation in the status bar.
	Pending *PendingDelete

	// CountBuf accumulates digit prefixes for motion counts (e.g. "12j").
	// PendingG is set after the first `g` of a two-key `g<x>` combo (gg, go,
	// gu) and cleared on the next keypress.
	CountBuf string
	PendingG bool

	// quitting is set when an internal Quit command was returned, so View can
	// short-circuit during the final render frame.
	quitting bool

	// PDF, Synctex are optional handles for the cursor-following PDF pane.
	// When either is nil the pane shows a placeholder. Both are injected by
	// the caller (cmd/mreview/main.go) after a successful build.
	PDF     *pdf.Doc
	Synctex *synctex.Index

	// PDFImage holds the most recent kitty-graphics escape string for the
	// PDF pane; PDFStatus holds the text placeholder when no image is
	// available (e.g. block outside PDF, render error).
	PDFImage  string
	PDFStatus string

	// pdfGen is a monotonic counter bumped whenever a render is scheduled;
	// it lets us discard stale pdfRenderMsg results delivered after the
	// user has already moved on.
	pdfGen int

	// pdfCache memoises kitty escape strings by (block, mtime, geometry).
	pdfCache *pdfCropCache

	// pageLayout memoises the per-page multi-column verdict so every
	// block on the same page reuses the decision instead of paying for
	// the median-region-width computation on each render.
	pageLayout *pageLayoutCache

	// ExternalIssues maps block IDs to format-report diagnostics loaded from
	// a paper.tex.fmt-report.md file. Populated by LoadExternalIssues.
	ExternalIssues map[string][]format.ReportDiag

	// Config holds the merged TOML configuration. nil-safe: DefaultConfig()
	// populates it when New is called.
	Config *Config

	// Layout selects between the 3-column and outline+stacked variants.
	Layout LayoutMode
	// SoftWrap controls whether long source lines wrap to additional rows
	// (the default) or get truncated with an ellipsis.
	SoftWrap bool

	// SourceLineCursor is the 1-based line number, within the current
	// cursor block, that the source pane has selected. Drives the line
	// annotation key (`a`) and the highlighted row in source rendering.
	// Reset to 1 whenever CursorBlockID changes (see Update).
	SourceLineCursor int

	// PDFManual switches the PDF pane from cursor-following crops to a
	// full-page manual viewer. The remaining Manual* fields only apply
	// when PDFManual is true; they emulate the controls of LP's
	// docviewer CLI so keyboard muscle memory carries over.
	PDFManual     bool
	ManualPDFPage int    // 0-based page index
	ManualPDFZoom int    // 0 = fit, +N = zoom in one step per N
	ManualPDFDual string // "" | "vertical" | "horizontal" — side-by-side/stacked
	ManualPDFDark string // "" | "smart" | "invert" — matches docviewer's two dark modes
	ManualPDFCropT float64
	ManualPDFCropB float64
	ManualPDFCropL float64
	ManualPDFCropR float64

	// BuildStale signals that m.Doc is ahead of m.PDF + m.Synctex —
	// the last reload couldn't deliver a coherent (doc, PDF, synctex)
	// triple, so any further auto render would lookup new line numbers
	// against a SyncTeX index from the previous build and produce
	// wrong crops. While true, schedulePDFRender returns nil and the
	// PDF pane keeps showing whatever PDFImage was last rendered.
	// Cleared by the next reload that succeeds end-to-end.
	BuildStale bool

	// reloadGen is the equivalent of pdfGen for the reload pipeline:
	// startReload bumps it, performReload captures it into the
	// reloadResultMsg, and applyReloadResult drops messages whose gen
	// no longer matches. This handles the "two reloads close together"
	// race where the slower-finishing one would otherwise apply last
	// and roll the model back to older state.
	reloadGen int

	// KittyAvailable reports whether the terminal is believed to
	// support the kitty graphics protocol. When false, schedulePDFRender
	// skips rendering and pdfPaneBody shows a text placeholder instead
	// of unconditionally spraying APC escape sequences into a terminal
	// that might render them as literal garbage. Set by main.go from
	// KittyGraphicsAvailable() during startup.
	KittyAvailable bool

	// EditUndo holds in-memory snapshots of paper.tex captured just
	// before each in-place edit (E / e). Pop on `u` to revert. Bounded
	// by maxEditUndo; cleared on quit — git is the durable safety net.
	EditUndo []EditSnapshot
}

// New constructs a Model from a parsed document and (possibly empty) sidecar.
// CursorBlockID defaults to the first unreviewed block when the sidecar does
// not pin a cursor or pins one that no longer exists, so resuming a partial
// review opens where there's still work to do. Falls back to the first
// content block if everything is already reviewed.
func New(doc *parser.Document, side *persist.Sidecar) Model {
	if side == nil {
		side = &persist.Sidecar{}
	}
	cursor := side.Cursor
	if cursor == "" || doc == nil || doc.ByID[cursor] == nil {
		cursor = firstUnreviewedOrAny(doc, side)
	}
	filter := DefaultFilter(side)
	// Edge case: a fully-reviewed paper would otherwise open with
	// FilterUnreviewed (because side.Reviewed is non-empty) and an
	// outline pane that renders `(no blocks)`. Downgrade to FilterAll
	// only in that genuinely-empty case — a partially-reviewed sidecar
	// where the saved cursor happens to land on a reviewed block must
	// stay on FilterUnreviewed, since outstanding work remains in the
	// outline even though the cursor itself is filtered out.
	if filter != FilterAll && doc != nil && FirstVisible(doc, side, filter) == "" {
		filter = FilterAll
	}
	m := Model{
		Doc:           doc,
		Sidecar:       side,
		CursorBlockID: cursor,
		Filter:        filter,
		Focus:         PaneOutline,
		Keymap:        DefaultKeymap(),
		Styles:        DefaultStyles(),
		pdfCache:         newPDFCropCache(pdfCropCacheMax),
		pageLayout:       newPageLayoutCache(),
		SoftWrap:         true,
		SourceLineCursor: 1,
		// Default optimistic — the real capability check runs in
		// main.go and overrides this. Tests that don't set up the
		// env get the rendering path enabled, matching the behaviour
		// before the capability gate was added.
		KittyAvailable: true,
	}
	m.Config = DefaultConfig()
	if n := len(side.Detached); n > 0 {
		m.Status = fmt.Sprintf("%d detached annotation(s) — see ## Detached in sidecar", n)
	}
	return m
}

// firstContentBlockID returns the ID of the first non-root block, or "" when
// the document is empty.
func firstContentBlockID(doc *parser.Document) string {
	if doc == nil || doc.Root == nil {
		return ""
	}
	for _, id := range doc.Root.ChildIDs {
		return id
	}
	return ""
}

// firstUnreviewedOrAny picks the cursor block for a fresh session: the
// first block not yet in side.Reviewed, falling back to any first block
// when everything is already reviewed (or the doc has no reviewable
// content). Used when the sidecar didn't pin a cursor — matches the
// remap.go docstring that says the UI defaults to "first unreviewed".
func firstUnreviewedOrAny(doc *parser.Document, side *persist.Sidecar) string {
	if id := FirstVisible(doc, side, FilterUnreviewed); id != "" {
		return id
	}
	return firstContentBlockID(doc)
}

// Init returns the initial command. The first cursor-following PDF render is
// deferred to the initial WindowSizeMsg in Update so the render runs with
// real terminal dimensions — scheduling it here would render at 0×0 cells
// and (because Init has a value receiver) mutate pdfGen on a discarded copy.
func (m Model) Init() tea.Cmd {
	return nil
}
