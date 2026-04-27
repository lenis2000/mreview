package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestRunConfig_CreatesAndOpens drives `mreview config` with a no-op editor
// (`true`) so the test is fast. It must:
//  1. create ~/.config/mreview/config.toml with the default template, and
//  2. invoke $EDITOR on the path, returning 0.
func TestRunConfig_CreatesAndOpens(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("EDITOR", "true") // no-op editor; exits 0 immediately
	t.Setenv("VISUAL", "")     // make EDITOR the source of truth

	var stdout, stderr bytes.Buffer
	code := run([]string{"config"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("expected exit 0, got %d (stderr=%q)", code, stderr.String())
	}

	path := filepath.Join(home, ".config", "mreview", "config.toml")
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("config file should have been created: %v", err)
	}
	if !strings.Contains(string(body), "[fmt]") {
		t.Fatalf("default template must contain [fmt] sub-table, got %q", body)
	}
	if !strings.Contains(stderr.String(), "created") {
		t.Fatalf("expected creation message on stderr, got %q", stderr.String())
	}
}

// TestRunConfig_NoShellInjection guards the fix that replaced `sh -c <editor>
// "<path>"` with a direct exec. An $EDITOR like `true; touch CANARY` must
// be tokenised into argv, and the would-be side-effect command must not
// execute — even though the run will exit non-zero (the literal "true;"
// is not a binary on $PATH).
func TestRunConfig_NoShellInjection(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	canary := filepath.Join(home, "CANARY")
	t.Setenv("EDITOR", "true; touch "+canary)
	t.Setenv("VISUAL", "")

	var stdout, stderr bytes.Buffer
	_ = run([]string{"config"}, &stdout, &stderr)

	if _, err := os.Stat(canary); err == nil {
		t.Fatalf("canary file %q exists — shell injection regressed", canary)
	}
}

func TestRunConfig_DoesNotOverwriteExisting(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("EDITOR", "true")
	t.Setenv("VISUAL", "")

	dir := filepath.Join(home, ".config", "mreview")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	path := filepath.Join(dir, "config.toml")
	custom := []byte("# my custom config\nbuild_cmd = \"latexmk -lualatex\"\n")
	if err := os.WriteFile(path, custom, 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}

	var stdout, stderr bytes.Buffer
	code := run([]string{"config"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("expected exit 0, got %d (stderr=%q)", code, stderr.String())
	}

	current, _ := os.ReadFile(path)
	if !bytes.Equal(current, custom) {
		t.Fatalf("existing config must NOT be overwritten; before=%q after=%q", custom, current)
	}
}
