# mreview

A LaTeX-aware terminal review tool for math papers. Parses `.tex` into semantic blocks
(theorems, proofs, display math, figures, …), auto-follows a rendered PDF pane as the
cursor walks blocks, and lets you attach free-text annotations that emit back as
structured markdown for downstream LLM consumption.

Think `revdiff` for prose, but navigating semantic blocks rather than diff lines.

## Status

MVP (v0.1.0). Single-user, kitty-terminal only (no iTerm2/Sixel fallbacks).

## Install

Requires Go 1.21+, a TeX distribution with `latexmk`, and kitty terminal for PDF
rendering.

```
git clone <repo> mreview
cd mreview
make build        # produces .bin/mreview
```

System deps used by the PDF pane:

- MuPDF (bundled via `gen2brain/go-fitz`)
- kitty graphics protocol (via `blacktop/go-termimg`)

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

## Keybindings

Navigation

```
j / k              next / prev outer sibling
J / K              next / prev inner block (proof-step, display, …)
{ / }              previous / next section
gg / G             first / last visible block
<N><motion>        repeat motion N times
go                 jump to first resolved ref in current block
gu                 list blocks referring to current label
gd                 show bib entry for first \cite in current block
Ctrl-O / Ctrl-I    jump back / forward (bounded stack of 50)
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
Ctrl-C             force quit
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
theorem_envs = ["theorem", "proposition", "lemma", "corollary"]
figure_envs  = ["figure", "figure*"]
build_cmd    = "latexmk -pdf -synctex=1 -interaction=nonstopmode -halt-on-error"
theme        = "dark"

[colors]
# optional palette overrides

[keybinds]
# optional key rebindings
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
cmd/mreview/       entry + CLI flags
pkg/parser/        LaTeX tokenizer, block model, label/ref index, .aux/.bbl
pkg/build/         latexmk runner
pkg/synctex/       .synctex.gz parser: (file, line) -> (page, bbox)
pkg/persist/       sidecar load/save, stale remap, stdout emit
pkg/pdf/           go-fitz crop + go-termimg kitty render
pkg/ui/            bubbletea model/update/view, lipgloss layouts
testdata/          synthetic .tex + .pdf + .synctex.gz fixtures
```
