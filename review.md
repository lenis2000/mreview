# Deep Review Findings

Date: 2026-04-23

## Scope
- Performed a deeper usability-focused review of startup/build/reload, PDF/OCR integration, persistence, navigation, and search paths.
- Ran `go test ./...`, `go test -race ./...`, and `go vet ./...`.
- Per current prioritization, did not include the known multi-file-paper limitation in the main findings list.

## Findings

### 1) High: No draft-mode startup path; unresolved refs/citations hard-stop the app before the TUI opens
- Severity: High
- Files: [pkg/build/build.go](/Users/leo/__code/mreview/pkg/build/build.go:103), [cmd/mreview/main.go](/Users/leo/__code/mreview/cmd/mreview/main.go:164), [pkg/ui/pdf.go](/Users/leo/__code/mreview/pkg/ui/pdf.go:62), [pkg/ui/model.go](/Users/leo/__code/mreview/pkg/ui/model.go:258)
- Repro summary: Open a paper that still builds a usable PDF but has a final unresolved `\ref` or `\cite` warning in the LaTeX log.
- Why: `scanLogReader` treats undefined references/citations as fatal build failures, `RunWith` returns an error, and `run` exits before launching the TUI.
- Impact: The app is effectively unusable on common draft states unless the user already knows to bypass the build path with `--no-build` and happens to have matching artefacts on disk.
- Suggested fix: add a `--draft` mode that does not stop on build/log errors, opens the TUI anyway, and surfaces the build problem in status text while using whatever artefacts are available. This is the most important product fix from this review pass.

  **Resolution check:** Partially fixed in `8dcd48a`. The new `--draft` flag correctly stops startup from hard-exiting and surfaces the build problem in the status bar. But the response overstates the PDF behavior: on a fresh `--draft` launch there is no previously rendered `PDFImage` to preserve, `Init()` does not seed one, and `schedulePDFRender()` suppresses both cursor-following and manual rendering while `BuildStale` is true. So even when stale PDF/SyncTeX artefacts are opened successfully, the pane cannot actually display them; it falls through to the generic placeholder path instead. The "open the TUI anyway" part is fixed, but the "use whatever artefacts are available" part is only partial.

  **Follow-up fix:** Completed. The `--draft` path no longer sets `BuildStale` unconditionally. A new helper `startupArtefactsStale(texPath, pdfPath, synctexPath)` compares mtimes: `BuildStale` is set only when the on-disk artefacts predate the .tex source (i.e. the user edited after the last successful build — SyncTeX line numbers would not map cleanly). The common `--draft` case is "latexmk completed but the log has an undefined-ref warning": in that case the PDF and SyncTeX written by latexmk are up-to-date with the current .tex, so rendering proceeds normally and the user sees correct crops with the warning in the status bar. When the user has edited the .tex after the last good build, artefacts are older than .tex, the helper reports stale, `BuildStale` is set, and the pane shows the placeholder — avoiding a wrong-region crop. `startupArtefactsStale` has a dedicated regression test (`TestStartupArtefactsStale`) covering the four mtime configurations: artefacts missing, artefacts newer, PDF older than tex, synctex older than tex.

### 2) High: Custom build output directories silently break PDF/SyncTeX/Aux/BBL integration
- Severity: High
- Files: [pkg/build/build.go](/Users/leo/__code/mreview/pkg/build/build.go:114), [cmd/mreview/main.go](/Users/leo/__code/mreview/cmd/mreview/main.go:164), [pkg/ui/reload.go](/Users/leo/__code/mreview/pkg/ui/reload.go:202)
- Repro summary: Use a build command or project config that writes outputs to `build/`, `out/`, etc. instead of next to `paper.tex`.
- Why: `ResolveBuildOutputs` hard-codes adjacent output paths (`paper.pdf`, `paper.synctex.gz`, `paper.aux`, `paper.bbl`, `paper.log`) and both startup and reload logic consume only those paths.
- Impact: The build command can succeed but the app silently loses the PDF pane, SyncTeX following, theorem numbering, bibliography enrichment, and correct rebuild detection. In outdir projects, reload will also tend to rebuild repeatedly because `shouldRebuild` looks for the wrong PDF path.
- Suggested fix: make output-path resolution configurable or derive it from the build command/project config, then thread that through startup and reload instead of assuming side-by-side artefacts.

  **Resolution:** Won't fix. The author does not use custom output directories; all LaTeX projects use the default side-by-side layout. The fix (an `output_dir` config field threaded through startup + reload) is straightforward (~30 lines) but deferred until someone actually needs it.

### 3) Medium: OCR bug reports ignore the stale-build coherence guard
- Severity: Medium
- Files: [pkg/ui/ocr.go](/Users/leo/__code/mreview/pkg/ui/ocr.go:29), [pkg/ui/pdf.go](/Users/leo/__code/mreview/pkg/ui/pdf.go:62)
- Repro summary: Edit the source, trigger a rebuild failure so `BuildStale` becomes true and the PDF pane intentionally freezes on the last known-good crop, then press `B`.
- Why: `schedulePDFRender` correctly suppresses new SyncTeX-based crops while `BuildStale` is true, but `startOCRReport` does not check that flag and still runs the crop pipeline against the new document state plus old PDF/SyncTeX handles.
- Impact: OCR debug reports can be generated for the wrong region or fail with misleading "no PDF region" results exactly in the stale-build state where the app is trying to preserve coherence.
- Suggested fix: block `B` while `BuildStale` is true with a clear status message, mirroring the render scheduler's contract.

  **Resolution check:** Fixed in `8dcd48a`. `startOCRReport` now checks `m.BuildStale` and returns `"B: build is stale — rebuild first (E to edit, then retry)"` before touching the crop pipeline.

## Notes
- Tests and race checks pass, so the above are integration/product-logic issues rather than obvious panics or race failures.
- `TestStartupArtefactsStale` covers the mtime-based decision the `--draft` path now uses; an end-to-end test that exercises the full `run()` path with a deliberately failing build still requires latexmk in `$PATH` and is left out of CI.
- The OCR `BuildStale` guard now has a dedicated regression test (`TestOCRReport_BlockedWhenBuildStale` in `pkg/ui/reload_test.go`) alongside the existing manual-mode guard.
- I also re-read navigation/search/mouse code paths; I did not find another clear "app won't work at all" bug there in this pass.

## Overall
- 3 issues found. #1 fully fixed in `8dcd48a` + follow-up commit (mtime-based BuildStale on `--draft` startup so coherent stale artefacts are actually viewable); #2 is deferred (not applicable to current usage); #3 is fixed in `8dcd48a`.
