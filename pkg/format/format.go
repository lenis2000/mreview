package format

import (
	"bytes"

	"mreview/pkg/parser"
)

// Options controls which rules the pipeline runs.
type Options struct {
	// PDFFix enables Tier-2 (PDF-fixing) rules in addition to Tier-1.
	PDFFix bool
	// DiagOnly enables Tier-3 diagnostics (no rewrites).
	Diag bool
	// Rules, if non-empty, restricts the run to only these rule IDs.
	Rules []string
}

// PipelineResult holds the output of a full Apply run.
type PipelineResult struct {
	Src   []byte // final (possibly rewritten) source
	Hits  []Hit
	Diags []Diag
}

// Apply runs the enabled rules from Registry against src and returns the
// (possibly rewritten) source together with all hits and diagnostics.
//
// The pipeline recomputes token/span/line indices after any rule that changes
// the source bytes (Protected spans and Lines are byte-offset-based, so any
// byte-level change — even one that preserves newline count — invalidates them).
// The Document (ctx.Doc) is parsed once before the first non-Safe rule that
// needs it (Tier-2 and Tier-3 rules reason about envs, labels, and refs).
func Apply(src []byte, opts Options) PipelineResult {
	var allHits []Hit
	var allDiags []Diag

	ctx := newCtx(src)

	enabled := enabledRules(opts)
	for _, rule := range enabled {
		// Parse the Document before the first rule that needs it.
		if rule.Tier >= PDFFix && ctx.Doc == nil {
			doc, _ := parser.Parse(ctx.Src)
			ctx.Doc = doc
		}

		result := rule.Apply(ctx)

		allHits = append(allHits, result.Hits...)
		allDiags = append(allDiags, result.Diags...)

		if !bytes.Equal(result.Src, ctx.Src) {
			nlBefore := bytes.Count(ctx.Src, []byte{'\n'})
			nlAfter := bytes.Count(result.Src, []byte{'\n'})
			ctx.Src = result.Src
			// Always reindex: Protected spans and Lines are byte-offset-based,
			// so any source change (even tab→spaces with same newline count)
			// invalidates them.
			reindex(ctx)
			if nlBefore != nlAfter {
				// Invalidate Doc so it gets re-parsed with correct
				// line numbers before the next tier that needs it.
				ctx.Doc = nil
			}
		}
	}

	return PipelineResult{
		Src:   ctx.Src,
		Hits:  allHits,
		Diags: allDiags,
	}
}

// newCtx builds a fresh Ctx from source bytes.
func newCtx(src []byte) *Ctx {
	return &Ctx{
		Src:       src,
		Tokens:    parser.Tokenize(src),
		Protected: parser.ProtectedSpans(src),
		Lines:     parser.LineOffsets(src),
	}
}

// reindex recomputes the mutable indices on ctx after a source change.
func reindex(ctx *Ctx) {
	ctx.Tokens = parser.Tokenize(ctx.Src)
	ctx.Protected = parser.ProtectedSpans(ctx.Src)
	ctx.Lines = parser.LineOffsets(ctx.Src)
}

// enabledRules filters Registry according to opts.
func enabledRules(opts Options) []Rule {
	ruleSet := make(map[string]bool, len(opts.Rules))
	for _, id := range opts.Rules {
		ruleSet[id] = true
	}

	var out []Rule
	for _, r := range Registry {
		// If explicit rule list given, filter to it.
		if len(ruleSet) > 0 {
			if !ruleSet[r.ID] {
				continue
			}
		} else {
			// Default filtering by tier.
			switch r.Tier {
			case Safe:
				// Always enabled.
			case PDFFix:
				if !opts.PDFFix {
					continue
				}
			case DiagOnly:
				if !opts.Diag {
					continue
				}
			}
		}
		out = append(out, r)
	}
	return out
}
