# CLAUDE.md

## Semantic diff review

- `cmd/mreview/diff.go` implements `mreview diff`. It supports `--base REV path.tex` and explicit `OLD NEW` endpoints.
- `pkg/diffreview` owns endpoint resolution/materialization, semantic alignment, pair IDs, diff sidecars, and stdout emit.
- `pkg/diffui` owns the diff Bubble Tea model, outline/source/PDF rendering, new-only editing, Zed comparison, and PDF reload behavior.
- Git blob endpoints are read with `git show` and materialized under `.mreview-diff/`. Do not checkout, switch branches, commit, push, or mutate git refs from diff mode.
- Editing must only write `Review.New.Path`, and only when `--allow-modifications` is set and the new endpoint is a real filesystem file.
- `MREVIEW_COMPARE_EDITOR` is for old+new comparison (`Z` and `--open-zed`); `$EDITOR` is for `E` editing of the new file.
- Focused test command: `go test ./pkg/diffreview ./pkg/diffui ./cmd/mreview`.
