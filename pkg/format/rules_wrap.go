package format

import (
	"bytes"
	"regexp"
	"strings"
)

// WrapOptions controls the space.wrap rule.
type WrapOptions struct {
	// Mode: "" (== off), "off", "column", "sentence", "sentence+column".
	// Default ("") behaves as "off" so unconfigured callers don't surprise.
	Mode string
	// Col is the target line length used by "column" / "sentence+column".
	// Defaults to 100 when zero.
	Col int
}

const defaultWrapCol = 100

// abbreviations whose terminal '.' must NOT be treated as a sentence end.
// (matched case-sensitively; entries should be lowercased forms when seen
// on the wire — we lowercase the candidate before lookup for safety.)
var sentenceAbbrevs = map[string]bool{
	"et al.": true,
	"e.g.":   true,
	"i.e.":   true,
	"cf.":    true,
	"etc.":   true,
	"vs.":    true,
	"vol.":   true,
	"no.":    true,
	"fig.":   true,
	"eq.":    true,
	"sec.":   true,
	"ch.":    true,
	"thm.":   true,
	"prop.":  true,
	"lem.":   true,
	"cor.":   true,
	"def.":   true,
	"resp.":  true,
	"approx.":true,
	"mr.":    true,
	"mrs.":   true,
	"ms.":    true,
	"dr.":    true,
	"prof.":  true,
	"st.":    true,
}

// refLikeCmds are inline \command{...} groups whose interior we will not
// break, even at sentence-end punctuation.
var refLikeCmds = map[string]bool{
	"cite": true, "citep": true, "citet": true,
	"ref": true, "eqref": true, "cref": true, "Cref": true,
	"label": true,
	"url":   true, "href": true,
}

func registerWrapRule() {
	Registry = append(Registry, Rule{
		ID:    "space.wrap",
		Tier:  Safe,
		Doc:   "Break long lines on sentence boundaries (and optionally at the column limit).",
		Apply: applyWrap,
	})
}

func applyWrap(ctx *Ctx) Result {
	mode := ctx.Wrap.Mode
	if mode == "" || mode == "off" {
		return Result{Src: ctx.Src}
	}
	col := ctx.Wrap.Col
	if col <= 0 {
		col = defaultWrapCol
	}

	nLines := bytes.Count(ctx.Src, []byte{'\n'})
	if len(ctx.Src) > 0 && ctx.Src[len(ctx.Src)-1] != '\n' {
		nLines++
	}
	if nLines == 0 {
		return Result{Src: ctx.Src}
	}

	var out bytes.Buffer
	out.Grow(len(ctx.Src))
	changed := false
	var hits []Hit

	doSentence := strings.Contains(mode, "sentence")
	doColumn := strings.Contains(mode, "column")

	for line := 1; line <= nLines; line++ {
		body := lineBytes(ctx, line)

		// Pass-through cases — preserve verbatim text and skip-masked lines.
		if ctx.LineSkipped(line) || lineWhollyProtected(ctx, line) {
			out.Write(body)
		} else {
			leadLen, allWS := leadingWS(body)
			lead := string(body[:leadLen])
			content := string(body[leadLen:])
			// Strip a trailing comment so we don't break inside it. We only
			// touch the prose portion; the comment part is re-attached
			// untouched at the end of its original line.
			commentStart := -1
			if !allWS {
				commentStart = unescapedPercentIdx(content)
			}
			prose := content
			tail := ""
			if commentStart >= 0 {
				// Pull the whitespace that connects prose to the comment
				// into the tail so it survives sentence trimming.
				cs := commentStart
				for cs > 0 && (content[cs-1] == ' ' || content[cs-1] == '\t') {
					cs--
				}
				prose = content[:cs]
				tail = content[cs:]
			}

			pieces := wrapProse(prose, lead, col, doSentence, doColumn)
			if len(pieces) == 1 {
				// no change at the source level
				out.WriteString(lead)
				out.WriteString(prose)
				out.WriteString(tail)
			} else {
				changed = true
				hits = append(hits, Hit{
					RuleID:  "space.wrap",
					Line:    line,
					Excerpt: truncExcerpt(prose),
				})
				for i, p := range pieces {
					if i == 0 {
						out.WriteString(lead)
						out.WriteString(p)
					} else {
						out.WriteByte('\n')
						out.WriteString(lead)
						out.WriteString(p)
					}
				}
				// Trailing comment (if any) sticks to the LAST wrapped piece.
				out.WriteString(tail)
			}
		}

		if line < nLines || endsWithNewline(ctx.Src) {
			out.WriteByte('\n')
		}
	}

	if !changed {
		return Result{Src: ctx.Src}
	}
	return Result{Src: out.Bytes(), Hits: hits}
}

// wrapProse splits prose into one-or-more pieces according to the requested
// mode. Returns a single-element slice when no break is needed.
func wrapProse(prose, lead string, col int, doSentence, doColumn bool) []string {
	if strings.TrimSpace(prose) == "" {
		return []string{prose}
	}

	pieces := []string{prose}

	if doSentence {
		pieces = splitSentencesAll(pieces)
	}

	if doColumn {
		var out []string
		leadW := visualWidth(lead)
		for _, p := range pieces {
			out = append(out, columnSplit(p, col-leadW)...)
		}
		pieces = out
	}

	// Drop empty pieces that may sneak in.
	cleaned := pieces[:0]
	for _, p := range pieces {
		if p == "" {
			continue
		}
		cleaned = append(cleaned, p)
	}
	if len(cleaned) == 0 {
		return []string{prose}
	}
	return cleaned
}

// splitSentencesAll runs splitSentences over each input piece.
func splitSentencesAll(pieces []string) []string {
	var out []string
	for _, p := range pieces {
		out = append(out, splitSentences(p)...)
	}
	return out
}

// sentenceEndRe matches a candidate sentence boundary: terminal '.', '?', or
// '!' followed by whitespace and an uppercase letter or backslash (a
// LaTeX-command sentence start). The capture group is the punctuation.
var sentenceEndRe = regexp.MustCompile(`([.?!])(\s+)(?:[A-Z\\])`)

// splitSentences breaks prose at sentence boundaries, returning each
// sentence (with its terminal punctuation but trimmed of the trailing
// whitespace that connects it to the next sentence).
func splitSentences(prose string) []string {
	if prose == "" {
		return nil
	}
	excluded := excludedRanges(prose)
	matches := sentenceEndRe.FindAllStringSubmatchIndex(prose, -1)

	var pieces []string
	cursor := 0
	for _, m := range matches {
		// m = [matchStart, matchEnd, punctStart, punctEnd, wsStart, wsEnd]
		punctEnd := m[3]
		wsStart := m[4]
		wsEnd := m[5]
		// Reject the boundary if it falls in an excluded range.
		if rangeOverlapsAny(punctEnd-1, wsEnd, excluded) {
			continue
		}
		// Reject if the word ending in '.' is an abbreviation.
		if isAbbreviation(prose, punctEnd) {
			continue
		}
		piece := strings.TrimRight(prose[cursor:wsStart], " \t")
		if piece != "" {
			pieces = append(pieces, piece)
		}
		cursor = wsEnd
	}
	tail := strings.TrimRight(prose[cursor:], " \t")
	if tail != "" {
		pieces = append(pieces, tail)
	}
	if len(pieces) == 0 {
		return []string{prose}
	}
	return pieces
}

// columnSplit greedily breaks s at the rightmost space within
// [floor, target] and recurses. Returns one or more pieces.
func columnSplit(s string, target int) []string {
	if target <= 10 {
		return []string{s}
	}
	if visualWidth(s) <= target {
		return []string{s}
	}
	excluded := excludedRanges(s)
	floor := target - 10
	if floor < 10 {
		floor = 10
	}
	bestIdx := -1
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c != ' ' {
			continue
		}
		if i > 0 && s[i-1] == '\\' {
			continue // escaped space; refuse to break
		}
		if rangeOverlapsAny(i, i+1, excluded) {
			continue
		}
		w := visualWidth(s[:i])
		if w > target {
			break
		}
		if w >= floor {
			bestIdx = i // keep walking; we want rightmost
		}
	}
	if bestIdx < 0 {
		// No break point in range. Try any space (any width) before target.
		for i := 0; i < len(s); i++ {
			if s[i] != ' ' {
				continue
			}
			if i > 0 && s[i-1] == '\\' {
				continue
			}
			if rangeOverlapsAny(i, i+1, excluded) {
				continue
			}
			w := visualWidth(s[:i])
			if w > target {
				break
			}
			bestIdx = i
		}
	}
	if bestIdx < 0 {
		return []string{s}
	}
	first := strings.TrimRight(s[:bestIdx], " \t")
	rest := strings.TrimLeft(s[bestIdx+1:], " \t")
	if first == "" || rest == "" {
		return []string{s}
	}
	tail := columnSplit(rest, target)
	return append([]string{first}, tail...)
}

// excludedRanges returns byte ranges within s that must not contain a wrap
// break: inline math `$...$` / `\(...\)`, and ref-like \command{...} groups.
// Ranges are sorted by start and may overlap (we consult any-overlap).
func excludedRanges(s string) [][2]int {
	var ranges [][2]int

	// Inline math: '$' toggles, '\(...\)' is paired.
	inDollar := false
	dollarStart := 0
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '\\' && i+1 < len(s) {
			next := s[i+1]
			if next == '(' {
				// Find matching \)
				j := i + 2
				for j+1 < len(s) && !(s[j] == '\\' && s[j+1] == ')') {
					j++
				}
				if j+1 < len(s) {
					ranges = append(ranges, [2]int{i, j + 2})
					i = j + 1
					continue
				}
			}
			i++ // skip escape
			continue
		}
		if c == '$' {
			if !inDollar {
				inDollar = true
				dollarStart = i
			} else {
				ranges = append(ranges, [2]int{dollarStart, i + 1})
				inDollar = false
			}
		}
	}

	// Ref-like \command{...} groups.
	for i := 0; i < len(s); i++ {
		if s[i] != '\\' {
			continue
		}
		j := i + 1
		for j < len(s) && isAlpha(s[j]) {
			j++
		}
		name := s[i+1 : j]
		if !refLikeCmds[name] {
			continue
		}
		// Skip optional [..]
		k := j
		for k < len(s) && s[k] == '[' {
			depth := 1
			k++
			for k < len(s) && depth > 0 {
				if s[k] == '[' {
					depth++
				} else if s[k] == ']' {
					depth--
				}
				k++
			}
		}
		// Consume {…} group(s) — \href takes two.
		ngroups := 1
		if name == "href" {
			ngroups = 2
		}
		for g := 0; g < ngroups && k < len(s); g++ {
			if s[k] != '{' {
				break
			}
			depth := 1
			k++
			for k < len(s) && depth > 0 {
				if s[k] == '{' {
					depth++
				} else if s[k] == '}' {
					depth--
				}
				k++
			}
		}
		ranges = append(ranges, [2]int{i, k})
		i = k - 1
	}
	return ranges
}

func rangeOverlapsAny(start, end int, ranges [][2]int) bool {
	for _, r := range ranges {
		if r[0] < end && start < r[1] {
			return true
		}
	}
	return false
}

// isAbbreviation reports whether the word ending at the period at position
// punctEnd-1 is a known abbreviation (including multi-word ones like
// "et al.").
func isAbbreviation(prose string, punctEnd int) bool {
	if punctEnd <= 0 || punctEnd > len(prose) || prose[punctEnd-1] != '.' {
		return false
	}
	// Walk back to find the start of the candidate token (last whitespace).
	start := punctEnd - 1
	for start > 0 {
		c := prose[start-1]
		if c == ' ' || c == '\t' || c == '\n' {
			break
		}
		start--
	}
	tok := strings.ToLower(prose[start:punctEnd])
	if sentenceAbbrevs[tok] {
		return true
	}
	// Multi-word abbreviation: try "<prev> <tok>".
	if start >= 2 {
		// Find one more word back.
		s2 := start - 1
		for s2 > 0 {
			c := prose[s2-1]
			if c == ' ' || c == '\t' {
				break
			}
			s2--
		}
		multi := strings.ToLower(prose[s2:punctEnd])
		if sentenceAbbrevs[multi] {
			return true
		}
	}
	// Single-letter initials: "J." — don't end the sentence.
	if punctEnd-start == 2 && isAlpha(prose[start]) {
		return true
	}
	return false
}

func isAlpha(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z')
}

// unescapedPercentIdx returns the byte index of the first unescaped '%' in
// s (start of a comment), or -1 if there is none.
func unescapedPercentIdx(s string) int {
	for i := 0; i < len(s); i++ {
		if s[i] != '%' {
			continue
		}
		n := 0
		for k := i - 1; k >= 0 && s[k] == '\\'; k-- {
			n++
		}
		if n%2 == 0 {
			return i
		}
	}
	return -1
}

// visualWidth approximates the rendered column count of s. Tabs count as 4
// columns (rough enough; LaTeX source is unlikely to mix tabs and prose).
func visualWidth(s string) int {
	w := 0
	for _, r := range s {
		if r == '\t' {
			w += 4
		} else {
			w++
		}
	}
	return w
}

