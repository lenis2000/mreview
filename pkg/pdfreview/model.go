package pdfreview

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"mreview/pkg/pdf"
)

// Model is the bubbletea state for `mreview pdf-review`.
type Model struct {
	// Immutable per session.
	PDFPath        string
	JSONPath       string
	LetterPath     string
	PDF            *pdf.Doc
	BBox           *BBoxCache
	NumPages       int
	Keymap         Keymap
	KittyAvailable bool

	// Mutable comment list (source of truth — written back on save/quit).
	Comments []Comment

	// Selected comment ID (stable across re-grouping). Zero when there are
	// no comments at all.
	CurID int

	// PDF pane state.
	Page            int     // 1-indexed; tracks selection on Enter, otherwise free
	ZoomDPI         float64 // base DPI; +/- adjusts in steps
	HighlightApprox bool    // last render: bbox lookup failed, highlight is page-only
	pdfEsc          string  // memoised kitty escape for the current state
	pdfKey          string  // cache key the current pdfEsc was rendered for

	// Geometry.
	Width, Height int

	// Status bar message and dirty flag.
	Status string
	Dirty  bool

	// Modal popup; nil = none. Currently only *HelpPopup.
	Popup interface{ popup() }

	quitting bool

	// Styles. Pulled from ui.StylesForTheme by the caller and assigned
	// before tea starts.
	BorderActive lipgloss.Style
	BorderIdle   lipgloss.Style
	HeaderStyle  lipgloss.Style
	SectionLabel lipgloss.Style
	StatusStyle  lipgloss.Style
	CursorStyle  lipgloss.Style
	DimStyle     lipgloss.Style
	WarnStyle    lipgloss.Style
}

// New builds a fresh Model from an opened PDF + a loaded report. The
// caller is responsible for: opening the PDF, calling LoadReport, and
// passing styles. The viewer takes ownership of pdfDoc and closes it
// on its own only via the caller's defer.
func New(pdfPath, jsonPath, letterPath string, doc *pdf.Doc, report *Report) Model {
	m := Model{
		PDFPath:    pdfPath,
		JSONPath:   jsonPath,
		LetterPath: letterPath,
		PDF:        doc,
		BBox:       NewBBoxCache(pdfPath),
		NumPages:   doc.NumPage(),
		Keymap:     DefaultKeymap(),
		Comments:   report.Comments,
		ZoomDPI:    144,
	}
	m.applyDefaultStyles()
	if len(m.Comments) > 0 {
		m.CurID = m.Comments[0].ID
		m.Page = m.currentPage()
		if m.Page < 1 {
			m.Page = 1
		}
	} else {
		m.Page = 1
	}
	return m
}

func (m *Model) applyDefaultStyles() {
	m.BorderActive = lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("12"))
	m.BorderIdle = lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("240"))
	m.HeaderStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("11"))
	m.SectionLabel = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("13"))
	m.StatusStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	m.CursorStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("15")).Background(lipgloss.Color("57"))
	m.DimStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	m.WarnStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("208"))
}

// Init implements tea.Model.
func (m Model) Init() tea.Cmd { return nil }

// currentComment returns a pointer to the comment with ID == CurID, or
// nil if no such comment exists.
func (m *Model) currentComment() *Comment {
	if m.CurID == 0 {
		return nil
	}
	for i := range m.Comments {
		if m.Comments[i].ID == m.CurID {
			return &m.Comments[i]
		}
	}
	return nil
}

// currentPage returns the page of the current comment, or 0 if none /
// unanchored.
func (m *Model) currentPage() int {
	if c := m.currentComment(); c != nil {
		return c.Page
	}
	return 0
}

// displayedOrder returns the comments grouped by kind in canonical order.
// Items within a group preserve their position in m.Comments.
func (m *Model) displayedOrder() []*Comment {
	out := make([]*Comment, 0, len(m.Comments))
	for _, k := range AllKinds {
		for i := range m.Comments {
			if m.Comments[i].Kind == k {
				out = append(out, &m.Comments[i])
			}
		}
	}
	return out
}

// indexOfCurrent returns the position of CurID in displayedOrder(),
// or -1 if not found.
func (m *Model) indexOfCurrent(disp []*Comment) int {
	for i, c := range disp {
		if c.ID == m.CurID {
			return i
		}
	}
	return -1
}

// counts returns the four status counts for the bar.
func (m *Model) counts() (kept, edited, dropped, pending int) {
	for _, c := range m.Comments {
		switch c.Status {
		case StatusKept:
			kept++
		case StatusEdited:
			edited++
		case StatusDropped:
			dropped++
		default:
			pending++
		}
	}
	return
}

// nextID returns the smallest unused positive ID. Used by `n` to assign
// stable IDs to manually-added comments without colliding with the
// anchoring run's IDs.
func (m *Model) nextID() int {
	max := 0
	for _, c := range m.Comments {
		if c.ID > max {
			max = c.ID
		}
	}
	return max + 1
}

// statusText is the bottom-bar text.
func (m Model) statusText() string {
	if m.Status != "" {
		return m.Status
	}
	k, e, d, p := m.counts()
	dirty := ""
	if m.Dirty {
		dirty = " ●"
	}
	return fmt.Sprintf("%d kept · %d edited · %d dropped · %d pending%s   [?] help  [w] write  [q] save+letter  [Q] save",
		k, e, d, p, dirty)
}
