# mreview fmt — PDF-preserving LaTeX source normalizer

## Overview

- A new `mreview fmt` subcommand and `pkg/format/` package that rewrite a paper's `.tex` source so mreview's downstream block segmentation has a cleaner input, while the rendered PDF is preserved (Tier 1) or improved in known author-bug cases (Tier 2, off by default).
- Modeled on `gofmt` / `cargo fmt` ergonomics: source rewrites are git-visible, never a hidden side effect of opening the reviewer.
- A verifier rebuilds before/after PDFs and refuses to write when an unexpected text-layer diff appears. Text-layer diff is the default; pixel-level (`diff-pdf`) is opt-in and used by the goldens.
- PNAS test ground: `~/local_git/Schubert_simulations/PNAS/main_pnas.tex` (1391 lines) and `si_pnas.tex` (2276 lines).

## Context

### Existing pieces this plan builds on

- `pkg/parser/tokenizer.go` — already knows the skip-envs (`verbatim`, `verbatim*`, `lstlisting`, `comment`) and `%`-line handling. The format pass extracts that logic into a shared helper AND extends it with inline-`\verb` / `\lstinline` detection (not currently handled — `handleCommand` has no `verb` case).
- `pkg/parser/parse.go` — emits `*Document` with `ByID`, `ByLabel`, theorem-env discovery, ref resolution. Tier-3 diagnostics consume this index directly.
- `pkg/build/` — existing `latexmk` runner (`Run`, `RunWith`); reused by the verifier so the build invocation stays in one place.
- `pkg/synctex/` — load-bearing for the verifier's expected-diff whitelist (maps source lines to `(page, bbox)` and thence to `pdftotext` lines).
- `pkg/ui/` — `FilterIssues` exists for parser-level diagnostics (unresolved refs); Task 7 extends it to surface Tier-3 fmt-report diagnostics. Non-trivial UI change (~150 LOC).

### Module layout (additions)

```
cmd/mreview/fmt.go            # new subcommand
pkg/parser/spans.go           # new: ProtectedSpan, ProtectedSpans, LineOffsets
pkg/parser/spans_test.go
pkg/format/                   # new package
  types.go                    # Rule, Tier, Ctx, Hit, Diag
  registry.go                 # var Registry []Rule
  format.go                   # pipeline: apply registry to src
  rules_safe.go               # Tier 1 (4 rules)
  rules_pdf_fix.go            # Tier 2 (math.paragraph-suppress + helpers)
  rules_diag.go               # Tier 3 (10 diagnostics)
  verify.go                   # text-layer + paranoid verifier
  verify_test.go
  report.go                   # paper.tex.fmt-report.md writer
  format_test.go              # per-rule table-driven tests
  golden_test.go              # PNAS round-trip (build tag pdfverify)
testdata/pnas-fixture/        # frozen PNAS snapshot for goldens
```

### Real-world test target

- `~/local_git/Schubert_simulations/PNAS/main_pnas.tex` and `si_pnas.tex`. Both build with `latexmk` from `latexmkrc` in that directory. The fixture directory under `testdata/` is a frozen copy at the snapshot commit; the live tree is for ad-hoc dev.

### Target user / install

- Single user (LP). `make install` symlinks `mreview` into `~/bin` (per global instructions). Every task ends with `make install` so the running binary stays current.

## Solution Overview

The format pass has three rule tiers and a verifier. The pipeline always runs the verifier unless `--no-verify` is passed.

### Rule taxonomy

**Tier 1 — Safe (PDF byte-identical):**

| ID | What it does |
|---|---|
| `space.trailing` | Strip trailing whitespace per line |
| `space.blank-runs` | `\n{3,}` → `\n\n` (LaTeX treats any blank-line run as one paragraph break) |
| `space.tabs` | Tabs → 4 spaces outside protected regions |
| `display.style` | `$$…$$` → `\[…\]` |

**Tier 2 — PDF-fixing (off by default; opt in via `--pdf-fix` or `--rule=<id>`):**

- `env.spacing` — ensure exactly one blank line above `\begin{theorem|lemma|proposition|corollary|definition|conjecture|figure|abstract}` and above `\section{…}` / `\subsection{…}` / `\subsubsection{…}`. Insert if missing; do not modify if already present (collapsing is `space.blank-runs`'s job and is Tier-1 safe). PDF-changing in ambiguous cases — when the line above is non-sentence-final continuation prose, inserting a blank line introduces a `\par` and ends the continuation. Verifier whitelist is the line above and the line of the env itself. Use case: source readability + more deterministic prose segmentation around envs in `pkg/parser/segmentRootProse`.

- `math.paragraph-suppress` — detect display-math envs (`equation`, `align`, `gather`, `multline`, their starred forms, and `\[…\]`) bracketed by blank lines, and remove the blank lines so the surrounding prose joins one paragraph.

  Default-drop heuristic (the right default for math papers — most surrounding-by-blank-lines is unintentional):

  1. Identify a candidate region: `\n\n+` immediately above and immediately below a display-math env.
  2. **Strong paragraph signal — leave alone**: line above ends with `.` `?` `!` AND line below starts with an uppercase letter (true sentence boundary, true paragraph break).
  3. Otherwise: drop both blank-line runs.

  This is intentionally aggressive: in PNAS the lines above display math read *"we simply define"*, *"satisfies"*, *"the analogous permutation"* — none would match an "above" trigger list, but none are sentence-final either, so default-drop catches them. The strong-paragraph-leave-alone rule preserves the rare cases where a real paragraph break was intended.

  **Chains of equations** (`text \n\n eq1 \n\n eq2 \n\n text`) are common in proofs. The rule treats any consecutive run of (blank-lines, display-math, blank-lines, display-math, …, blank-lines) as one region and applies the heuristic at the outer boundaries only — the gaps between consecutive equations always collapse, since "two equations separated by a paragraph break" is virtually never what the author meant.

  Rewrite: collapse `\n\n+` to `\n` at each end of the region. No `%` injection.

**Tier 3 — Diagnostics (no rewrite; emitted to `paper.tex.fmt-report.md` and mreview's `issues` filter):**

Cross-references:
- `lint.ref-undefined` — `\ref{X}` with no matching `\label{X}`
- `lint.label-unused` — `\label{X}` referenced nowhere
- `lint.label-duplicate` — same `\label{X}` declared twice
- `lint.ref-should-eqref` — `\ref{X}` where X labels a `KindDisplay` block; suggest `\eqref`
- `lint.cite-undefined` — `\cite{X}` with X not in `.bbl`

Theorem ecosystem:
- `lint.thm-unlabeled` — `KindTheoremLike` block with no `\label`
- `lint.thm-orphan-proof` — `KindProof` not preceded by a theorem-like block
- `lint.thm-no-proof` — theorem stated, no proof block within next 5 outer-sibling blocks

Workflow:
- `lint.todo-marker` — author's TODO pattern. LP uses `\colorbox{<color>}{\parbox{<width>}{<comment>}}` as an inline annotation marker (the color and parbox width vary; the comment is the TODO body). Detection regex: any `\colorbox{...}` whose first argument-group is followed by a `\parbox{...}{...}` group. Report the `<comment>` as the TODO body. Do NOT match bare `<++>` placeholders — those are vim snippet artifacts that disappear after editing and are not real TODOs.
- `lint.block-too-long` — a `KindParagraph` block whose source span exceeds 40 lines

### Verifier

- **Isolated tempdir, never the original tree.** The user runs `lmkf` continuously against `paper.tex` for incremental builds; it owns `.aux`, `.fdb_latexmk`, `.fls`, `.synctex.gz` in the source directory. The verifier MUST work in `/tmp/mr-fmt-XXX/{before,after}/` — full copy of `paper.tex`, `latexmkrc`, `*.cls`, `*.sty`, `*.bib`/`*.bbl`, figures, and any `\input` children. No symlinks back to the source tree (would re-introduce the `lmkf` collision via aux files). Yes, this is more I/O; the machine is fast.
- **Default text-layer comparison.** Runs `latexmk` on before/after copies; runs `pdftotext` (default mode, NOT `-layout` — layout mode produces noisy column-spacing diffs on minor reflow); whitespace-normalizes each line (collapse runs of spaces, strip trailing) before diffing. Page-count diff via `pdfinfo` is a hard-fail precondition.
- **Source-line → PDF-text-line mapping via synctex.** Each `Hit` emitted by a rule carries `ExpectedDiffSourceLines` — source line numbers in the BEFORE source whose PDF rendering is allowed to differ. The harness uses `pkg/synctex/` to map each source line to its `(page, bbox)`, then to the corresponding line(s) in the `pdftotext` output for that page. `math.paragraph-suppress` declares: expected diff at the source line of the prose immediately following each rewritten display-math close (the line whose indentation status changes). Without synctex, the whitelist is meaningless — this dependency is load-bearing, not optional.
- **Tier-1 rules emit Hits with `ExpectedDiffSourceLines == nil`.** Harness expects byte-identical normalized `pdftotext` output. Any diff = bug, refuse the write.
- **No-op detection.** If a Tier-2 rule fires but the verifier sees zero PDF change in the rule's expected-diff region, surface a warning (`<rule-id> fired N hits but PDF was unchanged — heuristic may be too aggressive`). Rule still applies; warning lands in the report.
- **No caching.** Machine is fast; `latexmk -pdf` on a 1391-line PNAS paper is a few seconds. Caching the before.pdf is fragile (depends on every `.cls`/`.sty`/figure/`\input` child; key is hard to get right) and saves little. Skip it.
- **Paranoid mode** (`--verify-pdf=visual`, build tag `pdfverify`): adds `diff-pdf --output-diff=…` pixel diff on top of the text-layer check. Used by the PNAS golden test.
- **Hard requirements:** `latexmk`, `pdftotext`, `pdfinfo` on `$PATH`. `diff-pdf` only for paranoid mode.

### CLI surface

```
mreview fmt paper.tex                      # Tier 1, verify, write in place
                                           # refuses if `git status` shows paper.tex dirty
mreview fmt --allow-dirty paper.tex        # bypass the dirty-tree check
mreview fmt --diff paper.tex               # show unified diff to stdout, no write
mreview fmt --check paper.tex              # exit 1 if changes needed (CI / pre-commit)
mreview fmt --pdf-fix paper.tex            # Tier 1 + Tier 2, verify, write
mreview fmt --rule=math.paragraph-suppress paper.tex   # one rule only (repeatable)
mreview fmt --no-verify paper.tex          # skip rebuild (escape hatch)
mreview fmt --verify-pdf=visual paper.tex  # paranoid verification (build tag pdfverify)
mreview fmt --report paper.tex             # also write paper.tex.fmt-report.md
```

No `.bak` file. The user is in git; the safety net is `git diff` / `git checkout`. `mreview fmt` refuses to overwrite a dirty working tree by default — exit with a message pointing at `git status`. `--allow-dirty` overrides for the rare case (e.g. running after `lmkf` touched a generated file).

The default `mreview paper.tex` (review UI) is unchanged. Users opt into rewrites with `mreview fmt …` first.

`--diff` output uses `pmezard/go-difflib` for portable Go-native unified diff (no shelling to `diff(1)`).

## Technical Details

### `pkg/parser/spans.go` API

```go
type ProtectedSpan struct {
    Start, End int    // half-open byte offsets into src
    Kind       string // "verbatim" | "lstlisting" | "comment-env" | "verb-inline" | "comment-line"
}

func ProtectedSpans(src []byte) []ProtectedSpan
func LineOffsets(src []byte) []int  // start byte of each 1-based line; len == numLines+1 (sentinel)
func OverlapsProtected(start, end int, spans []ProtectedSpan) bool
```

Reuses the same `skipEnvs` set already in `tokenizer.go` (`verbatim`, `verbatim*`, `lstlisting`, `comment`) plus `%`-comment-line handling. **New capability** added in this task (not currently in the tokenizer): inline `\verb<delim>…<delim>` and `\verb*<delim>…<delim>` regions, and `\lstinline<delim>…<delim>`. The current `handleCommand` has no `verb` case — these need to be added (in spans.go for the protected-region purpose; the tokenizer itself can keep ignoring them since they don't carry tokens of interest).

`spans.go` and the existing `Tokenize` share a small private helper for the skip-env / comment scan; no public API change to existing exports.

### `pkg/format/types.go`

```go
type Tier int
const (
    Safe Tier = iota
    PDFFix
    Diag
)

type Rule struct {
    ID    string
    Tier  Tier
    Doc   string
    Apply func(*Ctx) Result
    // No per-rule Verifier callback — each Hit carries its own
    // ExpectedDiffSourceLines, which the harness translates to PDF lines via synctex.
}

type Ctx struct {
    Src       []byte
    Tokens    []parser.Token
    Doc       *parser.Document   // nil for early Tier-1 passes that run before Parse
    Protected []parser.ProtectedSpan
    Lines     []int              // line-start offsets
}

type Result struct {
    Src   []byte    // possibly rewritten
    Hits  []Hit     // per-rewrite metadata; whitelist input
    Diags []Diag    // Tier-3 only; ignored for Safe/PDFFix
}

type Hit struct {
    RuleID                  string
    Line                    int     // 1-based source line of the rewrite, in the BEFORE source
    ExpectedDiffSourceLines []int   // source lines whose PDF rendering legitimately changes; nil for Tier-1
    Excerpt                 string  // ≤80 chars
}

type Diag struct {
    RuleID  string
    Line    int
    Message string
}
```

### Pipeline order

1. Read `src`. Compute `Tokens`, `Protected`, `Lines` once.
2. Parse `*Document` (needed for Tier-3 cross-ref diagnostics).
3. Apply enabled rules in registry order. Each rule returns a possibly-rewritten `[]byte`. **Stale-state rule:** if a rule changed any newline (any rule except `space.trailing` and `space.tabs`), the harness recomputes `Tokens`, `Protected`, `Lines` *before* running the next rule. This costs another tokenize pass per multi-line rule (cheap; tokenizer is linear) and removes the foot-gun of rules consuming stale line numbers. The `Doc` is recomputed once before Tier 2 (after all Tier-1 rules) since Tier 2 reasons about envs and may need updated boundaries.
4. If verifier enabled and any Hits exist: copy build inputs to `/tmp/mr-fmt-XXX/{before,after}/`, build both PDFs there, run verifier check, refuse on regression.
5. Write `paper.tex` directly (no `.bak`; git is the undo). Optionally write `paper.tex.fmt-report.md`.

### Report file format

```markdown
# mreview fmt report — paper.tex
date: 2026-04-24T15:32:11Z
tier: safe+pdf-fix
verify: text-layer (ok)

## Rewrites (12)
- space.trailing — 14 hits (lines 12, 88, 134, …)
- space.blank-runs — 4 hits (collapsed L221, L408, …)
- math.paragraph-suppress — 11 hits (L308, L330, L417, …)

## Verifier warnings (1)
- math.paragraph-suppress hit at L1188 produced no PDF change — heuristic may be too aggressive

## Diagnostics (Tier 3, 7 issues)
- lint.label-unused — `eq:tilde-w-extra` declared at L612, never referenced.
- lint.thm-no-proof — Theorem 4.2 at L451 has no following proof in next 5 blocks.
- ...
```

mreview's `issues` filter loads `paper.tex.fmt-report.md` (if present) and surfaces each diagnostic as an issue annotation on the corresponding block.

## Development Approach

- Each task ends with `make install` so the running binary in `~/bin` matches the source.
- Each task ends with `go test ./...` (and `make lint` if golangci-lint passes the existing baseline).
- Per-rule tests use small inline snippets — no fixture files required for unit-level rule testing. PNAS goldens are separate.
- **Stage 1 (Tasks 1–5)**: tokenizer prep + Tier-1 safe rules + CLI + verifier + Tier-2 rules (`math.paragraph-suppress`, `env.spacing`). Ships the user's pet peeve fix in the first stage — Stage 1 has visible PDF improvements, not just cosmetic source rewrites.
- **Stage 2 (Tasks 6–7)**: Tier-3 diagnostics + report file + mreview UI integration.
- **Stage 3 (Tasks 8–9)**: paranoid verification mode + PNAS goldens.
- **Stage 4 (Task 10)**: acceptance walkthrough on PNAS + README updates.
- The PNAS source tree is the dev playground; `testdata/pnas-fixture/` is the frozen golden, bumped intentionally when rules change.

## Testing Strategy

### Per-rule unit tests

Table-driven, in `pkg/format/format_test.go`. Each row is `(input snippet, expected snippet, expected hits)`. Tier-1 rules ship with at least 5 rows including: nominal, no-op, inside-verbatim (no rewrite), inside-`\verb|…|` (no rewrite), inside `% comment` (no rewrite).

`math.paragraph-suppress` rule ships with at least 12 rows covering all heuristic branches plus the explicit not-paragraph case (drop blanks) and the explicit paragraph case (leave alone).

### PNAS phases

**Phase A — Tier-1 baseline.** `mreview fmt main_pnas.tex` and `si_pnas.tex`. Verifier (text-layer) must be ok. Manually inspect `git diff` on the source.

**Phase B — Tier-2 dry run.** `mreview fmt --pdf-fix --diff main_pnas.tex`. Inspect proposed rewrites. Pre-scan suggests at least 11 `math.paragraph-suppress` hits in `main_pnas.tex` at L308, L330, L417, L421, L434, L471, L610, L899, L1086, L1188, L1343 (display math after continuation prose). Actual count will likely be higher under the new default-drop heuristic, since it triggers in the absence of a strong-paragraph signal rather than requiring a positive continuation match. Use Phase B to calibrate: if the count is wildly higher than 11 and a manual scan shows many false-positives, tighten the strong-paragraph rule before Phase C.

**Phase C — Tier-2 commit.** `mreview fmt --pdf-fix --report main_pnas.tex`. Verifier (text-layer) must be ok with all unexpected-diff regions whitelisted by the rule. Open the resulting PDF, eyeball three rewrite sites, confirm continuation indent is gone.

**Phase D — Goldens (build tag `pdfverify`, paranoid).** `go test -tags=pdfverify ./pkg/format -run TestGoldenPNAS`:
1. Loads `testdata/pnas-fixture/main_pnas.tex` (frozen input).
2. Runs the pipeline.
3. Diffs against `testdata/pnas-fixture/main_pnas.expected.tex` byte-for-byte.
4. Builds before/after PDFs; runs `diff-pdf` (pixel-level) plus `pdftotext` (text-level); both must pass against frozen `main_pnas.expected.txt`.

### Paranoid mode for tests

The `pdfverify` build tag enables Phase D goldens (slow, requires latex toolchain + diff-pdf). CI without latex installed runs Phases A–C as plain Go tests via mocked verifier; the `pdfverify` tag is local-only.

## Implementation Steps

### Task 1: Skeleton — `pkg/parser/spans.go` + `pkg/format/` scaffold

- [x] Create `pkg/parser/spans.go`. Reuse the existing `skipEnvs` map and `%`-comment logic from `tokenizer.go` (factor a small private helper if needed). **Add new code** for inline `\verb<delim>…<delim>`, `\verb*<delim>…<delim>`, `\lstinline<delim>…<delim>` — these are NOT currently in the tokenizer; `handleCommand` has no `verb` case. The new detection lives in spans.go and is independent of `Tokenize`.
- [x] Public API: `ProtectedSpans(src) []ProtectedSpan`, `LineOffsets(src) []int`, `OverlapsProtected(s, e, spans) bool`.
- [x] Tests in `spans_test.go`: nominal text (no protected spans), nested `verbatim`, `\verb|…|` with various delimiters (`|`, `+`, `!`), `\verb*+x+`, `\lstinline{x}`, `% line comment`, `\begin{lstlisting}`, `\begin{comment}`, mixed.
- [x] Verify existing parser tests still pass — Task 1 must not regress `pkg/parser/`.
- [x] Create `pkg/format/{types.go, registry.go, format.go}` with empty `Registry`, the types from "Technical Details" above, and a no-op `Apply(ctx) Result`.
- [x] `go test ./...` passes. `make install` runs cleanly (no behavior change in the binary; install just keeps `~/bin/mreview` current).

### Task 2: Tier-1 safe rules

- [x] Implement `space.trailing`, `space.blank-runs`, `space.tabs`, `display.style` in `rules_safe.go`. All consult `ctx.Protected` before any rewrite.
- [x] `format_test.go` table-driven tests, ≥5 rows per rule including the four protected-span "no-rewrite" cases (inside `verbatim`, inside `\verb|…|`, inside `% comment`, inside `lstlisting`).
- [x] Register all four rules in `Registry` with `Tier: Safe`.
- [x] Verify the pipeline's stale-state recompute fires after rules that change newlines — add a pipeline test that runs `space.blank-runs` before `display.style` on input with line shifts and confirms `display.style` sees correct line numbers.

### Task 3: CLI subcommand `mreview fmt`

- [x] `cmd/mreview/fmt.go` defines the subcommand parser using the same `jessevdk/go-flags` library as the main CLI. Flags: `--diff`, `--check`, `--pdf-fix`, `--rule=<id>` (repeatable), `--no-verify`, `--verify-pdf=visual`, `--report`, `--allow-dirty`.
- [x] `cmd/mreview/main.go` dispatches when `os.Args[1] == "fmt"`. Other args fall through to the existing review-UI path.
- [x] For Task 3, only `--diff`, `--check`, `--rule=`, and the dirty-tree precondition are functional; `--no-verify` is a no-op (verifier not built yet — Task 4); writes go straight through with no PDF check, behind a temporary stderr warning so a user doesn't think verification ran.
- [x] Add `--diff` formatting via `pmezard/go-difflib`.
- [x] Manual end-to-end: `mreview fmt --diff testdata/sample.tex` prints a unified diff. `mreview fmt testdata/sample.tex` writes the rewritten file (after dirty-tree check).

### Task 4: Verifier — text-layer (default)

- [x] `verify.go`: `Verify(buildInputs Tree, beforeSrc, afterSrc []byte, hits []Hit, rules []Rule, syncMap *synctex.Index) (ok bool, unexpected []Diff, warnings []string, err error)`.
- [x] **Tempdir copy.** `Tree` describes the build inputs (`paper.tex`, `latexmkrc`, `*.cls`, `*.sty`, `*.bib`, `*.bbl`, figures, `\input` children — discovered by walking from `paper.tex`). Verifier copies the full set into `/tmp/mr-fmt-XXX/before/` and `/tmp/mr-fmt-XXX/after/`. **No symlinks** — `lmkf` runs against the original tree and shares aux files, which would collide. Document the tempdir lifecycle: kept until next run; `mreview fmt --clean-tempdir` removes them.
- [x] **Build.** Reuse `pkg/build.RunWith(opts)` against each tempdir copy. Hard-fail if `latexmk` is missing.
- [x] **Compare.** `pdftotext` (default mode, NOT `-layout`) on each PDF. Whitespace-normalize each line (strip trailing, collapse internal runs of `\s+` to single space). Diff line-by-line. Page-count via `pdfinfo` is a precondition: page-count mismatch is hard-fail without consulting any whitelist.
- [x] **Whitelist via synctex.** Load `before.synctex.gz` via `pkg/synctex`. For each `Hit{RuleID, Line}`, look up `(page, bbox)` of that source line; identify the corresponding line(s) in the `pdftotext` output for that page (by Y-coordinate ordering). Diffs that fall on whitelisted PDF lines are tolerated; diffs anywhere else cause refusal.
- [x] **Tier-1 rules**: emit Hits with `ExpectedDiffSourceLines == nil`. No diffs allowed at all; whitespace-normalized `pdftotext` must be identical.
- [x] **No-op detection.** After applying the whitelist, scan whitelisted regions: if a Tier-2 rule's expected-diff region shows zero diff, append a warning (`<rule-id> hit at L<n> produced no PDF change`) to `warnings`. Rule still applies; warning lands in the report.
- [x] **No caching** — see Verifier section. Always rebuild before/after. Machine is fast.
- [x] `cmd/mreview/fmt.go`: wire verifier into the write path. `--no-verify` skips it. On regression, do not write; print unexpected diffs and warnings to stderr; exit 1. Leave tempdir intact for inspection.
- [x] `verify_test.go`: golden round-trip on `testdata/sample.tex` with all Tier-1 rules.

### Task 5: Tier-2 rules (closes Stage 1)

Two rules, registered together:

**`math.paragraph-suppress`:**
- [x] `rules_pdf_fix.go`: implement per the default-drop heuristic in "Solution Overview". Region detection includes chains-of-equations.
- [x] Each emitted `Hit` populates `ExpectedDiffSourceLines` with the source line of the prose immediately following the rewritten display-math close. Harness translates via synctex.
- [x] `format_test.go`: ≥12 table rows covering: continuation above (drop), continuation below (drop), default case with neither strong signal (drop — the new aggressive default), strong paragraph signal (leave alone), chain of two equations (collapse all gaps), chain of three equations (collapse all gaps), trailing display followed by section header (leave alone), `\[…\]` form, starred envs, `align*`, inside protected span (no rewrite), zero-blank-line input (no-op).

**`env.spacing`:**
- [x] Walk tokens for `TokBeginEnv` matching the env list (`theorem|lemma|proposition|corollary|definition|conjecture|figure|abstract`) and `TokSection` for section-like commands. For each, check the source bytes between the previous non-blank line and the env's start line: if zero blank lines, insert one. If ≥1 blank line, leave alone (no collapsing here — `space.blank-runs` already collapsed).
- [x] Each emitted `Hit` populates `ExpectedDiffSourceLines` with `[N-1, N]` where `N` is the env's source line. The line above (whose paragraph may now end) and the env line (whose vertical spacing may change) are both legitimately different in the PDF.
- [x] `format_test.go`: ≥8 table rows: insertion needed before `theorem`, `figure`, `section`, `subsection`; insertion not needed (already had blank); env in a protected span (no-op); env at start of file (no-op, no line above); chain of consecutive theorem envs.

**Both rules:**
- [x] Register with `Tier: PDFFix`. CLI flags `--pdf-fix` and `--rule=<id>` enable them.
- [x] Run Phase A, B, C on `main_pnas.tex` manually. Record the rewrite counts and verifier surprises in `docs/plans/2026-04-24-format-pass.notes.md` (sibling file, NOT this plan body — this plan moves to `completed/` after Task 10 and shouldn't carry execution scribbles).

### Task 6: Tier-3 diagnostics

- [x] `rules_diag.go`: implement the 10 diagnostics. They run during the format pipeline but only emit `Diags`; never rewrite.
- [x] Cross-ref diagnostics consume `ctx.Doc.ByLabel`, `ctx.Doc.ByID`, and `ctx.Doc.BibEntries` directly — no new index machinery.
- [x] `lint.thm-no-proof` walks the parser's outer-sibling chain.
- [x] `lint.todo-marker`: regex pass over `src` (consulting `ctx.Protected`) for `\\colorbox\{[^}]+\}\{\\parbox\{[^}]+\}\{...\}\}`. Match nested-brace–aware (use spans.go helpers). Extract the `<comment>` body for the diag message. Tokenizer extension is unnecessary — pure-regex over byte-protected regions.
- [x] Per-rule tests; cross-check on `main_pnas.tex` to confirm the diagnostic counts are reasonable (no thousands of false positives).

### Task 7: Report file + mreview `issues` filter integration

- [x] `pkg/format/report.go`: write `paper.tex.fmt-report.md` when `--report` is set. Include rewrites grouped by rule, diagnostics grouped by rule, verifier warnings (e.g. no-op rule firings).
- [x] `pkg/format/report.go`: also expose `LoadReport(path string) (*Report, error)` for the UI side.
- [x] `pkg/ui/` integration is non-trivial (the existing `FilterIssues` surfaces parser-level unresolved-ref diagnostics built into the block tree, per `outline_test.go:147`):
  - [x] Add a `ExternalIssues map[blockID][]Diag` field to the UI's outline state.
  - [x] On model init: if `<paper>.tex.fmt-report.md` exists alongside `<paper>.tex`, call `format.LoadReport`. Map each `Diag.Line` → owning block via the parser's line-to-block index (or build one if not present).
  - [x] Extend `BuildOutline` (or wrap it) so `FilterIssues` surfaces both built-in and external diagnostics. Add a glyph distinguishing them (e.g. `⚠` for built-in, `🔧` for fmt-report) — confirm with LP whether to differentiate visually.
  - [x] Add tests in `pkg/ui/outline_test.go` paralleling `TestBuildOutline_IssuesFilter_SurfacesUnresolvedRefs`.
- [x] Budget for UI integration: ~150 LOC in `pkg/ui/` plus tests.

### Task 8: Paranoid verifier mode

- [ ] `verify.go` (build tag `pdfverify`): page-count check via `pdfinfo`; pixel diff via `diff-pdf --output-diff=…`. Page-count mismatch is hard-fail. Pixel diff produces a diff PDF saved to `/tmp/mr-fmt-XXX/diff.pdf`.
- [ ] `--verify-pdf=visual` exposes the paranoid path on the CLI.
- [ ] Document `diff-pdf` install instructions in README (brew/apt).

### Task 9: PNAS goldens

- [ ] `testdata/pnas-fixture/main_pnas.tex` (frozen input copy, plus `latexmkrc`, `.cls`, `.sty`, `.bbl`, figures — minimal set needed to build).
- [ ] `testdata/pnas-fixture/main_pnas.expected.tex` (frozen Tier-1+Tier-2 output).
- [ ] `testdata/pnas-fixture/main_pnas.expected.txt` (frozen whitespace-normalized `pdftotext` (default mode) output of the expected PDF).
- [ ] `golden_test.go` under build tag `pdfverify`: runs the pipeline, diffs source byte-for-byte against expected, then runs paranoid verifier against the frozen text.
- [ ] Bump procedure (when a rule changes intentionally): regenerate fixtures via a helper `make pnas-fixture` that re-runs the pipeline + freezes the output. Document in `docs/plans/2026-04-24-format-pass.notes.md` (sibling notes file), not this plan body.

### Task 10: Acceptance + documentation

- [ ] Run Phases A → D on `main_pnas.tex` and `si_pnas.tex`. Phase D may require a second fixture for SI.
- [ ] Update `README.md`: add "Source normalization" section with CLI examples and the rule-tier table.
- [ ] `make install`. Manual smoke: `mreview fmt --pdf-fix --report ~/local_git/Schubert_simulations/PNAS/main_pnas.tex` produces a clean diff and a non-empty report.
- [ ] Move this plan to `docs/plans/completed/`.

## Known issues / open questions (deferred, not blocking)

These were flagged during plan review but are not gating Task 1. Address as they arise during implementation; if a real fix needs more than a few lines, propose an amendment.

- **Multi-file `\input` projects.** The current parser is single-file (per the MVP plan). The PNAS test ground has `main_pnas.tex` and `si_pnas.tex` as siblings, not as `\input` children, so the format pass works on each in turn. Authors who split chapters via `\input{ch1}` will not be served until parser supports multi-file. Out of scope for v0; raise when first user hits it.
- **PNAS fixture size in git.** Full source tree (`.cls`, `.sty`, `.bbl`, figures) is several hundred KB. Decision deferred to Task 9: either commit verbatim under `testdata/pnas-fixture/`, or ship a `go:generate` script that copies from `~/local_git/Schubert_simulations/PNAS/` at fixture-bump time.
- **`pkg/ui/` glyph for fmt-report diagnostics.** Whether to visually distinguish built-in parser issues from fmt-report issues. Confirm with LP during Task 7.
- **`mreview fmt --check` in CI.** No CI configured for mreview today. The plan supports the workflow (`--check` returns exit 1 on changes-needed) but no `.github/workflows/` is wired up — out of scope.

## Out of Scope (future work)

The "build & diff intelligence" pillar — flagged as the highest-value follow-on after the format pass lands — gets its own plan:

- Watch daemon (`mreview --watch`): sub-second incremental `latexmk` on save; PDF pane updates without re-cropping.
- Cross-version review (`mreview --against=v1 paper.tex`): annotations migrate forward, changed blocks highlighted, "what's new in v2" filter.
- Visual PDF diff for compile-to-compile changes (region-level, not pixel-blob).
- Git integration (`mreview --since=HEAD~5`): review only blocks changed in the last N commits.

These are independent of the format pass and can ship in any order after Stage 1 of this plan lands.

## Post-completion (manual / not executor-automatable)

- Add `latexmk`, `pdftotext`, `diff-pdf` install notes to README's prerequisites section.
- Optional: bump version in `cmd/mreview/main.go` once `mreview fmt` ships.
