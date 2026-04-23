package ui

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"

	"mreview/pkg/parser"
	"mreview/pkg/persist"
)

// Update is the bubbletea state-transition function. Handles window resizes,
// quit, filter cycling, (Task 11) vim-style navigation plus jump-stack
// manipulation, and (Task 15) the debounced PDF-pane render pipeline.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if rm, ok := msg.(pdfRenderMsg); ok {
		return m.handlePDFRender(rm)
	}
	if rlm, ok := msg.(reloadMsg); ok {
		if rlm.err != nil {
			m.Status = "editor: " + rlm.err.Error()
		}
		nm, cmd := m.startReload()
		return nm, cmd
	}
	if rr, ok := msg.(reloadResultMsg); ok {
		return m.applyReloadResult(rr)
	}
	before := m.CursorBlockID
	beforeW, beforeH := m.Width, m.Height
	var next tea.Model
	var cmd tea.Cmd
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.Width = msg.Width
		m.Height = msg.Height
		next = m
	case tea.KeyMsg:
		if m.Popup != nil {
			next, cmd = m.updatePopup(msg)
		} else {
			next, cmd = m.updateKey(msg)
		}
	case tea.MouseMsg:
		next = m.handleMouse(msg)
	default:
		return m, nil
	}

	nm, ok := next.(Model)
	if !ok {
		return next, cmd
	}
	geometryChanged := nm.Width != beforeW || nm.Height != beforeH
	cursorChanged := nm.CursorBlockID != before
	if cursorChanged {
		// Block changed — re-anchor the source line cursor at the top of the
		// new block so it always points at a real line of the visible source.
		nm.SourceLineCursor = 1
	}
	if (cursorChanged || geometryChanged) && !nm.quitting {
		if tick := nm.schedulePDFRender(); tick != nil {
			if cmd == nil {
				cmd = tick
			} else {
				cmd = tea.Batch(cmd, tick)
			}
		}
	}
	return nm, cmd
}

// handlePDFRender applies a freshly produced PDF crop (or its status fallback)
// to the model. Stale generations — produced before a more recent move — are
// silently dropped.
func (m Model) handlePDFRender(msg pdfRenderMsg) (tea.Model, tea.Cmd) {
	if msg.Generation != m.pdfGen {
		return m, nil
	}
	m.PDFImage = msg.Image
	m.PDFStatus = msg.Status
	return m, nil
}

// updateKey handles a key press when no popup is active.
func (m Model) updateKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()

	// A pending delete-confirmation intercepts most keys: y/Y confirms,
	// anything else cancels. Ctrl+C still force-quits so the user is never
	// trapped. The [y/N] prompt lives in the status bar.
	if m.Pending != nil {
		if msg.Type == tea.KeyCtrlC {
			m.Pending = nil
			m.quitting = true
			return m, tea.Quit
		}
		yes := key == "y" || key == "Y"
		return m.ConfirmDelete(yes), nil
	}

	// A pending-g combo intercepts the very next key.
	if m.PendingG {
		m.PendingG = false
		switch key {
		case "g":
			return m.gotoFirst(), nil
		case "o":
			return m.jumpToRef(), nil
		case "u":
			return m.openRefListPopup(), nil
		case "d":
			return m.OpenBibPopup(), nil
		}
		// Fall through — the key was not one of gg/go/gu/gd; treat it as a
		// fresh key press (may still be quit, filter, nav, etc.).
	}

	if m.Keymap.isQuitKey(msg) {
		m.quitting = true
		return m, tea.Quit
	}

	// Manual PDF mode: intercept its keys before the normal handlers so
	// `f`, `2`, `j`, `k`, `space`, `0` (which mean filter / digit / nav /
	// reviewed / digit in normal mode) take their docviewer-style meaning
	// while the user is driving the PDF pane manually.
	if matches(key, m.Keymap.PDFManual) {
		m.PDFManual = !m.PDFManual
		m.CountBuf = ""
		m.PDFImage = ""
		if m.PDFManual {
			m.Status = manualPDFStatusHint(m)
		} else {
			m.Status = ""
		}
		return m, m.schedulePDFRender()
	}
	if m.PDFManual {
		switch {
		case matches(key, m.Keymap.PDFNextPage):
			if m.PDF != nil {
				step := 1
				if m.ManualPDFDual != "" {
					step = 2
				}
				if m.ManualPDFPage+step < m.PDF.NumPage() {
					m.ManualPDFPage += step
				} else if m.ManualPDFPage < m.PDF.NumPage()-1 {
					m.ManualPDFPage = m.PDF.NumPage() - 1
				}
			}
			m.CountBuf = ""
			m.Status = manualPDFStatusHint(m)
			return m, m.schedulePDFRender()
		case matches(key, m.Keymap.PDFPrevPage):
			step := 1
			if m.ManualPDFDual != "" {
				step = 2
			}
			if m.ManualPDFPage >= step {
				m.ManualPDFPage -= step
			} else {
				m.ManualPDFPage = 0
			}
			m.CountBuf = ""
			m.Status = manualPDFStatusHint(m)
			return m, m.schedulePDFRender()
		case matches(key, m.Keymap.PDFZoomIn):
			m.ManualPDFZoom++
			m.CountBuf = ""
			m.Status = manualPDFStatusHint(m)
			return m, m.schedulePDFRender()
		case matches(key, m.Keymap.PDFZoomOut):
			if m.ManualPDFZoom > 0 {
				m.ManualPDFZoom--
			}
			m.CountBuf = ""
			m.Status = manualPDFStatusHint(m)
			return m, m.schedulePDFRender()
		case matches(key, m.Keymap.PDFGotoStart):
			m.ManualPDFPage = 0
			m.ManualPDFZoom = 0
			m.CountBuf = ""
			m.Status = manualPDFStatusHint(m)
			return m, m.schedulePDFRender()
		case matches(key, m.Keymap.PDFFitCycle):
			switch m.ManualPDFFit {
			case "", "auto":
				m.ManualPDFFit = "width"
			case "width":
				m.ManualPDFFit = "height"
			default:
				m.ManualPDFFit = "auto"
			}
			m.CountBuf = ""
			m.Status = manualPDFStatusHint(m)
			return m, m.schedulePDFRender()
		case matches(key, m.Keymap.PDFDualPage):
			switch m.ManualPDFDual {
			case "":
				m.ManualPDFDual = "horizontal"
			case "horizontal":
				m.ManualPDFDual = "vertical"
			default:
				m.ManualPDFDual = ""
			}
			m.CountBuf = ""
			m.Status = manualPDFStatusHint(m)
			return m, m.schedulePDFRender()
		case matches(key, m.Keymap.PDFDarkMode):
			m.ManualPDFDark = !m.ManualPDFDark
			m.CountBuf = ""
			m.Status = manualPDFStatusHint(m)
			return m, m.schedulePDFRender()
		}
	}

	if m.Keymap.isFilterKey(msg) {
		m.Filter = CycleFilter(m.Filter)
		m.CountBuf = ""
		return m, nil
	}
	if matches(key, m.Keymap.ToggleLayout) {
		if m.Layout == LayoutThreeCol {
			m.Layout = LayoutStacked
		} else {
			m.Layout = LayoutThreeCol
		}
		m.CountBuf = ""
		// Geometry of the PDF pane changes — invalidate the cached crop and
		// schedule a re-render at the new size.
		m.PDFImage = ""
		m.pdfCache = newPDFCropCache(pdfCropCacheMax)
		return m, m.schedulePDFRender()
	}
	if matches(key, m.Keymap.ToggleWrap) {
		m.SoftWrap = !m.SoftWrap
		m.CountBuf = ""
		return m, nil
	}
	if matches(key, m.Keymap.SourceLineUp) {
		m.SourceLineCursor = clampLineCursor(m.Doc, m.CursorBlockID, m.SourceLineCursor-1)
		m.CountBuf = ""
		return m, nil
	}
	if matches(key, m.Keymap.SourceLineDown) {
		m.SourceLineCursor = clampLineCursor(m.Doc, m.CursorBlockID, m.SourceLineCursor+1)
		m.CountBuf = ""
		return m, nil
	}
	if matches(key, m.Keymap.ExternalEdit) {
		m.CountBuf = ""
		return m.editInExternalEditor()
	}
	if matches(key, m.Keymap.InlineEdit) {
		m.CountBuf = ""
		return m.StartLineEdit()
	}

	// When the source pane has focus, hijack the standard sibling-nav
	// keys (j/k/down/up) to walk source lines instead of blocks. Crossing
	// the first/last line steps to the prev/next block in DFS order so
	// the motion feels continuous.
	if m.Focus == PaneSource {
		if matches(key, m.Keymap.NavNextOuter) {
			n := parseCount(m.CountBuf)
			m.CountBuf = ""
			for i := 0; i < n; i++ {
				m = m.scrollSource(+1)
			}
			return m, nil
		}
		if matches(key, m.Keymap.NavPrevOuter) {
			n := parseCount(m.CountBuf)
			m.CountBuf = ""
			for i := 0; i < n; i++ {
				m = m.scrollSource(-1)
			}
			return m, nil
		}
	}

	// Motion-count digit buffering. Bare "0" with an empty buffer resets
	// (cancels any pending count); other digits accumulate.
	if len(key) == 1 && key[0] >= '0' && key[0] <= '9' {
		if key == "0" && m.CountBuf == "" {
			return m, nil
		}
		m.CountBuf += key
		return m, nil
	}

	if matches(key, m.Keymap.NavPrefixG) {
		m.PendingG = true
		return m, nil
	}

	switch {
	case matches(key, m.Keymap.OpenHelp):
		return m.OpenHelp(), nil
	case matches(key, m.Keymap.OpenSearch):
		return m.OpenSearch()
	case matches(key, m.Keymap.OpenAnnotList):
		return m.OpenAnnotList()
	case matches(key, m.Keymap.Annotate):
		return m.StartLineAnnotation()
	case matches(key, m.Keymap.AnnotateEnv):
		return m.StartBlockAnnotation()
	case matches(key, m.Keymap.EditAnnotation):
		return m.EditAnnotation()
	case matches(key, m.Keymap.DeleteAnnotation):
		return m.BeginDelete(), nil
	case matches(key, m.Keymap.ToggleReviewed):
		return m.ToggleReviewed()
	case matches(key, m.Keymap.NavNextOuter):
		return m.applyMotion(NextSibling), nil
	case matches(key, m.Keymap.NavPrevOuter):
		return m.applyMotion(PrevSibling), nil
	case matches(key, m.Keymap.NavNextInner):
		return m.applyMotion(NextInner), nil
	case matches(key, m.Keymap.NavPrevInner):
		return m.applyMotion(PrevInner), nil
	case matches(key, m.Keymap.NavNextSec):
		return m.applyMotion(NextSection), nil
	case matches(key, m.Keymap.NavPrevSec):
		return m.applyMotion(PrevSection), nil
	case matches(key, m.Keymap.NavLast):
		return m.gotoLast(), nil
	case matches(key, m.Keymap.JumpBack):
		return m.jumpBack()
	case matches(key, m.Keymap.JumpForward):
		return m.jumpForward()
	}

	// Unrecognised key — drop any pending count so subsequent motions start
	// from a clean slate.
	m.CountBuf = ""
	return m, nil
}

// manualPDFStatusHint returns the status-bar string shown while PDF
// manual mode is active. Includes the current page, zoom level, and the
// active fit / dual / dark settings so the user can see at a glance
// what's wired up.
func manualPDFStatusHint(m Model) string {
	var page, total string
	if m.PDF != nil {
		total = fmt.Sprintf("/%d", m.PDF.NumPage())
		page = fmt.Sprintf("%d", m.ManualPDFPage+1)
	} else {
		page = "?"
		total = ""
	}
	dual := "off"
	if m.ManualPDFDual != "" {
		dual = m.ManualPDFDual
	}
	fit := "auto"
	if m.ManualPDFFit != "" {
		fit = m.ManualPDFFit
	}
	dark := "off"
	if m.ManualPDFDark {
		dark = "on"
	}
	return fmt.Sprintf("PDF manual · pg %s%s · zoom %d · fit:%s · dual:%s · dark:%s · n/p +/- f 2 i V",
		page, total, m.ManualPDFZoom, fit, dual, dark)
}

// blockLineCount returns the number of lines spanned by the cursor block,
// or 0 when the block has no source range (e.g. the synthetic root). Used
// by clampLineCursor and the line-annotation handler.
func blockLineCount(doc *parser.Document, blockID string) int {
	if doc == nil || blockID == "" {
		return 0
	}
	b := doc.ByID[blockID]
	if b == nil || b.StartLine == 0 || b.EndLine == 0 {
		return 0
	}
	n := b.EndLine - b.StartLine + 1
	if n < 0 {
		return 0
	}
	return n
}

// clampLineCursor keeps SourceLineCursor inside the current block's range
// [1, blockLineCount]. A zero block count clamps to 1 (the no-op default
// the annotation key reads).
func clampLineCursor(doc *parser.Document, blockID string, want int) int {
	n := blockLineCount(doc, blockID)
	if n <= 0 {
		return 1
	}
	if want < 1 {
		return 1
	}
	if want > n {
		return n
	}
	return want
}

// motionFn is the shared signature of NextSibling / PrevSibling / NextInner
// and their kin in nav.go.
type motionFn func(doc *parser.Document, side *persist.Sidecar, f Filter, cur string, n int) string

// applyMotion parses the pending count, invokes fn, and updates the cursor.
// j/k/J/K and {/} are "movements", not "jumps"; they do not push the jump
// stack — only gg/G/go/gu record jump history.
func (m Model) applyMotion(fn motionFn) Model {
	n := parseCount(m.CountBuf)
	m.CountBuf = ""
	id := fn(m.Doc, m.Sidecar, m.Filter, m.CursorBlockID, n)
	if id != "" {
		m.CursorBlockID = id
	}
	return m
}

// gotoFirst jumps to the first visible block. Pushes the jump stack.
func (m Model) gotoFirst() Model {
	m.CountBuf = ""
	id := FirstVisible(m.Doc, m.Sidecar, m.Filter)
	if id == "" || id == m.CursorBlockID {
		return m
	}
	m.JumpStack.Push(m.CursorBlockID)
	m.CursorBlockID = id
	return m
}

// gotoLast jumps to the last visible block. Pushes the jump stack.
func (m Model) gotoLast() Model {
	m.CountBuf = ""
	id := LastVisible(m.Doc, m.Sidecar, m.Filter)
	if id == "" || id == m.CursorBlockID {
		return m
	}
	m.JumpStack.Push(m.CursorBlockID)
	m.CursorBlockID = id
	return m
}

// jumpToRef resolves the first outgoing ref on the cursor block and jumps to
// the target. Pushes the jump stack. Silently no-ops when nothing resolves.
func (m Model) jumpToRef() Model {
	m.CountBuf = ""
	if m.Doc == nil {
		return m
	}
	b := m.Doc.ByID[m.CursorBlockID]
	target, ok := FirstResolvedRef(b)
	if !ok {
		m.Status = "go: no resolved ref in block"
		return m
	}
	dst := m.Doc.ByLabel[target]
	if dst == nil {
		m.Status = "go: target not found: " + target
		return m
	}
	if dst.ID == m.CursorBlockID {
		return m
	}
	m.JumpStack.Push(m.CursorBlockID)
	m.CursorBlockID = dst.ID
	m.Status = ""
	return m
}

// openRefListPopup opens a popup listing blocks that reference the current
// block's label. When the block has no label or no referrers, posts a status
// message and leaves the popup closed.
func (m Model) openRefListPopup() Model {
	m.CountBuf = ""
	if m.Doc == nil {
		return m
	}
	b := m.Doc.ByID[m.CursorBlockID]
	if b == nil || b.Label == "" {
		m.Status = "gu: current block has no label"
		return m
	}
	ids := BlocksReferencing(m.Doc, b.Label)
	if len(ids) == 0 {
		m.Status = "gu: no refs to " + b.Label
		return m
	}
	m.Popup = &RefListPopup{BlockIDs: ids, Label: b.Label}
	m.Status = ""
	return m
}

// jumpBack pops the back stack and moves the cursor to the most recent
// origin. No-op when stack is empty.
func (m Model) jumpBack() (tea.Model, tea.Cmd) {
	m.CountBuf = ""
	target, ok := m.JumpStack.Pop(m.CursorBlockID)
	if !ok {
		m.Status = "ctrl-o: jump stack empty"
		return m, nil
	}
	m.CursorBlockID = target
	m.Status = ""
	return m, nil
}

// jumpForward pops the forward stack.
func (m Model) jumpForward() (tea.Model, tea.Cmd) {
	m.CountBuf = ""
	target, ok := m.JumpStack.Redo(m.CursorBlockID)
	if !ok {
		m.Status = "ctrl-i: no forward jump"
		return m, nil
	}
	m.CursorBlockID = target
	m.Status = ""
	return m, nil
}

// updatePopup dispatches keys to the active popup. On close, it clears the
// Popup field and may jump the cursor to the popup's selected block.
func (m Model) updatePopup(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch p := m.Popup.(type) {
	case *AnnotationPopup:
		switch msg.Type {
		case tea.KeyEsc, tea.KeyCtrlS:
			return m.SubmitAnnotation()
		case tea.KeyCtrlC:
			return m.CancelAnnotation()
		}
		var cmd tea.Cmd
		p.TA, cmd = p.TA.Update(msg)
		return m, cmd
	case *RefListPopup:
		key := msg.String()
		switch key {
		case "esc", "q", "ctrl+c":
			m.Popup = nil
			return m, nil
		case "j", "down":
			p.Move(1)
			return m, nil
		case "k", "up":
			p.Move(-1)
			return m, nil
		case "enter":
			target := p.Selected()
			m.Popup = nil
			if target != "" && target != m.CursorBlockID {
				m.JumpStack.Push(m.CursorBlockID)
				m.CursorBlockID = target
			}
			return m, nil
		}
	case *SearchPopup:
		return m.updateSearchPopup(p, msg)
	case *AnnotListPopup:
		return m.updateAnnotListPopup(p, msg)
	case *BibPopup:
		key := msg.String()
		switch key {
		case "esc", "q", "ctrl+c", "?":
			m.Popup = nil
			return m, nil
		}
		_ = p
		return m, nil
	case *HelpPopup:
		key := msg.String()
		switch key {
		case "esc", "q", "ctrl+c", "?":
			m.Popup = nil
			return m, nil
		}
		_ = p
		return m, nil
	case *LineEditPopup:
		switch msg.Type {
		case tea.KeyEnter, tea.KeyCtrlS:
			return m.SubmitLineEdit()
		case tea.KeyEsc, tea.KeyCtrlC:
			return m.CancelLineEdit()
		}
		var cmd tea.Cmd
		p.TI, cmd = p.TI.Update(msg)
		return m, cmd
	}
	return m, nil
}

// updateSearchPopup routes keys to the fuzzy-search modal. Navigation uses
// arrow keys and Ctrl-N / Ctrl-P so the textinput can keep normal editing
// keys (no j/k — those type into the query).
func (m Model) updateSearchPopup(p *SearchPopup, msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEsc, tea.KeyCtrlC:
		m.Popup = nil
		return m, nil
	case tea.KeyEnter:
		return m.submitSearch()
	case tea.KeyDown, tea.KeyCtrlN:
		p.Move(1)
		return m, nil
	case tea.KeyUp, tea.KeyCtrlP:
		p.Move(-1)
		return m, nil
	}
	var cmd tea.Cmd
	p.Input, cmd = p.Input.Update(msg)
	p.refresh()
	return m, cmd
}

// updateAnnotListPopup routes keys to the `@` modal. j/k navigate, Enter
// jumps, e edits, d deletes, Esc / q / Ctrl-C close.
func (m Model) updateAnnotListPopup(p *AnnotListPopup, msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()
	switch key {
	case "esc", "q", "ctrl+c":
		m.Popup = nil
		return m, nil
	case "j", "down":
		p.Move(1)
		return m, nil
	case "k", "up":
		p.Move(-1)
		return m, nil
	case "enter":
		return m.jumpFromAnnotList(p)
	case "e":
		return m.editFromAnnotList(p)
	case "d":
		return m.deleteFromAnnotList(p)
	}
	return m, nil
}
