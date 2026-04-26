# Setting up mreview fmt for CI

## pre-commit hook

[pre-commit](https://pre-commit.com/) runs formatting checks automatically
before each commit.

### 1. Install pre-commit

```
pip install pre-commit
```

### 2. Add mreview fmt to `.pre-commit-config.yaml`

Create or edit `.pre-commit-config.yaml` in your repo root:

```yaml
repos:
  - repo: <mreview-repo-url>
    rev: <version>    # e.g. v0.1.0
    hooks:
      - id: mreview-fmt
```

This runs `mreview fmt --fail-on-change --no-verify` on all staged `.tex`
files. The hook formats files in place and fails the commit if any file
was changed, so you can review and re-stage the fixes.

### 3. Install the hook

```
pre-commit install
```

### 4. (Optional) Run on all files

```
pre-commit run mreview-fmt --all-files
```

### Local hook (no remote repo)

If mreview is installed locally and you don't want to reference a remote
repo, use a local hook instead:

```yaml
repos:
  - repo: local
    hooks:
      - id: mreview-fmt
        name: mreview fmt
        entry: mreview fmt --fail-on-change --no-verify
        language: system
        types: [tex]
        pass_filenames: true
```

## GitHub Actions

Copy `templates/github-actions/mreview-fmt.yml` to
`.github/workflows/mreview-fmt.yml` in your repository. Edit the
"Install mreview" step to point to your mreview repo URL.

The workflow runs `mreview fmt --check --no-verify` on `.tex` files
changed in pull requests and pushes to main/master. It exits non-zero
if any file needs formatting, blocking the PR until the source is
normalized.

## Flags reference

| Flag | Behavior |
|---|---|
| `--fail-on-change` | Format in place, exit 1 if any file changed (pre-commit) |
| `--check` | Dry run, exit 1 if any file would change (CI read-only) |
| `--no-verify` | Skip PDF verification (faster, suitable for CI) |
| `--summary` | Print rewrite counts without modifying files |
