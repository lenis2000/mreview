# Mreview — LaTeX-aware math paper review TUI

## Overview
- Build `mreview`, a terminal review tool for LLM-generated math papers in LaTeX — like `revdiff` for prose, but navigating semantic blocks (theorems, proofs, display math) rather than diff lines, because there is no diff
- Parses the `.tex`, auto-follows a rendered PDF pane (via SyncTeX + go-fitz + kitty graphics) as the cursor walks blocks, and lets the user attach free-text annotations that emit back to the LLM as structured markdown
- Multi-session: hundreds of blocks per paper, reviewed across many sittings; a sidecar `<paper>.mreview.md` holds annotations, reviewed-checkboxes, and cursor position; next launch resumes on the first unreviewed block
- Ref jumping (`\ref` / `\cref` / `\Cref` / `\eqref` / `\cite`) is a first-class feature, since math proofs constantly cite other results; unresolved refs flagged as review targets

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

### Sibling projects (LP-local, referenced for patterns — do not import)
- `~/__code/revdiff/` — bubbletea TUI for reviewing diffs with annotations, stdout-markdown output. The annotation/output shape here mirrors its design. Uses `charmbracelet/bubbletea` + `lipgloss` + `bubbles`
- `~/__code/CLI-PDF-EPUB-reader/` — PDF viewer in terminal. Uses `gen2brain/go-fitz` (MuPDF) + `blacktop/go-termimg` for kitty graphics, with latexmk-pvc-friendly auto-reload. The `pkg/pdf/` package mirrors its rendering approach (see `image.go`, `document.go` in that repo)
- `~/__code/ralphex/` — plan executor driving this plan. Follows its plan format

### Dependencies to adopt
- `github.com/charmbracelet/bubbletea` — TUI loop
- `github.com/charmbracelet/lipgloss` — styling, layouts
- `github.com/charmbracelet/bubbles` — textarea, list, viewport
- `github.com/gen2brain/go-fitz` — MuPDF bindings, PDF rendering
- `github.com/blacktop/go-termimg` — kitty graphics protocol
- `github.com/sahilm/fuzzy` — outline filter + `/` search

### Target users
- LP (math professor, arXiv author). Reviews LLM-generated drafts of research papers
- Single-user tool, kitty terminal only (no iTerm2/Sixel fallbacks)
- Papers are single-file `.tex` using `amsart`/`article` class with per-paper `\newtheorem` declarations (no `\input`/`\include` recursion required in MVP)
- Example real-world targets for manual testing (do NOT add to testdata):
  - `~/local_git/Colored_interchangeability_Ayyer_Martin_Petrov/June_2025_*.tex`
  - `~/local_git/Perturbed_Beta_Processes/nov2025_perturbed_beta.tex`
  - `~/local_git/Panova-Petrov/2026-01-27-AIHP-D-edits/Sep2024_skew_hook.tex`

## Solution Overview

- LaTeX-aware parser produces an ordered tree of typed `Block`s: `Section`, `Abstract`, `TheoremLike`, `Proof`, `Display`, `Figure`, `Paragraph`, `ProofStep`, `Bibliography`, `Other`
- Theorem-like environments are auto-discovered from `\newtheorem{...}` declarations in the source (not hardcoded); config file can override
- Each block carries `(file, start_line, end_line, source_slice, label?, number?, refs_out[], parent_id?, children_ids[], pdf_region?)`
- Cross-ref index resolves `\ref` / `\cref` / `\Cref` / `\eqref` / `\cite` at parse time; unresolved refs flagged as `⚠` in outline
- Stable block IDs: label if present, else `sha1(kind || parent-label || sibling-index || first-40-chars-of-source)[:10]` — survives line-number drift so session resume works after LP edits the `.tex`
- Build: one `latexmk -pdf -synctex=1 -interaction=nonstopmode` run at startup (strict — if it fails or any `\ref`/`\cite` is unresolved after final pass, tool exits with log tail). Read-only w.r.t. the `.tex`
- SyncTeX: parse `main.synctex.gz` once into an in-memory `(file, line) -> (page, bbox)` map; pre-compute per-block PDF region so cursor moves are O(1)
- UI: three-pane bubbletea TUI — outline (left) + source (middle) + PDF crop (right). PDF pane auto-follows cursor: on block change, crop bbox with padding, rasterize with go-fitz, push to right pane via go-termimg kitty graphics protocol
- Navigation: vim-style — `j`/`k` between envs at outer level, `J`/`K` between paragraphs/proof-steps at inner level, `{`/`}` between sections, `gg`/`G` start/end. Motion counts supported (`5j`, `10J`). `go` on a ref jumps to target; `Ctrl-O`/`Ctrl-I` navigate jump stack. `gd` on `\cite` pops bibliography entry
- Annotation: `a` opens an inline textarea; on submit, note is attached to current block and written to sidecar. `e` edits, `d` deletes. `space` toggles reviewed (✓) — on mark, if filter is `unreviewed`, auto-advances to next unreviewed block
- Outline filter (`f` cycles): `all` / `unreviewed` / `annotated` / `issues`. On resume, default filter is `unreviewed`
- Persistence: `<paper>.mreview.md` with YAML frontmatter (paper, pdf, cursor=block_id, reviewed=[block_ids]) plus annotation sections (breadcrumb, `file:Lstart-Lend` line range, blockquoted source snippet, note). Same markdown is what's written to stdout on `q`
- Stale state on reopen: best-effort remap (label then hash then text similarity). Unmatched annotations land in a "Detached" section at top of sidecar with old breadcrumb preserved
- CLI: `mreview <paper.tex>` (minimal); flags `--no-build`, `--build-cmd`, `--sidecar <path>`, `--stdout md|json|none`, `--config <path>`

## Technical Details

### Block model
```go
type Kind int
const (
    KindSection Kind = iota
    KindAbstract
    KindTheoremLike
    KindProof
    KindDisplay       // \[…\], equation, align, gather, multline, $$…$$, starred variants
    KindFigure        // figure, table, tikzpicture (top-level), center wrapping figure
    KindParagraph
    KindProofStep
    KindBibliography
    KindOther
)

type Block struct {
    ID         string  // stable id (label or hash-derived)
    Kind       Kind
    EnvName    string  // "theorem", "lemma", "genEx", "proof", "align", ...
    File       string
    StartLine  int
    EndLine    int
    Source     string  // raw .tex slice (trimmed comments for structural purposes but full source kept)
    Title      string  // optional \begin{theorem}[Title]... arg, or section heading
    Label      string  // from \label{...}; empty if none
    Number     string  // from .aux \newlabel; e.g. "3.2" for Theorem 3.2
    RefsOut    []Ref   // {kind: ref|cref|Cref|eqref|cite, target: "lem:A", lineOffset, colOffset, resolved bool}
    ParentID   string  // empty at root
    ChildIDs   []string
    PDFRegion  *Region // {Page, X0, Y0, X1, Y1}, nil if synctex missing
}
```

### Parser strategy
- Targeted tokenizer, not full LaTeX parser. Only recognizes: `\begin{X}` / `\end{X}`, `\section*?`, `\subsection*?`, `\subsubsection*?`, `\part*?`, `\chapter*?`, `\label{X}`, `\ref{X}`, `\cref{X}`, `\Cref{X}`, `\eqref{X}`, `\cite[opt]{X,Y,Z}`, display math delimiters (`\[`, `\]`, `$$`), `\newtheorem{X}[chain]{Label}` / `\newtheorem*{X}{Label}` / `\theoremstyle{...}`, paragraph breaks (blank lines at nesting depth 0), and balanced `{}` for argument matching
- Strips `%`-comments for structure detection but preserves full source in `Source` field
- Nesting-aware: tracks `\begin`/`\end` stack; a `Display` inside a `Proof` is a child of the proof-step that contains it, not a top-level block
- `ProofStep` splits only inside `\begin{proof}…\end{proof}` on blank lines; each step is a child of the enclosing proof block

### Auto-discovery
- First pass: scan for `\newtheorem{X}{...}`, `\newtheorem{X}[chain]{...}`, `\newtheorem*{X}{...}` and add each `X` to the `TheoremLike` env set
- Also scan `\theoremstyle{plain|definition|remark}` so outline can hint at the style (purely cosmetic)
- Built-in defaults if no declarations found: `theorem lemma proposition corollary definition remark example conjecture claim observation problem question`
- Config override in `.mreview.toml` (project-local) or `~/.config/mreview/config.toml`:
  ```toml
  [envs]
  theorem_like = ["theorem", "lemma", "myCustomEnv"]
  figure_like = ["figure", "table", "tikzpicture", "center"]
  ```

### Label / ref resolution
- Two passes: first collects all `\label{X}` across the source, building `label -> block_id`. Second walks each block's source, finding refs with their line/column offsets, resolving via the map. Unresolved → `Resolved: false`, flagged in outline as `⚠`
- `\cite` targets go to the bibliography block (either `\begin{thebibliography}` entries or `.bbl` file); each bib entry is its own block with `ID = bib:<key>`
- `.aux` parser reads `\newlabel{X}{{3.2}{14}}` etc. to populate `Number` on labeled blocks (cosmetic — shown as "Theorem 3.2" in outline)

### latexmk runner (strict)
- Command: `latexmk -pdf -synctex=1 -interaction=nonstopmode <file>` (configurable via `--build-cmd`, respects `.latexmkrc`)
- Runs in a temp working dir? No — run in the paper's dir so relative paths resolve
- Exit non-zero OR `.log` contains `!` error markers OR final pass still has unresolved refs → return error with the last ~40 log lines + the first error line number
- Tool prints error and exits; no degraded source-only mode
- `--no-build` flag: skip latexmk, use existing PDF; error out if missing

### SyncTeX
- Parse `.synctex.gz` (gzipped SyncTeX v1 format) with a small native parser. Format is line-oriented ASCII; we only need the `Input:`, `{` `}` (page), and `x` (horizontal box) records for line-to-region mapping
- In-memory structure: `map[file]map[line][](page, x, y, w, h)`. Small (~few MB for a 50-page paper)
- For a block spanning lines `[L1..L2]` in file `f`, region = union of per-line bboxes on the same page; if lines span pages, use first page's slice (cursor can still jump to later pages via PageDown in the PDF pane later — out of MVP scope)

### PDF rendering
- Mirror `CLI-PDF-EPUB-reader/image.go` patterns: load PDF once via `go-fitz`, keep page pixmaps LRU-cached per `(page, dpi)`, crop to block bbox + padding, transcode to PNG bytes, pass to go-termimg
- Kitty graphics protocol only: use go-termimg's kitty backend exclusively; no iTerm2/Sixel fallbacks
- On cursor move in outline/source, recompute current block, query its `PDFRegion`, render, emit. Target <50 ms felt latency. Cache keyed by `(block_id, pdf_mtime)`
- Read-only w.r.t. PDF; no watch loop in MVP (LP rebuilds externally if needed and relaunches)

### Persistence — sidecar format
Path: `<paper>.mreview.md` next to the `.tex`. YAML frontmatter for state, markdown body for annotations.
```markdown
---
paper: nov2025_perturbed_beta.tex
pdf: nov2025_perturbed_beta.pdf
cursor: thm:main.proof.step.2
reviewed:
  - def:beta-process
  - thm:main
  - thm:main.proof.step.1
---

# Annotations

## Theorem 3.2 — `thm:main` (nov2025_perturbed_beta.tex:L412-L428)

> \begin{theorem}\label{thm:main}
> Let $X$ be a compact metric space…
> \end{theorem}

The hypothesis on $X$ should be separable, not just compact — otherwise §4 application fails.

## Proof of Theorem 3.2, step [2] (nov2025_perturbed_beta.tex:L437-L445)

> By Lemma~\ref{lem:A}, we have $\int f \,d\mu = 0$.

`\ref{lem:A}` is unresolved — Lemma A isn't stated. Either add it or cite externally.
```
- Block IDs in `cursor:` and `reviewed:` are stable; section IDs use dotted paths (`thm:main.proof.step.2`)
- Annotation headings always include: human breadcrumb + backtick-quoted block ID + `file:Lstart-Lend` line range (LP explicit request)
- Source quote trimmed to ~6 lines, longer gets `…` middle-ellipsis
- On `q`, rewrite the file; on next launch, re-parse — single source of truth

### Output to stdout
- On `q` (default `--stdout md`), print just the `# Annotations` section of the sidecar to stdout so it can be piped directly into a Claude Code session or LLM prompt
- `--stdout json` emits `[{block_id, breadcrumb, file, start_line, end_line, source_quote, note}]` as a JSON array
- `--stdout none` skips stdout (sidecar is still written)

### TUI layout (lipgloss)
- Horizontal split: outline `25%` | source `40%` | PDF `35%`
- Bottom status line: breadcrumb · `<filter>` · `N/M blocks` · `● K annotations · ✓ R reviewed` · mode indicators
- Popups (modal): annotation textarea (`a`/`e`), annotation list (`@`), bibliography entry (`gd` on `\cite`), help (`?`), fuzzy search (`/`)

### Keybindings
```
Navigation (outline/source):
  j / k          next / prev sibling env at outer level
  J / K          next / prev paragraph or proof-step inside env
  { / }          prev / next section
  gg / G         first / last block
  <N><motion>    vim motion counts (5j, 10J, 3}, 20G)

Ref jumping:
  go             go to target of ref under cursor
  Ctrl-O         pop jump stack
  Ctrl-I         redo (inverse of Ctrl-O)
  gd             show bib entry for \cite{...} under cursor
  gu             "where used" — list all refs pointing to current block

Annotation:
  a              add note to current block
  A              add note to enclosing env (not paragraph)
  e              edit existing note
  d              delete note
  space          toggle reviewed ✓ (auto-advance if filter = unreviewed)

UI:
  /              fuzzy search titles + labels + text
  @              annotation list popup
  f              cycle outline filter (all / unreviewed / annotated / issues)
  ?              help overlay
  q              quit + write sidecar + emit stdout
  Q              quit without save
```

### CLI
- `mreview <paper.tex>` — build + open
- `--no-build` — skip latexmk (use existing PDF + synctex)
- `--build-cmd "<cmd>"` — override `latexmk -pdf -synctex=1 …`
- `--sidecar <path>` — override `<paper>.mreview.md`
- `--stdout md|json|none` — default `md`
- `--config <path>` — load config file (default `~/.config/mreview/config.toml` and/or `./.mreview.toml`)
- `--version` — print version

## Development Approach
- testing approach: regular (code first, then tests within same task)
- complete each task fully before moving to the next
- small focused commits per task
- **CRITICAL: every task MUST include new/updated tests** for code changes in that task
- **CRITICAL: all tests must pass before starting next task** — run `make test` and `make lint`
- do NOT add docs/README changes until Task 16 (polish) — reduces churn
- kitty-only is a firm constraint: if code is tempted to branch on terminal type, remove that branch

## Testing Strategy
- **unit tests** — per package, table-driven with testify. Parser / build / synctex / persist have high coverage (aim 85%); UI coverage is harder but bubbletea models are testable (update-function table tests)
- **fixtures** — `testdata/` contains a tiny synthetic paper (`sample.tex`, `sample.pdf`, `sample.synctex.gz`, `sample.aux`) with 1 section, 2 theorems, 1 proof with 3 steps, 1 align, 1 figure, 1 unresolved ref. Generate once via `testdata/gen.sh` (commit outputs)
- **manual verification** — after each UI task, open a real paper from `~/local_git/` and walk through. Note issues as `⚠️` in progress tracking
- **no e2e test harness in MVP** — kitty graphics can't be snapshot-tested easily; manual verification is the gate

## Progress Tracking
- mark completed items with `[x]` immediately when done
- add newly discovered tasks with ➕ prefix
- document issues/blockers with ⚠️ prefix

## Implementation Steps

### Task 1: CLI skeleton + project Makefile wiring

**Files:**
- Modify: `cmd/mreview/main.go`
- Modify: `go.mod`
- Create: `cmd/mreview/main_test.go`

- [ ] switch `cmd/mreview/main.go` to use `github.com/jessevdk/go-flags` for CLI parsing (follows ralphex convention)
- [ ] define `Opts` struct with fields: `File string (positional)`, `NoBuild bool`, `BuildCmd string`, `Sidecar string`, `Stdout string`, `Config string`, `Version bool`
- [ ] `--version` prints a version string and exits 0
- [ ] missing positional `<file>` → usage to stderr + exit 2
- [ ] non-existent file path → error to stderr + exit 1
- [ ] existing file → exits with `mreview: not implemented yet` (placeholder for now)
- [ ] `go mod tidy` to pull go-flags
- [ ] write tests in `main_test.go` covering: `--version`, missing arg, missing file, existing file placeholder
- [ ] run `make build`, `make test`, `make lint` — all must pass

### Task 2: LaTeX tokenizer

**Files:**
- Create: `pkg/parser/tokenizer.go`
- Create: `pkg/parser/tokenizer_test.go`
- Create: `testdata/sample.tex`

- [ ] implement `Tokenize(src []byte) []Token` returning stream of typed tokens: `BeginEnv{name, line, col}`, `EndEnv{name, line, col}`, `Section{level, title, line, col, starred bool}`, `Label{name, line, col}`, `Ref{kind, target, line, col}` (kind ∈ ref, cref, Cref, eqref, cite), `DisplayOpen{delim, line, col}` / `DisplayClose{delim, line, col}`, `NewTheorem{env, chain, label, starred bool, line}`, `TheoremStyle{style, line}`, `BlankLine{line}`, `CommentLine{line}` (% strip, for structural purposes)
- [ ] handle balanced `{}` for ref arguments (multi-key cite: `\cite{A,B,C}` → three `Ref` tokens)
- [ ] ignore content inside `verbatim`, `lstlisting`, `comment` envs (only treat them as opaque ranges)
- [ ] strip `%` comments but preserve their line so position reports stay accurate
- [ ] create `testdata/sample.tex` with ~60 lines covering: `\documentclass{amsart}`, 3 `\newtheorem` declarations (incl. starred and chained), `\theoremstyle`, 1 section, 1 theorem (with `\label`), 1 proof (3 paragraphs, containing 1 `\begin{align}` and 1 `\ref` + 1 `\cref`), 1 figure with tikzpicture, 1 `\cite`, 1 unresolved ref
- [ ] write table-driven tests: tokenize sample.tex, assert expected token sequence (count + kinds + key fields)
- [ ] tests for edge cases: nested envs, `$$…$$` display math, multi-key cite, starred section, `\newtheorem*`
- [ ] run `make test`, `make lint` — must pass

### Task 3: Block parser — hierarchy, kinds, auto-discovery, inner-level

**Files:**
- Create: `pkg/parser/block.go`
- Create: `pkg/parser/parse.go`
- Create: `pkg/parser/parse_test.go`

- [ ] define `Kind` enum and `Block` struct per Technical Details (without `ID`, `Number`, `RefsOut` — added in Task 4/5)
- [ ] implement `Parse(src []byte) (*Document, error)` where `Document` has `Blocks []*Block`, `Root *Block` (virtual), `ByLabel map[string]*Block` (populated in Task 4), `TheoremEnvs map[string]bool`
- [ ] auto-discover theorem envs from `\newtheorem` / `\newtheorem*` tokens; merge with built-in defaults (ensure `proof` always recognized separately as `KindProof`)
- [ ] build block tree: sections nest subsections, envs nest inside the current section, proofs are siblings of their theorem (not children — LaTeX-standard), `ProofStep` children generated by splitting proof contents on `BlankLine` tokens at depth 0 inside the proof
- [ ] display math inside a proof becomes a child `KindDisplay` of the current `ProofStep`
- [ ] figures (`figure`, `table`, top-level `tikzpicture`, `center` containing a figure) → `KindFigure`
- [ ] bibliography (`thebibliography` or detection of `\bibliography{...}` command) → single `KindBibliography` block with one child per bib entry (in Task 5 we enrich from `.bbl`; in this task just create the wrapper block if env present)
- [ ] unrecognized `\begin{X}` → `KindOther`
- [ ] write tests against `testdata/sample.tex`: assert block kinds, hierarchy, proof step count, auto-discovered env set, section nesting
- [ ] run `make test`, `make lint` — must pass

### Task 4: Label/ref resolution + stable block IDs

**Files:**
- Create: `pkg/parser/refs.go`
- Create: `pkg/parser/id.go`
- Create: `pkg/parser/refs_test.go`
- Create: `pkg/parser/id_test.go`
- Modify: `pkg/parser/parse.go`

- [ ] two-pass post-processing in `Parse`: (1) collect all labels, populate `Document.ByLabel`; (2) walk each block, extract refs from its source slice, resolve targets, set `Block.RefsOut[].Resolved` bool
- [ ] `Ref` struct: `{Kind string (ref|cref|Cref|eqref|cite), Target string, LineOffset, ColOffset int, Resolved bool}`
- [ ] implement stable ID: `Block.Label` if non-empty (prefixed with kind shortname: `thm:main` stays `thm:main`, but sections get `sec:foo`, etc. based on label), otherwise `fmt.Sprintf("%s:%s:%s:%d:%s", kindShort, parentID, hashPrefix, siblingIndex, titleSlug)` where `hashPrefix` = first 8 hex chars of `sha1(kind || parent-label || source[:40])`
- [ ] proof-steps under a labeled proof: IDs like `thm:main.proof.step.1` (dotted)
- [ ] tests: `refs_test.go` covers resolved + unresolved + multi-key cite + forward refs; `id_test.go` asserts stability under line shifts (shift all blocks down by 5 lines, IDs must stay equal)
- [ ] run `make test`, `make lint` — must pass

### Task 5: .aux parser for numbers + bibliography enrichment

**Files:**
- Create: `pkg/parser/aux.go`
- Create: `pkg/parser/aux_test.go`
- Create: `pkg/parser/bib.go`
- Create: `pkg/parser/bib_test.go`
- Create: `testdata/sample.aux`
- Create: `testdata/sample.bbl`

- [ ] parse `\newlabel{X}{{num}{page}…}` lines from a `.aux`-format file; populate `Block.Number` via the `ByLabel` map
- [ ] bbl parser: simple `\bibitem[key]{key} authors, title…` detection; populate bib-entry blocks with `Title`, author line, year (best-effort regex)
- [ ] if no `.aux` / `.bbl` exists, leave `Number` / bib fields empty — not an error
- [ ] tests with synthetic `sample.aux` and `sample.bbl` fixtures
- [ ] run `make test`, `make lint` — must pass

### Task 6: latexmk runner (strict)

**Files:**
- Create: `pkg/build/build.go`
- Create: `pkg/build/build_test.go`

- [ ] `func Run(texPath, buildCmd string) (*Result, error)` where `Result` has `PDFPath, SyncTeXPath, AuxPath, BBLPath, LogPath string`
- [ ] default command: `latexmk -pdf -synctex=1 -interaction=nonstopmode -halt-on-error -file-line-error <basename>`
- [ ] run in the paper's directory (use `exec.Cmd.Dir`)
- [ ] capture combined stdout/stderr + the last 40 lines of `.log`
- [ ] on non-zero exit: return wrapped error with log tail + first error line ("`./foo.tex:42: Undefined control sequence.`")
- [ ] post-compile sanity: re-parse `.log` for `LaTeX Warning: Reference ... on page ... undefined` or `Citation ... undefined` → also return error with the undefined refs listed
- [ ] `func ResolveBuildOutputs(texPath string) *Result` that locates PDF/synctex/aux/bbl next to the tex without running anything (for `--no-build`)
- [ ] tests: use a mock command (`sh -c 'exit 0'` for success, etc.) and a fake `.log` file in `testdata/` to verify error detection paths
- [ ] skip real-latexmk tests in CI via `testing.Short()` check — run them locally only
- [ ] run `make test`, `make lint` — must pass

### Task 7: SyncTeX parser

**Files:**
- Create: `pkg/synctex/synctex.go`
- Create: `pkg/synctex/synctex_test.go`
- Create: `testdata/sample.synctex.gz`

- [ ] parse gzipped SyncTeX v1 format into `Index` type: `{Files map[int]string, Lines map[string]map[int][]Region}` where `Region = {Page int, X, Y, W, H float64}` (units: TeX `sp` at input, converted to PDF bp/pt for downstream cropping)
- [ ] only need `Input:`, `{<page>` `}`, `v`/`h`/`x` records — ignore kern/glue unless they're the easiest way to get bboxes; start with page-level bbox via the outermost box on each page for a given line
- [ ] `(idx *Index) RegionForLines(file string, startLine, endLine int) *Region` returning union bbox on the first page that contains any of the lines (pages split handled in later task)
- [ ] generate `testdata/sample.synctex.gz` via `testdata/gen.sh` (runs `pdflatex -synctex=1 sample.tex`)
- [ ] tests: load fixture, assert a known line maps to a plausible region
- [ ] run `make test`, `make lint` — must pass

### Task 8: Sidecar persistence

**Files:**
- Create: `pkg/persist/sidecar.go`
- Create: `pkg/persist/remap.go`
- Create: `pkg/persist/sidecar_test.go`
- Create: `pkg/persist/remap_test.go`

- [ ] define `Sidecar` struct: `{Paper, PDF string, Cursor string, Reviewed []string, Annotations []Annotation}` where `Annotation = {BlockID string, Breadcrumb string, File string, StartLine, EndLine int, SourceQuote string, Note string}`
- [ ] `Load(path string) (*Sidecar, error)` — YAML frontmatter + markdown body; no file returns empty sidecar + nil error
- [ ] `Save(path string, s *Sidecar) error` — round-trip stable ordering
- [ ] annotation header format: `## <Breadcrumb> — \`<BlockID>\` (<File>:L<start>-L<end>)` (LP explicit: include line numbers)
- [ ] source quote as fenced blockquote, trimmed to 6 lines with middle-ellipsis `…` if longer
- [ ] remap: `Remap(old *Sidecar, newDoc *parser.Document) (remapped *Sidecar, detached []Annotation)` — match annotations by (1) exact block ID, (2) label if the old ID was label-based, (3) text similarity (Levenshtein on source quote, threshold 0.85). Unmatched → `detached`; add a "# Detached annotations" section to the sidecar output
- [ ] tests: round-trip, stale remap scenarios (renamed label, moved section, deleted block, unchanged block), line-number format
- [ ] run `make test`, `make lint` — must pass

### Task 9: Bubbletea TUI skeleton — three-pane layout

**Files:**
- Create: `pkg/ui/model.go`
- Create: `pkg/ui/update.go`
- Create: `pkg/ui/view.go`
- Create: `pkg/ui/keys.go`
- Create: `pkg/ui/styles.go`
- Create: `pkg/ui/model_test.go`
- Modify: `cmd/mreview/main.go`
- Modify: `go.mod`

- [ ] add deps: `github.com/charmbracelet/bubbletea`, `lipgloss`, `bubbles`
- [ ] define `Model` struct: `{Doc *parser.Document, Sidecar *persist.Sidecar, CursorBlockID string, Filter FilterMode, Width, Height int, Status string, JumpStack []string, Popup PopupState}`
- [ ] layout: horizontal split 25/40/35 via lipgloss `JoinHorizontal` — outline, source, pdf panes + status line at bottom
- [ ] render placeholder content in each pane (hardcoded strings); outline lists block titles; source shows selected block source; PDF pane shows "[no pdf yet]"
- [ ] keys: `q` quits, `Ctrl-C` quits; others no-op for now
- [ ] wire `main.go`: after successful parse (stub — Task 10 wires real parse result), create model and `tea.NewProgram(m).Run()`
- [ ] tests: model `Init`, `Update` with `KeyMsg{Type: KeyRunes, Runes: ['q']}` returns quit cmd; `WindowSizeMsg` updates dimensions
- [ ] manual verification: `make build && .bin/mreview testdata/sample.tex` shows three panes (with placeholder content)
- [ ] run `make test`, `make lint` — must pass

### Task 10: Outline pane + source pane + filter cycling

**Files:**
- Create: `pkg/ui/outline.go`
- Create: `pkg/ui/source.go`
- Create: `pkg/ui/filter.go`
- Create: `pkg/ui/outline_test.go`
- Modify: `pkg/ui/model.go`, `pkg/ui/view.go`, `pkg/ui/update.go`

- [ ] outline rendering: depth-indented tree with icons (`§` section, `⊞` theorem-like, `⊢` proof, `▤` figure, `≡` display, etc.); suffix markers `●` annotated, `✓` reviewed, `⚠` unresolved refs, `⊘` no pdf region
- [ ] truncate long titles to pane width; lipgloss styles for focused row
- [ ] source pane: render `Block.Source` for cursor block with lipgloss syntax-ish coloring (LaTeX commands in one color, math dollars in another — lightweight, no chroma in MVP)
- [ ] show block breadcrumb + file:line range in status line
- [ ] filter: `FilterAll`, `FilterUnreviewed`, `FilterAnnotated`, `FilterIssues`; `f` cycles. Outline skips blocks not matching filter. Indicator in status line
- [ ] on load from sidecar, default filter is `FilterUnreviewed` if any blocks are reviewed; else `FilterAll`
- [ ] tests: outline rendering snapshot-ish (structural assertions on rendered string tokens), filter transitions, marker application
- [ ] manual verification: open sample.tex, filter cycles, markers render
- [ ] run `make test`, `make lint` — must pass

### Task 11: Navigation + ref jumping + jump stack

**Files:**
- Create: `pkg/ui/nav.go`
- Create: `pkg/ui/nav_test.go`
- Modify: `pkg/ui/update.go`, `pkg/ui/keys.go`

- [ ] navigation: `j`/`k` next/prev sibling at "outer" level (Sections + TheoremLike siblings within section + Proofs — everything that's not ProofStep/Display/Paragraph); `J`/`K` next/prev inner-level (children of current outer block — proof-steps, inner paragraphs, display math)
- [ ] `{`/`}` prev/next section; `gg`/`G` first/last block (respecting filter)
- [ ] vim motion counts: intercept digit keys as `pendingCount`, apply to next motion, reset. `0`-prefix edge case: `0` alone = go to line-start (noop here) or reset; use the latter
- [ ] ref jumping: compute the ref under cursor based on source pane's cursor column (which for now is block-start + column offset of first unresolved ref; proper cursor-under-ref comes after source pane gets line/col tracking — acceptable MVP: `go` picks the first resolved outgoing ref in current block)
- [ ] jump stack: push `{fromBlockID, fromScrollOffset}` on `go`; `Ctrl-O` pops; `Ctrl-I` redoes. Bounded to 50 entries
- [ ] `gu` — list blocks whose `RefsOut` target current block's label (popup with jump)
- [ ] tests: motion counts (5j traverses 5 siblings), jump stack push/pop/redo bounds, filter-aware motion (skipped blocks)
- [ ] run `make test`, `make lint` — must pass

### Task 12: Annotation entry + reviewed toggle + auto-advance

**Files:**
- Create: `pkg/ui/annotation.go`
- Create: `pkg/ui/annotation_test.go`
- Modify: `pkg/ui/update.go`, `pkg/ui/view.go`, `pkg/ui/model.go`

- [ ] inline textarea popup (use `bubbles/textarea`): `a` opens for current block, `A` for enclosing env; submit on `Ctrl-S` or `Esc` → save; `Ctrl-C` → cancel
- [ ] save path: append `persist.Annotation` to `Sidecar.Annotations`; call `persist.Save` immediately so crash loses nothing
- [ ] `e` — if current block has annotation, reopen textarea with existing content; on submit replace
- [ ] `d` — delete annotation after a confirmation prompt (`[y/N]` line in status)
- [ ] `space` — toggle reviewed: add/remove from `Sidecar.Reviewed`; persist; if filter is `FilterUnreviewed` and block is now reviewed, auto-advance to next unreviewed block
- [ ] breadcrumb generator: `"Proof of Theorem 3.2, step [2]"` style — walk parent chain, emit human-readable path
- [ ] tests: textarea popup lifecycle, save persists, edit replaces, delete removes, reviewed toggle + auto-advance, breadcrumb formatting
- [ ] run `make test`, `make lint` — must pass

### Task 13: Fuzzy search + annotation list popup

**Files:**
- Create: `pkg/ui/search.go`
- Create: `pkg/ui/annlist.go`
- Create: `pkg/ui/search_test.go`

- [ ] `/` opens fuzzy search: query ranks blocks by title, label, number, first 200 chars of source (use `sahilm/fuzzy`); Enter jumps
- [ ] `@` opens annotation list: rows = `<breadcrumb> — <note first line>`; Enter jumps to block + focuses annotation; `e`/`d` work in-list
- [ ] both popups use `bubbles/list`
- [ ] tests: fuzzy ranking on known queries, list selection jump
- [ ] run `make test`, `make lint` — must pass

### Task 14: Stale-state remap on load + stdout emission on quit

**Files:**
- Modify: `pkg/ui/model.go` (load wiring)
- Modify: `cmd/mreview/main.go` (stdout emission)
- Create: `pkg/persist/stdout.go`
- Create: `pkg/persist/stdout_test.go`

- [ ] on startup: if sidecar exists, `Load` then `Remap(oldSidecar, newDoc)`; detached annotations rendered into a `## Detached` section at top of sidecar; show count in status line + outline marker on first load
- [ ] on quit (`q`): save sidecar, then emit `# Annotations` body to stdout per `--stdout` flag (md / json / none)
- [ ] markdown stdout exactly mirrors sidecar body (no frontmatter)
- [ ] json stdout: `[{block_id, breadcrumb, file, start_line, end_line, source_quote, note}]`
- [ ] tests: stdout format for md + json, quit flow saves before emitting
- [ ] run `make test`, `make lint` — must pass

### Task 15: PDF rendering + kitty graphics + cursor sync

**Files:**
- Create: `pkg/pdf/doc.go`
- Create: `pkg/pdf/render.go`
- Create: `pkg/pdf/display.go`
- Create: `pkg/pdf/doc_test.go`
- Modify: `pkg/ui/view.go`, `pkg/ui/update.go`, `pkg/ui/model.go`
- Modify: `go.mod`

- [ ] add deps: `github.com/gen2brain/go-fitz`, `github.com/blacktop/go-termimg`
- [ ] `pkg/pdf/doc.go`: `Open(pdfPath string) (*Doc, error)` wraps `fitz.Document`; lazy page pixmap cache keyed by `(pageIdx, dpi)`; `Close()`
- [ ] `pkg/pdf/render.go`: `Crop(doc *Doc, region synctex.Region, pad float64) (pngBytes []byte, err error)` — render the target page at suitable DPI, crop to bbox+padding
- [ ] `pkg/pdf/display.go`: kitty-only protocol emitter — use go-termimg's kitty backend directly (no auto-detect). Emit an escape sequence that places the image at a given terminal cell rect
- [ ] in model: on cursor move, compute new region, debounce 30ms, render crop, replace image in PDF pane
- [ ] cache crop PNGs keyed by `(block_id, pdf_mtime)`; eviction bound = 64 entries
- [ ] block without `PDFRegion` → pane shows "[no region — block outside PDF]"
- [ ] tests: `pkg/pdf/doc_test.go` opens a fixture PDF, renders a page, crops a region; assert PNG is non-empty and decodes
- [ ] manual verification (kitty only): open a real paper, navigate, PDF pane auto-follows; `⚠️` note any jitter or lag
- [ ] run `make test`, `make lint` — must pass

### Task 16: Bibliography popup, help overlay, config file, polish

**Files:**
- Create: `pkg/ui/bib.go`
- Create: `pkg/ui/help.go`
- Create: `pkg/ui/config.go`
- Create: `pkg/ui/config_test.go`
- Modify: various

- [ ] `gd` on `\cite{key}` (detected in block source near cursor) opens popup with bib entry (authors/title/year from `.bbl` parse); outside a cite → show status "gd: no cite under cursor"
- [ ] `?` help overlay: render keybindings table with section headers
- [ ] config loader: TOML file at `~/.config/mreview/config.toml` and/or `./.mreview.toml` (project-level overrides user-level). Options: `theorem_envs`, `figure_envs`, `colors.*`, `build_cmd`, `keybinds.*` (optional remap)
- [ ] accept `--config <path>` CLI flag; if missing falls back to default locations
- [ ] basic color theme (dim, bright, muted) with env var override `MREVIEW_THEME=dark|light`
- [ ] tests: config merge precedence, help rendering, bib popup content
- [ ] manual verification on a real LLM-generated paper end-to-end: open, walk every block, annotate a few, mark many reviewed, quit, inspect sidecar, re-open, confirm resume-on-first-unreviewed works
- [ ] run `make test`, `make lint` — must pass; bump version to `0.1.0` in `cmd/mreview/main.go`
