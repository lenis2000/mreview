package ui

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"

	"mreview/pkg/format"
	"mreview/pkg/parser"
	"mreview/pkg/persist"
)

// Update is the bubbletea state-transition function. Handles window resizes,
// quit, filter cycling, (Task 11) vim-style navigation plus jump-stack
// manipulation, and (Task 15) the debounced PDF-pane render pipeline.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if _, ok := msg.(tea.MouseMsg); !ok {
		m.mouseWheelEdge = mouseWheelEdgeState{}
	}
	if rm, ok := msg.(pdfRenderMsg); ok {
		return m.handlePDFRender(rm)
	}
	if rlm, ok := msg.(reloadMsg); ok {
		if rlm.err != nil {
			// $EDITOR failed — preserve the error text and skip the reload.
			// The source file either wasn't changed or was saved before the
			// crash; the next successful edit will pick up any stray changes.
			m.Status = "editor: " + rlm.err.Error()
			return m, nil
		}
		nm, cmd := m.startReload()
		return nm, cmd
	}
	if rd, ok := msg.(reloadDocMsg); ok {
		return m.applyReloadDocResult(rd)
	}
	if rr, ok := msg.(reloadResultMsg); ok {
		return m.applyReloadResult(rr)
	}
	if _, ok := msg.(tickSourceWatchMsg); ok {
		return m.handleSourceWatch()
	}
	if _, ok := msg.(tickPDFWatchMsg); ok {
		return m.handlePDFWatch()
	}
	if pw, ok := msg.(pdfWatchResultMsg); ok {
		return m.applyPDFWatchResult(pw)
	}
	if or, ok := msg.(ocrReportMsg); ok {
		m.Status = or.status
		return m, nil
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
		// Mouse is modal to popups, mirroring KeyMsg above. Without this
		// guard, a click or wheel during an open annotation popup would
		// move m.CursorBlockID / m.SourceLineCursor; the inline editor
		// (which renders against the live cursor) would disappear from
		// view while the popup textarea kept receiving keystrokes, and
		// submit would write to Popup.TargetID — a different block from
		// the one the user is now looking at.
		if m.Popup != nil {
			next = m
		} else {
			next = m.handleMouse(msg)
		}
	default:
		return m, nil
	}

	nm, ok := next.(Model)
	if !ok {
		return next, cmd
	}
	geometryChanged := nm.Width != beforeW || nm.Height != beforeH
	cursorChanged := nm.CursorBlockID != before
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
		// Leave m.PDFImage alone: pane geometry is unchanged by the
		// mode flip, so keeping the previous crop visible through the
		// render debounce (~30ms) avoids an avoidable blank window.
		// handlePDFRender replaces it atomically when the new render
		// lands. BuildStale doesn't need a special case anymore
		// because nothing is being cleared.
		if m.PDFManual {
			// Land on the page the cursor block currently
			// occupies, not whatever page V was last left on
			// (or page 1 for a fresh session). Falls through
			// to the existing ManualPDFPage if SyncTeX can't
			// resolve the block.
			if p, ok := cursorPDFPage(&m); ok {
				m.ManualPDFPage = p
			}
			m.Status = manualPDFStatusHint(m)
		} else {
			m.Status = ""
		}
		return m, m.schedulePDFRender()
	}
	// h / l (and left/right) step focus one pane left / right through
	// outline → source → PDF. Matches the visual left-to-right layout
	// so the muscle memory is "point at the pane you want". Arrow keys
	// are skipped while manual PDF mode is active so they keep their
	// docviewer-style page-nav meaning there.
	isArrow := key == "left" || key == "right"
	if matches(key, m.Keymap.FocusOutline) && (!isArrow || !m.PDFManual) {
		switch m.Focus {
		case PanePDF:
			m.Focus = PaneSource
		default:
			m.Focus = PaneOutline
		}
		m.CountBuf = ""
		return m, nil
	}
	if matches(key, m.Keymap.FocusSource) && (!isArrow || !m.PDFManual) {
		switch m.Focus {
		case PaneOutline:
			m.Focus = PaneSource
		default:
			m.Focus = PanePDF
		}
		m.CountBuf = ""
		return m, nil
	}

	// Focus-aware directional routing in V mode: j/k/up/down follow the
	// focused pane — PDF nav when the PDF pane is focused, source scroll
	// when the source pane is focused. Non-directional V-mode keys
	// (n/p/+/-/2/0/i/space/,/.) still act on the PDF regardless of focus.
	inVModeDirectional := m.PDFManual && (key == "j" || key == "k" || key == "up" || key == "down")
	if m.PDFManual && (!inVModeDirectional || m.Focus != PaneSource) {
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
			if m.ManualPDFDark == "smart" {
				m.ManualPDFDark = ""
			} else {
				m.ManualPDFDark = "smart"
			}
			m.CountBuf = ""
			m.Status = manualPDFStatusHint(m)
			return m, m.schedulePDFRender()
		case matches(key, m.Keymap.PDFDarkSimple):
			if m.ManualPDFDark == "invert" {
				m.ManualPDFDark = ""
			} else {
				m.ManualPDFDark = "invert"
			}
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
		// schedule a re-render at the new size. Preserve the previous
		// image + cache under BuildStale because schedulePDFRender is
		// suppressed then, and blanking would contradict the
		// "keep the last known-good crop until the next successful
		// reload" contract. The stale crop is at old geometry and may
		// look slightly off, but that's less jarring than an empty pane.
		if !m.BuildStale {
			m.PDFImage = ""
			m.pdfCache = newPDFCropCache(pdfCropCacheMax)
		}
		return m, m.schedulePDFRender()
	}
	if matches(key, m.Keymap.ToggleWrap) {
		m.SoftWrap = !m.SoftWrap
		m.CountBuf = ""
		return m, nil
	}
	if matches(key, m.Keymap.ResizeShrink) || matches(key, m.Keymap.ResizeGrow) {
		delta := -1
		if matches(key, m.Keymap.ResizeGrow) {
			delta = 1
		}
		m.CountBuf = ""
		if !resizeFocusedPane(m.Focus, m.Layout, delta) {
			return m, nil
		}
		// Pane geometry changed — same invalidation contract as
		// ToggleLayout: drop the cached crop and re-render at the new
		// width/height (unless BuildStale is hiding the live pipeline).
		if !m.BuildStale {
			m.PDFImage = ""
			m.pdfCache = newPDFCropCache(pdfCropCacheMax)
		}
		go saveLayoutFracs()
		return m, m.schedulePDFRender()
	}
	// `[` / `]` are pane-agnostic block navigation — explicit prev/next
	// sibling regardless of focus. In the source pane, j/k still scroll
	// by line (the focus-hijack below), so `[` / `]` are the way to
	// step blocks while keeping the source pane focused.
	if matches(key, m.Keymap.BlockPrev) {
		return m.applyMotion(PrevSibling), nil
	}
	if matches(key, m.Keymap.BlockNext) {
		return m.applyMotion(NextSibling), nil
	}
	if matches(key, m.Keymap.ExternalEdit) {
		m.CountBuf = ""
		if !m.AllowModifications {
			m.Status = "read-only — pass --allow-modifications to edit source"
			return m, nil
		}
		return m.editInExternalEditor()
	}
	if matches(key, m.Keymap.InlineEdit) {
		m.CountBuf = ""
		if !m.AllowModifications {
			m.Status = "read-only — pass --allow-modifications to edit source"
			return m, nil
		}
		return m.StartLineEdit()
	}
	if matches(key, m.Keymap.Undo) {
		m.CountBuf = ""
		return m.UndoEdit()
	}
	if matches(key, m.Keymap.Redo) {
		m.CountBuf = ""
		return m.RedoEdit()
	}
	if matches(key, m.Keymap.OCRReport) {
		m.CountBuf = ""
		return m.startOCRReport()
	}
	if matches(key, m.Keymap.Build) {
		m.CountBuf = ""
		nm, cmd := m.startReload()
		return nm, cmd
	}
	if matches(key, m.Keymap.OpenInSkim) {
		m.CountBuf = ""
		return m.openInSkim()
	}
	if matches(key, m.Keymap.ReloadSkim) {
		m.CountBuf = ""
		return m.reloadSkim()
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
	case matches(key, m.Keymap.SearchNext):
		return m.SearchAgain(true), nil
	case matches(key, m.Keymap.SearchPrev):
		return m.SearchAgain(false), nil
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
	case matches(key, m.Keymap.DeleteBlockAnnotation):
		return m.BeginDeleteBlock(), nil
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
	dark := "off"
	if m.ManualPDFDark != "" {
		dark = m.ManualPDFDark
	}
	return fmt.Sprintf("PDF manual · pg %s%s · zoom %d · dual:%s · dark:%s · n/p +/- 2 i D V",
		page, total, m.ManualPDFZoom, dual, dark)
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
type motionFn func(doc *parser.Document, side *persist.Sidecar, f Filter, cur string, n int, ext ...map[string][]format.ReportDiag) string

// applyMotion parses the pending count, invokes fn, and updates the cursor.
// j/k/J/K and {/} are "movements", not "jumps"; they do not push the jump
// stack — only gg/G/go/gu record jump history.
func (m Model) applyMotion(fn motionFn) Model {
	n := parseCount(m.CountBuf)
	m.CountBuf = ""
	id := fn(m.Doc, m.Sidecar, m.Filter, m.CursorBlockID, n, m.ExternalIssues)
	if id != "" && id != m.CursorBlockID {
		m.CursorBlockID = id
		m.SourceLineCursor = 1
	}
	return m
}

// gotoFirst jumps to the first visible block. Pushes the jump stack.
func (m Model) gotoFirst() Model {
	m.CountBuf = ""
	id := FirstVisible(m.Doc, m.Sidecar, m.Filter, m.ExternalIssues)
	if id == "" || id == m.CursorBlockID {
		return m
	}
	m.JumpStack.Push(m.CursorBlockID)
	m.CursorBlockID = id
	m.SourceLineCursor = 1
	return m
}

// gotoLast jumps to the last visible block. Pushes the jump stack.
func (m Model) gotoLast() Model {
	m.CountBuf = ""
	id := LastVisible(m.Doc, m.Sidecar, m.Filter, m.ExternalIssues)
	if id == "" || id == m.CursorBlockID {
		return m
	}
	m.JumpStack.Push(m.CursorBlockID)
	m.CursorBlockID = id
	m.SourceLineCursor = 1
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
	m.SourceLineCursor = 1
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
	if target != m.CursorBlockID {
		m.CursorBlockID = target
		m.SourceLineCursor = 1
	}
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
	if target != m.CursorBlockID {
		m.CursorBlockID = target
		m.SourceLineCursor = 1
	}
	m.Status = ""
	return m, nil
}

// updatePopup dispatches keys to the active popup. On close, it clears the
// Popup field and may jump the cursor to the popup's selected block.
func (m Model) updatePopup(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch p := m.Popup.(type) {
	case *AnnotationPopup:
		switch msg.Type {
		case tea.KeyEnter:
			return m.SubmitAnnotation()
		case tea.KeyEsc, tea.KeyCtrlC:
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
				m.SourceLineCursor = 1
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
		return m.updateLineEditPopup(p, msg)
	}
	return m, nil
}

// updateLineEditPopup routes keys for the inline single-line editor.
// Insert mode is the default and forwards keys to the textinput; Esc
// promotes to a minimal vim-style normal mode where w/b/e/h/l/0/$
// (with count prefixes) move the cursor. Enter submits from either
// mode; Ctrl-C always cancels; in normal mode Esc/q also cancel.
func (m Model) updateLineEditPopup(p *LineEditPopup, msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.Type == tea.KeyEnter || msg.Type == tea.KeyCtrlS {
		return m.SubmitLineEdit()
	}
	if msg.Type == tea.KeyCtrlC {
		return m.CancelLineEdit()
	}
	// Ctrl-R is the in-popup redo — symmetric to `u` in normal mode.
	// Bound at the popup level so it works from either mode without
	// the user having to hop into normal first.
	if msg.Type == tea.KeyCtrlR {
		if v, ok := p.popRedo(); ok {
			p.TI.SetValue(v)
			if pos := p.TI.Position(); pos > len([]rune(v)) {
				p.TI.SetCursor(len([]rune(v)))
			}
		}
		return m, nil
	}
	if p.NormalMode {
		return m.updateLineEditNormal(p, msg)
	}
	if msg.Type == tea.KeyEsc {
		p.NormalMode = true
		p.Count = ""
		return m, nil
	}
	prev := p.TI.Value()
	var cmd tea.Cmd
	p.TI, cmd = p.TI.Update(msg)
	p.pushHistory(prev)
	return m, cmd
}

// updateLineEditNormal handles keys while the inline editor is in
// vim normal mode. Digit keys build up a count; a motion key consumes
// the count (defaulting to 1) and moves the textinput cursor. i/a/I/A
// return to insert mode. Esc or q cancels the whole edit — consistent
// with the first Esc→normal, second Esc→cancel flow users asked for.
func (m Model) updateLineEditNormal(p *LineEditPopup, msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.Type == tea.KeyEsc {
		return m.CancelLineEdit()
	}
	s := msg.String()
	// Digit accumulator: leading 0 is the "start of line" motion, not a
	// count, matching vim.
	if len(s) == 1 && s[0] >= '1' && s[0] <= '9' {
		p.Count += s
		return m, nil
	}
	if len(s) == 1 && s[0] == '0' && p.Count != "" {
		p.Count += s
		return m, nil
	}
	n := 1
	if p.Count != "" {
		parsed := 0
		for _, r := range p.Count {
			parsed = parsed*10 + int(r-'0')
			if parsed > 10000 {
				parsed = 10000
				break
			}
		}
		if parsed > 0 {
			n = parsed
		}
	}
	p.Count = ""
	runes := []rune(p.TI.Value())
	pos := p.TI.Position()
	// Resolve a pending operator chord (currently just `d` + motion).
	// w/W complete the chord and delete [pos, motionEnd); any other
	// key cancels the pending operator silently and falls through to
	// the regular switch below so a typo'd `d` doesn't strand the user.
	if p.Pending == "d" {
		switch s {
		case "w", "W":
			total := p.PendingCount * n
			if total < 1 {
				total = 1
			}
			end := pos
			for i := 0; i < total; i++ {
				if s == "w" {
					end = motionWordForward(runes, end)
				} else {
					end = motionWORDForward(runes, end)
				}
			}
			p.Pending = ""
			p.PendingCount = 0
			if end > pos {
				prev := p.TI.Value()
				newRunes := append([]rune{}, runes[:pos]...)
				newRunes = append(newRunes, runes[end:]...)
				p.TI.SetValue(string(newRunes))
				if pos > len(newRunes) {
					pos = len(newRunes)
				}
				p.TI.SetCursor(pos)
				p.pushHistory(prev)
			}
			return m, nil
		}
		p.Pending = ""
		p.PendingCount = 0
	}
	switch s {
	case "w":
		for i := 0; i < n; i++ {
			pos = motionWordForward(runes, pos)
		}
		p.TI.SetCursor(pos)
	case "b":
		for i := 0; i < n; i++ {
			pos = motionWordBackward(runes, pos)
		}
		p.TI.SetCursor(pos)
	case "e":
		for i := 0; i < n; i++ {
			pos = motionWordEnd(runes, pos)
		}
		p.TI.SetCursor(pos)
	case "W":
		for i := 0; i < n; i++ {
			pos = motionWORDForward(runes, pos)
		}
		p.TI.SetCursor(pos)
	case "B":
		for i := 0; i < n; i++ {
			pos = motionWORDBackward(runes, pos)
		}
		p.TI.SetCursor(pos)
	case "E":
		for i := 0; i < n; i++ {
			pos = motionWORDEnd(runes, pos)
		}
		p.TI.SetCursor(pos)
	case "h", "left":
		p.TI.SetCursor(pos - n)
	case "l", "right":
		p.TI.SetCursor(pos + n)
	case "0":
		p.TI.CursorStart()
	case "$":
		p.TI.CursorEnd()
	case "^":
		i := 0
		for i < len(runes) && wordClass(runes[i]) == 0 {
			i++
		}
		p.TI.SetCursor(i)
	case "i":
		p.NormalMode = false
	case "a":
		p.TI.SetCursor(pos + 1)
		p.NormalMode = false
	case "I":
		i := 0
		for i < len(runes) && wordClass(runes[i]) == 0 {
			i++
		}
		p.TI.SetCursor(i)
		p.NormalMode = false
	case "A":
		p.TI.CursorEnd()
		p.NormalMode = false
	case "x":
		// Forward-delete under cursor, count times. Implemented via
		// SetValue since textinput has no public delete-char helper.
		if len(runes) == 0 {
			return m, nil
		}
		prev := p.TI.Value()
		end := pos + n
		if end > len(runes) {
			end = len(runes)
		}
		newRunes := append([]rune{}, runes[:pos]...)
		newRunes = append(newRunes, runes[end:]...)
		p.TI.SetValue(string(newRunes))
		if pos > len(newRunes) {
			pos = len(newRunes)
		}
		p.TI.SetCursor(pos)
		p.pushHistory(prev)
	case "d":
		// Start the `d` operator chord; the motion (`w`/`W`) on the
		// next key will perform the delete. Stash the count typed
		// before `d` so `2dw` works.
		p.Pending = "d"
		p.PendingCount = n
	case "u":
		// In-popup undo: revert one mutation. Empty stack is a no-op.
		// Cursor is clamped to the restored value's length so we never
		// land past EOL.
		if v, ok := p.popHistory(); ok {
			p.TI.SetValue(v)
			if pos := p.TI.Position(); pos > len([]rune(v)) {
				p.TI.SetCursor(len([]rune(v)))
			}
		}
	case "q":
		return m.CancelLineEdit()
	}
	return m, nil
}

// updateSearchPopup routes keys to the vim-style search prompt. The popup
// has no result list — Enter commits the query and jumps; Esc cancels.
// Everything else feeds the textinput.
func (m Model) updateSearchPopup(p *SearchPopup, msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEsc, tea.KeyCtrlC:
		m.Popup = nil
		return m, nil
	case tea.KeyEnter:
		return m.submitSearch()
	}
	var cmd tea.Cmd
	p.Input, cmd = p.Input.Update(msg)
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
