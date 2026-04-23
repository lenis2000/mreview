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
type AnnotationPopup struct {
	TA       textarea.Model
	TargetID string
	Editing  bool // true iff replacing an existing annotation
}

// popup marks AnnotationPopup as a Popup (dispatched via Model.Popup).
func (*AnnotationPopup) popup() {}

// PendingDelete records a pending `d` confirmation. The status bar reads
// `[y/N] delete annotation?` until the user answers.
type PendingDelete struct {
	TargetID string
}

// newAnnotationPopup constructs a focused textarea popup. `initial` pre-fills
// the editor with an existing note (empty string → blank popup).
func newAnnotationPopup(targetID, initial string, editing bool) (*AnnotationPopup, tea.Cmd) {
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
		TA:       ta,
		TargetID: targetID,
		Editing:  editing,
	}, cmd
}

// StartAnnotation opens the annotation popup targeting either the current
// cursor block (enclosing=false, key `a`) or its enclosing env (enclosing=true,
// key `A`). When an annotation already exists on the target, the popup is
// pre-filled so `a` on an annotated block is equivalent to `e`.
func (m Model) StartAnnotation(enclosing bool) (tea.Model, tea.Cmd) {
	if m.Doc == nil || m.CursorBlockID == "" {
		return m, nil
	}
	target := m.CursorBlockID
	if enclosing {
		if id := EnclosingEnv(m.Doc, target); id != "" {
			target = id
		}
	}
	initial, editing := findAnnotation(m.Sidecar, target)
	p, cmd := newAnnotationPopup(target, initial, editing)
	m.Popup = p
	m.CountBuf = ""
	m.PendingG = false
	return m, cmd
}

// EditAnnotation opens the popup only when the current cursor block has an
// existing annotation. Without one, the key is a silent no-op.
func (m Model) EditAnnotation() (tea.Model, tea.Cmd) {
	if _, ok := findAnnotation(m.Sidecar, m.CursorBlockID); !ok {
		return m, nil
	}
	return m.StartAnnotation(false)
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
		BlockID:     b.ID,
		Breadcrumb:  AnnotationBreadcrumb(m.Doc, b.ID),
		File:        fileOrDoc(m.Doc, b),
		StartLine:   b.StartLine,
		EndLine:     b.EndLine,
		SourceQuote: b.Source,
		Note:        text,
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

// BeginDelete puts the UI into delete-confirmation state for the cursor
// block's annotation. Silently no-ops when no annotation exists.
func (m Model) BeginDelete() Model {
	if _, ok := findAnnotation(m.Sidecar, m.CursorBlockID); !ok {
		return m
	}
	m.Pending = &PendingDelete{TargetID: m.CursorBlockID}
	m.Status = ""
	return m
}

// ConfirmDelete resolves the pending delete. `yes` removes the annotation;
// anything else cancels. The pending-delete flag is always cleared.
func (m Model) ConfirmDelete(yes bool) Model {
	if m.Pending == nil {
		return m
	}
	target := m.Pending.TargetID
	m.Pending = nil
	if !yes {
		return m
	}
	m.Sidecar.Annotations = removeAnnotation(m.Sidecar.Annotations, target)
	if err := m.saveSidecar(); err != nil {
		m.Status = "save failed: " + err.Error()
	}
	return m
}

// findAnnotation returns the note text for the first annotation matching
// id, and a boolean indicating whether one existed.
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

// upsertAnnotation replaces the annotation on a.BlockID when present, else
// appends. Order of pre-existing annotations is preserved.
func upsertAnnotation(xs []persist.Annotation, a persist.Annotation) []persist.Annotation {
	for i, x := range xs {
		if x.BlockID == a.BlockID {
			xs[i] = a
			return xs
		}
	}
	return append(xs, a)
}

// removeAnnotation drops the annotation for id, preserving the order of the
// remaining entries.
func removeAnnotation(xs []persist.Annotation, id string) []persist.Annotation {
	out := make([]persist.Annotation, 0, len(xs))
	for _, x := range xs {
		if x.BlockID == id {
			continue
		}
		out = append(out, x)
	}
	return out
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
