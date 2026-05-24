package diffui

import (
	"fmt"
	"os"
	"sort"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"mreview/pkg/diffreview"
	"mreview/pkg/pdf"
	"mreview/pkg/synctex"
	"mreview/pkg/ui"
)

// Filter selects which semantic diff pairs are visible in the outline.
type Filter int

const (
	// FilterChanged is the default diff-review view.
	FilterChanged Filter = iota
	FilterAll
	FilterUnreviewed
	FilterAnnotated
	FilterIssues
)

// String returns the filter label shown in the status bar.
func (f Filter) String() string {
	switch f {
	case FilterAll:
		return "all"
	case FilterChanged:
		return "changed"
	case FilterUnreviewed:
		return "unreviewed"
	case FilterAnnotated:
		return "annotated"
	case FilterIssues:
		return "issues"
	default:
		return "changed"
	}
}

// CycleFilter rotates through the diff outline filters.
func CycleFilter(f Filter) Filter {
	switch f {
	case FilterAll:
		return FilterChanged
	case FilterChanged:
		return FilterUnreviewed
	case FilterUnreviewed:
		return FilterAnnotated
	case FilterAnnotated:
		return FilterIssues
	default:
		return FilterAll
	}
}

// Options configures a new diff TUI model.
type Options struct {
	Config             *ui.Config
	Styles             ui.Styles
	Filter             Filter
	Sidecar            *diffreview.Sidecar
	Reviewed           map[string]bool
	Annotations        map[string]string
	Issues             map[string][]string
	AllowModifications bool
	RequestedAllowMods bool
	Status             string
	NoBuild            bool
	Draft              bool
	BuildCmd           string
	SidecarPath        string
	StdoutFormat       string
	OpenZed            bool
	PDF                *pdf.Doc
	Synctex            *synctex.Index
	KittyAvailable     bool
	BuildStale         bool
	PDFStatus          string
}

// Model is the Bubble Tea state for the semantic diff-review skeleton.
type Model struct {
	Review *diffreview.Review
	Config *ui.Config

	Cursor int
	Filter Filter
	// SourceLineCursor is 1-based within the selected new block. The current
	// diff skeleton does not expose source-line navigation yet, so it defaults
	// to the first line and is kept here for edit anchoring.
	SourceLineCursor int

	Width, Height int
	Status        string
	Styles        ui.Styles

	Sidecar            *diffreview.Sidecar
	Reviewed           map[string]bool
	Annotations        map[string]string
	Issues             map[string][]string
	AllowModifications bool
	RequestedAllowMods bool

	NoBuild        bool
	Draft          bool
	BuildCmd       string
	SidecarPath    string
	StdoutFormat   string
	OpenZed        bool
	PDF            *pdf.Doc
	Synctex        *synctex.Index
	BuildStale     bool
	PDFImage       string
	PDFStatus      string
	pdfGen         int
	pdfReloadGen   int
	KittyAvailable bool

	ShowHelp bool
	pendingG bool
	quitting bool

	Popup    *AnnotationPopup
	LineEdit *LineEditPopup
	Pending  *PendingDelete

	EditUndo []EditSnapshot
	EditRedo []EditSnapshot
	OpSeq    int
}

// AnnotationPopup is the block-level diff annotation editor.
type AnnotationPopup struct {
	TA      textarea.Model
	PairID  string
	Editing bool
}

// PendingDelete records a pending annotation delete confirmation.
type PendingDelete struct {
	PairID string
}

// New constructs a diff TUI model with the changed filter selected by
// default and the cursor snapped to the first visible semantic pair.
func New(review *diffreview.Review, opts Options) Model {
	side := opts.Sidecar
	if side == nil {
		side = diffreview.NewSidecar(review)
	}
	reviewed := side.ReviewedSet()
	for id, v := range opts.Reviewed {
		reviewed[id] = v
	}
	annotations := side.AnnotationNotes()
	for id, note := range opts.Annotations {
		annotations[id] = note
	}
	m := Model{
		Review:             review,
		Config:             opts.Config,
		Filter:             opts.Filter,
		Status:             opts.Status,
		Styles:             opts.Styles,
		Sidecar:            side,
		Reviewed:           reviewed,
		Annotations:        annotations,
		Issues:             copyIssueMap(opts.Issues),
		AllowModifications: opts.AllowModifications,
		RequestedAllowMods: opts.RequestedAllowMods,
		NoBuild:            opts.NoBuild,
		Draft:              opts.Draft,
		BuildCmd:           opts.BuildCmd,
		SidecarPath:        opts.SidecarPath,
		StdoutFormat:       opts.StdoutFormat,
		OpenZed:            opts.OpenZed,
		PDF:                opts.PDF,
		Synctex:            opts.Synctex,
		BuildStale:         opts.BuildStale,
		PDFStatus:          opts.PDFStatus,
		KittyAvailable:     opts.KittyAvailable,
		SourceLineCursor:   1,
	}
	if side.CursorPairID != "" {
		if idx := pairIndexByID(review, side.CursorPairID); idx >= 0 {
			m.Cursor = idx
		}
	}
	m.snapCursor()
	return m
}

// EditSnapshot captures a complete pre-edit copy of the new endpoint so
// diff-mode undo/redo can restore only that file.
type EditSnapshot struct {
	Path     string
	Bytes    []byte
	Label    string
	Sequence int
}

// LineEditPopup hosts the one-line inline editor for diff mode.
type LineEditPopup struct {
	TI           textinput.Model
	AbsoluteLine int
	Original     string
	Indent       string
}

const maxEditUndo = 100

func (m *Model) pushEditSnapshot(label string) error {
	if m.Review == nil || m.Review.New.Path == "" {
		return fmt.Errorf("no new source file")
	}
	data, err := os.ReadFile(m.Review.New.Path)
	if err != nil {
		return err
	}
	m.OpSeq++
	m.EditUndo = append(m.EditUndo, EditSnapshot{
		Path:     m.Review.New.Path,
		Bytes:    data,
		Label:    label,
		Sequence: m.OpSeq,
	})
	if len(m.EditUndo) > maxEditUndo {
		m.EditUndo = m.EditUndo[len(m.EditUndo)-maxEditUndo:]
	}
	m.EditRedo = nil
	return nil
}

// Init implements tea.Model.
func (m Model) Init() tea.Cmd {
	if m.OpenZed {
		return m.compareEditorCmd()
	}
	return nil
}

// CurrentPair returns the selected semantic pair.
func (m Model) CurrentPair() *diffreview.Pair {
	if m.Review == nil || m.Cursor < 0 || m.Cursor >= len(m.Review.Pairs) {
		return nil
	}
	return &m.Review.Pairs[m.Cursor]
}

// FinalSidecar returns the sidecar with cursor/reviewed/annotation state synced
// from the current model.
func (m Model) FinalSidecar() *diffreview.Sidecar {
	side := m.Sidecar
	if side == nil {
		side = diffreview.NewSidecar(m.Review)
	}
	pair := m.CurrentPair()
	if pair != nil {
		side.CursorPairID = pair.ID
	}
	side.Reviewed = reviewedList(m.Reviewed)
	return side
}

func (m Model) visibleIndices() []int {
	if m.Review == nil {
		return nil
	}
	visible := make([]int, 0, len(m.Review.Pairs))
	for i := range m.Review.Pairs {
		if pairMatchesFilter(m.Review.Pairs[i], m.Filter, m.Reviewed, m.Annotations, m.Issues) {
			visible = append(visible, i)
		}
	}
	return visible
}

func (m *Model) snapCursor() {
	if m.Review == nil || len(m.Review.Pairs) == 0 {
		m.Cursor = 0
		return
	}
	visible := m.visibleIndices()
	if len(visible) == 0 {
		if m.Cursor < 0 {
			m.Cursor = 0
		}
		if m.Cursor >= len(m.Review.Pairs) {
			m.Cursor = len(m.Review.Pairs) - 1
		}
		return
	}
	if containsIndex(visible, m.Cursor) {
		m.snapSourceLine()
		return
	}
	for _, idx := range visible {
		if idx >= m.Cursor {
			m.Cursor = idx
			m.snapSourceLine()
			return
		}
	}
	m.Cursor = visible[len(visible)-1]
	m.snapSourceLine()
}

func (m Model) statusText() string {
	pair := m.CurrentPair()
	selected := "-"
	if pair != nil {
		selected = pair.ID
	}
	stats := reviewStats(m.Review)
	base := fmt.Sprintf(
		"filter:%s pair:%s total:%d ~%d +%d -%d fmt%d %s%d",
		m.Filter.String(),
		selected,
		stats.Total,
		stats.Changed,
		stats.Added,
		stats.Deleted,
		stats.FormatOnly,
		StatusMarker(diffreview.Moved),
		stats.Moved,
	)
	if m.Status == "" {
		return base
	}
	return base + " | " + m.Status
}

func pairMatchesFilter(
	pair diffreview.Pair,
	filter Filter,
	reviewed map[string]bool,
	annotations map[string]string,
	issues map[string][]string,
) bool {
	switch filter {
	case FilterAll:
		return true
	case FilterChanged:
		return changedStatus(pair.Status)
	case FilterUnreviewed:
		return changedStatus(pair.Status) && !reviewed[pair.ID]
	case FilterAnnotated:
		return annotations[pair.ID] != ""
	case FilterIssues:
		return len(issues[pair.ID]) > 0
	default:
		return changedStatus(pair.Status)
	}
}

func changedStatus(status diffreview.PairStatus) bool {
	switch status {
	case diffreview.Changed, diffreview.Added, diffreview.Deleted, diffreview.Moved, diffreview.FormatOnly:
		return true
	default:
		return false
	}
}

func containsIndex(values []int, needle int) bool {
	for _, v := range values {
		if v == needle {
			return true
		}
	}
	return false
}

func copyIssueMap(in map[string][]string) map[string][]string {
	if len(in) == 0 {
		return map[string][]string{}
	}
	out := make(map[string][]string, len(in))
	for k, v := range in {
		out[k] = append([]string(nil), v...)
	}
	return out
}

func reviewedList(reviewed map[string]bool) []string {
	out := make([]string, 0, len(reviewed))
	for id, ok := range reviewed {
		if ok && id != "" {
			out = append(out, id)
		}
	}
	sort.Strings(out)
	return out
}

func pairIndexByID(review *diffreview.Review, pairID string) int {
	if review == nil || pairID == "" {
		return -1
	}
	for i := range review.Pairs {
		if review.Pairs[i].ID == pairID {
			return i
		}
	}
	return -1
}
