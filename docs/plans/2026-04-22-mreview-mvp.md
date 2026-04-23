# Mreview — LaTeX-aware math paper review TUI

## Overview

- Build `mreview`, a terminal review tool for LLM-generated math papers in LaTeX. Like `revdiff` for prose, but navigating semantic blocks (theorems, proofs, display math) rather than diff lines.
- Parses the `.tex`, auto-follows a rendered PDF pane (via SyncTeX + go-fitz + kitty graphics) as the cursor walks blocks, and lets the user attach free-text annotations that emit back to the LLM as structured markdown.
- Multi-session: hundreds of blocks per paper, reviewed across many sittings; a sidecar `<paper>.mreview.md` holds annotations, reviewed-checkboxes, and cursor position; next launch resumes on the first unreviewed block.
- Ref jumping (`\ref` / `\cref` / `\Cref` / `\eqref` / `\cite`) is a first-class feature.

## Reset instructions (Task 0 — MUST run first)

- All prior work on this branch (Tasks 1–11 from the previous plan + Task 12 WIP) is to be discarded. The executor must hard-reset the branch and delete any leftover files before starting Task 1.
- Previous plan file was `docs/plans/2026-04-22-mreview-mvp.md`. The new plan replaces it at the same location (this file).

## Sister project access (READ CAREFULLY — required for every UI/PDF task)

The previous executor failed because it did not read the sister projects. These projects are NOT dependencies (do not import from them) but they contain the reference patterns this project must mirror. The executor MUST read the specified files before writing code in each referenced task.

Before writing any code, the executor must verify read access to:

- `~/__code/revdiff/` — bubbletea annotation TUI; source of truth for annotation UX, stdout-markdown shape, lipgloss layout.
- `~/__code/CLI-PDF-EPUB-reader/` — go-fitz + go-termimg kitty-graphics PDF rendering; source of truth for `pkg/pdf/`.
- `~/__code/ralphex/` — plan format reference (already followed).

Verification: `ls ~/__code/revdiff ~/__code/CLI-PDF-EPUB-reader ~/__code/ralphex` must list files. If any of these paths are inaccessible, stop and surface the error before proceeding — do not guess at the patterns.

Each task below that touches UI or PDF includes a **Sister-project reading** block listing specific files to study before writing code. Reading is non-optional.

## Context

### Module layout

```
cmd/mreview/         # main entry, CLI flags
pkg/parser/          # LaTeX tokenizer, block model, label/ref index
pkg/build/           # latexmk runner, .log parsing, strict error exit
pkg/synctex/         # .synctex.gz parser, line -> (page, bbox) index
pkg/persist/         # sidecar .mreview.md load/save, stale remap
pkg/pdf/             # go-fitz PDF crop + go-termimg kitty render
pkg/ui/              # bubbletea model/update/view, lipgloss layouts
testdata/            # synthetic .tex + .pdf + .synctex.gz fixtures
docs/plans/          # this plan
```

### Dependencies to adopt

- `github.com/charmbracelet/bubbletea`, `lipgloss`, `bubbles`
- `github.com/gen2brain/go-fitz` — MuPDF bindings
- `github.com/blacktop/go-termimg` — kitty graphics
- `github.com/sahilm/fuzzy` — outline + search fuzzy match
- `github.com/jessevdk/go-flags` — CLI parsing

### Target users

- LP (math professor). Single-user tool, kitty terminal only (no iTerm2/Sixel fallbacks).
- Papers are single-file `.tex` using `amsart`/`article` with per-paper `\newtheorem` declarations (no `\input` recursion in MVP).
- Real-world test targets (do NOT add to testdata):
  - `~/local_git/Colored_interchangeability_Ayyer_Martin_Petrov/June_2025_*.tex`
  - `~/local_git/Perturbed_Beta_Processes/nov2025_perturbed_beta.tex`
  - `~/local_git/Panova-Petrov/2026-01-27-AIHP-D-edits/Sep2024_skew_hook.tex`

## Solution Overview

- LaTeX-aware parser → ordered tree of typed `Block`s: `Section`, `Abstract`, `TheoremLike`, `Proof`, `Display`, `Figure`, `Paragraph`, `ProofStep`, `Bibliography`, `Other`.
- Theorem envs auto-discovered from `\newtheorem{...}`; config file can override.
- Each block carries `(file, start_line, end_line, source_slice, label?, number?, refs_out[], parent_id?, children_ids[], pdf_region?)`.
- Cross-ref index resolves all ref kinds at parse time; unresolved refs flagged `⚠` in outline.
- Stable block IDs: label if present, else `sha1(kind || parent-label || sibling-index || first-40-chars-of-source)[:10]` — survives line drift.
- Build: one strict `latexmk -pdf -synctex=1 -interaction=nonstopmode` run at startup; fails hard on errors or unresolved refs.
- SyncTeX: parse `main.synctex.gz` once into `(file, line) -> (page, bbox)`; pre-compute per-block PDF region.
- UI: three-pane bubbletea — outline (25%) + source (40%) + PDF crop (35%); status line bottom.
- Navigation: vim-style — `j`/`k` outer, `J`/`K` inner, `{`/`}` sections, `gg`/`G` ends, motion counts, `go`/`Ctrl-O`/`Ctrl-I` jump, `gd` on `\cite`.
- Annotation: `a` inline textarea; `e` edit, `d` delete; `space` toggles reviewed (auto-advance if filter=unreviewed).
- Outline filter (`f` cycles): `all` / `unreviewed` / `annotated` / `issues`.
- Persistence: `<paper>.mreview.md` with YAML frontmatter + markdown annotations. Same markdown emitted to stdout on `q`.
- Stale state remap: label → hash → text similarity. Unmatched annotations → `## Detached` section.
- CLI: `mreview <paper.tex>` + flags `--no-build`, `--build-cmd`, `--sidecar`, `--stdout md|json|none`, `--config`.

## Technical Details

(Block model, parser strategy, auto-discovery, label/ref resolution, latexmk runner, SyncTeX, PDF rendering, persistence format, stdout, TUI layout, keybindings, CLI — all per the prior plan's spec; unchanged in substance.)

### Block model

```go
type Kind int
const (
    KindSection Kind = iota
    KindAbstract
    KindTheoremLike
    KindProof
    KindDisplay
    KindFigure
    KindParagraph
    KindProofStep
    KindBibliography
    KindOther
)
type Block struct {
    ID, EnvName, File string
    Kind              Kind
    StartLine, EndLine int
    Source, Title, Label, Number string
    RefsOut           []Ref
    ParentID          string
    ChildIDs          []string
    PDFRegion         *Region
}
```

### Keybindings (summary)

```
j/k J/K {/} gg/G       — navigation
<N><motion>            — motion counts
go Ctrl-O Ctrl-I gd gu — ref jumping
a A e d space          — annotations + reviewed
/ @ f ? q Q            — UI
```

### Sidecar format

`<paper>.mreview.md` — YAML frontmatter (paper, pdf, cursor, reviewed) + annotation sections with heading `` ## <Breadcrumb> — `<BlockID>` (<File>:L<start>-L<end>) ``, blockquoted source snippet (≤6 lines, middle-ellipsis if longer), note.

## Development Approach

- Testing approach: regular (code first, then tests within same task).
- Complete each task fully before the next.
- Small focused commits per task.
- **CRITICAL: every task MUST include new/updated tests.**
- **CRITICAL: all tests must pass before starting next task** — run `make test` and `make lint`.
- Kitty-only is a firm constraint: remove any terminal-type branching.
- **CRITICAL: every task that references a sister project MUST begin by reading the listed files; do not guess patterns.**

## Testing Strategy

- Unit tests per package, table-driven with testify. Parser / build / synctex / persist aim 85% coverage; UI is update-function table tests.
- `testdata/` fixture: synthetic paper (`sample.tex`, `sample.pdf`, `sample.synctex.gz`, `sample.aux`, `sample.bbl`). Generate via `testdata/gen.sh`.
- Manual verification after each UI task on a real paper from `~/local_git/`.
- No e2e harness (kitty graphics not snapshot-friendly); manual is the gate.

## Implementation Steps

### Task 0: Hard reset + sister-project access verification

**Files:**
- No code changes; branch state + environment verification only.

- [x] verify current branch is `mreview-mvp` and working directory is `/Users/leo/__code/mreview`
- [x] run `git log --oneline main..HEAD` to list all commits on this branch; the commits to discard are those belonging to Tasks 1–11 of the previous plan (e.g. `feat: SyncTeX v1 parser ...`, `feat: sidecar persistence ...`, `feat: bubbletea TUI skeleton ...`, `feat: outline pane ...`, `feat: vim-style navigation ...`, and any predecessors on this branch added after branching from `main`)
- [x] `git reset --hard <commit-before-task-1>` (typically the merge-base with `main`) to discard all Task 1–11 commits on this branch
- [x] remove untracked leftovers: `git clean -fdn` to preview, then `git clean -fd`
- [x] verify read access to sister projects:
  - `ls ~/__code/revdiff/ | head`
  - `ls ~/__code/CLI-PDF-EPUB-reader/ | head`
  - `ls ~/__code/ralphex/ | head`
  - if any fails: STOP, report, do not proceed
- [x] verify one file is readable from each sister project (not just directory listing): read `~/__code/revdiff/go.mod`, `~/__code/CLI-PDF-EPUB-reader/go.mod`, `~/__code/ralphex/go.mod`
- [x] mark the Task 0 checklist done in this plan file confirming access is verified
- [x] no tests required for this task, but confirm `make test` still runs (passes vacuously on a clean tree)

### Task 1: CLI skeleton + project Makefile wiring

**Sister-project reading (before writing code):**
- `~/__code/ralphex/cmd/*/main.go` — `jessevdk/go-flags` usage pattern.
- `~/__code/revdiff/cmd/*/main.go` — error-exit and stderr conventions.

**Files:**
- Modify: `cmd/mreview/main.go`
- Modify: `go.mod`
- Create: `cmd/mreview/main_test.go`
- Modify/create: `Makefile` if missing targets `build`, `test`, `lint`

- [ ] use `github.com/jessevdk/go-flags` for CLI parsing
- [ ] `Opts`: `File string (positional)`, `NoBuild bool`, `BuildCmd string`, `Sidecar string`, `Stdout string`, `Config string`, `Version bool`
- [ ] `--version` prints version + exits 0
- [ ] missing positional → usage to stderr + exit 2; missing file → error to stderr + exit 1; existing file → exits with `mreview: not implemented yet`
- [ ] `go mod tidy`
- [ ] tests in `main_test.go` for: `--version`, missing arg, missing file, existing-file placeholder
- [ ] run `make build && make test && make lint` — all must pass

### Task 2: LaTeX tokenizer

**Files:**
- Create: `pkg/parser/tokenizer.go`, `pkg/parser/tokenizer_test.go`
- Create: `testdata/sample.tex`

- [ ] `Tokenize(src []byte) []Token`: `BeginEnv`, `EndEnv`, `Section{level, title, starred}`, `Label`, `Ref{kind, target}` (kind ∈ ref/cref/Cref/eqref/cite), `DisplayOpen`/`DisplayClose`, `NewTheorem{env, chain, label, starred}`, `TheoremStyle`, `BlankLine`, `CommentLine`
- [ ] balanced `{}` for ref arguments (multi-key cite → multiple `Ref` tokens)
- [ ] ignore content inside `verbatim` / `lstlisting` / `comment` envs
- [ ] strip `%` comments but preserve positions
- [ ] create `testdata/sample.tex` (~60 lines) covering: `\documentclass{amsart}`, 3 `\newtheorem` (incl. starred + chained), `\theoremstyle`, 1 section, 1 theorem w/ `\label`, 1 proof (3 paragraphs, 1 align, 1 `\ref`, 1 `\cref`), 1 figure w/ tikzpicture, 1 `\cite`, 1 unresolved ref
- [ ] table-driven tests: token sequence counts + kinds + key fields; edge cases (nested envs, `$$…$$`, multi-key cite, starred section, `\newtheorem*`)
- [ ] `make test && make lint`

### Task 3: Block parser — hierarchy, kinds, auto-discovery, inner-level

**Files:**
- Create: `pkg/parser/block.go`, `pkg/parser/parse.go`, `pkg/parser/parse_test.go`

- [ ] `Kind` enum + `Block` struct per spec (ID/Number/RefsOut added in Task 4/5)
- [ ] `Parse(src []byte) (*Document, error)` with `Document{Blocks, Root, ByLabel, TheoremEnvs}`
- [ ] auto-discover theorem envs from `\newtheorem` tokens; merge with built-in defaults; `proof` is always `KindProof`
- [ ] tree: sections nest subsections; envs nest in current section; proofs are siblings of theorem; `ProofStep` = blank-line split inside `proof`
- [ ] display math inside a proof → child of current `ProofStep`
- [ ] figures → `KindFigure`; bibliography env → single `KindBibliography` wrapper (bbl enrichment Task 5)
- [ ] unrecognized env → `KindOther`
- [ ] tests against `testdata/sample.tex`: kinds, hierarchy, proof step count, env set, section nesting
- [ ] `make test && make lint`

### Task 4: Label/ref resolution + stable block IDs

**Files:**
- Create: `pkg/parser/refs.go`, `pkg/parser/id.go`, `pkg/parser/refs_test.go`, `pkg/parser/id_test.go`
- Modify: `pkg/parser/parse.go`

- [ ] two-pass: (1) collect labels → `Document.ByLabel`; (2) walk each block's source, extract refs, resolve, set `Resolved`
- [ ] `Ref{Kind, Target, LineOffset, ColOffset, Resolved}`
- [ ] stable ID: label if present, else `sha1(kind || parent-label || source[:40])[:8]` with sibling index + title slug prefix
- [ ] proof-steps under labeled proof → dotted IDs (`thm:main.proof.step.1`)
- [ ] tests: resolved/unresolved/multi-key cite/forward refs; ID stability under line shifts
- [ ] `make test && make lint`

### Task 5: .aux parser + bibliography enrichment

**Files:**
- Create: `pkg/parser/aux.go`, `pkg/parser/aux_test.go`, `pkg/parser/bib.go`, `pkg/parser/bib_test.go`
- Create: `testdata/sample.aux`, `testdata/sample.bbl`

- [ ] parse `\newlabel{X}{{num}{page}…}` → populate `Block.Number`
- [ ] simple `\bibitem[key]{key} authors, title…` bbl parser → bib-entry block fields
- [ ] missing `.aux`/`.bbl` → empty fields, not an error
- [ ] tests with synthetic fixtures
- [ ] `make test && make lint`

### Task 6: latexmk runner (strict)

**Files:**
- Create: `pkg/build/build.go`, `pkg/build/build_test.go`

- [ ] `Run(texPath, buildCmd string) (*Result, error)` with `Result{PDFPath, SyncTeXPath, AuxPath, BBLPath, LogPath}`
- [ ] default: `latexmk -pdf -synctex=1 -interaction=nonstopmode -halt-on-error -file-line-error <basename>` in the paper's dir
- [ ] capture combined stdout/stderr + last 40 log lines
- [ ] non-zero exit OR log has `!` OR undefined ref/citation warning after final pass → wrapped error with tail + first error line
- [ ] `ResolveBuildOutputs(texPath) *Result` for `--no-build`
- [ ] tests: mock command (`sh -c …`) + fake `.log` fixtures for error detection
- [ ] `testing.Short()` gates real-latexmk tests
- [ ] `make test && make lint`

### Task 7: SyncTeX parser

**Files:**
- Create: `pkg/synctex/synctex.go`, `pkg/synctex/synctex_test.go`
- Create: `testdata/sample.synctex.gz`

- [ ] parse gzipped SyncTeX v1 → `Index{Files map[int]string, Lines map[string]map[int][]Region}` where `Region{Page, X, Y, W, H}`
- [ ] parse only `Input:`, `{<page>`, `}`, `v`/`h`/`x` records
- [ ] `RegionForLines(file, start, end) *Region` returning union bbox on first containing page
- [ ] generate fixture via `testdata/gen.sh` (runs `pdflatex -synctex=1 sample.tex`)
- [ ] tests: known line → plausible region
- [ ] `make test && make lint`

### Task 8: Sidecar persistence + stale remap

**Sister-project reading (before writing code):**
- `~/__code/revdiff/` — find the annotation sidecar / stdout markdown writer and read it end-to-end. Mirror the markdown shape (heading format, blockquoted source quote, note body).
- Specifically: look for files writing `## ` headings with file/line refs and blockquoted source, and the YAML-frontmatter loader. Note exact conventions before writing `pkg/persist/sidecar.go`.

**Files:**
- Create: `pkg/persist/sidecar.go`, `pkg/persist/remap.go`, `pkg/persist/sidecar_test.go`, `pkg/persist/remap_test.go`

- [ ] `Sidecar{Paper, PDF, Cursor string, Reviewed []string, Annotations []Annotation}` with `Annotation{BlockID, Breadcrumb, File string, StartLine, EndLine int, SourceQuote, Note string}`
- [ ] `Load(path)` — YAML frontmatter + markdown body; missing file → empty sidecar + nil error
- [ ] `Save(path, *Sidecar)` — stable round-trip ordering
- [ ] heading: `` ## <Breadcrumb> — `<BlockID>` (<File>:L<start>-L<end>) `` (LP explicit: include line numbers)
- [ ] source quote as fenced blockquote, ≤6 lines, middle-ellipsis `…`
- [ ] `Remap(old *Sidecar, newDoc) (*Sidecar, []Annotation)` — (1) exact ID, (2) label, (3) Levenshtein similarity ≥0.85 on source quote; unmatched → detached
- [ ] tests: round-trip, stale remap (rename/move/delete/unchanged), line-number format
- [ ] `make test && make lint`

### Task 9: Bubbletea TUI skeleton — three-pane layout

**Sister-project reading (before writing code):**
- `~/__code/revdiff/` — read the bubbletea `Model`, `Update`, `View` files. Note the layout approach (lipgloss `JoinHorizontal`/`JoinVertical`), the status-line conventions, and how the input loop is structured.
- List the specific files you consulted in your task-progress notes before writing `pkg/ui/model.go`.

**Files:**
- Create: `pkg/ui/model.go`, `pkg/ui/update.go`, `pkg/ui/view.go`, `pkg/ui/keys.go`, `pkg/ui/styles.go`, `pkg/ui/model_test.go`
- Modify: `cmd/mreview/main.go`, `go.mod`

- [ ] add deps: `bubbletea`, `lipgloss`, `bubbles`
- [ ] `Model{Doc, Sidecar, CursorBlockID, Filter, Width, Height, Status, JumpStack, Popup}`
- [ ] horizontal split 25/40/35 via `JoinHorizontal` + bottom status line
- [ ] placeholder content per pane; `q` / `Ctrl-C` quit; others no-op
- [ ] wire `main.go`: on successful parse (stub for now), `tea.NewProgram(m).Run()`
- [ ] tests: `Init`, `Update` with `KeyMsg q` → quit cmd; `WindowSizeMsg` updates dims
- [ ] manual: `make build && ./bin/mreview testdata/sample.tex` shows three panes
- [ ] `make test && make lint`

### Task 10: Outline pane + source pane + filter cycling

**Sister-project reading (before writing code):**
- `~/__code/revdiff/` — read the outline/list rendering and filter cycling implementation. Mirror the icon/marker conventions where sensible.

**Files:**
- Create: `pkg/ui/outline.go`, `pkg/ui/source.go`, `pkg/ui/filter.go`, `pkg/ui/outline_test.go`
- Modify: `pkg/ui/model.go`, `pkg/ui/view.go`, `pkg/ui/update.go`

- [ ] outline: depth-indented tree, icons (`§` section, `⊞` thm-like, `⊢` proof, `▤` figure, `≡` display), suffix markers (`●` annotated, `✓` reviewed, `⚠` unresolved, `⊘` no-region); truncation, focused-row style
- [ ] source pane: render `Block.Source` with lightweight LaTeX coloring (commands, math delimiters — no chroma)
- [ ] breadcrumb + `file:Lstart-Lend` in status line
- [ ] filter: `FilterAll|Unreviewed|Annotated|Issues`; `f` cycles; status indicator
- [ ] default filter on load: `Unreviewed` if any reviewed blocks exist, else `All`
- [ ] tests: structural assertions on rendered strings; filter transitions; marker application
- [ ] manual: sample.tex, cycle filters, verify markers
- [ ] `make test && make lint`

### Task 11: Navigation + ref jumping + jump stack

**Files:**
- Create: `pkg/ui/nav.go`, `pkg/ui/nav_test.go`
- Modify: `pkg/ui/update.go`, `pkg/ui/keys.go`

- [ ] `j`/`k` outer sibling; `J`/`K` inner (proof-steps, inner paragraphs, display math)
- [ ] `{`/`}` section; `gg`/`G` first/last (respect filter)
- [ ] motion counts: buffer digits → apply to next motion → reset; `0`-alone resets
- [ ] ref jumping: `go` picks first resolved outgoing ref in current block (MVP); push/pop/redo jump stack (bounded 50)
- [ ] `gu`: list blocks whose `RefsOut` target current block's label (popup with jump)
- [ ] tests: motion counts, stack push/pop/redo bounds, filter-aware motion
- [ ] `make test && make lint`

### Task 12: Annotation entry + reviewed toggle + auto-advance

**Sister-project reading (before writing code):**
- `~/__code/revdiff/` — read the annotation textarea popup implementation. Mirror the `bubbles/textarea` wiring, the `Ctrl-S` / `Esc` / `Ctrl-C` handling, and the save-immediately-on-submit pattern.

**Files:**
- Create: `pkg/ui/annotation.go`, `pkg/ui/annotation_test.go`
- Modify: `pkg/ui/update.go`, `pkg/ui/view.go`, `pkg/ui/model.go`

- [ ] inline textarea popup (`bubbles/textarea`): `a` current block, `A` enclosing env; submit on `Ctrl-S` / `Esc`; `Ctrl-C` cancel
- [ ] on submit: append `persist.Annotation` + `persist.Save` immediately
- [ ] `e` reopens textarea with existing content → replace on submit
- [ ] `d` deletes after `[y/N]` status-line confirmation
- [ ] `space` toggles reviewed; persist; if filter=Unreviewed and now reviewed → auto-advance to next unreviewed
- [ ] breadcrumb generator: `"Proof of Theorem 3.2, step [2]"` from parent chain
- [ ] tests: popup lifecycle, save persists, edit replaces, delete removes, reviewed toggle + auto-advance, breadcrumb format
- [ ] `make test && make lint`

### Task 13: Fuzzy search + annotation list popup

**Sister-project reading (before writing code):**
- `~/__code/revdiff/` — read its fuzzy-search and annotation-list popup if present; otherwise study `bubbles/list` usage in sibling examples.

**Files:**
- Create: `pkg/ui/search.go`, `pkg/ui/annlist.go`, `pkg/ui/search_test.go`

- [ ] `/` fuzzy search over title, label, number, first 200 chars of source (`sahilm/fuzzy`); Enter jumps
- [ ] `@` annotation list (`<breadcrumb> — <first line>`); Enter jumps + focuses; `e`/`d` work in-list
- [ ] both use `bubbles/list`
- [ ] tests: fuzzy ranking, list selection jump
- [ ] `make test && make lint`

### Task 14: Stale-state remap on load + stdout emission on quit

**Sister-project reading (before writing code):**
- `~/__code/revdiff/` — read the stdout-on-quit code path; mirror the markdown shape exactly (it's what downstream LLM prompts depend on).

**Files:**
- Modify: `pkg/ui/model.go`, `cmd/mreview/main.go`
- Create: `pkg/persist/stdout.go`, `pkg/persist/stdout_test.go`

- [ ] on startup: if sidecar exists → `Load` + `Remap`; detached → `## Detached` section + status count + outline marker first-load
- [ ] on `q`: save sidecar, emit per `--stdout` flag
- [ ] `md` → annotations body (no frontmatter); `json` → `[{block_id, breadcrumb, file, start_line, end_line, source_quote, note}]`; `none` → skip
- [ ] tests: stdout md/json format, quit saves before emit
- [ ] `make test && make lint`

### Task 15: PDF rendering + kitty graphics + cursor sync

**Sister-project reading (before writing code — MANDATORY, cannot skip):**
- `~/__code/CLI-PDF-EPUB-reader/document.go` — fitz.Document loading & lifecycle.
- `~/__code/CLI-PDF-EPUB-reader/image.go` — pixmap cache + PNG encode.
- `~/__code/CLI-PDF-EPUB-reader/display.go` — go-termimg kitty-graphics invocation pattern.
- `~/__code/CLI-PDF-EPUB-reader/layout.go` — how the PDF pane is sized and composed in the TUI.
- Note exact DPI handling, cropping math, image placement escape sequences.

**Files:**
- Create: `pkg/pdf/doc.go`, `pkg/pdf/render.go`, `pkg/pdf/display.go`, `pkg/pdf/doc_test.go`
- Modify: `pkg/ui/view.go`, `pkg/ui/update.go`, `pkg/ui/model.go`, `go.mod`

- [ ] deps: `gen2brain/go-fitz`, `blacktop/go-termimg`
- [ ] `doc.go`: `Open(pdfPath) (*Doc, error)` wrapping `fitz.Document`; lazy `(pageIdx, dpi)` pixmap LRU cache; `Close()`
- [ ] `render.go`: `Crop(doc, region, pad) ([]byte pngBytes, error)` — render target page, crop bbox+pad
- [ ] `display.go`: kitty-only via go-termimg; emit escape that places image at terminal cell rect (mirror CLI-PDF-EPUB-reader's approach)
- [ ] on cursor move: compute region, debounce 30ms, render, replace image
- [ ] crop cache keyed by `(block_id, pdf_mtime)`, bound 64
- [ ] no `PDFRegion` → pane shows `"[no region — block outside PDF]"`
- [ ] tests: open fixture PDF, render page, crop region → non-empty decodable PNG
- [ ] manual (kitty): open a real paper, navigate, verify pane follows; note jitter/lag
- [ ] `make test && make lint`

### Task 16: Bibliography popup, help overlay, config file, polish

**Files:**
- Create: `pkg/ui/bib.go`, `pkg/ui/help.go`, `pkg/ui/config.go`, `pkg/ui/config_test.go`
- Modify: various

- [ ] `gd` on `\cite{key}` near cursor → popup w/ bib entry; else status `"gd: no cite under cursor"`
- [ ] `?` help overlay with keybinding table
- [ ] config: TOML at `~/.config/mreview/config.toml` + `./.mreview.toml` (project overrides user). Options: `theorem_envs`, `figure_envs`, `colors.*`, `build_cmd`, `keybinds.*`
- [ ] `--config <path>` override
- [ ] theme env var `MREVIEW_THEME=dark|light`
- [ ] tests: config merge precedence, help rendering, bib popup content
- [ ] manual end-to-end on a real LLM-generated paper: open, walk all blocks, annotate some, mark many reviewed, quit, inspect sidecar, re-open, verify resume-on-first-unreviewed
- [ ] `make test && make lint`; bump version to `0.1.0`

### Task 17: Verify acceptance criteria

- [ ] full test suite green: `make test`
- [ ] linter clean: `make lint`
- [ ] coverage ≥80% on `pkg/parser`, `pkg/persist`, `pkg/synctex`, `pkg/build`
- [ ] `make build` produces a working binary; run it on `testdata/sample.tex` and at least one real paper from `~/local_git/`

### Task 18: Update documentation

- [ ] write/update `README.md` with install + usage + keybindings
- [ ] update `CLAUDE.md` if internal patterns changed
- [ ] move this plan to `docs/plans/completed/`

## Post-completion (manual / not executor-automatable)

- Acceptance test on real papers in `~/local_git/` (kitty terminal required).
- Confirm stdout markdown piped into Claude Code produces usable review prompt.
