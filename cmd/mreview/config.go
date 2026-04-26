package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
)

// defaultGlobalConfig is the starter content written to
// ~/.config/mreview/config.toml when the user runs `mreview config` and
// the file doesn't yet exist. Every key is set to its built-in default
// and commented out — uncomment to override.
const defaultGlobalConfig = `# mreview global config — all values shown match the built-in defaults.
# Uncomment + edit any line you want to override. Project-local
# .mreview.toml files (walked up from cwd to git root) override this.
# Pass --noconfig to ignore everything below.

# ---------------------------------------------------------------------
# Top-level — ` + "`mreview`" + ` review TUI
# ---------------------------------------------------------------------

# build_cmd    = ""                    # override latexmk; "" uses the built-in
# theme        = "dark"                # "dark" | "light"
# theorem_envs = []                    # extra envs treated as theorem-like
# figure_envs  = []                    # extra envs treated as figure-like

# [colors]                             # palette overrides; see ui/styles.go
# status = "orange"

# [keybinds]                           # keybinding overrides; see ui/keys.go
# quit = "ZZ"

# ---------------------------------------------------------------------
# [fmt] — ` + "`mreview fmt`" + ` defaults
# ---------------------------------------------------------------------

[fmt]

# Tier-2 PDF-fixing rules (math.paragraph-suppress, env.spacing).
# Default: ON — set to true to disable.
# no_pdf_fix = false

# Verifier strictness:
#   "visual"  text-layer pdftotext diff + pixel-level diff-pdf  (default)
#   "text"    text-layer only (faster, looser)
# verify_pdf = "visual"

# Persistently skip PDF verification. Faster but loses the safety net.
# (One-off escape hatch: --no-verify on the CLI.)
# no_verify = false

# Persistently suppress paper.tex.fmt-report.md. The TUI surfaces lint
# diagnostics from this file in the ` + "`issues`" + ` filter, so most users want
# it ON. (One-off escape hatch: --no-report on the CLI.)
# no_report = false

# Extra environments to treat as opaque (verbatim-like) on top of the
# built-in list (verbatim, Verbatim, lstlisting, minted, comment).
# verbatim_envs = ["mycustomlisting"]

# Indentation pass.
# indent      = true                   # default: ON
# indent_char = "tab"                  # "tab" | "space"
# indent_size = 1                      # 1 tab/level (or e.g. 2 for spaces)

# Per-environment indent overrides. Key = env name, value = literal indent
# string per nesting level. Empty string = no indent (like document).
# [fmt.indent_rules]
# tikzpicture = "  "                   # 2 spaces inside tikzpicture
# tikzcd      = ""                     # no indent inside tikzcd

# Wrap mode and target column.
#   "off"               no wrapping
#   "column"            break at rightmost space ≤ wrap_col
#   "sentence"          one sentence per line, no column cap
#   "sentence+column"   sentences first, then column-cap any too-long sentence
# wrap     = "sentence+column"
# wrap_col = 80
`

// runConfig opens the user-global mreview config in $EDITOR. If the file
// does not yet exist, it is created with the default template above so
// the editor opens to a useful starting point rather than an empty buffer.
func runConfig(args []string, stdout, stderr io.Writer) int {
	if len(args) > 0 {
		fmt.Fprintln(stderr, "mreview config: unexpected argument")
		fmt.Fprintln(stderr, "usage: mreview config")
		return 2
	}

	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintf(stderr, "mreview config: cannot find home directory: %v\n", err)
		return 1
	}
	dir := filepath.Join(home, ".config", "mreview")
	path := filepath.Join(dir, "config.toml")

	if err := os.MkdirAll(dir, 0o755); err != nil {
		fmt.Fprintf(stderr, "mreview config: mkdir %q: %v\n", dir, err)
		return 1
	}
	if _, statErr := os.Stat(path); os.IsNotExist(statErr) {
		if writeErr := os.WriteFile(path, []byte(defaultGlobalConfig), 0o644); writeErr != nil {
			fmt.Fprintf(stderr, "mreview config: write %q: %v\n", path, writeErr)
			return 1
		}
		fmt.Fprintf(stderr, "mreview config: created %s with defaults\n", path)
	}

	editor := os.Getenv("VISUAL")
	if editor == "" {
		editor = os.Getenv("EDITOR")
	}
	if editor == "" {
		editor = "vim"
	}

	cmd := exec.Command("sh", "-c", editor+" \""+path+"\"")
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if runErr := cmd.Run(); runErr != nil {
		fmt.Fprintf(stderr, "mreview config: editor exited: %v\n", runErr)
		return 1
	}
	return 0
}
