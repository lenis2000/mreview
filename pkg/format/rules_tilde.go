package format

import (
	"bytes"
	"regexp"

	"mreview/pkg/parser"
)

// TildeOptions controls the prose.tilde-refs rule.
type TildeOptions struct {
	// Refs is the set of cite/ref command names that should be preceded by
	// a tilde rather than a regular space. When nil, defaultTildeRefs is used.
	Refs []string
}

// defaultTildeRefs is the built-in set of cite/ref-like commands that the
// tilde rule applies to.
var defaultTildeRefs = []string{
	"cite", "citep", "citet",
	"ref", "eqref", "cref", "Cref",
	"autoref", "nameref",
}

// tildeRefSet returns the effective ref command set as a map for O(1) lookup.
func tildeRefSet(opts TildeOptions) map[string]bool {
	cmds := opts.Refs
	if len(cmds) == 0 {
		cmds = defaultTildeRefs
	}
	m := make(map[string]bool, len(cmds))
	for _, c := range cmds {
		m[c] = true
	}
	return m
}

// tildeRefRe matches a backslash-command that might be a cite/ref. We
// capture the command name and verify it against the configured set.
var tildeRefRe = regexp.MustCompile(`\\([a-zA-Z]+)`)

// registerTildeRule appends the prose.tilde-refs rule to the registry.
// Called from registry.go's init().
func registerTildeRule() {
	Registry = append(Registry, Rule{
		ID:   "prose.tilde-refs",
		Tier: PDFFix,
		Doc:  "Insert ~ between a word and a following \\cite/\\ref command when separated by a regular space.",
		Apply: applyTildeRefs,
	})
}

// applyTildeRefs replaces " \cite" with "~\cite" (and similar for other
// ref-like commands) when the preceding character is a word character. The
// rule is conservative: it skips cases where the preceding character is
// `(`, `[`, `~`, `\`, start-of-line, or punctuation, and it skips
// occurrences inside protected spans or inline math.
func applyTildeRefs(ctx *Ctx) Result {
	src := ctx.Src
	refSet := tildeRefSet(ctx.Tilde)

	if len(refSet) == 0 {
		return Result{Src: src}
	}

	// Find all backslash-command positions in the source.
	matches := tildeRefRe.FindAllSubmatchIndex(src, -1)
	if len(matches) == 0 {
		return Result{Src: src}
	}

	// Build excluded ranges: inline math only. We do NOT use excludedRanges
	// here because it also excludes ref-like commands, which is exactly what
	// this rule targets. We only need to skip inline math.
	inlineMathExcluded := inlineMathRanges(src)

	// Collect replacement sites: byte positions where a space before a
	// \command should become a tilde.
	type replacement struct {
		pos     int // byte offset of the space to replace
		line    int // 1-based source line
		excerpt string
	}
	var replacements []replacement

	for _, m := range matches {
		cmdStart := m[0]  // position of '\'
		cmdEnd := m[1]    // position after the command name
		nameStart := m[2] // start of command name (after \)
		nameEnd := m[3]   // end of command name

		cmdName := string(src[nameStart:nameEnd])
		if !refSet[cmdName] {
			continue
		}

		// The space (if any) is the byte immediately before the '\'.
		if cmdStart == 0 {
			continue // start of file
		}

		spacePos := cmdStart - 1
		if src[spacePos] != ' ' {
			continue // not preceded by a regular space
		}

		// Check what precedes the space.
		if spacePos == 0 {
			continue // space is at start of file (nothing before it)
		}
		preceding := src[spacePos-1]
		if shouldSkipPreceding(preceding) {
			continue
		}

		// Check if the space is at the start of a line (only whitespace
		// before it on this line).
		line := lineAt(ctx.Lines, spacePos)
		lineStart := ctx.Lines[line]
		if isOnlyWhitespace(src[lineStart:spacePos]) {
			continue // start-of-line (after indent)
		}

		// Check if inside a protected span.
		if parser.OverlapsProtected(spacePos, cmdEnd, ctx.Protected) {
			continue
		}

		// Check if inside an excluded range (inline math).
		if rangeOverlapsAny(spacePos, cmdEnd, inlineMathExcluded) {
			continue
		}

		// Check if on a skipped line.
		if ctx.LineSkipped(line) {
			continue
		}

		_ = cmdEnd // used in overlap checks above
		replacements = append(replacements, replacement{
			pos:     spacePos,
			line:    line,
			excerpt: truncExcerpt(string(src[spacePos-min(spacePos, 20) : min(len(src), cmdEnd+10)])),
		})
	}

	if len(replacements) == 0 {
		return Result{Src: src}
	}

	// Apply replacements: replace each space with '~'.
	var out bytes.Buffer
	out.Grow(len(src))
	var hits []Hit
	prev := 0

	// Track which source lines are affected for ExpectedDiffSourceLines.
	for _, r := range replacements {
		out.Write(src[prev:r.pos])
		out.WriteByte('~')
		prev = r.pos + 1 // skip the space byte

		hits = append(hits, Hit{
			RuleID:                  "prose.tilde-refs",
			Line:                    r.line,
			ExpectedDiffSourceLines: []int{r.line},
			Excerpt:                 r.excerpt,
		})
	}
	out.Write(src[prev:])

	result := out.Bytes()
	if bytes.Equal(result, src) {
		return Result{Src: src}
	}
	return Result{Src: result, Hits: hits}
}

// shouldSkipPreceding returns true if the byte preceding the space should
// prevent tilde insertion. We skip when the preceding character is:
//   - '(' or '[' — opening bracket/paren (the reference is clause-initial)
//   - '~' — already a tilde
//   - '\\' — a TeX escape
//   - punctuation: '.', ',', ';', ':', '!', '?'
//   - '{' or '}' — brace group boundary
func shouldSkipPreceding(b byte) bool {
	switch b {
	case '(', '[', '~', '\\', '{', '}':
		return true
	case '.', ',', ';', ':', '!', '?':
		return true
	}
	return false
}

// inlineMathRanges returns byte ranges of inline math in src: $...$ and \(...\).
// Unlike excludedRanges (in rules_wrap.go), this does NOT include ref-like
// \command{...} groups, because the tilde rule specifically targets those.
func inlineMathRanges(src []byte) [][2]int {
	var ranges [][2]int

	inDollar := false
	dollarStart := 0
	for i := 0; i < len(src); i++ {
		c := src[i]
		if c == '\\' && i+1 < len(src) {
			next := src[i+1]
			if next == '(' {
				// Find matching \)
				j := i + 2
				for j+1 < len(src) && !(src[j] == '\\' && src[j+1] == ')') {
					j++
				}
				if j+1 < len(src) {
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
	return ranges
}

// isOnlyWhitespace reports whether b contains only spaces and tabs (or is empty).
func isOnlyWhitespace(b []byte) bool {
	for _, c := range b {
		if c != ' ' && c != '\t' {
			return false
		}
	}
	return true
}
