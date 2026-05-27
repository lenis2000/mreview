# mreview diff

`mreview diff` is the semantic before/after review mode for LaTeX papers. It
compares parsed LaTeX blocks instead of raw git hunks, shows old and new source
side by side, follows the new PDF when available, and saves review annotations
to a diff sidecar.

## Primary workflow

Run from the branch or working tree you want to review and edit:

```bash
mreview diff --base master --open-zed --allow-modifications paper.tex
```

This means:

- old endpoint: `master:<repo-relative path to paper.tex>`
- new endpoint: working-tree `paper.tex`
- old source: materialized under `.mreview-diff/` as a read-only snapshot
- new source: the file shown in the new pane and, when allowed, the only file
  edited by `e` or `E`
- Zed comparison: opened once at startup because `--open-zed` was supplied

Use `--no-build` when a PDF, SyncTeX, aux, and bbl already exist and you do not
want `mreview diff` to run latexmk:

```bash
mreview diff --base master --no-build --open-zed --allow-modifications paper.tex
```

Use `--draft` to open the TUI with a warning when the new PDF build fails. The
diff source panes and annotations still work; the PDF pane may be unavailable
until a successful rebuild.

## Flags

```text
--base REV          old endpoint is REV:<repo-relative path>, new is path
--no-build          skip latexmk for the new endpoint
--draft             open the TUI even when the new build fails
--build-cmd CMD     override latexmk invocation for the new endpoint
--sidecar PATH      diff sidecar path
--stdout FMT        md | json | none (default: md)
--config PATH       config file
--noconfig          ignore config files
--open-zed          open old+new comparison once after startup
--allow-modifications  enable e/E edits to the new endpoint only
```

## Endpoint syntax

There are two command forms.

The `--base` shorthand compares a git revision against a working-tree file:

```bash
mreview diff --base REV paper.tex
```

The old endpoint is resolved as `REV:<repo-relative paper.tex>`. The new
endpoint is resolved as the filesystem path `paper.tex`.

The explicit form accepts two endpoints:

```bash
mreview diff OLD NEW
mreview diff master:paper.tex paper.tex
mreview diff master:paper.tex aop-submission-prep:paper.tex
```

Endpoint specs are:

- filesystem path: read from disk; editable only when it is the new endpoint
- `REV:path`: read with `git show REV:path`; materialized read-only under
  `.mreview-diff/`

`mreview diff` never checks out branches, switches branches, commits, pushes,
or mutates git refs. It may read git objects, but git state is left alone.

## Editing rules

Diff review starts in read-only comparison mode. Annotations, reviewed toggles,
navigation, filtering, rebuilds, and Zed comparison are still available.

`--allow-modifications` enables edit commands only when the new endpoint is a
real filesystem path:

- `E` opens only the new file in the configured editor.
- `e` inline-edits only the selected line in the new file.
- The old endpoint is never opened by `E` and never written by `e`.
- Deleted-only rows cannot be edited because they have no new source block.
- A new endpoint like `branch:paper.tex` is read-only, even with
  `--allow-modifications`.

After an edit, the new file is reread, reparsed, realigned against the unchanged
old source, and the diff sidecar is remapped so reviewed state and annotations
stay attached where possible.

When the new endpoint is read-only and you need to edit it, switch to the branch
or worktree that owns the file and use the `--base` form:

```bash
git switch branch-to-edit
mreview diff --base master --allow-modifications paper.tex
```

## Zed comparison

Press `Z` in the diff TUI to open the old snapshot and new file in Zed:

```bash
zed <old-snapshot> <new-file>
```

`--open-zed` runs the same action once after startup. The old snapshot is a
stable file under `.mreview-diff/`, so Zed can keep it open after the command
returns.

The compare editor is resolved in this order:

- `MREVIEW_COMPARE_EDITOR`
- `zed`

There is no fallback to `$EDITOR` for comparison. If neither command is
available, the TUI shows a status message and keeps running.

For matched rows, mreview passes line-suffixed paths when supported by the Zed
helper. For added or deleted rows, it opens both files and chooses the nearest
useful line on the side that has no corresponding block.

Zed comparison is not the `E` edit command. If you edit the new file from Zed,
use `B` in the TUI to rebuild and reload in this first implementation.

## Sidecars and stdout

Diff sidecars default to:

```text
<new-file>.mreview-diff.<safe-base-label>.md
```

For example:

```text
paper.tex.mreview-diff.master.md
```

Override the path with `--sidecar PATH`.

The sidecar records the old and new specs, cursor pair ID, reviewed pair IDs,
annotations keyed by stable pair IDs, and source quotes from the old or new
side as appropriate. If an annotation no longer maps after later source edits,
it is preserved as detached instead of being discarded.

Sidecar frontmatter and JSON output use these top-level fields:

```text
old_spec
old_label
new_spec
new_path
cursor_pair_id
reviewed
pairs [{ id, status }]
annotations
detached
```

Each annotation entry uses:

```text
pair_id
status
side
file
start_line
end_line
source_quote
note
```

On quit, mreview saves the sidecar and emits according to `--stdout`:

```text
--stdout md      markdown review output, the default
--stdout json    JSON review output
--stdout none    save sidecar only
```

## TUI behavior

The default diff layout is:

```text
Outline | Old source | New source | PDF(new)
```

On narrower terminals, old and new source can be combined into a single source
diff pane. The PDF pane follows the selected new block. Deleted-only rows show:

```text
(deleted block — no new PDF location)
```

Outline rows are semantic block pairs:

```text
~    changed matched block
+    added block
-    deleted block
≡    unchanged block
fmt  raw source changed, normalized source equal
↷    moved or changed block matched by label/ID across sections
```

The default filter is `changed`. Press `f` to cycle:

```text
all / changed / unreviewed / annotated / issues
```

Common diff-mode keys:

```text
j/k, 10j/5k      navigate semantic pairs
J/K               jump 10 down / 5 up pairs
m                 toggle semantic vs coalesced rewrite outline mode
{/}               previous/next section
[/]               select previous/next new source line
space             toggle reviewed and auto-advance in changed/unreviewed filters
a                 annotate current pair
ctrl+a            edit current annotation
d                 delete annotation with confirmation
e                 inline edit new file only, when allowed
E                 open new file only, when allowed
u                 undo last diff-mode edit to the new file
ctrl+r            redo undone diff-mode edit
B                 rebuild/reload the new endpoint
Z                 open old snapshot and new file in Zed
?                 help
q                 quit, save sidecar, emit stdout
```

## Limitations

- `mreview diff` does not check out, switch, commit, push, or mutate git state.
- The old endpoint is a read-only source snapshot for the session.
- Only the new endpoint PDF is built and rendered in the first version.
- Old PDF comparison is not implemented.
- Editing requires the new endpoint to be a real filesystem path.
- A `REV:path` new endpoint is read-only even with `--allow-modifications`.
- Source/PDF watching is limited in the first version; after external edits,
  use `B` to rebuild and reload.
- Matching uses normalized text internally, but displayed source and edit line
  numbers refer to the original raw files.

## Troubleshooting

If `e` or `E` says edits are disabled, rerun with:

```bash
mreview diff --base master --allow-modifications paper.tex
```

If the TUI says the new endpoint is read-only, the new endpoint is probably a
git object such as `branch:paper.tex`. Run from the branch or worktree you want
to edit and compare against the old revision with `--base`.

If the PDF pane is stale or unavailable, use `B` to rebuild/reload the new
endpoint. If the project already has build artifacts and TeX is not installed
in the current environment, use `--no-build`.

If `Z` cannot open Zed, install the `zed` CLI or point comparison at another
command for smoke testing:

```bash
MREVIEW_COMPARE_EDITOR=/bin/true mreview diff --base master --open-zed --no-build paper.tex
```
