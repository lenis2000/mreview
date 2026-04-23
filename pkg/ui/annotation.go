package ui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"

	"mreview/pkg/parser"
	"mreview/pkg/persist"
)

// annotTextareaWidth / Height are reasonable defaults for the inline popup.
// The real width is clamped against the source-pane width at render time.
const (
	annotTextareaWidth  = 60
	annotTextareaHeight = 6
	annotCharLimit      = 4000
)

// AnnotationPopup is the modal hosting the bubbles textarea while the user
// writes or edits a note. Mirrors revdiff's annotation popup pattern — one
// target block, one textarea, submit-immediately semantics.
//
// LineOffset > 0 marks a line-pinned annotation (`a` key). 0 marks a
// block-level annotation (`A` key). Stored on the popup so SubmitAnnotation
// can write the right anchor through to persist.Annotation.
type AnnotationPopup struct {
	TA         textarea.Model
	TargetID   string
	LineOffset int
	Editing    bool
}

// popup marks AnnotationPopup as a Popup (dispatched via Model.Popup).
func (*AnnotationPopup) popup() {}

// PendingDelete records a pending `d` confirmation. The status bar reads
// `[y/N] delete annotation?` until the user answers. LineOffset matches
// persist.Annotation: 0 = block-level annotation, >0 = line-pinned.
type PendingDelete struct {
	TargetID   string
	LineOffset int
}

// newAnnotationPopup constructs a focused textarea popup. `initial` pre-fills
// the editor with an existing note (empty string → blank popup). lineOffset
// 0 = block-level; >0 = line-pinned at that 1-based offset within the block.
func newAnnotationPopup(targetID, initial string, editing bool, lineOffset int) (*AnnotationPopup, tea.Cmd) {
	ta := textarea.New()
	ta.Prompt = "│ "
	ta.ShowLineNumbers = false
	ta.CharLimit = annotCharLimit
	ta.SetWidth(annotTextareaWidth)
	ta.SetHeight(annotTextareaHeight)
	if initial != "" {
		ta.SetValue(initial)
	}
	cmd := ta.Focus()
	return &AnnotationPopup{
		TA:         ta,
		TargetID:   targetID,
		LineOffset: lineOffset,
		Editing:    editing,
	}, cmd
}

// StartBlockAnnotation opens a popup that will write a whole-block
// annotation (LineOffset = 0) on the cursor block. Bound to `A`.
func (m Model) StartBlockAnnotation() (tea.Model, tea.Cmd) {
	if m.Doc == nil || m.CursorBlockID == "" {
		return m, nil
	}
	target := m.CursorBlockID
	initial, editing := findAnnotationFor(m.Sidecar, target, 0)
	p, cmd := newAnnotationPopup(target, initial, editing, 0)
	m.Popup = p
	m.CountBuf = ""
	m.PendingG = false
	return m, cmd
}

// StartLineAnnotation opens a popup pinned to the current SourceLineCursor
// inside the cursor block. Bound to `a`. Falls back to a block annotation
// if the block has no source range to anchor against.
func (m Model) StartLineAnnotation() (tea.Model, tea.Cmd) {
	if m.Doc == nil || m.CursorBlockID == "" {
		return m, nil
	}
	target := m.CursorBlockID
	offset := clampLineCursor(m.Doc, target, m.SourceLineCursor)
	if blockLineCount(m.Doc, target) == 0 {
		offset = 0
	}
	initial, editing := findAnnotationFor(m.Sidecar, target, offset)
	p, cmd := newAnnotationPopup(target, initial, editing, offset)
	m.Popup = p
	m.CountBuf = ""
	m.PendingG = false
	return m, cmd
}

// EditAnnotation re-opens the most relevant existing annotation on the
// cursor block: prefer one matching the current SourceLineCursor; fall back
// to any block-level annotation; otherwise silent no-op.
func (m Model) EditAnnotation() (tea.Model, tea.Cmd) {
	target := m.CursorBlockID
	if target == "" {
		return m, nil
	}
	offset := clampLineCursor(m.Doc, target, m.SourceLineCursor)
	if _, ok := findAnnotationFor(m.Sidecar, target, offset); ok {
		return m.StartLineAnnotation()
	}
	if _, ok := findAnnotationFor(m.Sidecar, target, 0); ok {
		return m.StartBlockAnnotation()
	}
	return m, nil
}

// RefreshRemappedAnnotations walks side.Annotations and rewrites the
// Breadcrumb and SourceQuote fields from the current document state.
// persist.Remap only updates structural pointers (BlockID, file, line
// range); breadcrumb and quote are UI-derived and would otherwise stay
// frozen at whatever the block looked like when the sidecar was
// written. Call this right after persist.Remap so the `@` list, the
// round-tripped sidecar headings, and any similarity matching all see
// fresh text.
func RefreshRemappedAnnotations(doc *parser.Document, side *persist.Sidecar) {
	if doc == nil || side == nil {
		return
	}
	for i := range side.Annotations {
		a := &side.Annotations[i]
		b := doc.ByID[a.BlockID]
		if b == nil {
			continue
		}
		a.Breadcrumb = AnnotationBreadcrumb(doc, b.ID)
		if a.LineOffset > 0 {
			a.SourceQuote = nthBlockLine(doc, b, a.LineOffset)
		} else {
			a.SourceQuote = b.Source
		}
	}
}

// SubmitAnnotation persists the popup's textarea contents on the target block
// and closes the popup. An empty note is treated as a cancel, not a wipe.
func (m Model) SubmitAnnotation() (tea.Model, tea.Cmd) {
	p, ok := m.Popup.(*AnnotationPopup)
	if !ok {
		return m, nil
	}
	text := strings.TrimSpace(p.TA.Value())
	if text == "" {
		m.Popup = nil
		return m, nil
	}
	b := m.Doc.ByID[p.TargetID]
	if b == nil {
		m.Popup = nil
		return m, nil
	}
	a := persist.Annotation{
		BlockID:    b.ID,
		Breadcrumb: AnnotationBreadcrumb(m.Doc, b.ID),
		File:       fileOrDoc(m.Doc, b),
		LineOffset: p.LineOffset,
		Note:       text,
	}
	if p.LineOffset > 0 && b.StartLine > 0 {
		ln := b.StartLine + p.LineOffset - 1
		if ln > b.EndLine {
			ln = b.EndLine
		}
		a.StartLine = ln
		a.EndLine = ln
		a.SourceQuote = nthBlockLine(m.Doc, b, p.LineOffset)
	} else {
		a.LineOffset = 0
		a.StartLine = b.StartLine
		a.EndLine = b.EndLine
		a.SourceQuote = b.Source
	}
	m.Sidecar.Annotations = upsertAnnotation(m.Sidecar.Annotations, a)
	if err := m.saveSidecar(); err != nil {
		m.Status = "save failed: " + err.Error()
	} else {
		m.Status = ""
	}
	m.Popup = nil
	return m, nil
}

// CancelAnnotation dismisses the popup without saving.
func (m Model) CancelAnnotation() (tea.Model, tea.Cmd) {
	m.Popup = nil
	return m, nil
}

// ToggleReviewed flips the reviewed state of the cursor block. When the
// filter is Unreviewed and the block just became reviewed, the cursor
// auto-advances to the next still-visible block.
func (m Model) ToggleReviewed() (tea.Model, tea.Cmd) {
	if m.Sidecar == nil || m.CursorBlockID == "" {
		return m, nil
	}
	was := isReviewed(m.Sidecar, m.CursorBlockID)
	m.Sidecar.Reviewed = toggleReviewedList(m.Sidecar.Reviewed, m.CursorBlockID)
	if err := m.saveSidecar(); err != nil {
		m.Status = "save failed: " + err.Error()
		return m, nil
	}
	m.Status = ""
	if !was && m.Filter == FilterUnreviewed {
		next := advanceAfterReview(m.Doc, m.Sidecar, m.Filter, m.CursorBlockID)
		if next != "" && next != m.CursorBlockID {
			m.CursorBlockID = next
		}
	}
	return m, nil
}

// advanceAfterReview picks the next visible block after the current one under
// the given filter. Used after `space` in the unreviewed view, where the
// current block has just been filtered out.
func advanceAfterReview(doc *parser.Document, side *persist.Sidecar, f Filter, cur string) string {
	order := visibleOrder(doc, side, f)
	if len(order) == 0 {
		return ""
	}
	// current block is (usually) no longer in `order` — pick the first
	// entry coming "after" it in document position.
	for _, id := range order {
		if positionOf(doc, id) > positionOf(doc, cur) {
			return id
		}
	}
	return order[0]
}

// positionOf returns the declaration-order index of a block, or -1 when
// missing. Used as a stable ordering key for advanceAfterReview.
func positionOf(doc *parser.Document, id string) int {
	if doc == nil {
		return -1
	}
	for i, b := range doc.Blocks {
		if b != nil && b.ID == id {
			return i
		}
	}
	return -1
}

// BeginDelete puts the UI into delete-confirmation state. Targets the line-
// pinned annotation matching the SourceLineCursor when one exists; otherwise
// any block-level annotation on the cursor block. Silent no-op if neither
// is present.
func (m Model) BeginDelete() Model {
	target := m.CursorBlockID
	if target == "" {
		return m
	}
	offset := clampLineCursor(m.Doc, target, m.SourceLineCursor)
	if _, ok := findAnnotationFor(m.Sidecar, target, offset); ok {
		m.Pending = &PendingDelete{TargetID: target, LineOffset: offset}
		m.Status = ""
		return m
	}
	if _, ok := findAnnotationFor(m.Sidecar, target, 0); ok {
		m.Pending = &PendingDelete{TargetID: target, LineOffset: 0}
		m.Status = ""
		return m
	}
	return m
}

// ConfirmDelete resolves the pending delete. `yes` removes the annotation;
// anything else cancels. The pending-delete flag is always cleared.
func (m Model) ConfirmDelete(yes bool) Model {
	if m.Pending == nil {
		return m
	}
	target := m.Pending.TargetID
	offset := m.Pending.LineOffset
	m.Pending = nil
	if !yes {
		return m
	}
	m.Sidecar.Annotations = removeAnnotation(m.Sidecar.Annotations, target, offset)
	if err := m.saveSidecar(); err != nil {
		m.Status = "save failed: " + err.Error()
	}
	return m
}

// findAnnotation returns the note text for the first annotation matching
// id (any LineOffset), and a boolean indicating whether one existed.
// Retained as a thin wrapper for callers that don't care about line anchors.
func findAnnotation(side *persist.Sidecar, id string) (string, bool) {
	if side == nil {
		return "", false
	}
	for _, a := range side.Annotations {
		if a.BlockID == id {
			return a.Note, true
		}
	}
	return "", false
}

// findAnnotationFor returns the note matching exactly (blockID, lineOffset).
// Used by the popup-open path so a `a` on line 3 reuses the existing line-3
// note if any, while `A` reuses an existing block-level note. Block + line
// notes coexist on the same block.
func findAnnotationFor(side *persist.Sidecar, id string, lineOffset int) (string, bool) {
	if side == nil {
		return "", false
	}
	for _, a := range side.Annotations {
		if a.BlockID == id && a.LineOffset == lineOffset {
			return a.Note, true
		}
	}
	return "", false
}

// upsertAnnotation replaces the annotation matching (BlockID, LineOffset)
// when present, else appends. Treating the line offset as part of the key
// lets a block carry one block-level note plus N line-pinned notes side by
// side without one overwriting another.
func upsertAnnotation(xs []persist.Annotation, a persist.Annotation) []persist.Annotation {
	for i, x := range xs {
		if x.BlockID == a.BlockID && x.LineOffset == a.LineOffset {
			xs[i] = a
			return xs
		}
	}
	return append(xs, a)
}

// removeAnnotation drops the annotation matching (id, lineOffset). Pass
// lineOffset=0 to remove a block-level annotation; pass the offset to
// remove a specific line-pinned one.
func removeAnnotation(xs []persist.Annotation, id string, lineOffset int) []persist.Annotation {
	out := make([]persist.Annotation, 0, len(xs))
	for _, x := range xs {
		if x.BlockID == id && x.LineOffset == lineOffset {
			continue
		}
		out = append(out, x)
	}
	return out
}

// nthBlockLine returns the n-th source line (1-based) of block b, or "" when
// out of range. Used to populate the SourceQuote of a line-pinned
// annotation so the sidecar shows the right one-line snippet.
func nthBlockLine(doc *parser.Document, b *parser.Block, n int) string {
	if doc == nil || b == nil || n < 1 {
		return ""
	}
	if b.StartLine == 0 || b.EndLine == 0 {
		return ""
	}
	target := b.StartLine + n - 1
	if target > b.EndLine {
		return ""
	}
	lines := strings.Split(string(doc.Source), "\n")
	if target-1 >= len(lines) {
		return ""
	}
	return lines[target-1]
}

// toggleReviewedList adds id when absent, removes it when present. The
// result is kept sorted so the on-disk sidecar has stable frontmatter order.
func toggleReviewedList(xs []string, id string) []string {
	for i, x := range xs {
		if x == id {
			out := append([]string(nil), xs[:i]...)
			out = append(out, xs[i+1:]...)
			return out
		}
	}
	out := append([]string(nil), xs...)
	out = append(out, id)
	sort.Strings(out)
	return out
}

// fileOrDoc returns the block's File when set, else falls back to the
// document-level file path. Empty paths are substituted with "-" so the
// sidecar heading `(...:Lx-Ly)` stays parseable on round-trip.
func fileOrDoc(doc *parser.Document, b *parser.Block) string {
	if b != nil && b.File != "" {
		return b.File
	}
	if doc != nil && doc.File != "" {
		return doc.File
	}
	return "-"
}

// saveSidecar routes through SaveFn (set by tests) when present, else uses
// SidecarPath + persist.Save. An empty path is treated as "no persistence
// configured" so model-only tests can run without disk I/O.
func (m Model) saveSidecar() error {
	if m.Sidecar != nil {
		m.Sidecar.Cursor = m.CursorBlockID
	}
	if m.SaveFn != nil {
		return m.SaveFn(m.Sidecar)
	}
	if m.SidecarPath == "" {
		return nil
	}
	return persist.Save(m.SidecarPath, m.Sidecar)
}

// EnclosingEnv returns the ID of the nearest ancestor (including self) that
// acts as an environment container: section, abstract, theorem-like, proof,
// figure, bibliography. Falls back to id when no container is found.
func EnclosingEnv(doc *parser.Document, id string) string {
	if doc == nil {
		return id
	}
	b := doc.ByID[id]
	if b == nil {
		return id
	}
	cur := b
	for cur != nil && cur.ID != "root" && cur.ID != "" {
		switch cur.Kind {
		case parser.KindSection, parser.KindAbstract, parser.KindTheoremLike,
			parser.KindProof, parser.KindFigure, parser.KindBibliography:
			return cur.ID
		}
		if cur.ParentID == "" {
			break
		}
		cur = doc.ByID[cur.ParentID]
	}
	return id
}

// AnnotationBreadcrumb returns a prose breadcrumb for a block.
// Examples:
//
//	"Section 2: Main Results"
//	"Theorem 3.2"
//	"Proof of Theorem 3.2"
//	"Proof of Theorem 3.2, step [2]"
//
// For unnamed kinds (paragraph, display math) a nearest-named ancestor is
// prefixed so the annotation sidecar still reads naturally.
func AnnotationBreadcrumb(doc *parser.Document, id string) string {
	if doc == nil {
		return ""
	}
	b := doc.ByID[id]
	if b == nil {
		return ""
	}
	switch b.Kind {
	case parser.KindProofStep:
		parent := doc.ByID[b.ParentID]
		n := 1
		if parent != nil {
			n = proofStepIndex(doc, parent, b.ID)
		}
		head := ""
		if parent != nil {
			head = AnnotationBreadcrumb(doc, parent.ID)
		}
		if head == "" {
			return fmt.Sprintf("step [%d]", n)
		}
		return fmt.Sprintf("%s, step [%d]", head, n)
	case parser.KindProof:
		if thm := precedingTheoremHead(doc, b); thm != "" {
			return "Proof of " + thm
		}
		return "Proof"
	case parser.KindTheoremLike:
		return theoremHead(b)
	case parser.KindSection:
		head := "Section"
		if b.Number != "" {
			head += " " + b.Number
		}
		if b.Title != "" {
			head += ": " + b.Title
		}
		return head
	case parser.KindFigure:
		head := "Figure"
		if b.Number != "" {
			head += " " + b.Number
		}
		if b.Title != "" {
			head += ": " + b.Title
		}
		return head
	case parser.KindAbstract:
		return "Abstract"
	case parser.KindBibliography:
		return "Bibliography"
	}
	if anc := nearestNamedAncestor(doc, b); anc != "" {
		return anc + ", " + b.Kind.String()
	}
	return b.Kind.String()
}

// theoremHead formats a theorem-like block heading — "Theorem 3.2",
// "Lemma: Title", etc.
func theoremHead(b *parser.Block) string {
	head := titleCase(b.EnvName)
	if head == "" {
		head = "Theorem"
	}
	if b.Number != "" {
		head += " " + b.Number
	}
	if b.Title != "" {
		head += ": " + b.Title
	}
	return head
}

// proofStepIndex returns the 1-based position of id among the ProofStep
// children of parent. Returns 0 when id is not a proof-step child.
func proofStepIndex(doc *parser.Document, parent *parser.Block, id string) int {
	n := 0
	for _, cid := range parent.ChildIDs {
		c := doc.ByID[cid]
		if c == nil || c.Kind != parser.KindProofStep {
			continue
		}
		n++
		if cid == id {
			return n
		}
	}
	return 0
}

// precedingTheoremHead finds the nearest preceding theorem-like sibling of
// proof and returns its heading. Empty string when none is present.
func precedingTheoremHead(doc *parser.Document, proof *parser.Block) string {
	parent := doc.ByID[proof.ParentID]
	if parent == nil {
		return ""
	}
	idx := -1
	for i, cid := range parent.ChildIDs {
		if cid == proof.ID {
			idx = i
			break
		}
	}
	for i := idx - 1; i >= 0; i-- {
		c := doc.ByID[parent.ChildIDs[i]]
		if c != nil && c.Kind == parser.KindTheoremLike {
			return theoremHead(c)
		}
	}
	return ""
}

// nearestNamedAncestor walks up until it finds a structurally named block
// and returns its breadcrumb. Used to prefix paragraphs / display math.
func nearestNamedAncestor(doc *parser.Document, b *parser.Block) string {
	cur := doc.ByID[b.ParentID]
	for cur != nil && cur.ID != "root" && cur.ID != "" {
		switch cur.Kind {
		case parser.KindSection, parser.KindTheoremLike, parser.KindProof, parser.KindFigure:
			return AnnotationBreadcrumb(doc, cur.ID)
		}
		if cur.ParentID == "" {
			break
		}
		cur = doc.ByID[cur.ParentID]
	}
	return ""
}
