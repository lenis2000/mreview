package ui

import (
	tea "github.com/charmbracelet/bubbletea"

	"mreview/pkg/parser"
	"mreview/pkg/persist"
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
	return Model{
		Doc:           doc,
		Sidecar:       side,
		CursorBlockID: cursor,
		Filter:        DefaultFilter(side),
		Focus:         PaneOutline,
		Keymap:        DefaultKeymap(),
		Styles:        DefaultStyles(),
	}
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

// Init returns the initial command. The skeleton has no startup work to
// schedule; Task 14+ will trigger the first PDF render here.
func (m Model) Init() tea.Cmd {
	return nil
}
