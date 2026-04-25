package format

import (
	"bytes"
	"regexp"

	"mreview/pkg/parser"
)

func init() {
	Registry = append(Registry,
		Rule{
			ID:   "space.trailing",
			Tier: Safe,
			Doc:  "Strip trailing whitespace per line.",
			Apply: func(ctx *Ctx) Result {
				return applyTrailing(ctx)
			},
		},
		Rule{
			ID:   "space.blank-runs",
			Tier: Safe,
			Doc:  "Collapse runs of 3+ consecutive blank lines to 2 (one blank line).",
			Apply: func(ctx *Ctx) Result {
				return applyBlankRuns(ctx)
			},
		},
		Rule{
			ID:   "space.tabs",
			Tier: Safe,
			Doc:  "Replace tabs with 4 spaces outside protected regions.",
			Apply: func(ctx *Ctx) Result {
				return applyTabs(ctx)
			},
		},
		Rule{
			ID:   "display.style",
			Tier: Safe,
			Doc:  "Replace $$...$$ with \\[...\\].",
			Apply: func(ctx *Ctx) Result {
				return applyDisplayStyle(ctx)
			},
		},
	)
}

// applyTrailing strips trailing whitespace (spaces and tabs) from each line,
// skipping lines that fall entirely within a protected region.
func applyTrailing(ctx *Ctx) Result {
	var hits []Hit
	var out []byte

	lines := bytes.Split(ctx.Src, []byte{'\n'})
	offset := 0
	changed := false

	for i, line := range lines {
		lineStart := offset
		lineEnd := lineStart + len(line)

		trimmed := bytes.TrimRight(line, " \t")
		// Only check whether the trailing whitespace region itself is protected.
		trailStart := lineStart + len(trimmed)
		if len(trimmed) < len(line) && !parser.OverlapsProtected(trailStart, lineEnd, ctx.Protected) {
			out = append(out, trimmed...)
			changed = true
			hits = append(hits, Hit{
				RuleID:  "space.trailing",
				Line:    i + 1,
				Excerpt: truncExcerpt(string(line)),
			})
		} else {
			out = append(out, line...)
		}

		// Add newline separator between lines (not after the last line).
		if i < len(lines)-1 {
			out = append(out, '\n')
		}
		offset = lineEnd + 1 // +1 for the newline consumed by Split
	}

	if !changed {
		return Result{Src: ctx.Src}
	}
	return Result{Src: out, Hits: hits}
}

// blankRunRe matches runs of 3+ consecutive newlines (\n\n\n+).
// LaTeX treats any blank-line run as one paragraph break, so collapsing
// \n{3,} to \n\n is safe.
var blankRunRe = regexp.MustCompile(`\n{3,}`)

// applyBlankRuns collapses runs of 3+ consecutive newlines to exactly 2.
func applyBlankRuns(ctx *Ctx) Result {
	var hits []Hit

	locs := blankRunRe.FindAllIndex(ctx.Src, -1)
	if len(locs) == 0 {
		return Result{Src: ctx.Src}
	}

	var out []byte
	prev := 0
	for _, loc := range locs {
		start, end := loc[0], loc[1]
		// Check if the entire blank-line run overlaps a protected region.
		if parser.OverlapsProtected(start, end, ctx.Protected) {
			out = append(out, ctx.Src[prev:end]...)
			prev = end
			continue
		}
		out = append(out, ctx.Src[prev:start]...)
		out = append(out, '\n', '\n')
		lineNum := lineAt(ctx.Lines, start)
		hits = append(hits, Hit{
			RuleID:  "space.blank-runs",
			Line:    lineNum,
			Excerpt: "collapsed blank-line run",
		})
		prev = end
	}
	out = append(out, ctx.Src[prev:]...)

	if len(hits) == 0 {
		return Result{Src: ctx.Src}
	}
	return Result{Src: out, Hits: hits}
}

// applyTabs replaces tab characters with 4 spaces, skipping protected regions.
func applyTabs(ctx *Ctx) Result {
	if !bytes.ContainsRune(ctx.Src, '\t') {
		return Result{Src: ctx.Src}
	}

	var hits []Hit
	out := make([]byte, 0, len(ctx.Src))

	for i := 0; i < len(ctx.Src); i++ {
		if ctx.Src[i] == '\t' && !parser.OverlapsProtected(i, i+1, ctx.Protected) {
			out = append(out, "    "...)
			lineNum := lineAt(ctx.Lines, i)
			hits = append(hits, Hit{
				RuleID:  "space.tabs",
				Line:    lineNum,
				Excerpt: "tab replaced",
			})
		} else {
			out = append(out, ctx.Src[i])
		}
	}

	if len(hits) == 0 {
		return Result{Src: ctx.Src}
	}
	return Result{Src: out, Hits: dedupeTabHits(hits)}
}

// applyDisplayStyle replaces $$...$$ with \[...\] outside protected regions.
func applyDisplayStyle(ctx *Ctx) Result {
	src := ctx.Src
	var hits []Hit
	var out []byte
	prev := 0
	changed := false

	for i := 0; i < len(src)-1; i++ {
		if src[i] != '$' || src[i+1] != '$' {
			continue
		}
		// Found '$$' at position i. Check if inside a protected region.
		if parser.OverlapsProtected(i, i+2, ctx.Protected) {
			i++ // skip the second $
			continue
		}

		// Find the matching closing '$$'.
		closePos := findClosingDollarDollar(src, i+2, ctx.Protected)
		if closePos < 0 {
			i++ // no match; skip
			continue
		}

		// Extract the content between $$...$$.
		content := src[i+2 : closePos]

		out = append(out, src[prev:i]...)
		out = append(out, `\[`...)
		out = append(out, content...)
		out = append(out, `\]`...)
		prev = closePos + 2
		changed = true

		lineNum := lineAt(ctx.Lines, i)
		hits = append(hits, Hit{
			RuleID:  "display.style",
			Line:    lineNum,
			Excerpt: truncExcerpt("$$" + string(content)),
		})

		i = closePos + 1 // advance past the closing $$
	}

	if !changed {
		return Result{Src: ctx.Src}
	}
	out = append(out, src[prev:]...)
	return Result{Src: out, Hits: hits}
}

// findClosingDollarDollar finds the position of the next '$$' after pos,
// skipping protected regions. Returns -1 if not found.
func findClosingDollarDollar(src []byte, start int, protected []parser.ProtectedSpan) int {
	for i := start; i < len(src)-1; i++ {
		if src[i] == '$' && src[i+1] == '$' {
			if !parser.OverlapsProtected(i, i+2, protected) {
				return i
			}
			i++ // skip
		}
	}
	return -1
}

// lineAt returns the 1-based line number for the given byte offset using
// the precomputed line-start offsets.
func lineAt(lines []int, offset int) int {
	// lines[0] = 0 (sentinel), lines[1] = start of line 1, etc.
	// Binary search for the last entry <= offset.
	lo, hi := 1, len(lines)-1
	for lo <= hi {
		mid := (lo + hi) / 2
		if lines[mid] <= offset {
			lo = mid + 1
		} else {
			hi = mid - 1
		}
	}
	return hi
}

// truncExcerpt truncates s to at most 80 characters.
func truncExcerpt(s string) string {
	if len(s) <= 80 {
		return s
	}
	return s[:77] + "..."
}

// dedupeTabHits collapses consecutive tab hits on the same line into one.
func dedupeTabHits(hits []Hit) []Hit {
	if len(hits) == 0 {
		return hits
	}
	out := []Hit{hits[0]}
	for _, h := range hits[1:] {
		if h.Line == out[len(out)-1].Line {
			continue
		}
		out = append(out, h)
	}
	return out
}
