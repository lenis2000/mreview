// Package format implements mreview's LaTeX source normalizer ("mreview fmt").
// It applies a pipeline of rules to the source, optionally verifying that the
// rendered PDF is preserved, and writes the result back.
package format

import "mreview/pkg/parser"

// Tier classifies a formatting rule by its impact on the rendered PDF.
type Tier int

const (
	// Safe rules produce byte-identical PDFs (e.g. trailing whitespace removal).
	Safe Tier = iota
	// PDFFix rules intentionally change the PDF to fix known author bugs
	// (e.g. spurious paragraph breaks around display math). Off by default.
	PDFFix
	// DiagOnly rules emit diagnostics only; they never rewrite the source.
	DiagOnly
)

// String returns a short label for a Tier.
func (t Tier) String() string {
	switch t {
	case Safe:
		return "safe"
	case PDFFix:
		return "pdf-fix"
	case DiagOnly:
		return "diag"
	}
	return "unknown"
}

// Rule is a single formatting/diagnostic rule registered in the pipeline.
type Rule struct {
	ID    string
	Tier  Tier
	Doc   string
	Apply func(*Ctx) Result
}

// Ctx is the input context for a rule's Apply function. Rules read Src and
// the precomputed indices; they return a possibly-rewritten Src in Result.
type Ctx struct {
	Src       []byte
	Tokens    []parser.Token
	Doc       *parser.Document // nil for early Tier-1 passes that run before Parse
	Protected []parser.ProtectedSpan
	Lines     []int // line-start byte offsets (from parser.LineOffsets)
}

// Result is the output of a rule's Apply function.
type Result struct {
	Src   []byte // possibly rewritten source
	Hits  []Hit  // per-rewrite metadata (Tier 1/2); verifier whitelist input
	Diags []Diag // Tier-3 only; ignored for Safe/PDFFix
}

// Hit records a single rewrite site for the verifier.
type Hit struct {
	RuleID                  string
	Line                    int   // 1-based source line of the rewrite, in the BEFORE source
	ExpectedDiffSourceLines []int // source lines whose PDF rendering legitimately changes; nil for Tier-1
	Excerpt                 string
}

// Diag records a diagnostic (Tier-3 only) — no source rewrite, just a message.
type Diag struct {
	RuleID  string
	Line    int
	Message string
}
