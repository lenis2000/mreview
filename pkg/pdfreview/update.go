package pdfreview

import (
	"fmt"
	"os"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// pdfRenderedMsg is delivered by the async render command with the kitty
// escape string for the current (page, quote, paneSize) state.
type pdfRenderedMsg struct {
	key         string
	escape      string
	highlighted bool
	wantHL      bool
	err         error
}

// statusClearMsg clears the status bar after a delay.
type statusClearMsg struct{}

// Update implements tea.Model. Routes WindowSizeMsg, key events, and the
// async pdfRenderedMsg.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.Width = msg.Width
		m.Height = msg.Height
		return m, m.schedulePDFRender()

	case pdfRenderedMsg:
		if msg.err != nil {
			m.Status = "PDF render: " + msg.err.Error()
			return m, clearStatusAfter(3 * time.Second)
		}
		if msg.key == m.pdfRenderKey() {
			m.pdfEsc = msg.escape
			m.pdfKey = msg.key
			// Show "approximate" only when we wanted a highlight and couldn't.
			m.HighlightApprox = msg.wantHL && !msg.highlighted
		}
		return m, nil

	case statusClearMsg:
		m.Status = ""
		return m, nil

	case editFinishedMsg:
		return m.applyEditFinished(msg)

	case tea.KeyMsg:
		return m.handleKey(msg)
	}
	return m, nil
}

// handleKey dispatches a single key event. Popups intercept all input
// when active.
func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	s := msg.String()

	if m.Popup != nil {
		// Any key dismisses the popup.
		m.Popup = nil
		return m, nil
	}

	k := m.Keymap
	switch {
	case keyMatches(s, k.Help):
		m.Popup = &HelpPopup{}
		m.Status = ""
		return m, nil

	case keyMatches(s, k.Quit):
		return m.quitWithLetter()
	case keyMatches(s, k.QuitNoLetter):
		return m.quitWithoutLetter()

	case keyMatches(s, k.Down):
		return m.moveCursor(+1), nil
	case keyMatches(s, k.Up):
		return m.moveCursor(-1), nil
	case keyMatches(s, k.NextBkt):
		return m.moveBucket(+1), nil
	case keyMatches(s, k.PrevBkt):
		return m.moveBucket(-1), nil
	case keyMatches(s, k.First):
		return m.moveTo(0), nil
	case keyMatches(s, k.Last):
		return m.moveTo(-1), nil

	case keyMatches(s, k.JumpPage):
		if c := m.currentComment(); c != nil && c.Page > 0 {
			m.Page = c.Page
			return m, m.schedulePDFRender()
		}
		m.Status = "no page anchor for this comment"
		return m, clearStatusAfter(2 * time.Second)

	case keyMatches(s, k.PageNext):
		if m.Page < m.NumPages {
			m.Page++
			return m, m.schedulePDFRender()
		}
		return m, nil
	case keyMatches(s, k.PagePrev):
		if m.Page > 1 {
			m.Page--
			return m, m.schedulePDFRender()
		}
		return m, nil

	case keyMatches(s, k.ZoomIn):
		m.ZoomDPI *= 1.2
		if m.ZoomDPI > 400 {
			m.ZoomDPI = 400
		}
		return m, m.schedulePDFRender()
	case keyMatches(s, k.ZoomOut):
		m.ZoomDPI /= 1.2
		if m.ZoomDPI < 60 {
			m.ZoomDPI = 60
		}
		return m, m.schedulePDFRender()

	case keyMatches(s, k.MarkKept):
		return m.setStatus(StatusKept), nil
	case keyMatches(s, k.MarkDrop):
		return m.setStatus(StatusDropped), nil
	case keyMatches(s, k.CycleKind):
		return m.cycleKind(), nil

	case keyMatches(s, k.EditText):
		return m.startEditText()
	case keyMatches(s, k.EditYAML):
		return m.startEditYAML()
	case keyMatches(s, k.NewItem):
		return m.newCommentAtCurrentPage(), nil

	case keyMatches(s, k.WriteNow):
		return m.writeLetterNow()
	}
	return m, nil
}

func (m Model) moveCursor(delta int) Model {
	disp := m.displayedOrder()
	if len(disp) == 0 {
		return m
	}
	idx := m.indexOfCurrent(disp)
	if idx < 0 {
		idx = 0
	}
	idx += delta
	if idx < 0 {
		idx = 0
	}
	if idx >= len(disp) {
		idx = len(disp) - 1
	}
	m.CurID = disp[idx].ID
	if disp[idx].Page > 0 {
		m.Page = disp[idx].Page
	}
	return m
}

func (m Model) moveBucket(delta int) Model {
	disp := m.displayedOrder()
	if len(disp) == 0 {
		return m
	}
	curK := ""
	if c := m.currentComment(); c != nil {
		curK = c.Kind
	}
	// Find the next-different-kind item in the requested direction.
	idx := m.indexOfCurrent(disp)
	if idx < 0 {
		idx = 0
	}
	step := delta
	if step == 0 {
		step = 1
	}
	for i := idx + step; i >= 0 && i < len(disp); i += step {
		if disp[i].Kind != curK {
			m.CurID = disp[i].ID
			if disp[i].Page > 0 {
				m.Page = disp[i].Page
			}
			return m
		}
	}
	return m
}

func (m Model) moveTo(idx int) Model {
	disp := m.displayedOrder()
	if len(disp) == 0 {
		return m
	}
	if idx < 0 || idx >= len(disp) {
		idx = len(disp) - 1
	}
	m.CurID = disp[idx].ID
	if disp[idx].Page > 0 {
		m.Page = disp[idx].Page
	}
	return m
}

func (m Model) setStatus(st string) Model {
	c := m.currentComment()
	if c == nil {
		return m
	}
	c.Status = st
	m.Dirty = true
	m.Status = fmt.Sprintf("comment #%d → %s", c.ID, st)
	return m
}

var kindCycle = []string{KindComment, KindMinor, KindFramingIntro, KindFramingOutro, KindMeta}

func (m Model) cycleKind() Model {
	c := m.currentComment()
	if c == nil {
		return m
	}
	cur := -1
	for i, k := range kindCycle {
		if k == c.Kind {
			cur = i
			break
		}
	}
	next := (cur + 1) % len(kindCycle)
	c.Kind = kindCycle[next]
	if c.Kind == KindFramingIntro || c.Kind == KindFramingOutro || c.Kind == KindMeta {
		c.Page = 0
		c.Quote = ""
	}
	m.Dirty = true
	m.Status = fmt.Sprintf("comment #%d → kind=%s", c.ID, c.Kind)
	return m
}

func (m Model) newCommentAtCurrentPage() Model {
	id := m.nextID()
	nc := Comment{
		ID:           id,
		OriginalText: "(new comment — press e to edit)",
		Page:         m.Page,
		Quote:        "",
		Confidence:   "low",
		Kind:         KindComment,
		Status:       StatusKept,
	}
	m.Comments = append(m.Comments, nc)
	m.CurID = id
	m.Dirty = true
	m.Status = fmt.Sprintf("inserted comment #%d at p.%d — press e to edit", id, m.Page)
	return m
}

// quitWithLetter saves JSON, writes the letter, then exits.
func (m Model) quitWithLetter() (tea.Model, tea.Cmd) {
	if err := m.saveJSON(); err != nil {
		m.Status = "save: " + err.Error()
		return m, clearStatusAfter(4 * time.Second)
	}
	if err := m.writeLetter(); err != nil {
		m.Status = "letter: " + err.Error()
		return m, clearStatusAfter(4 * time.Second)
	}
	m.quitting = true
	return m, tea.Quit
}

func (m Model) quitWithoutLetter() (tea.Model, tea.Cmd) {
	if err := m.saveJSON(); err != nil {
		m.Status = "save: " + err.Error()
		return m, clearStatusAfter(4 * time.Second)
	}
	m.quitting = true
	return m, tea.Quit
}

func (m Model) writeLetterNow() (tea.Model, tea.Cmd) {
	if err := m.writeLetter(); err != nil {
		m.Status = "letter: " + err.Error()
		return m, clearStatusAfter(4 * time.Second)
	}
	m.Status = "wrote " + m.LetterPath
	return m, clearStatusAfter(3 * time.Second)
}

func (m *Model) saveJSON() error {
	r := &Report{
		SourceMD:  "",
		SourcePDF: m.PDFPath,
		Generated: time.Now().UTC().Format(time.RFC3339),
		Model:     "",
		Comments:  m.Comments,
	}
	// Reuse SourceMD / Model from existing JSON if present.
	if existing, err := LoadReport(m.JSONPath); err == nil {
		r.SourceMD = existing.SourceMD
		r.Model = existing.Model
		// Preserve Generated as the original anchoring run timestamp:
		r.Generated = existing.Generated
	}
	if err := SaveReport(m.JSONPath, r); err != nil {
		return err
	}
	m.Dirty = false
	return nil
}

func (m *Model) writeLetter() error {
	out := RenderLetter(m.Comments)
	tmp := m.LetterPath + ".tmp"
	if err := os.WriteFile(tmp, []byte(out), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, m.LetterPath)
}

// pdfRenderKey returns the memo key for the current PDF render state.
// Different selections of the same comment on the same page reuse the
// cached escape; changing page, quote, dpi, or pane size invalidates.
func (m Model) pdfRenderKey() string {
	q := ""
	if c := m.currentComment(); c != nil && c.Page == m.Page {
		q = c.Quote
	}
	listW, pdfW := splitWidths(m.Width)
	_ = listW
	return fmt.Sprintf("p%d|dpi=%g|w=%d|h=%d|q=%s",
		m.Page, m.ZoomDPI, pdfW, m.Height-statusBarHeight, q)
}

// schedulePDFRender returns a tea.Cmd that renders the current PDF state
// off the bubbletea Update goroutine. The result lands as pdfRenderedMsg.
func (m Model) schedulePDFRender() tea.Cmd {
	if m.PDF == nil || m.Width <= 0 || m.Height <= 0 {
		return nil
	}
	if !m.KittyAvailable {
		return nil
	}
	page := m.Page
	if page < 1 {
		page = 1
	}
	if page > m.NumPages {
		page = m.NumPages
	}
	listW, pdfW := splitWidths(m.Width)
	_ = listW
	innerW := pdfW - 2
	if innerW < 4 {
		innerW = 4
	}
	innerH := m.Height - statusBarHeight - 3 // top/bottom border + title row
	if innerH < 1 {
		innerH = 1
	}
	quote := ""
	if c := m.currentComment(); c != nil && c.Page == page {
		quote = c.Quote
	}
	wantHL := quote != ""
	dpi := m.ZoomDPI
	doc := m.PDF
	bbox := m.BBox
	key := m.pdfRenderKey()
	return func() tea.Msg {
		esc, hl, err := renderPageEscape(doc, bbox, page, quote, dpi, innerW, innerH)
		return pdfRenderedMsg{
			key:         key,
			escape:      esc,
			highlighted: hl,
			wantHL:      wantHL,
			err:         err,
		}
	}
}

func clearStatusAfter(d time.Duration) tea.Cmd {
	return tea.Tick(d, func(time.Time) tea.Msg { return statusClearMsg{} })
}

// Quitting returns whether the model is in the post-quit state. Exposed
// for the runner to discriminate normal exits from tea internal quits.
func (m Model) Quitting() bool { return m.quitting }

// ApplyTheme overrides the default styles with externally-provided ones.
// Called by cmd/mreview/pdf_review.go after pulling ui.StylesForTheme.
func (m *Model) ApplyTheme(active, idle, header, section, status, cursor, dim, warn lipgloss.Style) {
	m.BorderActive = active
	m.BorderIdle = idle
	m.HeaderStyle = header
	m.SectionLabel = section
	m.StatusStyle = status
	m.CursorStyle = cursor
	m.DimStyle = dim
	m.WarnStyle = warn
}
