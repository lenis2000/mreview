package parser

import (
	"bytes"
	"fmt"
)

// TheoremEnv describes a theorem-like environment — either discovered via
// \newtheorem in the source, or supplied as a built-in default.
type TheoremEnv struct {
	Env     string
	Title   string
	Chain   string
	Starred bool
}

// Document is the result of Parse: a flat list of blocks plus a synthetic
// Root whose ChildIDs name the top-level blocks.
type Document struct {
	Source      []byte
	File        string
	Blocks      []*Block
	ByID        map[string]*Block
	ByLabel     map[string]*Block
	Root        *Block
	TheoremEnvs map[string]TheoremEnv
}

// Parse tokenizes src and builds the block tree.
// Only structural errors would be returned; the current implementation is
// best-effort and always returns a non-nil Document with a nil error.
func Parse(src []byte) (*Document, error) {
	p := newParser(src)
	p.collectTheoremEnvs()
	p.buildTree()
	p.segmentProofs()
	return p.doc, nil
}

// builtinTheoremEnvs is merged with \newtheorem declarations so callers can
// rely on a reasonable set of names even if the author omitted declarations.
var builtinTheoremEnvs = map[string]TheoremEnv{
	"theorem":     {Env: "theorem", Title: "Theorem"},
	"lemma":       {Env: "lemma", Title: "Lemma"},
	"proposition": {Env: "proposition", Title: "Proposition"},
	"corollary":   {Env: "corollary", Title: "Corollary"},
	"definition":  {Env: "definition", Title: "Definition"},
	"conjecture":  {Env: "conjecture", Title: "Conjecture"},
	"remark":      {Env: "remark", Title: "Remark"},
	"example":     {Env: "example", Title: "Example"},
	"claim":       {Env: "claim", Title: "Claim"},
}

var figureEnvs = map[string]bool{
	"figure":  true,
	"figure*": true,
	"table":   true,
	"table*":  true,
}

var displayMathEnvs = map[string]bool{
	"equation":    true,
	"equation*":   true,
	"align":       true,
	"align*":      true,
	"gather":      true,
	"gather*":     true,
	"multline":    true,
	"multline*":   true,
	"eqnarray":    true,
	"eqnarray*":   true,
	"displaymath": true,
	"alignat":     true,
	"alignat*":    true,
	"flalign":     true,
	"flalign*":    true,
}

var bibEnvs = map[string]bool{
	"thebibliography": true,
}

// transparentEnvs are consumed by the parser without creating a block; their
// children become direct children of the enclosing container.
var transparentEnvs = map[string]bool{
	"document": true,
}

type parser struct {
	src           []byte
	tokens        []Token
	lineStarts    []int // byte offset of the start of line i (1-based: lineStarts[i-1])
	totalLines    int
	doc           *Document
	stack         []*Block
	nextID        int
	sectionLevels map[string]int // sectionID -> level
}

func newParser(src []byte) *parser {
	tokens := Tokenize(src)
	lineStarts := []int{0}
	for i, b := range src {
		if b == '\n' {
			lineStarts = append(lineStarts, i+1)
		}
	}
	root := &Block{Kind: KindOther, ID: "root"}
	doc := &Document{
		Source:      src,
		Blocks:      []*Block{root},
		ByID:        map[string]*Block{"root": root},
		ByLabel:     map[string]*Block{},
		Root:        root,
		TheoremEnvs: copyTheoremEnvs(builtinTheoremEnvs),
	}
	return &parser{
		src:           src,
		tokens:        tokens,
		lineStarts:    lineStarts,
		totalLines:    len(lineStarts),
		doc:           doc,
		stack:         []*Block{root},
		sectionLevels: map[string]int{},
	}
}

func copyTheoremEnvs(m map[string]TheoremEnv) map[string]TheoremEnv {
	out := make(map[string]TheoremEnv, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

func (p *parser) collectTheoremEnvs() {
	for _, tk := range p.tokens {
		if tk.Kind == TokNewTheorem {
			p.doc.TheoremEnvs[tk.EnvName] = TheoremEnv{
				Env: tk.EnvName, Title: tk.Title, Chain: tk.Chain, Starred: tk.Starred,
			}
		}
	}
}

func (p *parser) newID() string {
	id := fmt.Sprintf("b%d", p.nextID)
	p.nextID++
	return id
}

func (p *parser) top() *Block { return p.stack[len(p.stack)-1] }

func (p *parser) pushBlock(b *Block) {
	parent := p.top()
	b.ParentID = parent.ID
	parent.ChildIDs = append(parent.ChildIDs, b.ID)
	p.doc.Blocks = append(p.doc.Blocks, b)
	p.doc.ByID[b.ID] = b
	p.stack = append(p.stack, b)
}

// popBlock pops the top block, finalises its EndLine and Source.
func (p *parser) popBlock(endLine int) *Block {
	b := p.top()
	if endLine < b.StartLine {
		endLine = b.StartLine
	}
	b.EndLine = endLine
	b.Source = p.extractSource(b.StartLine, b.EndLine)
	p.stack = p.stack[:len(p.stack)-1]
	return b
}

// extractSource returns src[startLine..endLine] (inclusive, 1-based lines)
// with any trailing newline trimmed.
func (p *parser) extractSource(startLine, endLine int) string {
	if startLine < 1 || endLine < startLine || startLine > p.totalLines {
		return ""
	}
	from := p.lineStarts[startLine-1]
	var to int
	if endLine >= p.totalLines {
		to = len(p.src)
	} else {
		to = p.lineStarts[endLine]
		if to > 0 && p.src[to-1] == '\n' {
			to--
		}
	}
	if to > len(p.src) {
		to = len(p.src)
	}
	return string(p.src[from:to])
}

// buildTree walks the token stream and constructs the block hierarchy.
// Proof-step segmentation is performed in a separate pass (segmentProofs).
func (p *parser) buildTree() {
	for _, tk := range p.tokens {
		switch tk.Kind {
		case TokSection:
			p.closeSectionsAtLevel(tk.Level, tk.Line)
			b := &Block{
				ID:        p.newID(),
				Kind:      KindSection,
				Title:     tk.Title,
				StartLine: tk.Line,
			}
			p.pushBlock(b)
			p.sectionLevels[b.ID] = tk.Level
		case TokBeginEnv:
			p.handleBeginEnv(tk)
		case TokEndEnv:
			p.handleEndEnv(tk)
		case TokDisplayOpen:
			b := &Block{
				ID:        p.newID(),
				Kind:      KindDisplay,
				StartLine: tk.Line,
			}
			p.pushBlock(b)
		case TokDisplayClose:
			if p.top().Kind == KindDisplay && p.top().EnvName == "" {
				p.popBlock(tk.Line)
			}
		case TokLabel:
			p.attachLabel(tk.Target)
		case TokBlankLine, TokCommentLine, TokNewTheorem, TokTheoremStyle, TokRef:
			// Not used for tree structure in Task 3; refs/newtheorem are
			// handled by other passes.
		}
	}
	// Close any remaining blocks at end of document (usually an outer section).
	for len(p.stack) > 1 {
		p.popBlock(p.totalLines)
	}
}

func (p *parser) closeSectionsAtLevel(newLevel, line int) {
	// Only sections auto-close on a new section; envs must close via TokEndEnv.
	for len(p.stack) > 1 {
		top := p.top()
		if top.Kind != KindSection {
			break
		}
		lvl, ok := p.sectionLevels[top.ID]
		if !ok || lvl < newLevel {
			break
		}
		endLine := line - 1
		if endLine < top.StartLine {
			endLine = top.StartLine
		}
		p.popBlock(endLine)
	}
}

func (p *parser) handleBeginEnv(tk Token) {
	env := tk.EnvName
	if transparentEnvs[env] {
		return
	}
	b := &Block{
		ID:        p.newID(),
		Kind:      p.envKind(env),
		EnvName:   env,
		StartLine: tk.Line,
	}
	p.pushBlock(b)
}

func (p *parser) handleEndEnv(tk Token) {
	env := tk.EnvName
	if transparentEnvs[env] {
		return
	}
	// Walk down looking for the matching env. Pop any mismatched intermediaries
	// defensively — malformed input shouldn't wedge the parser.
	for i := len(p.stack) - 1; i > 0; i-- {
		b := p.stack[i]
		if b.EnvName == env {
			for len(p.stack)-1 >= i {
				p.popBlock(tk.Line)
			}
			return
		}
	}
}

func (p *parser) envKind(env string) Kind {
	switch env {
	case "proof":
		return KindProof
	case "abstract":
		return KindAbstract
	}
	if figureEnvs[env] {
		return KindFigure
	}
	if displayMathEnvs[env] {
		return KindDisplay
	}
	if bibEnvs[env] {
		return KindBibliography
	}
	if _, ok := p.doc.TheoremEnvs[env]; ok {
		return KindTheoremLike
	}
	return KindOther
}

func (p *parser) attachLabel(target string) {
	if target == "" {
		return
	}
	b := p.top()
	if b == p.doc.Root {
		return
	}
	if b.Label == "" {
		b.Label = target
	}
	p.doc.ByLabel[target] = b
}

// segmentProofs turns each KindProof block's flat child list into a tree of
// ProofStep blocks. Each maximal run of non-blank source lines inside the
// proof becomes one ProofStep; any pre-existing children (e.g. a KindDisplay
// from an align environment) are reparented to the step containing their
// start line.
func (p *parser) segmentProofs() {
	for _, b := range p.doc.Blocks {
		if b.Kind == KindProof {
			p.segmentProof(b)
		}
	}
}

func (p *parser) segmentProof(proof *Block) {
	if proof.StartLine == 0 || proof.EndLine == 0 {
		return
	}
	startLine := proof.StartLine + 1
	endLine := proof.EndLine - 1
	if endLine < startLine {
		return
	}

	var spans [][2]int
	i := startLine
	for i <= endLine {
		for i <= endLine && p.lineIsBlank(i) {
			i++
		}
		if i > endLine {
			break
		}
		s := i
		for i <= endLine && !p.lineIsBlank(i) {
			i++
		}
		spans = append(spans, [2]int{s, i - 1})
	}
	if len(spans) == 0 {
		return
	}

	oldChildIDs := proof.ChildIDs
	proof.ChildIDs = nil

	type stepInfo struct {
		block *Block
		start int
		end   int
	}
	steps := make([]stepInfo, 0, len(spans))
	for _, sp := range spans {
		step := &Block{
			ID:        p.newID(),
			Kind:      KindProofStep,
			StartLine: sp[0],
			EndLine:   sp[1],
			ParentID:  proof.ID,
		}
		step.Source = p.extractSource(step.StartLine, step.EndLine)
		p.doc.Blocks = append(p.doc.Blocks, step)
		p.doc.ByID[step.ID] = step
		proof.ChildIDs = append(proof.ChildIDs, step.ID)
		steps = append(steps, stepInfo{step, sp[0], sp[1]})
	}

	for _, cid := range oldChildIDs {
		c := p.doc.ByID[cid]
		target := proof
		for idx := range steps {
			si := &steps[idx]
			if c.StartLine >= si.start && c.StartLine <= si.end {
				target = si.block
				if c.EndLine > target.EndLine {
					target.EndLine = c.EndLine
					target.Source = p.extractSource(target.StartLine, target.EndLine)
				}
				break
			}
		}
		c.ParentID = target.ID
		target.ChildIDs = append(target.ChildIDs, cid)
	}
}

func (p *parser) lineIsBlank(line int) bool {
	if line < 1 || line > p.totalLines {
		return true
	}
	from := p.lineStarts[line-1]
	var to int
	if line >= p.totalLines {
		to = len(p.src)
	} else {
		to = p.lineStarts[line]
	}
	raw := p.src[from:to]
	raw = bytes.TrimRight(raw, "\r\n")
	trimmed := bytes.TrimLeft(raw, " \t")
	if len(trimmed) == 0 {
		return true
	}
	if trimmed[0] == '%' {
		return true
	}
	return false
}
