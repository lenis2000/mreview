# mreview fmt — ergonomics, per-env indent, range, tilde, math-aware

## Overview

Extend `mreview fmt` with four phases of functionality: CLI ergonomics (--stdin, --fail-on-change, --summary, CI templates), per-environment indent overrides + tilde-before-refs rule, --lines=START:END range formatting, and math-aware pretty printing (align-columns, continuation-indent, wrap-at-break-op).

## Context

- Files involved: `cmd/mreview/fmt.go`, `cmd/mreview/config.go`, `pkg/format/format.go`, `pkg/format/types.go`, `pkg/format/registry.go`, `pkg/format/rules_wrap.go`, `pkg/format/rules_indent.go`, `pkg/parser/spans.go`, `pkg/ui/config.go`, `pkg/format/verify.go`, `config.example.toml`, `README.md`
- Related patterns: existing rule registration in `pkg/format/registry.go`, table-driven tests in `pkg/format/*_test.go`, config layering via `resolveBool` in `cmd/mreview/fmt.go`, `FmtConfig` in `pkg/ui/config.go`
- Dependencies: none new (existing Go toolchain, latexmk, pdftotext, pdfinfo)

## Development Approach

- **Testing approach**: Regular (code first, then tests)
- Complete each task fully before moving to the next
- Each task ends with `make install` so the running binary stays current
- Each task ends with `go test ./...` and `make lint`
- Per-rule tests use small inline snippets (table-driven, per current convention)
- PNAS papers under `~/local_git/Schubert_simulations/PNAS/` are the real-world test target
- **CRITICAL: every task MUST include new/updated tests**
- **CRITICAL: all tests must pass before starting next task**

## Implementation Steps

### Task 1: --stdin flag

**Files:**
- Modify: `cmd/mreview/fmt.go`

- [x] add `Stdin bool` field to `fmtOpts` struct with `long:"stdin" description:"read source from stdin, write formatted to stdout"`
- [x] implement stdin read path: read all of os.Stdin, run `format.Apply`, write to stdout
- [x] stdin implies --no-verify, --no-report, no dirty-tree check, no path arg
- [x] mutual exclusion: --stdin + {file args, --check, --diff, --print, --fail-on-change, --summary} -> exit 2
- [x] errors go to stderr with label `<stdin>`
- [x] write tests in `cmd/mreview/fmt_test.go` for stdin happy path and mutual-exclusion errors
- [x] run `go test ./... && make install`

### Task 2: --fail-on-change flag

**Files:**
- Modify: `cmd/mreview/fmt.go`

- [x] add `FailOnChange bool` field to `fmtOpts` with `long:"fail-on-change" description:"format in place AND exit 1 when changed (CI/pre-commit)"`
- [x] implement: write file in place, exit 1 when `len(Hits) > 0`
- [x] allowed with multi-file; mutually exclusive with --check, --diff, --print, --stdin, --summary
- [x] write tests: happy path (no changes -> exit 0, changes -> exit 1), multi-file, mutual-exclusion
- [x] run `go test ./... && make install`

### Task 3: --summary flag

**Files:**
- Modify: `cmd/mreview/fmt.go`

- [ ] add `Summary bool` field to `fmtOpts` with `long:"summary" description:"scan only; print N rewrites across M files to stderr"`
- [ ] implement: per-file Hit/Diag accumulator, single stderr line `mreview fmt: N rewrites across M files (K with diagnostics)`, exit 0
- [ ] implies --no-verify; allowed with multi-file
- [ ] mutually exclusive with --diff, --print, --check, --fail-on-change, --stdin, --lines
- [ ] write tests: summary output format, multi-file accumulation, mutual-exclusion
- [ ] run `go test ./... && make install`

### Task 4: CI templates and documentation

**Files:**
- Create: `templates/pre-commit-hooks.yaml`
- Create: `templates/github-actions/mreview-fmt.yml`
- Create: `docs/install/pre-commit.md`
- Modify: `README.md`

- [ ] create pre-commit hook config declaring `mreview-fmt` hook with `entry: mreview fmt --fail-on-change --no-verify`
- [ ] create GH Actions workflow: install Go, build mreview, run `mreview fmt --check --no-verify` on changed .tex files
- [ ] write copy-paste setup instructions in docs/install/pre-commit.md
- [ ] add link from README to pre-commit docs
- [ ] no code tests needed; verify YAML is valid
- [ ] run `make install`

### Task 5: Per-env indent overrides

**Files:**
- Modify: `pkg/ui/config.go`
- Modify: `pkg/format/types.go`
- Modify: `pkg/format/rules_indent.go`
- Modify: `cmd/mreview/config.go`
- Modify: `config.example.toml`

- [ ] add `IndentRules map[string]string` to `FmtConfig`; empty string = no indent, non-empty = literal indent unit
- [ ] thread through `IndentOptions.Rules` in types.go
- [ ] in rules_indent.go: walk env-stack inside-out, consult `Rules[envName]` first, fall back to global
- [ ] update `mergeFmtConfig` for map merging (overlay wins per key, base keys preserved)
- [ ] update `defaultGlobalConfig` template to document `[fmt.indent_rules]`
- [ ] update config.example.toml with indent_rules examples
- [ ] write tests: indent override per env (tikzpicture 2-space, cd no-indent, default fallback), nested envs
- [ ] run `go test ./... && make install`

### Task 6: prose.tilde-refs rule

**Files:**
- Create: `pkg/format/rules_tilde.go`
- Create: `pkg/format/rules_tilde_test.go`
- Modify: `pkg/format/registry.go`
- Modify: `pkg/ui/config.go`
- Modify: `config.example.toml`

- [ ] implement rule: insert `~` between word char and cite/ref commands when preceding char is a regular space
- [ ] conservative skip conditions: preceding is `(`, `[`, `~`, `\`, start-of-line, punctuation; inside protected span; inside excluded ranges (inline math)
- [ ] reuse `excludedRanges()` from rules_wrap.go to find cite/ref call sites
- [ ] cite-like command set: cite, citep, citet, ref, eqref, cref, Cref, autoref, nameref
- [ ] configurable via `FmtConfig.TildeRefs []string` (omit = use defaults)
- [ ] register as Tier 2 in registry; Hits carry ExpectedDiffSourceLines for every line where the rule fires
- [ ] update config.example.toml with tilde_refs
- [ ] write tests: insertion cases (Theorem 1.2 \cite, see \eqref), skip cases (opening paren, start-of-line, already tilde, inside math), protected span no-rewrite
- [ ] run `go test ./... && make install`

### Task 7: --lines=START:END range formatting

**Files:**
- Create: `pkg/format/range.go`
- Create: `pkg/format/range_test.go`
- Modify: `cmd/mreview/fmt.go`
- Modify: `pkg/format/types.go`

- [ ] add `Lines string` field to `fmtOpts` with `long:"lines" description:"format only lines START:END (1-based, inclusive)"`
- [ ] extend `Hit` with `BeforeRange [2]int` (byte offsets) and `AfterText string`
- [ ] implement clip mask + per-line replay in range.go: run format.Apply on whole file, build mask, emit formatted lines for in-range, original for out-of-range
- [ ] --lines forces off line-count-changing rules (space.blank-runs, space.wrap, math.paragraph-suppress) with report note "skipped under --lines"
- [ ] mutual exclusion: --lines + {multi-file, --check, --summary, --fail-on-change} -> exit 2; allowed with --stdin, --diff, --print, default in-place
- [ ] plumb `Options.LineRange` through to rules that need to self-disable
- [ ] write tests: range preserves out-of-range bytes identically, in-range rewrites apply, force-off rules report skip, parse validation
- [ ] run `go test ./... && make install`

### Task 8: math.align-columns rule

**Files:**
- Create: `pkg/format/rules_math_align.go`
- Create: `pkg/format/rules_math_align_test.go`
- Modify: `pkg/format/registry.go`
- Modify: `pkg/ui/config.go`
- Modify: `cmd/mreview/config.go`
- Modify: `config.example.toml`

- [ ] implement env-body parser: tokenize rows by `\\` at depth 0, cells by `&` at depth 0
- [ ] align `&` columns in: align, align*, alignat, alignat*, aligned, array, matrix, pmatrix, bmatrix, vmatrix, Vmatrix, Bmatrix, cases, tabular, tabular*, tabularx
- [ ] refusal cases: unequal cell counts, `%` line comments, nested aligned envs -> emit Tier-3 note in report
- [ ] compute colWidth per column, right-pad cells, emit with preserved `\\`
- [ ] skip if env inside protected span or skip-directive region
- [ ] add `MathAlign *bool`, `MathAlignEnvs []string`, `MathAlignSkip []string` to FmtConfig
- [ ] register as Tier 1; Tier-1 contract: whitespace-only changes, pixel-identical PDF
- [ ] update defaultGlobalConfig and config.example.toml
- [ ] write tests: basic alignment, unequal rows refusal, comment refusal, nested env, protected span, various env types
- [ ] run `go test ./... && make install`

### Task 9: math.continuation-indent rule

**Files:**
- Create: `pkg/format/rules_math_cont.go`
- Create: `pkg/format/rules_math_cont_test.go`
- Modify: `pkg/format/registry.go`

- [ ] implement: find first `=`/`\equiv`/`:=`/`\le`/`\ge` on first row of equation envs (equation, equation*, gather, gather*, multline, multline*, align, align*)
- [ ] for subsequent rows starting with a binop: pad leading whitespace so binop sits one column past anchor
- [ ] share env-body parser with math.align-columns
- [ ] skip when no anchor found; skip when env is aligned by columns (math.align-columns already handled it)
- [ ] register as Tier 1; pure leading whitespace
- [ ] write tests: basic continuation indent, no anchor (skip), various binops, interaction with align-columns
- [ ] run `go test ./... && make install`

### Task 10: math.wrap-at-break-op rule (opt-in)

**Files:**
- Create: `pkg/format/rules_math_wrap.go`
- Create: `pkg/format/rules_math_wrap_test.go`
- Modify: `pkg/format/registry.go`
- Modify: `pkg/ui/config.go`
- Modify: `config.example.toml`

- [ ] implement: for inline display equations exceeding math_wrap_col, split at rightmost break operator within budget
- [ ] move operator to start of new line, apply math.continuation-indent alignment
- [ ] off by default (math_wrap = false); enable via --rule or config
- [ ] add `MathWrap *bool`, `MathWrapCol int` to FmtConfig
- [ ] register as Tier 1 (whitespace-only, opt-in)
- [ ] update config.example.toml
- [ ] write tests: wrap fires only on overflow, respects break-op set, short equation no-op, preserves structure
- [ ] run `go test ./... && make install`

### Task 11: PNAS golden tests for new rules

**Files:**
- Modify: `pkg/format/golden_test.go` (or create new golden for math)

- [ ] extend PNAS golden round-trip to cover Phase 2-4 rules
- [ ] create new fixture with hand-crafted misaligned align/tabular blocks for columnizer testing
- [ ] verify PDF byte-identity for Tier-1 math rules
- [ ] verify expected-diff-only for Tier-2 tilde rule
- [ ] run `go test ./... && make install`

### Task 12: Verify acceptance criteria

- [ ] manual test: `mreview fmt --stdin < paper.tex | diff - paper.tex` produces expected output
- [ ] manual test: `mreview fmt --fail-on-change paper.tex` exits 1 on dirty source
- [ ] manual test: `mreview fmt --lines=42:120 paper.tex` rewrites only the target range
- [ ] manual test: `mreview fmt --rule=math.align-columns paper.tex` aligns tabular columns
- [ ] run full test suite: `go test ./...`
- [ ] run linter: `make lint`
- [ ] verify test coverage meets 80%+

### Task 13: Update documentation

- [ ] update README.md with new flags (--stdin, --fail-on-change, --summary, --lines) + per-env config + math rules
- [ ] update config.example.toml if any keys still undocumented
- [ ] update CLAUDE.md if internal patterns changed
- [ ] move this plan to `docs/plans/completed/`
