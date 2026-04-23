package ui

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"

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

	// Config holds the merged TOML configuration. nil-safe: DefaultConfig()
	// populates it when New is called.
	Config *Config

	// Layout selects between the 3-column and outline+stacked variants.
	Layout LayoutMode
	// SoftWrap controls whether long source lines wrap to additional rows
	// (the default) or get truncated with an ellipsis.
	SoftWrap bool
}

// New constructs a Model from a parsed document and (possibly empty) sidecar.
// CursorBlockID defaults to the first non-root block when the sidecar does
// not pin a cursor or pins one that no longer exists.
func New(doc *parser.Document, side *persist.Sidecar) Model {
	if side == nil {
		side = &persist.Sidecar{}
	}
	cursor := side.Cursor
	if cursor == "" || doc == nil || doc.ByID[cursor] == nil {
		cursor = firstContentBlockID(doc)
	}
	m := Model{
		Doc:           doc,
		Sidecar:       side,
		CursorBlockID: cursor,
		Filter:        DefaultFilter(side),
		Focus:         PaneOutline,
		Keymap:        DefaultKeymap(),
		Styles:        DefaultStyles(),
		pdfCache:      newPDFCropCache(pdfCropCacheMax),
		SoftWrap:      true,
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

// Init returns the initial command. The first cursor-following PDF render is
// deferred to the initial WindowSizeMsg in Update so the render runs with
// real terminal dimensions — scheduling it here would render at 0×0 cells
// and (because Init has a value receiver) mutate pdfGen on a discarded copy.
func (m Model) Init() tea.Cmd {
	return nil
}
