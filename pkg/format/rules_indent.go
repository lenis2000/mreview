package format

import (
	"bytes"
	"strings"

	"mreview/pkg/parser"
)

// IndentOptions controls the space.indent rule.
type IndentOptions struct {
	// Enabled gates the whole pass; when false, applyIndent is a no-op.
	Enabled bool
	// UseTab selects '\t' as the per-level indent character; otherwise ' '.
	UseTab bool
	// Size is the number of chars emitted per nesting level. Default 1 for
	// tab, 2 for space (resolved by the caller).
	Size int
	// ExtraNoIndentEnvs adds caller-supplied environments to the no-indent
	// list (in addition to the built-in defaults — currently `document`).
	ExtraNoIndentEnvs []string
}

// noIndentEnvs are environments whose contents are NOT indented relative to
// the surrounding scope. `document` is the universal one; nothing else is
// hardcoded. Tier-2 / user lists can extend this via IndentOptions.
var noIndentEnvs = map[string]bool{
	"document": true,
}

// listIndentEnvs are environments where a `\item` line dedents one level
// relative to the item body. (We do not currently distinguish list items
// at the indentation level — `\item foo` lives at the same depth as the
// surrounding env body. This list is reserved for future tuning.)
var listIndentEnvs = map[string]bool{
	"itemize":     true,
	"enumerate":   true,
	"description": true,
}

func registerIndentRule() {
	Registry = append(Registry, Rule{
		ID:    "space.indent",
		Tier:  Safe,
		Doc:   "Re-indent lines based on environment nesting depth.",
		Apply: applyIndent,
	})
}

func applyIndent(ctx *Ctx) Result {
	if !ctx.Indent.Enabled {
		return Result{Src: ctx.Src}
	}

	// Count real content lines. parser.LineOffsets returns one entry per
	// actual line PLUS a trailing entry just past the last \n; so
	// len(Lines)-2 is the real line count when src ends with \n, and
	// len(Lines)-1 when it doesn't.
	nLines := bytes.Count(ctx.Src, []byte{'\n'})
	if len(ctx.Src) > 0 && ctx.Src[len(ctx.Src)-1] != '\n' {
		nLines++
	}
	if nLines <= 0 {
		return Result{Src: ctx.Src}
	}
	beginCount := make([]int, nLines+2)
	endCount := make([]int, nLines+2)

	noIndentExtra := make(map[string]bool, len(ctx.Indent.ExtraNoIndentEnvs))
	for _, e := range ctx.Indent.ExtraNoIndentEnvs {
		noIndentExtra[e] = true
	}
	noIndent := func(env string) bool {
		return noIndentEnvs[env] || noIndentExtra[env]
	}

	for _, tk := range ctx.Tokens {
		if tk.Line < 1 || tk.Line > nLines {
			continue
		}
		switch tk.Kind {
		case parser.TokBeginEnv:
			if noIndent(tk.EnvName) {
				continue
			}
			off := tokenByteOffset(ctx.Lines, tk)
			if off < 0 || parser.OverlapsProtected(off, off+1, ctx.Protected) {
				continue
			}
			beginCount[tk.Line]++
		case parser.TokEndEnv:
			if noIndent(tk.EnvName) {
				continue
			}
			off := tokenByteOffset(ctx.Lines, tk)
			if off < 0 || parser.OverlapsProtected(off, off+1, ctx.Protected) {
				continue
			}
			endCount[tk.Line]++
		}
	}

	indentChar := byte(' ')
	if ctx.Indent.UseTab {
		indentChar = '\t'
	}
	size := ctx.Indent.Size
	if size <= 0 {
		if ctx.Indent.UseTab {
			size = 1
		} else {
			size = 2
		}
	}

	var out bytes.Buffer
	out.Grow(len(ctx.Src))
	actual := 0
	changed := false
	var hits []Hit

	for line := 1; line <= nLines; line++ {
		body := lineBytes(ctx, line)

		// Honour skip masks and protected lines: emit verbatim, do not
		// reindent. The token-level scan above already excluded any
		// protected/masked tokens from depth, so depth math stays
		// consistent.
		if ctx.LineSkipped(line) || lineWhollyProtected(ctx, line) {
			out.Write(body)
		} else {
			// Effective depth for this line: dedent for \end markers on
			// this line, but never below zero.
			depth := actual - endCount[line]
			if depth < 0 {
				depth = 0
			}
			leadLen, allWS := leadingWS(body)
			if allWS {
				// Blank line: preserve as-is (don't synthesise a phantom
				// indent on empty lines — common house style).
				out.Write(body)
			} else {
				want := strings.Repeat(string(indentChar), depth*size)
				if string(body[:leadLen]) == want {
					out.Write(body)
				} else {
					out.WriteString(want)
					out.Write(body[leadLen:])
					changed = true
					hits = append(hits, Hit{
						RuleID:  "space.indent",
						Line:    line,
						Excerpt: truncExcerpt(string(body[leadLen:])),
					})
				}
			}
		}

		actual += beginCount[line] - endCount[line]
		if actual < 0 {
			actual = 0
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

// lineBytes returns the bytes of 1-based line L without the trailing newline.
func lineBytes(ctx *Ctx, line int) []byte {
	if line < 1 || line >= len(ctx.Lines) {
		return nil
	}
	start := ctx.Lines[line]
	if start > len(ctx.Src) {
		return nil
	}
	var end int
	if line+1 < len(ctx.Lines) {
		end = ctx.Lines[line+1]
		if end > 0 && end <= len(ctx.Src) && ctx.Src[end-1] == '\n' {
			end-- // strip trailing \n on full lines
		}
	} else {
		end = len(ctx.Src)
		if end > 0 && ctx.Src[end-1] == '\n' {
			end--
		}
	}
	if end < start {
		end = start
	}
	return ctx.Src[start:end]
}

// leadingWS returns the byte length of the leading whitespace prefix and
// whether the entire line is whitespace.
func leadingWS(line []byte) (int, bool) {
	i := 0
	for ; i < len(line); i++ {
		c := line[i]
		if c != ' ' && c != '\t' {
			return i, false
		}
	}
	return i, true
}

// lineWhollyProtected reports whether every byte of line lies inside one of
// ctx.Protected (verbatim/listing body, comment env). Comment-line spans
// only cover the comment portion, so a line with code-then-comment is NOT
// wholly protected and is still re-indented.
func lineWhollyProtected(ctx *Ctx, line int) bool {
	if line < 1 || line >= len(ctx.Lines) {
		return false
	}
	start := ctx.Lines[line]
	var end int
	if line+1 < len(ctx.Lines) {
		end = ctx.Lines[line+1]
		if end > 0 && end <= len(ctx.Src) && ctx.Src[end-1] == '\n' {
			end--
		}
	} else {
		end = len(ctx.Src)
	}
	if end <= start {
		return false
	}
	for _, sp := range ctx.Protected {
		if sp.Start <= start && end <= sp.End {
			if sp.Kind == "comment-line" {
				return false
			}
			return true
		}
	}
	return false
}

func endsWithNewline(src []byte) bool {
	return len(src) > 0 && src[len(src)-1] == '\n'
}
