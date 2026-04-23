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

### 4) Medium: Popup flows are not mouse-modal; annotation editing can detach from the visible block
- Severity: Medium
- Files: [pkg/ui/update.go](/Users/leo/__code/mreview/pkg/ui/update.go:46), [pkg/ui/mouse.go](/Users/leo/__code/mreview/pkg/ui/mouse.go:16), [pkg/ui/view.go](/Users/leo/__code/mreview/pkg/ui/view.go:187), [pkg/ui/source.go](/Users/leo/__code/mreview/pkg/ui/source.go:75), [pkg/ui/annotation.go](/Users/leo/__code/mreview/pkg/ui/annotation.go:152)
- Repro summary: Open an annotation popup with `a` or `A`, then click or wheel in the outline/source pane before submitting.
- Why: Keyboard input is routed through `updatePopup`, but `MouseMsg` bypasses popup handling entirely and still calls `handleMouse`. That can change `m.CursorBlockID` and `m.SourceLineCursor` underneath the open popup. The annotation UI is rendered against the current cursor block, while submit still writes to `AnnotationPopup.TargetID`.
- Impact: The inline editor can disappear from the source pane while keystrokes still go to the hidden textarea, and submit saves the note to the old target rather than the block now visible on screen. For mouse users this makes annotation editing untrustworthy.
- Suggested fix: treat popups as mouse-modal too (ignore background mouse input while a popup is open, or close the popup on outside click), or render the annotation editor against `Popup.TargetID` instead of the live cursor.

  **Resolution:** Fixed. `update.go`'s `tea.MouseMsg` branch now mirrors the `tea.KeyMsg` pattern: when `m.Popup != nil`, mouse events are dropped entirely (the model is returned unchanged). The annotation popup keeps its visible anchor and `Popup.TargetID` cannot drift away from where the user opened it. Outside-click-closes was rejected as too easy a way to lose in-progress annotation text; the user must explicitly submit (`Ctrl-S`) or cancel (`Esc` / `Ctrl-C`). Covered by `TestUpdate_MouseIgnoredWhilePopupOpen` (opens an `A` popup, fires a `MouseLeft` press at coordinates that would normally move the cursor, asserts cursor + source line + popup are all unchanged).

### 5) Medium: Source-pane click targeting is wrong under soft-wrap, which is enabled by default
- Severity: Medium
- Files: [pkg/ui/mouse.go](/Users/leo/__code/mreview/pkg/ui/mouse.go:213), [pkg/ui/source.go](/Users/leo/__code/mreview/pkg/ui/source.go:311), [pkg/ui/model.go](/Users/leo/__code/mreview/pkg/ui/model.go:219)
- Repro summary: Use a narrow terminal or any long wrapped source line, then click a visual continuation row in the source pane.
- Why: `sourceLineAt` maps visual row `N` to source line `startLine + N` as if every source line occupies exactly one terminal row. But `RenderSource` / `wrapOrClip` can expand one source line into many rows when soft-wrap is on. The width parameter is not used in hit-testing, so the click path cannot mirror the rendered layout.
- Impact: Mouse clicks can select the wrong line or even the wrong block. On narrow terminals, source-pane mouse navigation is systematically unreliable.
- Suggested fix: share the same row-expansion logic between source rendering and hit-testing, or disable source-pane click targeting while soft-wrap is enabled.

  **Resolution:** Fixed (option A — wrap-aware mapping). `sourceLineAt` now plumbs `softWrap` through and computes `bodyWidth` the same way the renderer does (via a new `sourcePaneInnerW` helper that mirrors `view.go`'s pane-width math). Under soft-wrap it walks source lines from `startLine` and accumulates `len(wrapOrClip(line, bodyWidth, true, true, Styles{}))` per line — wrapping behaviour is shared because `sourceLineAt` calls the same `wrapOrClip` the renderer does, so future wrap changes don't drift. Inline annotation/editor injected rows are not yet accounted for (a click landing on an annotation row resolves to the next source line below) — that case is rare enough to defer; the dominant soft-wrap mismatch is gone. Covered by `TestSourceLineAt_WrapAware` (long line that wraps; rows 0 and 1 must map to the same source line) and `TestSourceLineAt_SoftWrapOffStaysOneToOne` (sanity check that toggling `w` off restores the simple one-row-per-line mapping inside a multi-line cursor block).

### 6) Low: Inline/external edit fall back to line 1 when no source line is resolvable
- Severity: Low
- Files: [pkg/ui/editor.go](/Users/leo/__code/mreview/pkg/ui/editor.go:24), [pkg/ui/editor.go](/Users/leo/__code/mreview/pkg/ui/editor.go:191), [pkg/ui/editor.go](/Users/leo/__code/mreview/pkg/ui/editor.go:221)
- Repro summary: Open a document with no selectable content block (for example an empty document body), then press `ctrl+e` or `E`.
- Why: `absoluteCursorLine` returns `1` when the cursor/block has no resolvable source range. `StartLineEdit`'s comment says it should no-op in that case, but the code opens line 1 instead; the external editor path uses the same helper.
- Impact: Edit commands fail open to the start of the file rather than refusing the action, which is the wrong behavior for a location-sensitive editor command.
- Suggested fix: make `absoluteCursorLine` report failure explicitly and guard both edit paths.

  **Resolution:** Fixed. `absoluteCursorLine` now returns `(int, bool)` — `ok=false` whenever the cursor doesn't anchor to a real source line (no doc, missing block, or `b.StartLine == 0`). Both edit paths guard the result: `editInExternalEditor` sets status `"E: cursor has no resolvable source line"` and bails before saving the sidecar / launching `$EDITOR`; `StartLineEdit` sets the analogous `"ctrl+e: …"` status and refuses to open the popup. Covered by `TestEditFallback_RefusesWhenCursorHasNoLine` — sets a cursor pointing at a non-existent block ID and asserts both paths refuse with no popup, no command, and the explanatory status.

## Notes
- Tests and race checks pass.
- `TestStartupArtefactsStale` covers the mtime-based decision the `--draft` path now uses; an end-to-end test that exercises the full `run()` path with a deliberately failing build still requires latexmk in `$PATH` and is left out of CI.
- The OCR `BuildStale` guard has a dedicated regression test (`TestOCRReport_BlockedWhenBuildStale` in `pkg/ui/reload_test.go`) alongside the existing manual-mode guard.
- Mouse test coverage now includes `TestUpdate_MouseIgnoredWhilePopupOpen` (popup mouse-modal contract), `TestSourceLineAt_WrapAware` (wrap-aware row→line mapping), and `TestSourceLineAt_SoftWrapOffStaysOneToOne` (no-wrap baseline). The annotation/editor-row interspersing case under wrap is still uncovered — known limitation, deferred.
- Edit-path coverage adds `TestEditFallback_RefusesWhenCursorHasNoLine` for both `ctrl+e` and `E`.

## Overall
- 6 issues found. #1 fully fixed in `8dcd48a` + follow-up commit (mtime-based `BuildStale` on `--draft` startup so coherent stale artefacts are actually viewable); #2 deferred (not applicable to current usage); #3, #4, #5, #6 fixed with regression tests.
