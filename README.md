# mreview

> **PRs welcome.** This is a single-developer tool built and tested on the
> author's exact setup (see [Supported environment](#supported-environment)
> below). Anyone wanting to make it work on iTerm2, WezTerm, Linux, Windows,
> non-kitty graphics protocols, alternative PDF backends, etc. — please open
> an issue or send a pull request. The architecture has hooks for
> alternative renderers (see `pkg/pdf/`); they just haven't been written yet
> because the author only uses one terminal.

A LaTeX-aware terminal review tool for math papers. Parses `.tex` into semantic
blocks (theorems, proofs, display math, figures, …), auto-follows a rendered PDF
pane as the cursor walks blocks, and lets you attach free-text annotations that
emit back as structured markdown for downstream LLM consumption.

Inspired by [umputun/revdiff](https://github.com/umputun/revdiff) — same
TUI-driven, annotation-emitting review philosophy, but navigating semantic
LaTeX blocks rather than diff hunks.

## Status

MVP. Single-user, opinionated, no compatibility guarantees yet.

## Supported environment

mreview is **only known to work on**:

- **macOS** (Apple Silicon, recent versions).
- **kitty terminal** — required for the PDF pane, which uses the
  [kitty graphics protocol](https://sw.kovidgoyal.net/kitty/graphics-protocol/)
  via [`blacktop/go-termimg`](https://github.com/blacktop/go-termimg). No
  iTerm2 inline-image, no Sixel, no fallback ASCII renderer.
- **Skim.app** — the `S` (forward-search) and `R` (reload PDF) keys shell out
  to Skim's `displayline` SyncTeX helper. macOS-only.
- **TeX Live with `latexmk`** — for builds (`--no-build` skips this if you
  have pre-built `.pdf` / `.synctex.gz` artefacts).
- The author's external `lmkf` continuous-build wrapper is what feeds fresh
  PDFs to mreview during a session — without it, you'll need to lean on the
  `B` (manual rebuild) key or the built-in latexmk fallback.

Other terminals run the outline / source / status panes fine but show a
placeholder where the PDF crop should be. No graceful degradation work has
been done.

## Install

Requires Go 1.21+ and a TeX distribution with `latexmk` (unless running with
`--no-build` against pre-built artefacts). kitty terminal is required for the
PDF pane; other panes work in any terminal (the PDF pane shows a placeholder
elsewhere).

```
git clone <repo> mreview
cd mreview
make build        # produces .bin/mreview
```

System deps used by the PDF pane:

- MuPDF (bundled via `gen2brain/go-fitz`)
- kitty graphics protocol (via `blacktop/go-termimg`)

Optional deps for `mreview fmt` verification:

- `pdftotext` and `pdfinfo` (from poppler-utils) — required for text-layer verification
- `diff-pdf` — required only for paranoid pixel-level verification (`--verify-pdf=visual`).
  Build with `go build -tags=pdfverify` to enable.

  Install `diff-pdf`:

  ```
  # macOS
  brew install diff-pdf

  # Debian / Ubuntu
  sudo apt-get install diff-pdf

  # From source: https://github.com/vslavik/diff-pdf
  ```

## Usage

```
mreview [OPTIONS] paper.tex
```

On startup mreview:

1. Parses `paper.tex` into a block tree.
2. Loads `paper.tex.mreview.md` if present and remaps annotations to current blocks
   (detached annotations are preserved under a `## Detached` section).
3. Renders the three-pane TUI: outline (25%) / source (40%) / PDF crop (35%).
4. On quit, saves the sidecar and emits annotations to stdout in the format selected
   by `--stdout`.

### Flags

```
--no-build          skip latexmk; use existing .pdf / .synctex.gz / .aux / .bbl
--build-cmd CMD     override latexmk invocation
--sidecar PATH      sidecar path (default: <paper>.mreview.md)
--stdout FMT        md | json | none (default: md)
--config PATH       config file (default: ~/.config/mreview/config.toml, then ./.mreview.toml)
-v, --version       print version and exit
```

### Piping to an LLM

```
mreview paper.tex > review.md
# or
mreview --stdout json paper.tex | jq .
```

## Source normalization (`mreview fmt`)

`mreview fmt` rewrites a paper's `.tex` source to normalize whitespace, fix
common formatting issues, and run diagnostics — while verifying the rendered
PDF is preserved. Modeled on `gofmt` / `cargo fmt`: source rewrites are
git-visible, never a hidden side effect.

```
mreview fmt paper.tex                      # default: Tier 1 + Tier 2, paranoid verify, write report
mreview fmt --diff paper.tex               # show unified diff, no write
mreview fmt --print paper.tex              # print formatted source to stdout, no write
mreview fmt --check paper.tex              # exit 1 if changes needed (CI)
mreview fmt --stdin < paper.tex            # read from stdin, write formatted to stdout
mreview fmt --fail-on-change paper.tex     # format in place AND exit 1 when changed (CI/pre-commit)
mreview fmt --summary a.tex b.tex          # scan only; print rewrite count to stderr
mreview fmt --lines=42:120 paper.tex       # format only lines 42–120 (1-based, inclusive)
mreview fmt --rule=math.paragraph-suppress paper.tex  # one rule only
mreview fmt --no-verify paper.tex          # one-off: skip PDF verification
mreview fmt --no-report paper.tex          # one-off: do not write paper.tex.fmt-report.md
mreview fmt --allow-dirty paper.tex        # bypass dirty-tree check
mreview fmt --clean-tempdir                # remove verification tempdirs
mreview fmt a.tex b.tex c.tex              # multi-file
```

Refuses to overwrite a dirty working tree by default (safety net is `git diff`
/ `git checkout`). Pass `--allow-dirty` to override.

`--lines` automatically disables rules that change line counts (e.g.
`space.blank-runs`, `space.wrap`, `space.item-per-line`) since the per-line
replay cannot preserve out-of-range bytes when line counts shift. A note is
printed to stderr for each skipped rule.

Persistent behaviour (PDF-fix on/off, verifier mode, indent style with per-env
overrides, wrap mode and column, tilde-before-refs commands, math column
alignment, custom verbatim envs, …) lives in `~/.config/mreview/config.toml`
or a project-local `.mreview.toml` walked up from the cwd to the git root.
The CLI surface stays small on purpose: only one-off escape hatches
(`--no-verify`, `--no-report`) and per-invocation modes (`--diff`, `--print`,
`--check`, `--rule`) are exposed as flags. Run `mreview config` to open the
global config in `$EDITOR` (auto-creates a starter file). See
[`config.example.toml`](config.example.toml) for every available option.

### CI integration

See [docs/install/pre-commit.md](docs/install/pre-commit.md) for pre-commit
hook and GitHub Actions setup.

### Rule tiers

**Tier 1 — Safe (PDF byte-identical):**

| ID | What it does |
|---|---|
| `space.trailing` | Strip trailing whitespace per line |
| `space.blank-runs` | Collapse 3+ consecutive blank lines to one blank line |
| `space.tabs` | Tabs → 4 spaces outside protected regions |
| `display.style` | `$$…$$` → `\[…\]` |
| `space.item-per-line` | Ensure `\item` starts on its own line |
| `space.proof-delim-per-line` | Ensure `\begin{proof}` / `\end{proof}` start on own lines |
| `space.display-delim-per-line` | Ensure display-math delimiters start on own lines |
| `space.indent` | Normalize indentation inside environments (configurable per env) |
| `space.wrap` | Sentence-aware line wrapping at target column |
| `math.align-columns` | Align `&` columns in align/tabular/matrix environments |
| `math.continuation-indent` | Indent continuation rows in equation environments past the relation operator |
| `math.wrap-at-break-op` | Wrap long equation rows at break operators (opt-in, off by default) |

**Tier 2 — PDF-fixing (on by default; opt out via `[fmt] no_pdf_fix = true`):**

| ID | What it does |
|---|---|
| `math.paragraph-suppress` | Remove blank lines around display-math envs that cause unwanted paragraph breaks/indentation |
| `env.spacing` | Ensure one blank line above theorem-like envs and section commands |
| `prose.tilde-refs` | Insert `~` (non-breaking space) before `\cite`, `\ref`, and related commands |

**Tier 3 — Diagnostics (no rewrite; emitted to report and `issues` filter):**

| ID | What it checks |
|---|---|
| `lint.ref-undefined` | `\ref{X}` with no matching `\label{X}` |
| `lint.label-unused` | `\label{X}` referenced nowhere |
| `lint.label-duplicate` | Same `\label{X}` declared twice |
| `lint.ref-should-eqref` | `\ref` targeting display math (suggest `\eqref`) |
| `lint.cite-undefined` | `\cite{X}` not in `.bbl` |
| `lint.thm-unlabeled` | Theorem-like block with no `\label` |
| `lint.thm-orphan-proof` | Proof not preceded by a theorem-like block |
| `lint.thm-no-proof` | Theorem with no proof in next 5 sibling blocks |
| `lint.todo-marker` | `\colorbox{…}{\parbox{…}{…}}` TODO patterns |
| `lint.block-too-long` | Paragraph block exceeding 40 source lines |

### Verification

The verifier rebuilds before/after PDFs in an isolated tempdir and compares
`pdftotext` output (whitespace-normalized). Tier-2 rules declare expected-diff
regions via synctex source-line mapping; diffs outside those regions cause
refusal. The default `[fmt] verify_pdf = "visual"` also runs a pixel-level
`diff-pdf` comparison; set `verify_pdf = "text"` in config to skip the pixel
pass, or pass `--no-verify` for a one-off skip of verification entirely.

### Report file

By default `mreview fmt` writes `paper.tex.fmt-report.md` listing all rewrites,
diagnostics, and verifier warnings. The mreview review UI loads this file
automatically and surfaces diagnostics in the `issues` filter. Pass
`--no-report` to suppress it.

## Keybindings

Navigation

```
j / k (↓ / ↑)      next / prev outer sibling
J / K              next / prev inner block (proof-step, display, …)
{ / }              previous / next section
gg / G             first / last visible block
<N><motion>        repeat motion N times
go                 jump to first resolved ref in current block
gu                 list blocks referring to current label
gd                 show bib entry for first \cite in current block
Ctrl-O / Ctrl-I    jump back / forward (bounded stack of 50; Tab also jumps forward)
```

Annotation

```
a                  annotate current block
A                  annotate enclosing env
e                  edit existing annotation
d                  delete annotation (y/N confirm)
space              toggle reviewed (auto-advance on Unreviewed filter)
```

UI

```
/                  fuzzy search
@                  annotation list
f                  cycle filter (all / unreviewed / annotated / issues)
?                  toggle help overlay
q                  quit (saves sidecar, emits to stdout)
Ctrl-C             quit (same as q)
```

## Sidecar format

`<paper>.mreview.md` holds YAML frontmatter (paper, pdf, cursor, reviewed block IDs)
plus one markdown section per annotation:

```
## <Breadcrumb> — `<BlockID>` (<file>:L<start>-L<end>)
> <source quote, ≤6 lines, middle-ellipsised>

<free-text note>
```

Stale-state remap on load uses (1) exact ID, (2) label, (3) Levenshtein ≥0.85 on the
source quote. Unmatched annotations land in `## Detached`.

## Config

TOML at `~/.config/mreview/config.toml` (user) and `./.mreview.toml` (project; wins).
`--config PATH` replaces both layers. Environment: `MREVIEW_THEME=dark|light`
overrides the configured theme.

```toml
theme     = "dark"
build_cmd = "latexmk -pdf -synctex=1 -interaction=nonstopmode -halt-on-error"

# theorem_envs, figure_envs, [colors], and [keybinds] are parsed but not yet
# applied (reserved for a future release). Only `theme` and `build_cmd` are
# honored today.
```

## Development

```
make test          # go test -cover ./...
make lint          # golangci-lint run (falls back to go vet)
make build         # builds .bin/mreview
make fmt           # gofmt + goimports
```

Module layout:

```
cmd/mreview/       entry + CLI flags (including fmt subcommand)
pkg/parser/        LaTeX tokenizer, block model, label/ref index, .aux/.bbl
pkg/format/        fmt pipeline: rules, verifier, report writer
pkg/build/         latexmk runner
pkg/synctex/       .synctex.gz parser: (file, line) -> (page, bbox)
pkg/persist/       sidecar load/save, stale remap, stdout emit
pkg/pdf/           go-fitz crop + go-termimg kitty render
pkg/ui/            bubbletea model/update/view, lipgloss layouts
testdata/          synthetic .tex + .pdf + .synctex.gz fixtures
```
