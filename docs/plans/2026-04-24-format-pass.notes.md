# mreview fmt — execution notes

## Task 5: Tier-2 rules

### Phase A/B/C PNAS validation (pending manual run)

The PNAS source files (`main_pnas.tex`, `si_pnas.tex`) are not available in the
CI/build container. Phases A, B, C must be run manually on LP's machine after
`make install`:

```sh
# Phase A — Tier-1 baseline
mreview fmt --diff main_pnas.tex
mreview fmt --diff si_pnas.tex

# Phase B — Tier-2 dry run
mreview fmt --pdf-fix --diff main_pnas.tex

# Phase C — Tier-2 commit
mreview fmt --pdf-fix --report main_pnas.tex
```

Record rewrite counts and verifier results below after running.

### Rewrite counts (to be filled after manual run)

- space.trailing: ?
- space.blank-runs: ?
- math.paragraph-suppress: ? (expected ~11+ hits per plan)
- env.spacing: ?
- Verifier surprises: ?
