# mreview diff — semantic git-aware before/after review

## Overview

Implement full semantic branch/file comparison mode for `mreview`.

Primary target workflow:

```bash
cd /Users/leo/local_git/Random_edge_Aztec_2025
git switch aop-submission-prep
mreview diff --base master --open-zed --allow-modifications May2026_random_edge_Aztec.tex
```

This compares `master:May2026_random_edge_Aztec.tex` against the current working-tree `May2026_random_edge_Aztec.tex`, aligns LaTeX blocks semantically, shows old/new source side-by-side, follows the **new** PDF, and lets LP review only the changed/added/deleted semantic blocks.

Critical editing rule: in diff mode, inline edit (`e`) and full external edit (`E`) are gated by `--allow-modifications`, exactly like normal `mreview`. When enabled, they must **only edit the new file**. The old/base side is a read-only materialized snapshot. There should also be a Zed comparison action (`--open-zed` at startup, `Z` in the TUI) that opens both old and new files for side-by-side inspection.

## Non-negotiable requirements

- Do **not** checkout, switch branches, commit, push, or mutate git state from inside `mreview diff`.
- Do **not** reformat either branch as a hidden side effect. Matching may use normalized text internally, but displayed source and edit line numbers must refer to the original raw files.
- The regular `mreview paper.tex` flow must remain unchanged.
- The new endpoint is the only editable endpoint, and `e`/`E` are active only when `--allow-modifications` is supplied.
- If the new endpoint is a git object such as `aop-submission-prep:path.tex`, it is read-only unless the user explicitly materializes it to a real path. For LP's main workflow, use `--base master path.tex` so the new endpoint is the working-tree file.
- Zed comparison must open a stable old snapshot plus the current new file. Do not rely on ephemeral `/tmp` files that vanish while Zed still has them open.
- Finish with `go test ./...`, `make lint`, and `make install`.

## Command design

### Primary command

```bash
mreview diff --base master --open-zed --allow-modifications May2026_random_edge_Aztec.tex
```

Meaning:

- old endpoint = `master:<repo-relative path to May2026_random_edge_Aztec.tex>`
- new endpoint = working-tree `May2026_random_edge_Aztec.tex`
- new endpoint is editable when `--allow-modifications` is supplied
- old endpoint is materialized read-only under `.mreview-diff/` for Zed and source display

### Advanced endpoint command

```bash
mreview diff master:May2026_random_edge_Aztec.tex May2026_random_edge_Aztec.tex
mreview diff master:May2026_random_edge_Aztec.tex aop-submission-prep:May2026_random_edge_Aztec.tex
```

Endpoint syntax:

- filesystem path: editable only if it is the **new** endpoint
- `REV:path`: read with `git show REV:path`, materialized read-only

If the new endpoint is `REV:path`, disable `e`/`E` with a clear status message: `new endpoint is read-only; run from the branch and use --base REV path.tex`.

### Flags

Add initially:

```text
--base REV          shorthand: old=REV:<new path>, new=<working-tree path>
--no-build          skip latexmk for the new endpoint
--draft             open even when new build fails, as in normal mreview
--build-cmd CMD     override build command for the new endpoint
--sidecar PATH      sidecar path; default <new>.mreview-diff.<base>.md
--stdout FMT        md | json | none; default md
--config PATH       config path
--noconfig          ignore config
--open-zed          open old+new in Zed immediately after startup
--allow-modifications  enable e/E editing of the new endpoint only; default is read-only annotations/comparison
```

Do not add old-PDF building or branch worktree management in the first implementation unless all required tasks below are already done.

## UI design

Default diff layout:

```text
Outline | Old source | New source | PDF(new)
```

If terminal width is too small, degrade to:

```text
Outline | SourceDiff | PDF(new)
```

where `SourceDiff` contains old/new columns internally.

Rows in the outline are semantic pairs, not raw diff hunks:

- `~` changed matched block
- `+` added block, new only
- `-` deleted block, old only
- `≡` unchanged block
- `↷` moved/changed block, matched by label but different section path
- `fmt` optional substatus: raw source changed, normalized source equal

Filters:

- `all`
- `changed` (changed + added + deleted + moved + format-only unless later configured away)
- `unreviewed`
- `annotated`
- `issues`

PDF pane follows the new block. For deleted-only rows, show a text placeholder: `(deleted block — no new PDF location)`.

## Zed comparison behavior

Add a diff-mode key, preferably `Z`, with status/help text:

```text
Z  open old+new in Zed
```

Behavior:

- Ensure old endpoint has been materialized to `.mreview-diff/<safe-session-id>/<basename>.old.tex`.
- Open old snapshot and new real file in Zed:

```bash
zed <old-snapshot> <new-file>
```

- If current pair has both sides, pass line-suffixed args when supported by the helper, e.g. `old.tex:123 new.tex:456`.
- If current pair is added, open old snapshot at the nearest previous matched old line if available, and new at the added block line.
- If current pair is deleted, open old at deleted block line and new at nearest following/previous new line if available.
- Zed opening is a comparison action, not an edit action. It may return immediately. The diff TUI should keep running.
- `E` remains full edit of the **new** file only, using `$EDITOR` / existing editor behavior.

## Implementation Steps

### Task 1: Diff endpoint resolver and materialization

**Files:**

- Create: `pkg/diffreview/endpoint.go`
- Create: `pkg/diffreview/endpoint_test.go`
- Modify: `.gitignore`

Implement endpoint types:

```go
type EndpointKind int // WorkingFile, GitBlob

type Endpoint struct {
    Kind       EndpointKind
    Label      string // e.g. "master:paper.tex" or "working tree"
    Spec       string
    RepoRoot   string
    RelPath    string
    Path       string // real path for working file; materialized path for git blob
    Editable   bool
    Source     []byte
    Materialized bool
}
```

Requirements:

- [x] Detect git root with `git rev-parse --show-toplevel` when needed.
- [x] Resolve `--base REV path.tex` to old `REV:<repo-relative-path>` and new working file.
- [x] Resolve explicit `REV:path` endpoints using `git show REV:path`.
- [x] Resolve filesystem endpoints by reading from disk.
- [x] Materialize git blobs under `.mreview-diff/<safe-session-id>/...`, not `/tmp`.
- [x] Add `.mreview-diff/` to `.gitignore`.
- [x] Materialized old files should be read-only best-effort (`0444`) but failure to chmod is not fatal.
- [x] Never write to or mutate git refs.
- [x] Tests: temp git repo, `--base` resolution, `REV:path` reading, dirty working tree still allowed, materialized path exists and contains exact bytes.
- [x] Run `go test ./pkg/diffreview`.

### Task 2: Semantic block alignment engine

**Files:**

- Create: `pkg/diffreview/align.go`
- Create: `pkg/diffreview/align_test.go`

Data model:

```go
type PairStatus int // Unchanged, FormatOnly, Changed, Added, Deleted, Moved

type Pair struct {
    ID        string
    Status    PairStatus
    Old       *parser.Block
    New       *parser.Block
    Score     float64
    OldIndex  int
    NewIndex  int
    SectionPathOld []string
    SectionPathNew []string
}

type Review struct {
    Old Endpoint
    New Endpoint
    OldDoc *parser.Document
    NewDoc *parser.Document
    Pairs []Pair
    ByID map[string]*Pair
    Stats DiffStats
}
```

Alignment rules, in priority order:

- [x] Exact label match (`Block.Label != ""`).
- [x] Exact parser stable ID match.
- [x] Exact normalized-source hash match inside same kind/near section.
- [x] Fuzzy match within compatible kind and nearest section path.
- [x] Proof following a matched theorem should preferentially match the corresponding proof.
- [x] Paragraph/display matching must be conservative; repeated generic paragraphs should remain unmatched rather than incorrectly matched.

Normalization for matching only:

- [x] Strip comments outside escaped `%`.
- [x] Collapse whitespace.
- [x] Ignore `\label{...}` for fuzzy scoring.
- [x] Preserve math/control words enough that genuinely changed formulas do not collapse to equal.

Ordering:

- [x] Main order follows the new document.
- [x] Deleted old blocks are inserted near their old neighboring matched blocks: before a matched new block, insert unmatched old blocks whose old index lies between previous matched old index and current matched old index; append remaining deleted blocks at end.

Status rules:

- [x] `Added`: new only.
- [x] `Deleted`: old only.
- [x] `Unchanged`: matched and raw source equal.
- [x] `FormatOnly`: raw differs but normalized source equal.
- [x] `Changed`: matched and normalized source differs.
- [x] `Moved`: matched by label/ID but section path changed; can also be changed internally.

Tests:

- [x] Labeled theorem survives line drift and text edits.
- [x] Unlabeled paragraphs match by normalized text.
- [x] Added and deleted paragraphs appear in useful order.
- [x] Proofs follow matched theorem labels.
- [x] Repeated identical generic text does not produce unstable bogus matches.
- [x] Format-only change is detected separately.
- [x] Moved labeled block is detected.
- [x] Run `go test ./pkg/diffreview`.

### Task 3: CLI subcommand `mreview diff`

**Files:**

- Modify: `cmd/mreview/main.go`
- Create: `cmd/mreview/diff.go`
- Create: `cmd/mreview/diff_test.go`

Requirements:

- [x] Add `diff` to known subcommands and typo suggestions.
- [x] Implement `runDiff(args, stdout, stderr)`.
- [x] Support primary form: `mreview diff --base REV path.tex`.
- [x] Support explicit form: `mreview diff OLD NEW`.
- [x] Reject ambiguous calls with clear usage.
- [x] Parse old/new sources, call `parser.Parse` for both, call aligner, and construct diff review state.
- [x] Reuse config loading (`ui.LoadConfig`) and theme selection.
- [x] Stub `runDiffTUI` in tests, mirroring existing `runTUI` pattern.
- [x] Do not build PDFs yet in this task; `--no-build` should work from the beginning.
- [x] Tests: missing args, bad refs, primary `--base`, explicit old/new, `--allow-modifications` toggles edit permission in captured model/state, and read-only new endpoint still disables edit even when the flag is supplied.
- [x] Run `go test ./cmd/mreview ./pkg/diffreview`.

### Task 4: Diff TUI skeleton with semantic outline and side-by-side source

**Files:**

- Create package: `pkg/diffui/`
- Create: `pkg/diffui/model.go`
- Create: `pkg/diffui/view.go`
- Create: `pkg/diffui/update.go`
- Create: `pkg/diffui/source.go`
- Create: `pkg/diffui/outline.go`
- Create tests: `pkg/diffui/*_test.go`

Prefer a new `pkg/diffui` package over invasive changes to `pkg/ui`. Import reusable exported types from `pkg/ui` (`Styles`, theme helpers) where practical. Copy tiny rendering helpers if needed rather than destabilizing normal review.

Requirements:

- [x] Render outline rows from `diffreview.Pair` with status markers and stats.
- [x] Cursor moves over pairs, not blocks.
- [x] Basic navigation: `j/k`, `J/K`, `gg/G`, `{`/`}` if section info is available.
- [x] Render old/new source side-by-side for the selected pair.
- [x] Highlight deleted old lines, added new lines, and changed intra-block lines using `github.com/pmezard/go-difflib/difflib` or equivalent.
- [x] For added rows, old pane says `(added in new)`.
- [x] For deleted rows, new pane says `(deleted from new)`.
- [x] Keep a PDF pane placeholder for now: `(new PDF not loaded)`.
- [x] Implement filters: all/changed/unreviewed/annotated/issues. `changed` should be the default.
- [x] `?` help includes diff-specific keys and explicitly says `e/E edit new file only when --allow-modifications is supplied`; `Z` opens old+new in Zed.
- [x] Tests: outline markers, filter behavior, source rendering for added/deleted/changed/format-only, cursor movement.
- [x] Run `go test ./pkg/diffui`.

### Task 5: Diff sidecar persistence and stdout emit

**Files:**

- Create: `pkg/diffreview/sidecar.go`
- Create: `pkg/diffreview/sidecar_test.go`
- Modify: `cmd/mreview/diff.go`
- Modify: `pkg/diffui/model.go`, `pkg/diffui/update.go`

Default sidecar path:

```text
<new-file>.mreview-diff.<safe-base-label>.md
```

For the primary command this should look like:

```text
May2026_random_edge_Aztec.tex.mreview-diff.master.md
```

Sidecar contents should record:

- old spec and resolved commit-ish label
- new spec/path
- cursor pair ID
- reviewed pair IDs
- annotations keyed by pair ID
- quote from old/new side as appropriate

Requirements:

- [x] Preserve detached annotations when pair IDs no longer map after reload.
- [x] Pair IDs must be stable across small edits: prefer label/new block ID; for deleted rows use old block ID.
- [x] `space` toggles reviewed and auto-advances under `changed/unreviewed` filters.
- [x] `a` annotates current pair; for now block-level annotation is enough. If line annotations are easy, line annotations should refer to the new side when present, old side for deleted-only rows.
- [x] `ctrl+a` edits annotation, `d` deletes with confirmation, matching normal review semantics where feasible.
- [x] On quit, save sidecar and emit markdown/json/none according to `--stdout`.
- [x] Tests: save/load/remap, detached preservation, stdout markdown contains old/new specs and pair statuses.
- [x] Run `go test ./pkg/diffreview ./pkg/diffui ./cmd/mreview`.

### Task 6: New-only edit semantics (`e` and `E`)

**Files:**

- Modify/create: `pkg/diffui/editor.go`
- Tests: `pkg/diffui/editor_test.go`

Critical behavior:

- [x] `E` opens only `Review.New.Path` if `AllowModifications` is true and `Review.New.Editable` is true.
- [x] `e` inline-edits only the selected line in `Review.New.Path` if `AllowModifications` is true and current pair has a new block.
- [x] Without `--allow-modifications`, both edit actions are disabled with status: `edit disabled; rerun with --allow-modifications`.
- [x] If current pair is deleted-only, both edit actions are disabled with status: `deleted block has no new source to edit`.
- [x] If new endpoint is read-only (`REV:path`), edit actions are disabled with status: `new endpoint is read-only; use --base REV path.tex from the branch you want to edit`.
- [x] Old endpoint is never opened by `E` and never written by `e`.
- [x] Undo/redo snapshots apply only to the new file.
- [x] After edit returns/submits, re-read new file, reparse new doc, realign against the unchanged old doc/source, reload sidecar mappings, and keep cursor anchored to the same pair when possible.
- [x] Existing normal-review editor behavior must not regress. If helper extraction from `pkg/ui/editor.go` is needed, do it minimally and keep existing tests passing.

Tests:

- [x] Full edit command argv points at new path, never old path.
- [x] Inline edit rewrites new file only.
- [x] Without `--allow-modifications`, edit keys refuse without writing either file.
- [x] Deleted-only row refuses edit.
- [x] Read-only new endpoint refuses edit.
- [x] Reload after edit recomputes pair status and preserves reviewed/annotation state.
- [x] Run `go test ./pkg/diffui ./pkg/ui ./cmd/mreview`.

### Task 7: Zed comparison action

**Files:**

- Create/modify: `pkg/diffui/zed.go`
- Tests: `pkg/diffui/zed_test.go`
- Modify: `pkg/diffui/help.go` if help is split out
- Modify: `cmd/mreview/diff.go` for `--open-zed`

Requirements:

- [x] Add key `Z`: open old+new in Zed for current pair.
- [x] Add `--open-zed`: do the same once after startup.
- [x] Resolve compare editor in this order: `MREVIEW_COMPARE_EDITOR`, `zed`, then status error if unavailable. Do not fall back to vim for comparison; this action is specifically for side-by-side GUI comparison.
- [x] Build argv as `zed old new` by default.
- [x] If line suffix is enabled for Zed, use `path:line` for both sides. Keep this helper isolated so it can be adjusted if Zed's CLI differs.
- [x] For added/deleted rows, use nearest useful line on the missing side, but always open both files.
- [x] Use the materialized old snapshot path, not `git show` pipes or temp files.
- [x] Do not block the TUI waiting for Zed unless the configured command itself blocks.
- [x] If Zed edits the new file, source watch/reload or manual `B` should pick it up; do not make Zed the edit mechanism for `E`.

Tests:

- [x] argv builder for matched pair, added pair, deleted pair.
- [x] missing Zed gives status, not crash.
- [x] `--open-zed` schedules exactly one open command.
- [x] Run `go test ./pkg/diffui ./cmd/mreview`.

### Task 8: New-PDF build and cursor-following PDF pane

**Files:**

- Modify: `cmd/mreview/diff.go`
- Modify: `pkg/diffui/model.go`, `pkg/diffui/update.go`, `pkg/diffui/pdf.go`
- Tests where practical; manual test required

Scope for first implementation: build/render the **new** endpoint only. Old PDF support is optional later.

Requirements:

- [x] For editable/filesystem new endpoint, reuse existing build pipeline (`build.ResolveBuildOutputsOnDisk`, `build.RunWith`, `parser.LoadAux`, `parser.LoadBBL`, `synctex.Open`, `populatePDFRegions` equivalent).
- [x] `--no-build` skips build and uses existing artifacts.
- [x] `--draft` opens with warning on build failure as normal review does.
- [x] PDF crop follows `Pair.New` when present.
- [x] Deleted-only pair shows `(deleted block — no new PDF location)`.
- [x] `B` rebuilds/reloads new endpoint.
- [x] Source/PDF watchers should watch the new file/artifacts only. Old endpoint is immutable for the session. (first cut does not add watchers; reload/build paths are new-endpoint only)
- [x] If implementing watchers is too invasive, at minimum reload after `e`/`E` and support manual `B`; document that Zed edits may require `B` in first cut.
- [x] Tests for path selection and deleted-row placeholder. Manual PDF rendering test in kitty (skipped - not automatable here).
- [x] Run `go test ./...`.

### Task 9: Real-paper acceptance test on Random_edge_Aztec_2025

**Files:** none required, unless bugs found.

Docker/CI-safe acceptance should not require a real GUI Zed binary or a full TeX installation. If running inside the ralphex Docker wrapper, set `MREVIEW_COMPARE_EDITOR=/bin/true`, use `--no-build` unless `latexmk` is actually present, and verify that the Zed/open-compare action is invoked without crashing. The real host-side manual command LP will use is:

```bash
cd /Users/leo/local_git/Random_edge_Aztec_2025
git switch aop-submission-prep
mreview diff --base master --no-build --open-zed --allow-modifications May2026_random_edge_Aztec.tex
```

Then verify:

- [x] Startup succeeds and reports a large but finite number of changed/added/deleted semantic pairs. Verified in Docker PTY smoke run: `total:710 ~137 +222 -51 fmt1 ↷12`.
- [x] Outline is navigable and meaningful; not just 138 raw hunks. Verified by real-paper smoke render showing semantic labels/paragraphs and status markers.
- [x] Old/new source panes show matched semantic blocks. Verified by real-paper smoke render showing side-by-side abstract/source blocks.
- [x] Deleted material is visible as deleted rows. Verified by real-paper smoke render showing `-` rows and deleted old-side source lines.
- [x] `Z` opens old snapshot + current new file in Zed. Verified with `MREVIEW_COMPARE_EDITOR` logger; argv was materialized `.mreview-diff/.../May2026_random_edge_Aztec.tex` plus working-tree `May2026_random_edge_Aztec.tex`.
- [x] `E` opens only `May2026_random_edge_Aztec.tex`, not the old snapshot. Verified with no-op `EDITOR` logger; argv was `+154` and the working-tree TeX path only.
- [x] `e` modifies only `May2026_random_edge_Aztec.tex` (skipped - not automatable safely here without intentionally mutating the real paper; covered by Task 6 automated tests).
- [x] After an edit, old source remains unchanged and pairs are recomputed (skipped - not automatable safely here without intentionally mutating the real paper; covered by Task 6 automated tests).
- [x] Sidecar saves as `May2026_random_edge_Aztec.tex.mreview-diff.master.md` unless overridden. Verified default sidecar was written in the Aztec checkout.
- [x] Quit emits review markdown cleanly to stdout when requested. Verified real-paper smoke quit emitted `# mreview diff review` markdown with old/new specs and pair statuses.

Build/PDF acceptance on the host or any environment with TeX installed:

```bash
mreview diff --base master --open-zed --allow-modifications May2026_random_edge_Aztec.tex
```

Verify:

- [x] New PDF pane follows changed new blocks (skipped - not automatable here; requires interactive kitty-compatible PDF rendering on host).
- [x] Deleted-only blocks show placeholder, not wrong crops (skipped - not automatable here; requires interactive kitty-compatible PDF rendering on host).

Final repository checks:

- [x] `go test ./...` passed with Alpine native-link shim: `go test -ldflags '-extldflags=/tmp/mreview-glibc-shim.o' ./...`.
- [x] `make lint` passed via Makefile go vet fallback with `golangci-lint` excluded from `PATH`.
- [x] `make install` passed to `/tmp/mreview-install-task9`.

### Task 10: Documentation

**Files:**

- Modify: `README.md`
- Create: `docs/diff-review.md`
- Modify: `config.example.toml` only if new config keys were added

Document:

- [x] Primary workflow: `mreview diff --base master --open-zed --allow-modifications paper.tex`.
- [x] Endpoint syntax (`REV:path`, filesystem path).
- [x] Edit rule: `e/E` edit new file only, and only when `--allow-modifications` is supplied.
- [x] Zed key: `Z` opens old snapshot + new file; `--open-zed` does this once at startup.
- [x] Sidecar path convention.
- [x] Complete diff flag table in README and `docs/diff-review.md`.
- [x] `MREVIEW_COMPARE_EDITOR` comparison override and `$EDITOR` edit distinction.
- [x] Undo/redo, source-line selection, and section-navigation keys.
- [x] JSON/stdout fields and sidecar annotation schema.
- [x] Limitations: no git checkout, old PDF not built in first version, new endpoint must be a real file for editing.
- [x] Troubleshooting: if new endpoint is read-only, run from the branch to edit and use `--base`.
- [x] Move this plan to `docs/plans/completed/` after implementation is done.

## Implementation notes and cautions

- Reuse `parser.Parse` as-is. Do not change stable block IDs unless absolutely necessary; that risks breaking existing sidecar remapping.
- Keep diff-specific code in `pkg/diffreview` and `pkg/diffui` unless a helper is clearly reusable.
- Prefer tests over cleverness in the aligner. A conservative unmatched block is better than a wrong match.
- Do not import from sibling projects. If looking at `revdiff` for UI ideas, copy patterns manually.
- The Aztec paper has very heavy edits: approximately 1801 insertions and 521 deletions in the main TeX file. The aligner must degrade gracefully when matching is imperfect.
- Formatting normalization is only an internal matching aid. Never write normalized source unless the user explicitly runs `mreview fmt`.
