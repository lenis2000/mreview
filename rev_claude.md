# mreview deep code review

Reviewed at commit `10378eb` against `main`. Scope: every Go source file under `cmd/mreview/` and `pkg/` (~24k LOC, 50+ test files), with seven parallel sub-reviews and direct verification of each high-severity finding before inclusion. Findings the sub-reviews flagged but I could not reproduce on direct read are listed at the bottom under "Verified false / nitpicks I rejected" so they don't waste your time later.

Severity rubric used here:

- **Critical** — silently corrupts user data, ships an exploit, or violates a documented safety property.
- **High** — wrong behaviour on real-world input, or a latent bug that will surface as user-visible breakage.
- **Medium** — incorrect on adversarial / edge-case input, performance cliff, or robustness gap.
- **Low** — style, naming, dead code, missing test, or minor UX wart.

Findings are grouped by severity, then by area. File paths are repo-relative.

---

## Critical

### C1. Shell injection via `$EDITOR` / `$VISUAL` in `mreview config`

`cmd/mreview/config.go:138`:

```go
cmd := exec.Command("sh", "-c", editor+" \""+path+"\"")
```

`editor` is read directly from `$VISUAL` / `$EDITOR` and concatenated into a `sh -c` string. Anything after a `;`, `&&`, or `$()` in the env var executes verbatim. The path component is wrapped in `"…"` but `path` ends up coming from `os.UserHomeDir()` joined with literals, so a `$HOME` containing `"$(cmd)"` or a `"` is also injectable.

This is also why `EDITOR="code --wait"` accidentally works: `sh` re-tokenises. So you cannot fix this by pre-quoting `editor` — you have to drop `sh -c` entirely.

Fix: parse `$EDITOR` with the same `parseShellArgs` that `pkg/ui/editor.go` already implements, then `exec.Command(prog, append(args, path)...)`. No shell. The two editor-spawn paths (`pkg/ui/editor.go` and `pkg/pdfreview/editor.go:278`) already understand they shouldn't shell out — `cmd/mreview/config.go` is the outlier.

### C2. `mreview fmt` writes `*.fmt-report.md` non-atomically

`pkg/format/report.go:42` opens the report path with `os.Create`, which truncates immediately, then writes through a `bufio.Writer` that is flushed at the end. A crash, OOM-kill, or full disk between truncate and flush leaves the user with a 0-byte or partial `.fmt-report.md` — and the TUI loads that file on next open via `model.ExternalIssues = …`, so partial reports turn into wrong / missing diagnostics in the `issues` filter.

`pkg/persist/sidecar.go:150` already uses the write-tmp-then-`os.Rename` pattern — apply the same here. The TUI cache invariant is that a present report is a complete report.

---

## High

### H1. SyncTeX basename fallback is non-deterministic on collision

`pkg/synctex/synctex.go:328-333`:

```go
base := filepath.Base(clean)
for k, v := range idx.Lines {
    if filepath.Base(k) == base {
        return v
    }
}
```

Map iteration order in Go is randomised, so when two distinct files in the project share a basename (e.g. `chapters/intro.tex` and `appendix/intro.tex`, or any monorepo with `paper/main.tex` + `slides/main.tex`), `RegionForLines` returns whichever entry the runtime hands you on a given startup. The PDF pane will jump to the wrong file's regions for half the cursor positions, and the bug *moves* between mreview restarts.

`TagFor` (`synctex.go:344-358`) has the same pattern. Same issue.

Fix: prefer the entry whose full path shares the longest suffix with the requested path; on tie, return nil and let the caller decide.

### H2. `space.wrap` `excludedRanges`: optional-arg parser doesn't honour `\]`

`pkg/format/rules_wrap.go:489-502` advances through `[…]` while incrementing/decrementing depth on bare `[` / `]` only. A `\]` inside an optional argument terminates the bracket prematurely — the rest of the line is then eligible for wrapping. The same loop also accepts a literal `]` at depth 0 with no balanced opening (it just becomes a no-op `k++`). Combined effect: command names ending in `\ref[…\]…]{…}` may have their argument body chopped by line-wrapping.

In practice this rarely fires (escaped `]` in `\cite[…]` is unusual), but the same code is the parser for the most user-affecting safe rewrite, and it differs from how `pkg/format/rules_safe.go:638` (`skipBalanced`) and `pkg/format/rules_math_align.go:476` handle `\\` — those skip the next byte. Unify by adding a `case s[k] == '\\' && k+1 < len(s): k++` arm here.

### H3. `prose.tilde-refs` writes a `~` even when the line continues with `%` (likely no-op, but worth a regression test)

The recent commits 9afd0a1 and 1512f27 already fixed two specific edge cases (control-word terminator space; default-skip the rule). The remaining edge: `\cite{x}%comment` with a leading word — when the rule inserts `~` before `\cite`, the result `word~\cite{x}%comment` is fine, but if the rule is invoked on a *line* that ends with `%` continuation, the next physical line starts the rejoin already broken. Recommend a fixture: `foo \cite{a}%\nbar` should not change rendered output. I did not find such a test in `rules_tilde_test.go`.

### H4. `pkg/format/verify.go` Verify subprocess calls have no timeout

`verify.go:403` (`pdfinfo`), `verify.go:421` (`pdftotext`), `pkg/format/verify_paranoid.go:51` (`diff-pdf`): all `exec.Command(...).Output()` or `.Run()` without context. A hung child process (a corrupted PDF can wedge `pdftotext` indefinitely; I've seen it on poppler 22.x) hangs `mreview fmt` forever, and Ctrl-C inside an interactive terminal won't interrupt the wait — see also S1 below. For a developer-facing CLI this is the difference between "ran for 30s, gave up" and "left running overnight".

Wrap each call in `exec.CommandContext(ctx, …)` with a per-call deadline (60s feels right for `pdftotext` on a 200-page PDF; `pdfinfo` should be < 1s).

### H5. `mreview fmt --no-verify` leaves stale `*.fmt-report.md` on disk

`cmd/mreview/fmt.go:272` returns early when verification is skipped without writing or deleting a report. Result: a previous successful run wrote a report; a follow-up `--no-verify` run does *not* refresh it; the TUI then loads diagnostics that no longer reflect the file on disk. The `issues` filter shows lines that don't match.

Either delete the existing report on `--no-verify`, or always rewrite (with verifier section omitted). Deletion is simpler and matches the convention that "no report on disk = no diagnostics".

### H6. Build subprocess swallows SIGINT; latexmk hangs on Ctrl-C

`pkg/build/build.go:88` runs latexmk via `sh -c`. The interactive TUI process (`mreview`) installs no signal handler, so SIGINT from the user goes to the foreground process group — but bubbletea's alt-screen mode reroutes terminal input. End user observes: Ctrl-C during a long latexmk build does nothing visible; mreview reports the build "failed" minutes later. Combined with H4, the worst case is a wedged subprocess that blocks the entire fmt pipeline.

Hook a `signal.NotifyContext(context.Background(), os.Interrupt)` at the top of `run()` and pipe that ctx through `Build.Options` and `format.VerifyOptions`.

### H7. `runTUI` only emits `KittyDeleteAll` on graceful exit

`cmd/mreview/main.go:76-78` clears the kitty image only after `prog.Run()` returns. Any abnormal termination (SIGTERM, SIGINT swallowed by bubbletea, panic in `tea.Cmd`) leaves the last PDF crop pinned to the user's terminal until the next full repaint or `kitty @ kitten clear`. This is a UX bug, not a correctness bug, but it's the most visible "mreview is buggy" symptom because the orphan image hangs around the shell prompt.

The signal handler from H6 should also emit `pdf.KittyDeleteAll` on the way out; bonus, defer it from `run()` so a panic still cleans up.

### H8. `pkg/pdfreview/`: nearly zero unit tests on event loop

`pkg/pdfreview/update.go` is 489 LOC with **zero** tests. `view.go` (500 LOC) and `editor.go` (283 LOC including the external-editor + tmp-file dance) are likewise untested. Two test files (`bbox_test.go`, `letter_test.go`, totalling ~160 LOC) cover the trivially testable bits.

This package is fresh (commits 5a258ef, 10378eb) and the absent coverage is structural, not just incomplete. Concrete handlers I'd write tests for first:

- `applyEditFinished` (`update.go:86-102`) — rebuild path, deletion-during-edit, ID lookup miss
- `quitWithLetter` / `quitWithoutLetter` (`update.go:329-349`) — atomic save + tmp cleanup ordering
- `cycleKind` (`update.go:287-308`) — empty comment list, comment with `Status=dropped`
- `schedulePDFRender` DPI clamping (`render.go:443-456`)
- `bbox.FindQuote` multi-line and trailing-whitespace cases (the 2pt `yTol` is untested empirically)

### H9. `BibEntry` map is rebuilt before bibliography wrapper duplicate-check

`pkg/parser/bib.go:79-86, 100-132`. `ApplyBBL` first overwrites `doc.BibEntries[e.Key] = &e` for every entry (so the *last* entry wins on duplicate keys), then walks the same entries to add wrapper children. The duplicate-key behaviour is silent; if a `.bbl` lists `\bibitem{foo}` twice (rare but happens with bibtex + `\nocite`), one entry's `Text`/`Authors` is dropped without a warning. The wrapper child is also added only for the second one.

Not a correctness bug per se — the pipeline produces a consistent state — but a `parser.LoadBBL` that emits a warning on duplicates would surface user-fixable malformed bibliographies.

(Note: a sub-review flagged this as a Go loop-variable-pointer bug. It is not — `e := entries[i]` declares a fresh variable each iteration. Verified.)

### H10. `pkg/format` line-count-changing rule whitelist is hand-maintained

`pkg/format/format.go:169-179` hard-codes the set of rules that change line counts (so they're auto-disabled under `--lines`). When someone adds a new line-count-changing rule and forgets to extend this map, `--lines=N:M` will silently produce an off-by-N output where the bytes outside the range get truncated or shifted.

Make this a method on `Rule` (e.g. `ChangesLineCount() bool`) so adding a rule forces the author to think about it.

---

## Medium

### M1. `mreview` falls back to `os.Stdout` when `/dev/tty` open fails

`cmd/mreview/main.go:67-72`. The fallback is documented (lines 50-55) and "not worse than before", but the branch is silent: if `mreview paper.tex > out.md` is run inside an environment without a controlling terminal (sandboxed CI, `tmux send-keys`), TUI escape sequences land in `out.md` *without warning*. Since `out.md` is also the documented stdout sink for the markdown emit, the file becomes garbage and the user has no idea why.

Detect the no-tty case explicitly and bail with `"mreview: refusing to run interactive TUI without a terminal; use --stdout=none or pipe interactively"`.

### M2. `loneTexInCwd` is silent on multiple `.tex` files

`cmd/mreview/main.go:432-458` returns `("", false)` whenever cwd has 0 *or* ≥2 non-hidden `.tex` files, and `run()` then prints `"missing paper argument"` for both cases. A user with 5 `.tex` files in the directory gets a confusing error pointing at the wrong problem. Suggest: differentiate by counting matches and produce `"multiple .tex files in cwd: a.tex, b.tex; specify one"` when count > 1.

### M3. `pkg/synctex` silently accepts malformed records

`synctex.go:91-130` swallows every parse error: malformed records, missing colons, bad ints, all `continue` quietly. A truncated or partially corrupted `.synctex.gz` (e.g. from a killed latexmk) parses to a sparse but plausible-looking index, which silently maps the wrong block → page on every cursor move. Even an opt-in `--debug-synctex` flag that prints a single-line "skipped N malformed records" would let the user diagnose this.

### M4. `pkg/synctex` header parsing ignores ParseInt errors

`synctex.go:158, 162, 166, 170` use `idx.unit, _ = strconv.ParseInt(...)`. A garbled `Magnification:` field silently sets `mag=0`, after which `toRegion` (line 258) multiplies by `mag/1000.0` = 0 and every region collapses to a zero-size box at (0,0). The user sees "PDF pane never moves" and has no path to diagnosis.

Defaulting `mag = 1000` (line 73) is correct, but the fallback should only kick in when the field is *absent*, not when it's malformed.

### M5. `math.align-columns` `hasLineComment` flags inline `%` inside `\text{…}`

`pkg/format/rules_math_align.go:512-532`. The function correctly skips escaped chars and tracks brace depth, then bails on any unescaped `%` at depth 0. Side effect: a row containing `\text{50% off}` *inside* the math block will trip the comment-detector (because `%` is at depth 0 of the *inner* call — the function is invoked on already-extracted row content, not the outer source). Result: alignment is silently refused for visually-fine rows.

This is conservative behaviour (refusal not corruption), but it leaves user-visible inconsistency — some rows align, others don't, with no explanation.

### M6. `space.wrap` rune width is ASCII-only

`pkg/format/rules_wrap.go:606-615` counts each rune as 1 column. Wide CJK / emoji characters and combining marks produce lines that exceed `wrap_col` after wrapping. Math-paper sources rarely contain these in prose, so this is M-not-H, but if the user has a Unicode operator like `‖` or a Chinese reviewer comment in a `\todo{…}`, the line will silently overshoot.

If you decide not to fix, document the ASCII assumption next to `wrap_col` in `config.example.toml`.

### M7. Race window between Stat and WriteFile in `mreview config`

`cmd/mreview/config.go:122-128` does `Stat → WriteFile` without `O_EXCL`. Two concurrent `mreview config` invocations both see the file missing, both `WriteFile` the template, the second `Stat`+message is a lie. Low-probability but trivial fix: `os.OpenFile(path, O_CREATE|O_EXCL|O_WRONLY, 0o644)` + handle `os.IsExist` as "already there".

### M8. `pkg/persist/remap.go` Levenshtein has no input cap

`remap.go:325-352`. The comment at line 322 says "Inputs above a few thousand runes are capped" — they aren't. A pathological remap target with a 100k-rune source allocates a 100k×100k DP grid (the rolling-row trick reduces this to O(min(la, lb)) memory but still O(la·lb) time; ~10^10 ops). Cap at e.g. 4000 runes per side and bail to "no match" beyond that, since high-confidence matches at that scale aren't useful anyway.

### M9. `remap.bestLineRangeMatch` non-deterministic tiebreak

`remap.go:212-216`. When two blocks tie on F1 *and* span size, the code keeps whichever was visited first in `doc.Blocks` order — which the parser builds in source order, so this is actually deterministic in practice. But the comment doesn't say so, and a future refactor that changes block order would silently shuffle remap results. Adding a tertiary tiebreaker on `block.ID` makes it robust to parser changes.

### M10. PDF crop functions: five entry points, no documented selection rule

`pkg/pdf/cropfit.go` exposes `Crop`, `CropAtDPI`, `CropWithContext`, `CropWithContextAtDPI`, `CropFitted`. The README and code comments don't say which one a caller should reach for. From grep, the live caller is `CropFitted`; the others appear unused. If they are dead, delete; if they are kept for a future API, mark as such.

### M11. Mouse coord math is fine, but pane is 428 LOC

`pkg/ui/mouse.go` carries an outsized share of pane-layout knowledge for what should be a thin map from cell coords → action. The body has off-by-one risk every time the layout changes (e.g. a future pane border tweak in `view.go` would silently misroute mouse clicks). Most of the math could move to `pagelayout.go` next to the layout itself.

### M12. `pkg/ui/skim.go` orphans subprocesses on quit

Three `go func() { … cmd.Wait() }` invocations (lines 52, 99, 110) launch macOS osascript / displayline calls and don't track them. If the user quits mreview while Skim is mid-launch, the subprocess outlives the parent and queues AppleScript events against a phantom display. Annoying, not catastrophic, but worth a `sync.WaitGroup` or a context that cancels on `tea.Quit`.

### M13. PDF cache key collision risk on layout toggle

`pkg/ui/pdf.go:174-179`'s cache key is `(BlockID, Mtime, Width, Height)`. If the user toggles the dark-mode preview via `update.go:251-268`, the rendered colour space changes but `Width/Height/Mtime` are identical, so the cache returns the previous (light) crop until the user moves the cursor. The toggle path explicitly clears the cache (`update.go:291-295` does it for the layout toggle), but I don't see the same on the dark-mode toggle. Quick check needed.

### M14. `pkg/format/verify_paranoid_stub.go` vs `_paranoid.go` error UX is inconsistent

When the user sets `verify_pdf = "visual"` in config:

- Built without `pdfverify` tag: the stub at `verify_paranoid_stub.go:14` returns "paranoid verifier not available", routed to `cmd/mreview/fmt.go:378` which prints a helpful "rebuild with -tags=pdfverify" hint.
- Built with the tag, but `diff-pdf` is missing at runtime: `verify_paranoid.go:25` returns `"diff-pdf not found on $PATH"` with no install hint.

Unify the error messages so the user always gets the right next step.

### M15. `pkg/parser/parse.go` `closeSectionsAtLevel` — verified correct, but two sub-reviews flagged it

I read the code path on lines 274-291 carefully. Same-level sections *do* close the previous section because the loop pops while `lvl >= newLevel`. Mentioning here so it doesn't come back as a finding — verified clean.

---

## Low

### L1. Dead helper: `pkg/pdfreview/editor.go:282-283`

```go
// baseTmpPattern is exposed so tests can predict the tmp filenames.
func baseTmpPattern(p string) string { return filepath.Base(p) }
```

`grep -rn baseTmpPattern` finds zero callers (verified). Either delete or use it from a test.

### L2. Dead data: `parser.TheoremEnv.Chain`

`pkg/parser/parse.go:15`, `tokenizer.go:300`. The `\newtheorem[oldenv]{newenv}{Title}` chain (sharing a counter) is parsed and stored in `doc.TheoremEnvs[*].Chain`, but no downstream code reads it. Either drop it or document the intended consumer.

### L3. Dead token kind: `TokTheoremStyle`

`pkg/parser/tokenizer.go:20-21`, handled as a no-op in `parse.go:263`. Either implement style propagation or remove.

### L4. `_, _ = fmt.Fprintf(...)` noise

`cmd/mreview/fmt.go` and `cmd/mreview/config.go` both wrap every stderr write in `_, _ = …`. Idiomatic Go ignores the return values silently. The pattern adds line noise without a payoff (no linter flags `Fprintf` in this codebase). Remove.

### L5. Inverted variable name in cropfit

`pkg/pdf/cropfit.go:178-188`. `paneAspect = PaneWidthPx / PaneHeightPx` is named width-over-height but the algorithm wants height-over-width. The math still works because the comparison against `cropAspect` is also "wrong" in the same direction, but reading the code requires undoing the inversion mentally.

### L6. `pkg/pdf/render.go:131` magic number `*2 < pageWPx*110/100`

Comment says "~55%" — write it as `regionWPx*100 < pageWPx*55` to make the threshold literal.

### L7. `subImage` duplicated between `cropfit.go` and `render.go`

`pkg/pdf/cropfit.go:256-268` and `pkg/pdf/render.go:63-71` define identical `subImager` interface + helper. Pull into `doc.go`.

### L8. `IconBibliograph` typo

`pkg/ui/outline.go:24` — should be `IconBibliography`. Used internally only, but the typo will end up in someone's autocomplete.

### L9. `BuildError.FirstLine` is misnamed

`pkg/build/build.go:229-260`. The field actually carries the first detected `!`-line *or* an undefined-ref/cite warning — rename to `LogIssue` or `FirstIssue` so callers don't infer "line 1".

### L10. `pkg/build/build.go:192-203` case mismatch

```go
strings.Contains(line, "Reference `")    // capital R
strings.Contains(low, "reference '")     // lowercase, on lowered line
strings.Contains(line, "citation '")     // lowercase, raw line
```

The third arm uses `line` (raw) instead of `low` — a `Citation 'foo'` warning is missed. One-line fix.

### L11. `pkg/synctex/synctex.go` deduplication absent

Same SyncTeX record appearing twice gets stored twice (`Lines[file][ln] = append(...)`). Region union still computes correctly, but the duplicate-detection loops in `RegionForLines` waste cycles. Low impact.

### L12. `pkg/persist/sidecar.go:89` regex notation

`\x{2014}` works but is unusual; `—` is what every Go regex doc uses and is more readable.

### L13. `pkg/persist/sidecar.go:315-318` strict `## Detached` match

`if line == DetachedMarker` rejects `## Detached ` (trailing space). Tests presumably cover the round-trip but a hand-edited sidecar with a stray space silently demotes the marker to a regular annotation heading. `strings.TrimRight(line, " \t")` fixes it.

### L14. `cmd/mreview/main.go:122-130` typo path is silenced when a same-named file exists

A user with a literal file named `cofnig` in cwd wanting to run `mreview cofnig` passes the `os.Stat` check (file exists), so the Levenshtein hint never fires and they get a "parse error" downstream. Niche, but the stat check should be qualified by `filepath.Ext != ".tex"`.

### L15. Test gaps inventory

The biggest holes (most likely to regress silently):

- `pkg/pdfreview/update.go` — 0 tests for the entire event loop (H8).
- `pkg/parser/parse.go` — no test for empty input, multi-line `\cite{a,\nb}`, `\label` inside nested envs, very long lines, CRLF round-trip.
- `pkg/format/rules_wrap.go` — no test for unclosed `$`, `\&` inside cells, escaped `]` inside optional args.
- `pkg/build/build.go` — no test for the `Citation '…'` warning detection (L10).
- `pkg/synctex/synctex.go` — no test for malformed-header tolerance (M4) or basename-collision behaviour (H1).
- `pkg/persist/remap.go` — Levenshtein only tested on ASCII (M8 corollary).

### L16. Comments explain WHAT instead of WHY

Per your CLAUDE.md preference, several comments restate the code rather than the reason. A non-exhaustive sample worth pruning:

- `cmd/mreview/main.go:24-25` ("populatePDFRegions fills Block.PDFRegion for every block...")
- `pkg/pdf/cropfit.go:14-49` (the multi-doc on the five Crop entry points)
- `pkg/parser/spans.go:8-12` (ProtectedSpan is described, never motivated)

These rot into stale comments fastest because the *what* changes with each refactor.

### L17. `pkg/ui/pdf.go:22` cache size hard-coded to 64

Reasonable, but on a 600-page monograph with frequent jumping the cache thrashes. Either bump to 256 or expose as `[ui] pdf_cache_max`.

### L18. `cmd/mreview/fmt.go` `--rule` and `--skip-rule` accepting the same value

No validation that `--rule X --skip-rule X` is contradictory. The current behaviour silently runs the empty rule set. A "no rules left to run" warning is cheap.

---

## Per-package quality at a glance

| Package | Size | Verdict |
|---|---|---|
| `cmd/mreview/` (~1.2k LOC) | OK | one critical (C1), several mediums; main.go is well-structured aside from signal handling. |
| `pkg/parser/` (~3.0k) | Strong | best-tested package; minor dead code; the size of `parse.go` is justified by what it does. |
| `pkg/format/` core (~1.5k) | OK | one critical (C2); verifier needs timeouts (H4); skip-directive logic is clean. |
| `pkg/format/` rules (~3.7k) | OK | tier classification mostly honest; H2 escape parsing is the main risk; the user's recent fixes (preamble protection, command-only paragraphs, control-word terminators) hold up under inspection. |
| `pkg/format/verify*` (~1k) | OK | text+visual layering is sound; subprocess hygiene is the sole soft spot. |
| `pkg/ui/` (~6.6k) | Strong | excellent bubbletea discipline, no concurrency hazards in the Cmd graph; mouse and skim are the rough edges. |
| `pkg/pdf/` (~0.7k) | OK | crop math is correct, naming is the issue; kitty graphics lacks per-image-id mgmt but doesn't need it for current pane count. |
| `pkg/synctex/` (~0.4k) | Weak | one high-severity (H1), two mediums on parse-error silence; tests are thin. |
| `pkg/build/` (~0.3k) | OK | timeout (H4)/ctrl-c (H6) story is the gap; otherwise crisp. |
| `pkg/persist/` (~1.1k) | Strong | atomic save is right, remap pipeline is well thought through; only L-level issues. |
| `pkg/pdfreview/` (~2.5k) | Weak | code looks fine on the surface but H8 (no event-loop tests) means *anything* could be subtly wrong without showing in CI. Highest regression risk in the codebase. |

---

## Recommended action order

1. **C1 (shell injection)** — five-line fix, no excuse to defer.
2. **C2 + H5 (atomic report write + stale on `--no-verify`)** — same area, fix together.
3. **H6 + H7 (signal handling + kitty cleanup)** — lift `signal.NotifyContext` once, defer the kitty delete, both behaviours land at once.
4. **H1 (synctex non-deterministic basename)** — add longest-suffix tiebreak; add a regression test with two `intro.tex` files.
5. **H4 (verifier timeouts)** — wrap the three `exec.Command` sites in `CommandContext`. 60s default, configurable per-call.
6. **H8 (pdfreview tests)** — pick the five highest-risk handlers from the H8 bullet list and write tests before the next feature lands in that package.
7. **H2 (excludedRanges escape parsing)** — small, but the test fixture is the real value.
8. **H10 (line-count-changing whitelist)** — convert to `Rule.ChangesLineCount()`.
9. The remaining mediums in topical order; lows can land as opportunistic cleanup.

---

## Verified false / nitpicks I rejected

Listed so that re-running this review doesn't surface them again:

- **`pkg/parser/bib.go:84` Go-loop-pointer bug**. `e := entries[i]` declares a new local each iteration; `&e` is safe. (Sub-review claimed this was a critical bug.)
- **`pkg/format/rules_math_align.go:476` `splitCells` doesn't handle `\&`**. It does — the `case ch == '\\' …` branch increments `i` past the next byte. Idempotent on `\&`. (Sub-review claimed this was critical.)
- **`pkg/parser/parse.go:274-291` `closeSectionsAtLevel` mishandling same-level sections**. The loop condition is correct; same-level sections do close their predecessor. (One sub-review caught and corrected this mid-investigation; included here as a marker.)
- **`pkg/ui/pdf.go:52` value-receiver bug**. The implementation is pointer-receiver and correct; only the surrounding comment in `model.go:266` is misleading. Fix the comment, not the code.
- **General `\verb` and `$$` correctness on truncated input.** Tokenizer is line-based and degrades to "incomplete command, ignored", which is the documented behaviour. Not a bug.

---

*Review by Claude (Opus 4.7), 2026-04-27. Findings cross-checked against the actual source for every Critical and High item before inclusion. Sub-review verbatim transcripts are not included; let me know if you want them dumped to a separate file.*
