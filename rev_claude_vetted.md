# Vetted Review Of `rev_claude.md`

Date: 2026-04-27

Scope: this file validates the claims in [rev_claude.md](/Users/leo/__code/mreview/rev_claude.md) against the current workspace. It keeps the findings that hold up, downgrades the ones that were overstated, and records the claims that do not survive direct source inspection.

This is intentionally narrower than [rev_codex.md](/Users/leo/__code/mreview/rev_codex.md): it is a vetting pass on Claude's review, not a fresh full review.

## Validated Findings

### 1. High: SyncTeX basename fallback is nondeterministic on collision

- Files: [pkg/synctex/synctex.go](/Users/leo/__code/mreview/pkg/synctex/synctex.go:323), [pkg/synctex/synctex.go](/Users/leo/__code/mreview/pkg/synctex/synctex.go:344)
- `linesFor` and `TagFor` first try an exact cleaned path match, then fall back to a basename-only match by iterating a Go map.
- When two distinct source files share the same basename, the fallback result depends on randomized map iteration order. That makes the PDF region lookup unstable across runs and can point the cursor-following pane at the wrong file's regions.
- This finding from `rev_claude.md` holds as written.

### 2. Medium: `mreview config` shells through `sh -c` using `$EDITOR` / `$VISUAL`

- Files: [cmd/mreview/config.go](/Users/leo/__code/mreview/cmd/mreview/config.go:130), [cmd/mreview/config.go](/Users/leo/__code/mreview/cmd/mreview/config.go:138), [pkg/ui/editor.go](/Users/leo/__code/mreview/pkg/ui/editor.go:77)
- `runConfig` builds `exec.Command("sh", "-c", editor+" \""+path+"\"")` directly from `$VISUAL` / `$EDITOR`.
- That is a real shell-invocation hazard and an unnecessary inconsistency with the safer editor-launch path already implemented in `pkg/ui/editor.go`.
- I do not agree with the original `Critical` severity. This is a local-environment footgun in a CLI the user runs themselves, not a remote exploit path.

### 3. Medium: `mreview fmt` report writes are non-atomic

- Files: [pkg/format/report.go](/Users/leo/__code/mreview/pkg/format/report.go:40), [pkg/persist/sidecar.go](/Users/leo/__code/mreview/pkg/persist/sidecar.go:115)
- `WriteReport` uses `os.Create`, which truncates the existing report before the new content is fully flushed.
- A crash or write failure in the middle of the rewrite can leave a partial or empty `*.fmt-report.md`, and the UI will later try to consume that file as if it were complete.
- The issue is real. I do not agree with the original `Critical` label; this is a robustness/data-loss window, not guaranteed corruption or code execution.

### 4. Medium: `space.wrap` optional-argument scanner mishandles escaped `\]`

- Files: [pkg/format/rules_wrap.go](/Users/leo/__code/mreview/pkg/format/rules_wrap.go:488)
- The optional-argument scanner for excluded ranges increments and decrements nesting on bare `[` and `]`, but it does not skip escaped closers.
- An escaped `\]` inside a ref-like optional argument can terminate the scan early and expose the remainder of the construct to wrapping logic.
- This is edge-case input, but the claim itself holds.

### 5. Medium: verifier subprocesses lack timeout and cancellation plumbing

- Files: [pkg/format/verify.go](/Users/leo/__code/mreview/pkg/format/verify.go:403), [pkg/format/verify.go](/Users/leo/__code/mreview/pkg/format/verify.go:421), [pkg/format/verify_paranoid.go](/Users/leo/__code/mreview/pkg/format/verify_paranoid.go:23), [pkg/build/build.go](/Users/leo/__code/mreview/pkg/build/build.go:84), [cmd/mreview/fmt.go](/Users/leo/__code/mreview/cmd/mreview/fmt.go:343)
- `pdfinfo`, `pdftotext`, and `diff-pdf` are launched without any per-call timeout.
- `build.RunWith` does support a context, but the main call sites do not pass a cancellable one, so long-running or wedged child processes have no deadline and no structured cancellation path.
- This is the real issue behind several of the original review's signal-handling complaints.

## Rejected Or Reclassified Claims

### 1. Rejected: `mreview fmt --no-verify` leaves stale reports behind

- Original claim: [rev_claude.md](/Users/leo/__code/mreview/rev_claude.md:77)
- Status: false.
- `--no-verify` skips verifier execution, but it does not skip report maintenance. On the write path, `writeReportIfNeeded` still runs at [cmd/mreview/fmt.go](/Users/leo/__code/mreview/cmd/mreview/fmt.go:406), and it removes stale reports when there are no hits or diagnostics at [cmd/mreview/fmt.go](/Users/leo/__code/mreview/cmd/mreview/fmt.go:275).

### 2. Rejected: `isUndefinedRefWarning` misses `Citation '...'` due to case mismatch

- Original claim: [rev_claude.md](/Users/leo/__code/mreview/rev_claude.md:243)
- Status: false.
- The function already matches `Citation \`` on the raw line at [pkg/build/build.go](/Users/leo/__code/mreview/pkg/build/build.go:201) and lowercased `citation '` at [pkg/build/build.go](/Users/leo/__code/mreview/pkg/build/build.go:203). The specific missed-warning scenario described in the original review does not hold.

### 3. Reclassified: the SIGINT / kitty-cleanup claims were too broad

- Original claims: [rev_claude.md](/Users/leo/__code/mreview/rev_claude.md:83), [rev_claude.md](/Users/leo/__code/mreview/rev_claude.md:89)
- Status: partially valid, but overstated.
- The code does not "swallow SIGINT everywhere." Inside the TUI, `ctrl+c` is explicitly a quit key at [pkg/ui/keys.go](/Users/leo/__code/mreview/pkg/ui/keys.go:111) and [pkg/ui/keys.go](/Users/leo/__code/mreview/pkg/ui/keys.go:177).
- The narrower real problem is that quitting does not propagate cancellation into already-running child processes, and abnormal termination paths do not get the same explicit kitty cleanup as a normal `prog.Run()` return.

### 4. Reclassified: several original "High" items are engineering debt, not high-severity defects

- [rev_claude.md](/Users/leo/__code/mreview/rev_claude.md:95) `pkg/pdfreview` test coverage is a valid test gap, but not a high-severity bug by itself.
- [rev_claude.md](/Users/leo/__code/mreview/rev_claude.md:107) duplicate `.bbl` key handling is a reasonable robustness note, but the original review itself admits it is "not a correctness bug per se."
- [rev_claude.md](/Users/leo/__code/mreview/rev_claude.md:115) the hand-maintained line-count-changing rule list is a maintenance risk, not a present user-facing defect.

### 5. Rejected as a bug, retained only as a possible test gap: `prose.tilde-refs` with `%` continuation

- Original claim: [rev_claude.md](/Users/leo/__code/mreview/rev_claude.md:67)
- Status: not verified as a real defect.
- The review frames this as "likely no-op, but worth a regression test," which is not enough to keep it as a validated finding. If someone wants to harden the rule further, this belongs in the test-gap bucket, not in the defect list.

## Summary

- The strongest validated findings from `rev_claude.md` are the SyncTeX basename collision bug, the shell-based editor launch in `mreview config`, the non-atomic fmt report write, the escaped-`\]` parsing gap in `space.wrap`, and the missing timeout/cancellation plumbing around verifier and build subprocesses.
- At least two original findings are plainly false on direct inspection.
- Several others are useful notes, but they were severity-inflated in the original review.
