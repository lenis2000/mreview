# mreview fmt — roadmap: ergonomics, per-env, range, tilde, math-aware

## Status (2026-04-26)

Updated after the config-first ergonomics cut and the paragraph-aware wrap fix.

**Already shipped — landed since this plan was first drafted:**

- **Config-first ergonomics.** Persistent fmt settings moved out of the
  CLI and into `[fmt]` of `~/.config/mreview/config.toml` /
  `.mreview.toml`. Removed `--no-pdf-fix`, `--no-indent`, `--verify-pdf`,
  `--wrap` from `mreview fmt`. Kept `--no-verify` and `--no-report` as
  one-off escape hatches. The remaining CLI surface is per-invocation
  modes (`--diff`, `--print`, `--check`, `--rule`) plus workflow
  (`--allow-dirty`, `--clean-tempdir`, `--config`, `--noconfig`).
- **`mreview config` subcommand.** Opens the user-global config in
  `$VISUAL`/`$EDITOR`/`vim`. Auto-creates the file from a defaults
  template on first run.
- **`config.example.toml`** at the repo root, linked from README.
- **Paragraph-aware wrap reflow.** `space.wrap` now groups contiguous
  prose lines into paragraphs, joins them on single spaces, then
  sentence-splits + column-wraps the joined string. Fixes the
  "already-hand-wrapped at 80 → resplit into a four-line mess" bug.
  Lines with trailing inline comments and structural commands
  (`\begin`, `\end`, `\section`, `\item`, display fences, `\\`) are
  paragraph terminators and emitted as-is.
- **Default `wrap_col` lowered to 80** (from 100). Configurable via
  `[fmt] wrap_col` only — no CLI flag, per the config-first cut.

**Still pending — this plan covers the rest:**

- Phase 1: `--stdin`, `--fail-on-change`, `--summary`, pre-commit /
  GitHub Actions templates.
- Phase 2: per-env `[fmt.indent_rules]` overrides; `prose.tilde-refs`
  Tier-2 rule.
- Phase 3: `--lines=START:END` range formatting.
- Phase 4: math-aware printing — `math.align-columns`,
  `math.continuation-indent`, `math.wrap-at-break-op`.

## Overview

Four phases that extend `mreview fmt` along the axes called out in the v0.2
review: editor/CI ergonomics, per-environment indent control, range
formatting, an academic-prose tilde rule, and finally math-aware
pretty printing for align/array/tabular and multi-line equations.

The CLI cut shipped before this plan resumes makes Phase 1 cheaper: any
new flag must justify itself as a per-invocation mode (output-shape,
range, scan-only) rather than a persistent toggle. All three Phase-1
flags pass that test (`--stdin` is stream-mode, `--fail-on-change` is a
CI exit-code mode, `--summary` is a read-only scan).

Phases ship low-risk-first so the verifier stress-tests the small
changes before the math rules land. The non-obvious sequencing claims:

- **Per-env indent overrides ship before math.** Math rules need a
  user-controllable opt-out per env (`tikzpicture`, `cd`, custom listings).
  Building that knob first means the math sub-rules drop in with a single
  config check, not a retrofit.
- **`--lines=START:END` ships before math, not after.** Range mode
  requires changes to how `format.Apply` results are committed (per-line
  replay vs. whole-file overwrite). Doing that refactor *before* adding
  ~600 LOC of new rules keeps every math rule range-aware out of the
  box.

## Context

### Existing pieces this plan builds on

- `cmd/mreview/fmt.go` — go-flags CLI; per-file loop already supports
  multi-file. New flags slot in here; the per-file loop is the natural
  seam for `--summary`, `--lines`, `--stdin`. Helper `resolveBool`
  (CLI ⊳ config ⊳ default) is the layering primitive.
- `cmd/mreview/config.go` — `mreview config` subcommand and the
  default-template content. Template extends naturally to include any
  new `[fmt]` keys we add (Phase 2's `[fmt.indent_rules]`, Phase 4's
  `math_*` knobs).
- `pkg/format/format.go` — `Apply(src, opts) PipelineResult`. Pure
  function: bytes in, bytes + Hits + Diags out. No file I/O, no path
  awareness. Trivially callable from `--stdin` and from `--lines`'s
  whole-file pass.
- `pkg/format/types.go` — `Ctx` carries `Indent IndentOptions` and
  `Wrap WrapOptions`. New `Indent.Rules map[string]string` and the
  range-clipping mask plug into the same struct.
- `pkg/format/registry.go` — ordered rule list. `prose.tilde-refs`,
  `math.align-columns`, `math.continuation-indent`,
  `math.wrap-at-break-op` register in the same place; pipeline guarantees
  reindex-on-mutation already covers them.
- `pkg/format/rules_wrap.go` — paragraph-aware reflow. `excludedRanges()`
  understands inline math and ref-like commands; reused by
  `prose.tilde-refs` to find `\cite/\ref/\eqref/...` call sites without
  re-implementing the scan. The new line-classifier
  (`classifyLineForWrap`) is also reusable for any future paragraph-
  oriented rule.
- `pkg/format/rules_indent.go` — current indent rule produces a flat
  per-env-depth indent. Per-env override threads through here as a
  per-env multiplier/replacement keyed on the active env-stack.
- `pkg/parser/spans.go` — `ProtectedSpans` + `LineOffsets`. Both already
  used by every rule. Range mode reuses `LineOffsets` for the clip mask.
- `pkg/ui/config.go` — `FmtConfig` is the single source of truth for
  `[fmt]` defaults. After the config-first cut it already carries
  `NoPDFFix *bool`, `VerifyPDF`, `NoVerify *bool`, `NoReport *bool`,
  `VerbatimEnvs`, `Indent *bool`, `IndentChar`, `IndentSize`, `Wrap`,
  `WrapCol`. New fields below extend the same pattern.
- Verifier — `pkg/format/verify.go` declares expected-diff regions per
  Hit. Tier-2 additions (`prose.tilde-refs`, math rules that change
  layout) carry expected-diff lines; Tier-1 (alignment, continuation
  indent) declare none and rely on PDF-byte safety.

### Module layout (additions)

```
cmd/mreview/fmt.go               # extended: --stdin, --fail-on-change, --summary, --lines
cmd/mreview/config.go            # extended: defaultGlobalConfig grows new [fmt] keys
pkg/format/rules_tilde.go        # new: prose.tilde-refs (Tier 2)
pkg/format/rules_tilde_test.go
pkg/format/rules_math_align.go   # new: math.align-columns (Tier 1)
pkg/format/rules_math_align_test.go
pkg/format/rules_math_cont.go    # new: math.continuation-indent (Tier 1)
pkg/format/rules_math_cont_test.go
pkg/format/rules_math_wrap.go    # new: math.wrap-at-break-op (Tier 1, opt-in)
pkg/format/rules_math_wrap_test.go
pkg/format/range.go              # new: --lines clip-mask + per-line replay
pkg/format/range_test.go
pkg/format/rules_indent.go       # extended: per-env override lookup
pkg/ui/config.go                 # extended: FmtConfig.IndentRules, MathWrapCol, …
config.example.toml              # extended: documents new [fmt.*] keys
templates/pre-commit-hooks.yaml  # new: pre-commit framework hook entry
templates/github-actions/        # new: drop-in workflow + composite action
  mreview-fmt.yml
docs/install/pre-commit.md       # new: copy-paste setup instructions
README.md                        # extended: new flags + per-env config
```

### Real-world test target

PNAS papers under `~/local_git/Schubert_simulations/PNAS/` — same fixtures
used by the format-pass plan. Each phase adds new golden assertions; math
rules require a new fixture with hand-crafted misaligned `align`/`tabular`
to exercise the columnizer.

## Solution Overview

Four phases. Each is one PR with `make install` + a README diff at the end.

### Phase 1 — CLI ergonomics (~150 LOC + ~30 lines YAML)

Three new flags + two template files. No new rules. No `pkg/format`
changes beyond calling `format.Apply` on `os.Stdin` for the `--stdin`
path; the per-file loop is already in place from the multi-file change.

- `--stdin` — read source from stdin, write formatted source to stdout.
  Implies `--no-verify`, `--no-report`, no dirty-tree check, no path arg.
  Mutually exclusive with file args, `--diff`, `--check`, `--print`,
  `--fail-on-change`, `--summary`. Errors land on stderr with the literal
  label `<stdin>` so users can grep for it.
- `--fail-on-change` — write the file in place AND exit 1 when any rule
  fired. Distinct from `--check` (which never writes). Allowed with
  multi-file (no stdout interleaving issue). Mutually exclusive with
  `--check`, `--diff`, `--print`, `--stdin`, `--summary`.
- `--summary` — read-only scan: do not write files, do not write reports,
  print `mreview fmt: N rewrites across M files (K with diagnostics)` to
  stderr, exit 0. Implies `--no-verify` for speed. Allowed with multi-file
  (multi is the use case). Mutually exclusive with `--diff`, `--print`,
  `--check`, `--fail-on-change`, `--stdin`, `--lines`.
- `templates/pre-commit-hooks.yaml` — pre-commit framework hook config
  (declares the `mreview-fmt` hook with `entry: mreview fmt
  --fail-on-change --no-verify`). Users add `repos: [...]` pointing at the
  mreview repo.
- `templates/github-actions/mreview-fmt.yml` — GH Actions workflow that
  installs Go, builds mreview, runs `mreview fmt --check --no-verify
  $(git diff --name-only origin/main... | grep '\.tex$')` on changed
  files. Composite action variant for reuse across repos.
- `docs/install/pre-commit.md` — copy-paste section: how to wire either
  template. README links to it.

#### Why each is a CLI flag, not config

These all describe per-invocation operations, which is the criterion
applied in the config-first cut: stream mode, exit-code-on-change mode,
read-only scan mode. Persistent behaviour (verifier strictness, wrap
mode, indent style) stays in `[fmt]`.

### Phase 2 — per-env indent overrides + tilde-before-refs (~250 LOC)

#### Per-env indent overrides

Two-layer config:

```toml
[fmt]
indent_char = "tab"          # global default (existing)
indent_size = 1              # existing

[fmt.indent_rules]
tikzpicture = "  "           # 2-space indent inside this env's body
"my-listing" = ""            # no indent for body
cd          = ""             # tikz-cd: leave alone (alignment matters)
align       = "tab"          # explicit (would be the default; doc-by-example)
```

- `IndentRules map[string]string` on `FmtConfig`. Empty string = "no
  indent for this env". Non-empty string = literal indent unit per nesting
  level inside that env.
- Threaded through `IndentOptions.Rules`. `rules_indent.go` consults
  `Rules[currentEnvName]` first, falls back to global `Size`/`UseTab`.
- Tier-1 (still pure whitespace from TeX's POV).

#### `prose.tilde-refs` (Tier 2)

Insert `~` between a word character and `\cite/\ref/\eqref/cref/Cref/autoref/nameref` when the
preceding char is a regular space. Conservative: do not insert when

- preceding char is `(`, `[`, `~`, `\`, start-of-line, or punctuation
- already inside a protected span (verbatim, lstlisting, comment)
- inside excluded ranges (inline math `$...$`, `\(...\)`)

Example rewrites:

```
Theorem 1.2 \cite{Foo}     -> Theorem 1.2~\cite{Foo}
see \eqref{eq:1} for       -> see~\eqref{eq:1} for
(see \cite{Foo})           -> unchanged   # opening paren protects
\cite{Foo}                 -> unchanged   # start-of-line
~\cite{Foo}                -> unchanged   # already non-breaking
```

Cite-like command set (matches `refLikeCmds` in `rules_wrap.go` minus
`url`/`href`/`label`): `cite`, `citep`, `citet`, `ref`, `eqref`, `cref`,
`Cref`, `autoref`, `nameref`. Configurable via
`[fmt] tilde_refs = ["cite", ...]` (omit field = use defaults).

Tier-2 rationale: PDF *layout* can change (`~` forbids the line break the
space allowed). Verifier whitelist: every line where the rule fires.
Verifier mode `text` is the right setting; visual mode flags this as a
real reflow change which is exactly what we want — but the diff is
in-rule-region so it's accepted.

### Phase 3 — `--lines=START:END` (~250 LOC)

Range formatting via per-line replay, not slice-then-format.

```
mreview fmt --lines=42:120 paper.tex
```

Algorithm:

1. Read original source.
2. Run `format.Apply(src, opts)` over the **whole file** so every rule
   has full context. Capture `Hits` with their BEFORE line numbers.
3. Build a clip mask: `mask[line] = true` for `START <= line <= END`.
4. Walk lines of original source. For each line N:
   - If `mask[N]`: emit the formatted line (looked up by Hit replay,
     see below).
   - Else: emit the original line verbatim.
5. Write/diff/print as usual.

Catch: line-count-changing rules (`space.blank-runs`, `space.wrap`,
`math.paragraph-suppress`) break the per-line correspondence. Mitigation:

- `--lines` forces these rules off for v1 (validated at CLI parse:
  `wrap = off`, `space.blank-runs` and `math.paragraph-suppress` skipped
  via internal flag passed through `Options.LineRange`).
- The forced-off rules still appear in the final report with a note
  `skipped under --lines`.

Hit-replay machinery (~80 LOC in `pkg/format/range.go`): extends `Hit`
with optional `BeforeRange [2]int` (byte offsets into BEFORE src) and
`AfterText string`. Range mode walks Hits, applies AfterText only when
`BeforeRange` falls inside `[START, END]` line bounds. Outside-range
Hits are dropped from the rewrite (reported in the summary as
`skipped (out of range)`).

CLI integration: `--lines=START:END` is mutually exclusive with
multi-file, `--check` (no clean semantics for "would change in this
range"), `--summary`, `--fail-on-change`. Allowed with `--stdin` (range
refers to the input stream's lines), `--diff`, `--print`, default
in-place write.

### Phase 4 — math-aware printing (2 PRs, ~600 LOC)

Three sub-rules, all Tier-1 (whitespace-only). Default-on except
`math.wrap-at-break-op` (opt-in via `--rule=math.wrap-at-break-op` or
config).

PR-A ships `math.align-columns` + `math.continuation-indent` together
since they share the env-body parser. PR-B ships `math.wrap-at-break-op`.

#### `math.align-columns` (~300 LOC)

Aligns `&` columns inside:

```
align align* alignat alignat* aligned
array
matrix pmatrix bmatrix vmatrix Vmatrix Bmatrix
cases
tabular tabular* tabularx
```

Algorithm:

1. For each matching env occurrence (use `parser.Doc.Envs`):
   a. Extract body bytes (between `\begin{E}...` and `\end{E}`).
   b. Tokenize into rows by `\\` at depth 0 (braces and protected spans
      respect depth).
   c. Tokenize each row into cells by `&` at depth 0.
   d. **Refuse to align** when:
      - rows have unequal cell counts (likely `\multicolumn`)
      - any row contains a `%` line comment (kills the rebuild contract)
      - body contains nested `\begin{}` of another aligned env (recurse
        first; outer is then a single non-aligned cell)
   e. Compute `colWidth[j] = max(visualWidth(cell[i][j]))` for each j.
   f. Right-pad each cell to `colWidth[j]` with spaces. Preserve a
      single space between cells and the `&`.
   g. Emit. Re-attach `\\` and trailing whitespace.
2. Skip if env is inside a protected span or skip-directive region.

Verifier: Tier-1 contract holds — `&` and inter-cell whitespace inside
math/tabular envs are non-significant in PDF output. Pixel-identical.

Config:

```toml
[fmt]
math_align = true                                # default true
math_align_envs = ["align", ...]                 # override the default set
math_align_skip_envs = ["my-custom-grid"]        # carve-outs
```

Refusal cases (unequal rows, comments) emit a Tier-3-style note in the
report so the user knows alignment was skipped.

#### `math.continuation-indent` (~150 LOC)

Inside `equation`, `equation*`, `gather`, `gather*`, `multline`,
`multline*`, `align`, `align*` (only when not aligned by columns above —
i.e. equations split across explicit `\\` with continuation lines that
START with a binary operator):

1. Find the column of the first `=`/`\equiv`/`:=`/`\le`/`\ge` on the
   FIRST physical row of the equation.
2. For each subsequent row whose first non-whitespace token is a binop
   (`+`, `-`, `*`, `=`, `\le`, `\ge`, `\leq`, `\geq`, `\cdot`,
   `\times`, `\to`, `\implies`, `<`, `>`, `\sim`, `\approx`):
   - Pad leading whitespace so the binop sits one column past the anchor.
3. Skip when no anchor found.

Tier-1: pure leading whitespace inside math env body.

#### `math.wrap-at-break-op` (~150 LOC, opt-in)

For inline display equations (`\[...\]`, `equation`, `equation*`) whose
content exceeds `[fmt] math_wrap_col` columns:

1. Split at the rightmost break operator within budget (`+`, `-`, `=`,
   `\le`, `\cdot`).
2. Move the operator to the START of the new line.
3. Apply `math.continuation-indent` to align it.

Off by default (`math_wrap = false`). Enable via flag or config.

## Technical Details

### Current CLI surface for `mreview fmt` (post-config-first cut)

```
--diff           # show unified diff to stdout, do not write
--print, -p      # print formatted source to stdout, do not write
--check          # exit 1 if changes needed (CI / pre-commit)
--rule=ID        # restrict to these rule IDs (repeatable)
--allow-dirty    # skip dirty-tree check before writing
--no-verify      # one-off: skip PDF verification
--no-report      # one-off: do not write paper.tex.fmt-report.md
--clean-tempdir  # remove all mr-fmt-* verification tempdirs
--config=PATH    # use a specific config file
--noconfig       # ignore config files; built-in defaults only
```

Everything else (PDF-fix on/off, verifier mode, indent style, wrap mode,
wrap column, custom verbatim envs) is in `[fmt]` of the config.

### CLI grammar additions (`cmd/mreview/fmt.go`)

```go
type fmtOpts struct {
    // ...existing...
    Stdin        bool   `long:"stdin" description:"read source from stdin, write formatted to stdout"`
    FailOnChange bool   `long:"fail-on-change" description:"format in place AND exit 1 when changed (CI/pre-commit)"`
    Summary      bool   `long:"summary" description:"scan only; print N rewrites across M files to stderr"`
    Lines        string `long:"lines" description:"format only lines START:END (1-based, inclusive)"`
}
```

Validation pre-loop:

- `--stdin` + any of {file args, `--check`, `--diff`, `--print`,
  `--fail-on-change`, `--summary`} → exit 2.
- `--lines` requires exactly one file (or `--stdin`).
- `--lines` + `--check` → exit 2.
- `--lines` + `--summary` → exit 2.
- `--lines` + `--fail-on-change` → exit 2.
- `--summary` is mutually exclusive with `--diff`, `--print`, `--check`,
  `--fail-on-change`, `--stdin`, `--lines`.
- `--fail-on-change` + `--check`/`--diff`/`--print`/`--stdin`/`--summary`
  → exit 2.

### `FmtConfig` additions (`pkg/ui/config.go`)

```go
type FmtConfig struct {
    // ...existing...
    IndentRules   map[string]string `toml:"indent_rules"`
    TildeRefs     []string          `toml:"tilde_refs"`     // override default cite-cmd set
    MathAlign     *bool             `toml:"math_align"`
    MathAlignEnvs []string          `toml:"math_align_envs"`
    MathAlignSkip []string          `toml:"math_align_skip_envs"`
    MathWrap      *bool             `toml:"math_wrap"`
    MathWrapCol   int               `toml:"math_wrap_col"`
}
```

`mergeFmtConfig`: maps merge per-key (overlay wins per key, base keys
preserved). Slices replace wholesale (matches existing pattern).

### `defaultGlobalConfig` template (`cmd/mreview/config.go`)

Each new `[fmt]` key documented in the template Phase-by-Phase, all
commented out with the built-in default value as a literal. So when LP
runs `mreview config` after each phase ships, the freshly-created file
already documents the new knobs.

### `IndentOptions` extension (`pkg/format/types.go`)

```go
type IndentOptions struct {
    Enabled bool
    UseTab  bool
    Size    int
    Rules   map[string]string  // env name → indent unit (literal); "" = no indent
}
```

`rules_indent.go`: when computing the indent string for nesting depth N
inside env stack `[E1, E2, ..., En]`, walk inside-out and consult
`Rules[Ei]` for the closest match; first hit wins. Default if no match:
the global unit.

### `Hit` extension (`pkg/format/types.go`)

```go
type Hit struct {
    RuleID                  string
    Line                    int
    ExpectedDiffSourceLines []int
    Excerpt                 string
    BeforeRange             [2]int  // NEW: byte offsets in BEFORE src; [0,0] when unset
    AfterText               string  // NEW: replacement text for BeforeRange; "" when unset
}
```

Existing rules can keep emitting the legacy fields only; the range-replay
path falls back to "whole-file rewrite" when `BeforeRange == [0,0]`,
which is what we want for `--lines` v1 anyway (the line-count-changing
rules that don't carry BeforeRange get force-disabled under `--lines`).

Range-aware rules (the math sub-rules and `prose.tilde-refs`) populate
`BeforeRange`/`AfterText` from day one so they work under `--lines`
without a follow-up.

### Templates

`templates/pre-commit-hooks.yaml`:

```yaml
- id: mreview-fmt
  name: mreview fmt
  description: PDF-preserving LaTeX source normalizer
  entry: mreview fmt --fail-on-change --no-verify
  language: system
  files: \.tex$
  pass_filenames: true
```

`templates/github-actions/mreview-fmt.yml`:

```yaml
name: mreview fmt
on: [pull_request]
jobs:
  fmt:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
        with: { fetch-depth: 0 }
      - uses: actions/setup-go@v5
        with: { go-version: '1.21' }
      - run: go install github.com/<owner>/mreview/cmd/mreview@latest
      - run: |
          changed=$(git diff --name-only origin/${{ github.base_ref }}... | grep '\.tex$' || true)
          if [ -n "$changed" ]; then
            mreview fmt --check --no-verify $changed
          fi
```

## Testing

Per phase:

- Unit tests in `pkg/format/*_test.go` for each new rule (table-driven,
  per current convention).
- CLI tests in `cmd/mreview/fmt_test.go` covering each new flag's happy
  path and mutual-exclusion errors.
- Golden integration: PNAS round-trip with verifier asserts each phase's
  rules don't break PDF byte-identity (Tier-1) or expected-diff-only
  (Tier-2).
- Phase 4 needs a new fixture: hand-crafted misaligned `align` and
  `tabular` blocks to validate the columnizer; same-PDF assertion before
  vs after.

Each phase ends with `make install` so the running binary stays current.

## Phasing / Tasks

Legend: ☑︎ done, ☐ pending.

### Phase 0 — Config-first cut (already shipped)

- ☑︎ Drop persistent-setting CLI flags; keep only per-invocation modes
- ☑︎ `mreview config` subcommand opens global config in `$EDITOR`,
  auto-creates from default template
- ☑︎ `config.example.toml` at repo root
- ☑︎ README updated for new CLI surface
- ☑︎ Paragraph-aware `space.wrap` reflow (rejoin already-wrapped paragraphs)
- ☑︎ Default `wrap_col = 80`

### Phase 1 — CLI ergonomics

- ☐ `--stdin`: parse, route src through `format.Apply`, write to
      stdout, mutual-exclusion checks
- ☐ `--fail-on-change`: in-place write + exit 1 when `len(Hits) > 0`
- ☐ `--summary`: per-file Hit/Diag accumulator, single stderr line
- ☐ `templates/pre-commit-hooks.yaml`
- ☐ `templates/github-actions/mreview-fmt.yml`
- ☐ `docs/install/pre-commit.md` + README link
- ☐ Tests: per-flag happy path, multi-file behaviour, mutual-exclusion
- ☐ `make install`

### Phase 2 — per-env + tilde

- ☐ `FmtConfig.IndentRules` + merge + plumb through `IndentOptions`
- ☐ `defaultGlobalConfig` documents `[fmt.indent_rules]`
- ☐ `rules_indent.go`: env-stack lookup against `IndentRules`
- ☐ `rules_tilde.go`: `prose.tilde-refs` rule + `excludedRanges` reuse
- ☐ Configurable cite-cmd set via `FmtConfig.TildeRefs`
- ☐ Tier-2 verifier whitelist for tilde Hits
- ☐ Tests: indent override per env, tilde insert/skip cases
- ☐ `make install`

### Phase 3 — --lines=START:END

- ☐ `Hit.BeforeRange` + `Hit.AfterText`
- ☐ `pkg/format/range.go`: clip mask + per-line replay
- ☐ `Options.LineRange` plumbed through to rules that need to
      self-disable (`space.blank-runs`, `space.wrap`,
      `math.paragraph-suppress`)
- ☐ `--lines` flag + parse + mutual-exclusion checks
- ☐ Report note: rules skipped under `--lines`
- ☐ Tests: range mode preserves out-of-range bytes byte-identically;
      in-range rewrites apply; force-off rules report skip
- ☐ `make install`

### Phase 4 — math-aware (PR-A)

- ☐ `rules_math_align.go`: `math.align-columns`
- ☐ `rules_math_cont.go`: `math.continuation-indent`
- ☐ Env-body parser shared between the two (rows by `\\`, cells by
      `&`, depth/protected aware)
- ☐ `FmtConfig.MathAlign`, `MathAlignEnvs`, `MathAlignSkip`
- ☐ `defaultGlobalConfig` documents the math knobs
- ☐ Refusal-case reporting (unequal rows, comments)
- ☐ PNAS golden: pixel-identical PDF
- ☐ New fixture with misaligned `align`/`tabular` for golden
- ☐ `make install`

### Phase 4 — math-aware (PR-B)

- ☐ `rules_math_wrap.go`: `math.wrap-at-break-op` (opt-in)
- ☐ `FmtConfig.MathWrap`, `MathWrapCol`
- ☐ Tests: wrap fires only on overflow; respects break-op set;
      preserves PDF
- ☐ `make install`

## Out of scope

- LaTeX `\begin{cd}` (tikz-cd commutative diagrams) — alignment there
  is layout-significant; default to skip via `MathAlignSkip`.
- `latexindent`-style indent rules with regex matching. The map-keyed
  per-env override is enough for v1.
- Range mode for `space.wrap`, `space.blank-runs`,
  `math.paragraph-suppress` — force-off under `--lines` v1; revisit if
  there's demand.
- Footnote-aware wrapping. Per LP: wrapping before `\footnote` corrupts
  the typeset output; do not attempt.
- New persistent-setting CLI flags. The config-first cut is intentional;
  any new toggle that's not a per-invocation mode goes in `[fmt]`.
