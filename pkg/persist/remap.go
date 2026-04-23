package persist

import "mreview/pkg/parser"

// SimilarityThreshold is the minimum ratio between Levenshtein-edit distance
// and the length of the longer string required to treat two source quotes as
// "the same block".
const SimilarityThreshold = 0.85

// Remap ages a previously-saved sidecar against a freshly parsed document. It
// returns a new *Sidecar containing only annotations that mapped to a block
// in newDoc (with their BlockID / File / StartLine / EndLine updated to the
// current values) plus the list of reviewed IDs that still exist. Annotations
// that could not be matched are returned in the second slice so the caller
// can render them in a `## Detached` outline section.
//
// Matching is attempted in three stages for each old annotation:
//  1. exact BlockID match against newDoc.ByID;
//  2. label match when the old BlockID looks like a LaTeX label that appears
//     in newDoc.ByLabel;
//  3. best-effort source-quote similarity: pick the block whose Source is
//     closest to the old SourceQuote by normalised Levenshtein distance,
//     accepting it only if the similarity ratio is ≥ SimilarityThreshold.
func Remap(old *Sidecar, doc *parser.Document) (*Sidecar, []Annotation) {
	out := &Sidecar{
		Paper:  old.Paper,
		PDF:    old.PDF,
		Cursor: old.Cursor,
	}
	var detached []Annotation

	for _, a := range old.Annotations {
		if b, ok := resolveBlock(a, doc); ok {
			mapped := a
			mapped.BlockID = b.ID
			mapped.File = blockFile(b, doc)
			// Line-pinned annotations retain their offset when the new block
			// still has enough lines; otherwise they fall back to a whole-
			// block annotation (LineOffset 0) on the same block so the
			// reviewer's note is never silently detached.
			if a.LineOffset > 0 {
				blockLines := b.EndLine - b.StartLine + 1
				if blockLines >= a.LineOffset && b.StartLine > 0 {
					mapped.StartLine = b.StartLine + a.LineOffset - 1
					mapped.EndLine = mapped.StartLine
				} else {
					mapped.LineOffset = 0
					mapped.StartLine = b.StartLine
					mapped.EndLine = b.EndLine
				}
			} else {
				mapped.StartLine = b.StartLine
				mapped.EndLine = b.EndLine
			}
			out.Annotations = append(out.Annotations, mapped)
			continue
		}
		detached = append(detached, a)
	}

	// Keep only reviewed IDs that still exist — old IDs pointing at blocks
	// that have vanished are dropped, and any labels are rewritten to the
	// current block's ID so downstream code can look them up uniformly.
	// Dedupe as we go: a sidecar that holds both a legacy label and the
	// stable ID for the same block would otherwise produce two entries and
	// break the space-to-toggle path (which removes only the first match).
	seen := map[string]bool{}
	for _, r := range old.Reviewed {
		id := ""
		if _, ok := doc.ByID[r]; ok {
			id = r
		} else if b, ok := doc.ByLabel[r]; ok {
			id = b.ID
		}
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		out.Reviewed = append(out.Reviewed, id)
	}

	// If the saved cursor no longer exists, clear it so the UI falls back
	// to its default (first unreviewed block).
	if out.Cursor != "" {
		if _, ok := doc.ByID[out.Cursor]; !ok {
			if b, ok := doc.ByLabel[out.Cursor]; ok {
				out.Cursor = b.ID
			} else {
				out.Cursor = ""
			}
		}
	}

	return out, detached
}

// blockFile returns the block's File when set, else the document-level path.
// Empty strings leak into the sidecar heading's `(...:Lx-Ly)` group and
// break round-trip parsing, so a fallback is always emitted.
func blockFile(b *parser.Block, doc *parser.Document) string {
	if b != nil && b.File != "" {
		return b.File
	}
	if doc != nil && doc.File != "" {
		return doc.File
	}
	return "-"
}

func resolveBlock(a Annotation, doc *parser.Document) (*parser.Block, bool) {
	if b, ok := doc.ByID[a.BlockID]; ok {
		return b, true
	}
	if b, ok := doc.ByLabel[a.BlockID]; ok {
		return b, true
	}
	return bestSimilarityMatch(a.SourceQuote, doc)
}

func bestSimilarityMatch(quote string, doc *parser.Document) (*parser.Block, bool) {
	q := normaliseForSimilarity(quote)
	if q == "" {
		return nil, false
	}
	var best *parser.Block
	var bestScore float64
	for _, b := range doc.Blocks {
		if b == nil || b == doc.Root {
			continue
		}
		cand := normaliseForSimilarity(b.Source)
		if cand == "" {
			continue
		}
		score := similarity(q, cand)
		if score > bestScore {
			bestScore = score
			best = b
		}
	}
	if best != nil && bestScore >= SimilarityThreshold {
		return best, true
	}
	return nil, false
}

// normaliseForSimilarity collapses internal whitespace so Levenshtein
// distance is not dominated by reflowed newlines or indentation shifts.
func normaliseForSimilarity(s string) string {
	var b []byte
	prevSpace := true
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '\n' || c == '\r' || c == '\t' || c == ' ' {
			if !prevSpace {
				b = append(b, ' ')
				prevSpace = true
			}
			continue
		}
		b = append(b, c)
		prevSpace = false
	}
	// Trim a trailing separator space, if any.
	if len(b) > 0 && b[len(b)-1] == ' ' {
		b = b[:len(b)-1]
	}
	return string(b)
}

func similarity(a, b string) float64 {
	if a == b {
		return 1
	}
	ar, br := []rune(a), []rune(b)
	la, lb := len(ar), len(br)
	if la == 0 || lb == 0 {
		return 0
	}
	d := levenshtein(ar, br)
	m := la
	if lb > m {
		m = lb
	}
	return 1 - float64(d)/float64(m)
}

// levenshtein computes the classical edit distance with an O(min(la, lb))
// rolling row. Inputs above a few thousand runes are capped because we do
// not expect block sources to be larger than that in practice.
func levenshtein(a, b []rune) int {
	if len(a) < len(b) {
		a, b = b, a
	}
	if len(b) == 0 {
		return len(a)
	}
	prev := make([]int, len(b)+1)
	curr := make([]int, len(b)+1)
	for j := 0; j <= len(b); j++ {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		curr[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			del := prev[j] + 1
			ins := curr[j-1] + 1
			sub := prev[j-1] + cost
			curr[j] = min3(del, ins, sub)
		}
		prev, curr = curr, prev
	}
	return prev[len(b)]
}

func min3(a, b, c int) int {
	m := a
	if b < m {
		m = b
	}
	if c < m {
		m = c
	}
	return m
}
