package ui

import (
	"strconv"

	"mreview/pkg/parser"
	"mreview/pkg/persist"
)

// DefaultJumpLimit bounds the per-side size of the jump stack.
const DefaultJumpLimit = 50

// JumpStack holds the back/forward history used by Ctrl-O / Ctrl-I. Push is
// called at the origin of a jump; Pop moves backwards (shifting the origin
// onto Forward); Redo moves forward again.
type JumpStack struct {
	Back    []string
	Forward []string
	Limit   int
}

func (s *JumpStack) limit() int {
	if s.Limit <= 0 {
		return DefaultJumpLimit
	}
	return s.Limit
}

// Push records `from` as a jump origin and clears the redo forward-stack.
func (s *JumpStack) Push(from string) {
	if from == "" {
		return
	}
	s.Back = append(s.Back, from)
	if lim := s.limit(); len(s.Back) > lim {
		s.Back = append([]string(nil), s.Back[len(s.Back)-lim:]...)
	}
	s.Forward = nil
}

// Pop returns the most recent back entry and moves `current` onto Forward.
// Reports false when the stack is empty.
func (s *JumpStack) Pop(current string) (string, bool) {
	if len(s.Back) == 0 {
		return "", false
	}
	target := s.Back[len(s.Back)-1]
	s.Back = s.Back[:len(s.Back)-1]
	if current != "" {
		s.Forward = append(s.Forward, current)
		if lim := s.limit(); len(s.Forward) > lim {
			s.Forward = append([]string(nil), s.Forward[len(s.Forward)-lim:]...)
		}
	}
	return target, true
}

// Redo pops from Forward and pushes `current` back onto Back. Reports false
// when the forward-stack is empty.
func (s *JumpStack) Redo(current string) (string, bool) {
	if len(s.Forward) == 0 {
		return "", false
	}
	target := s.Forward[len(s.Forward)-1]
	s.Forward = s.Forward[:len(s.Forward)-1]
	if current != "" {
		s.Back = append(s.Back, current)
		if lim := s.limit(); len(s.Back) > lim {
			s.Back = append([]string(nil), s.Back[len(s.Back)-lim:]...)
		}
	}
	return target, true
}

// visibleOrder walks the document tree in pre-order and returns the IDs of
// blocks passing the filter. Root is not emitted.
func visibleOrder(doc *parser.Document, side *persist.Sidecar, f Filter) []string {
	if doc == nil || doc.Root == nil {
		return nil
	}
	out := make([]string, 0, len(doc.Blocks))
	var walk func(id string)
	walk = func(id string) {
		b := doc.ByID[id]
		if b == nil {
			return
		}
		if blockMatchesFilter(b, side, f) {
			out = append(out, b.ID)
		}
		for _, c := range b.ChildIDs {
			walk(c)
		}
	}
	for _, id := range doc.Root.ChildIDs {
		walk(id)
	}
	return out
}

// isInnerBlock reports whether b is considered "inner" — reachable only via
// J/K, not j/k. Children of a proof (proof-steps) and any further descendants
// of a proof-step (display math, nested paragraphs) count as inner.
func isInnerBlock(doc *parser.Document, b *parser.Block) bool {
	if doc == nil || b == nil {
		return false
	}
	for p := doc.ByID[b.ParentID]; p != nil && p.ID != "root" && p.ID != ""; p = doc.ByID[p.ParentID] {
		if p.Kind == parser.KindProof || p.Kind == parser.KindProofStep {
			return true
		}
	}
	return false
}

// outerOrder is visibleOrder with inner blocks filtered out.
func outerOrder(doc *parser.Document, side *persist.Sidecar, f Filter) []string {
	order := visibleOrder(doc, side, f)
	out := order[:0:0]
	for _, id := range order {
		b := doc.ByID[id]
		if b == nil || isInnerBlock(doc, b) {
			continue
		}
		out = append(out, id)
	}
	return out
}

// outerAnchor returns the ID of cur if it is outer, else the ID of its
// nearest outer ancestor. Returns "" when none exists.
func outerAnchor(doc *parser.Document, cur string) string {
	b := doc.ByID[cur]
	for b != nil && b.ID != "root" && b.ID != "" {
		if !isInnerBlock(doc, b) {
			return b.ID
		}
		b = doc.ByID[b.ParentID]
	}
	return ""
}

func indexOf(ids []string, id string) int {
	for i, v := range ids {
		if v == id {
			return i
		}
	}
	return -1
}

// step returns ids[i+delta], clamped to the ends. When ids is empty or i is
// out of range and ids is non-empty, returns the nearest endpoint.
func step(ids []string, i, delta int) string {
	if len(ids) == 0 {
		return ""
	}
	if i < 0 {
		if delta > 0 {
			return ids[0]
		}
		return ids[len(ids)-1]
	}
	t := i + delta
	if t < 0 {
		t = 0
	}
	if t >= len(ids) {
		t = len(ids) - 1
	}
	return ids[t]
}

// NextSibling moves n outer-level rows forward from cur. When cur is inner,
// the motion anchors at the nearest outer ancestor.
func NextSibling(doc *parser.Document, side *persist.Sidecar, f Filter, cur string, n int) string {
	if n < 1 {
		n = 1
	}
	order := outerOrder(doc, side, f)
	anchor := outerAnchor(doc, cur)
	if id := step(order, indexOf(order, anchor), n); id != "" {
		return id
	}
	return cur
}

// PrevSibling moves n outer-level rows backward from cur.
func PrevSibling(doc *parser.Document, side *persist.Sidecar, f Filter, cur string, n int) string {
	if n < 1 {
		n = 1
	}
	order := outerOrder(doc, side, f)
	anchor := outerAnchor(doc, cur)
	if id := step(order, indexOf(order, anchor), -n); id != "" {
		return id
	}
	return cur
}

// NextInner walks forward through every visible block in DFS order.
func NextInner(doc *parser.Document, side *persist.Sidecar, f Filter, cur string, n int) string {
	if n < 1 {
		n = 1
	}
	order := visibleOrder(doc, side, f)
	if id := step(order, indexOf(order, cur), n); id != "" {
		return id
	}
	return cur
}

// PrevInner walks backward through every visible block in DFS order.
func PrevInner(doc *parser.Document, side *persist.Sidecar, f Filter, cur string, n int) string {
	if n < 1 {
		n = 1
	}
	order := visibleOrder(doc, side, f)
	if id := step(order, indexOf(order, cur), -n); id != "" {
		return id
	}
	return cur
}

// NextSection moves n sections forward in visible order. Clamps at last.
func NextSection(doc *parser.Document, side *persist.Sidecar, f Filter, cur string, n int) string {
	if n < 1 {
		n = 1
	}
	order := visibleOrder(doc, side, f)
	i := indexOf(order, cur)
	if i < 0 {
		return cur
	}
	hits := 0
	last := cur
	for k := i + 1; k < len(order); k++ {
		b := doc.ByID[order[k]]
		if b != nil && b.Kind == parser.KindSection {
			last = order[k]
			hits++
			if hits == n {
				return order[k]
			}
		}
	}
	return last
}

// PrevSection moves n sections backward.
func PrevSection(doc *parser.Document, side *persist.Sidecar, f Filter, cur string, n int) string {
	if n < 1 {
		n = 1
	}
	order := visibleOrder(doc, side, f)
	i := indexOf(order, cur)
	if i < 0 {
		return cur
	}
	hits := 0
	last := cur
	for k := i - 1; k >= 0; k-- {
		b := doc.ByID[order[k]]
		if b != nil && b.Kind == parser.KindSection {
			last = order[k]
			hits++
			if hits == n {
				return order[k]
			}
		}
	}
	return last
}

// FirstVisible returns the first visible block ID, or "" when none exists.
func FirstVisible(doc *parser.Document, side *persist.Sidecar, f Filter) string {
	order := visibleOrder(doc, side, f)
	if len(order) == 0 {
		return ""
	}
	return order[0]
}

// LastVisible returns the last visible block ID, or "" when none exists.
func LastVisible(doc *parser.Document, side *persist.Sidecar, f Filter) string {
	order := visibleOrder(doc, side, f)
	if len(order) == 0 {
		return ""
	}
	return order[len(order)-1]
}

// FirstResolvedRef returns (target, true) for the first resolved outgoing
// label-style ref on b (ref/cref/Cref/eqref). Cite refs are skipped — their
// targets are bib keys that do not appear in doc.ByLabel, so they are not a
// valid `go` target; the `gd` command handles cites separately.
func FirstResolvedRef(b *parser.Block) (string, bool) {
	if b == nil {
		return "", false
	}
	for _, r := range b.RefsOut {
		if r.Kind == "cite" {
			continue
		}
		if r.Resolved && r.Target != "" {
			return r.Target, true
		}
	}
	return "", false
}

// BlocksReferencing returns the IDs of blocks whose RefsOut includes a ref
// whose Target equals label. Order matches doc.Blocks (declaration order).
// Root is never included.
func BlocksReferencing(doc *parser.Document, label string) []string {
	if doc == nil || label == "" {
		return nil
	}
	var out []string
	for _, b := range doc.Blocks {
		if b == nil || b.ID == "root" {
			continue
		}
		for _, r := range b.RefsOut {
			if r.Target == label {
				out = append(out, b.ID)
				break
			}
		}
	}
	return out
}

// parseCount turns a digit buffer into a repeat count. Empty / malformed /
// zero buffers all map to 1.
func parseCount(buf string) int {
	if buf == "" {
		return 1
	}
	n, err := strconv.Atoi(buf)
	if err != nil || n < 1 {
		return 1
	}
	return n
}

// RefListPopup is the modal shown by `gu`: the list of blocks that reference
// the cursor block's label. j/k moves the selection; Enter jumps; Esc closes.
type RefListPopup struct {
	BlockIDs []string
	Index    int
	Label    string
}

// popup is the marker method for the Popup interface.
func (*RefListPopup) popup() {}

// Move shifts the selection index, wrapping at both ends.
func (p *RefListPopup) Move(delta int) {
	if p == nil || len(p.BlockIDs) == 0 {
		return
	}
	n := len(p.BlockIDs)
	p.Index = ((p.Index+delta)%n + n) % n
}

// Selected returns the currently highlighted ID, or "" when the popup is
// empty.
func (p *RefListPopup) Selected() string {
	if p == nil || len(p.BlockIDs) == 0 {
		return ""
	}
	if p.Index < 0 || p.Index >= len(p.BlockIDs) {
		return ""
	}
	return p.BlockIDs[p.Index]
}
