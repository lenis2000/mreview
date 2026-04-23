package ui

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
)

// Config is the merged user+project configuration. All fields are optional
// and only override the defaults when populated.
//
// The precedence rule is: project (./.mreview.toml) overrides user
// (~/.config/mreview/config.toml), and an explicit `--config` replaces both
// layers entirely.
type Config struct {
	// Parser overrides.
	TheoremEnvs []string `toml:"theorem_envs"`
	FigureEnvs  []string `toml:"figure_envs"`

	// Build override — empty string uses the built-in latexmk invocation.
	BuildCmd string `toml:"build_cmd"`

	// UI overrides.
	Theme    string            `toml:"theme"`
	Colors   map[string]string `toml:"colors"`
	Keybinds map[string]string `toml:"keybinds"`
}

// DefaultConfig returns an empty Config with non-nil maps so callers can
// merge layer-by-layer without nil-guarding every key access.
func DefaultConfig() *Config {
	return &Config{
		Colors:   map[string]string{},
		Keybinds: map[string]string{},
	}
}

// LoadConfig resolves the configuration layer stack.
//
//	explicit != ""  -> load that file exclusively (missing file is an error)
//	explicit == ""  -> merge ~/.config/mreview/config.toml (if any), then
//	                   ./.mreview.toml (if any). Either missing file is
//	                   silently ignored.
func LoadConfig(explicit string) (*Config, error) {
	cfg := DefaultConfig()
	if explicit != "" {
		if err := applyConfigFile(cfg, explicit, true); err != nil {
			return nil, err
		}
		return cfg, nil
	}
	if home, err := os.UserHomeDir(); err == nil {
		userPath := filepath.Join(home, ".config", "mreview", "config.toml")
		if err := applyConfigFile(cfg, userPath, false); err != nil {
			return nil, err
		}
	}
	if err := applyConfigFile(cfg, ".mreview.toml", false); err != nil {
		return nil, err
	}
	return cfg, nil
}

// applyConfigFile decodes path into an overlay and merges it into cfg.
// strict=true turns a missing file into an error; otherwise it is ignored.
func applyConfigFile(cfg *Config, path string, strict bool) error {
	if _, err := os.Stat(path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			if strict {
				return fmt.Errorf("config %q: %w", path, err)
			}
			return nil
		}
		return err
	}
	var overlay Config
	if _, err := toml.DecodeFile(path, &overlay); err != nil {
		return fmt.Errorf("config %q: %w", path, err)
	}
	mergeConfig(cfg, &overlay)
	return nil
}

// mergeConfig applies any fields set in overlay on top of base. Slices are
// replaced wholesale (not appended); scalars are replaced when non-zero; maps
// merge per-key.
func mergeConfig(base, overlay *Config) {
	if base == nil || overlay == nil {
		return
	}
	if len(overlay.TheoremEnvs) > 0 {
		base.TheoremEnvs = append([]string(nil), overlay.TheoremEnvs...)
	}
	if len(overlay.FigureEnvs) > 0 {
		base.FigureEnvs = append([]string(nil), overlay.FigureEnvs...)
	}
	if overlay.BuildCmd != "" {
		base.BuildCmd = overlay.BuildCmd
	}
	if overlay.Theme != "" {
		base.Theme = overlay.Theme
	}
	if base.Colors == nil {
		base.Colors = map[string]string{}
	}
	for k, v := range overlay.Colors {
		base.Colors[k] = v
	}
	if base.Keybinds == nil {
		base.Keybinds = map[string]string{}
	}
	for k, v := range overlay.Keybinds {
		base.Keybinds[k] = v
	}
}

// ApplyThemeEnv returns cfg with its Theme replaced by MREVIEW_THEME when
// that env var is set to "dark" or "light". Other values are ignored so a
// stray setting does not clobber a user-configured theme.
func ApplyThemeEnv(cfg *Config) *Config {
	if cfg == nil {
		cfg = DefaultConfig()
	}
	v := strings.ToLower(os.Getenv("MREVIEW_THEME"))
	if v == "dark" || v == "light" {
		cfg.Theme = v
	}
	return cfg
}

// StylesForTheme returns a Styles palette for the given theme label. Unknown
// labels (including the empty string) fall back to DefaultStyles.
func StylesForTheme(theme string) Styles {
	switch strings.ToLower(theme) {
	case "light":
		return lightStyles()
	case "dark":
		return DefaultStyles()
	}
	return DefaultStyles()
}
