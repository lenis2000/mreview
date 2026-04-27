# Codex Review Findings

Date: 2026-04-27

## Findings

### 1. High: Custom build output directories break every downstream artifact consumer

- Files: [pkg/build/build.go](/Users/leo/__code/mreview/pkg/build/build.go:132), [cmd/mreview/main.go](/Users/leo/__code/mreview/cmd/mreview/main.go:202), [pkg/ui/reload.go](/Users/leo/__code/mreview/pkg/ui/reload.go:211)
- `build.RunWith` lets the user override the build command, but the rest of the application still assumes that `paper.pdf`, `paper.synctex.gz`, `paper.aux`, `paper.bbl`, and `paper.log` live next to `paper.tex`.
- That assumption is baked into startup, reload, stale-artifact detection, sidecar metadata, PDF opening, SyncTeX opening, and aux/bbl enrichment. A build command that writes to `build/`, `out/`, or any latexmk `-outdir` layout can succeed while the TUI silently loses the PDF pane, cursor-following, theorem numbering, bibliography resolution, and correct rebuild detection.
- This is not just a missing convenience feature: the CLI advertises `--build-cmd` as a generic override, but the rest of the program cannot consume the outputs of many valid overrides.

### 2. Medium: `mreview fmt` verifier cannot bootstrap projects with out-of-tree inputs unless a prior `.fls` already exists

- Files: [pkg/format/verify.go](/Users/leo/__code/mreview/pkg/format/verify.go:476), [pkg/format/verify.go](/Users/leo/__code/mreview/pkg/format/verify.go:487), [pkg/format/verify.go](/Users/leo/__code/mreview/pkg/format/verify.go:637)
- The verifier correctly uses `.fls` when it exists, but the fallback path is only `filepath.Walk(dir)` over the paper's own directory.
- That means a first verification run on a project that depends on `../figures`, a sibling `shared/` input tree, or any other out-of-tree asset will build an incomplete temp tree even though the real paper builds fine.
- In practice this turns verification into a false failure mode on common LaTeX layouts: the user has to pre-build once to materialize `.fls`, or disable verification, before `mreview fmt` can succeed.

### 3. Medium: `pdf-comments` strips valid anchors because its validator contradicts its own prompt contract

- Files: [cmd/mreview/pdf_comments.go](/Users/leo/__code/mreview/cmd/mreview/pdf_comments.go:148), [cmd/mreview/pdf_comments.go](/Users/leo/__code/mreview/cmd/mreview/pdf_comments.go:159), [cmd/mreview/pdf_comments.go](/Users/leo/__code/mreview/cmd/mreview/pdf_comments.go:366)
- The prompt explicitly allows the model to collapse runs of spaces, tabs, and newlines when producing `quote` / `quote_focus`.
- The post-processing validator then checks both fields with raw `strings.Contains` against the unnormalized `pdftotext -layout` page text. Any anchor that is semantically valid under the prompt but differs only in whitespace gets blanked out and, for `quote`, downgraded to low confidence.
- The result is systematic loss of highlight anchors for multiline prose, padded layout text, and many equation-adjacent snippets even when the model followed the instructions exactly.

### 4. Medium: `pdf-review` rewrites private review files as `0644` and uses shared temp names

- Files: [pkg/pdfreview/store.go](/Users/leo/__code/mreview/pkg/pdfreview/store.go:44), [pkg/pdfreview/store.go](/Users/leo/__code/mreview/pkg/pdfreview/store.go:52), [pkg/pdfreview/update.go](/Users/leo/__code/mreview/pkg/pdfreview/update.go:382), [pkg/persist/sidecar.go](/Users/leo/__code/mreview/pkg/persist/sidecar.go:120)
- Anchored comment JSON can contain `meta` items and other reviewer-private material, but `SaveReport` always rewrites through `path + ".tmp"` with mode `0644`, and `writeLetter` does the same for the rendered letter.
- That means a user who intentionally tightened an existing file to `0600` will silently have it widened again on the next save. The codebase already avoids this exact problem for sidecars by preserving mode bits and using unique temp files.
- The fixed `*.tmp` names also make concurrent viewer sessions race with each other in a way that the sidecar writer explicitly avoids.

## Validation

- Ran `go test ./...`
- Ran `go vet ./...`
- Ran `go test ./... -cover`
- `go test -race ./...` was not a reliable signal in this environment because `cmd/mreview` test setup hit local git/SSH signing prompts while creating fixture commits.
