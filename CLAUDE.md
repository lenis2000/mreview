# CLAUDE.md

> ## ⚠️ DEPRECATED — work in mrevdiff instead
>
> This repo is deprecated. The semantic diff-review TUI described below was
> peeled into a standalone, actively maintained repo:
> **`/Users/leo/__code/mrevdiff`** (public, MIT — `github.com/lenis2000/mrevdiff`).
>
> **If you (or the user) launched a session here by accident, switch to
> `../mrevdiff`.** Both halves have moved: the diff-review TUI is
> `mrevdiff`, and the LaTeX formatter/linter is now `mrevdiff fmt`
> (`pkg/format` + `cmd/mrevdiff/fmt.go` in that repo). Do not implement
> new features here — all new work lands in mrevdiff.

## Semantic diff review (moved to mrevdiff — kept for historical reference)

- `cmd/mreview/diff.go` implements `mreview diff`. It supports `--base REV path.tex` and explicit `OLD NEW` endpoints.
- `pkg/diffreview` owns endpoint resolution/materialization, semantic alignment, pair IDs, diff sidecars, and stdout emit.
- `pkg/diffui` owns the diff Bubble Tea model, outline/source/PDF rendering, new-only editing, Zed comparison, and PDF reload behavior.
- Git blob endpoints are read with `git show` and materialized under `.mreview-diff/`. Do not checkout, switch branches, commit, push, or mutate git refs from diff mode.
- Editing must only write `Review.New.Path`, and only when `--allow-modifications` is set and the new endpoint is a real filesystem file.
- `MREVIEW_COMPARE_EDITOR` is for old+new comparison (`Z` and `--open-zed`); `$EDITOR` is for `E` editing of the new file.
- Focused test command: `go test ./pkg/diffreview ./pkg/diffui ./cmd/mreview`.
