package ui

import (
	"fmt"
	"os"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// sourceWatchInterval is the cadence of the auto-reload poll. One
// second is fine-grained enough that the user perceives "I saved in the
// agent terminal, glanced back, and mreview already has it" without
// burning measurable CPU on filesystem stats.
const sourceWatchInterval = 1 * time.Second

// tickSourceWatchMsg is the bubbletea message delivered by tea.Tick on
// every poll cycle. Carries no data — the handler restats from m.Doc.
type tickSourceWatchMsg struct{}

// tickSourceWatch is the recurring command. The handler reschedules
// itself by returning this on every tick so the watcher self-perpetuates
// for the lifetime of the program (Bubbletea cancels outstanding cmds
// on Quit, so there's no shutdown bookkeeping).
func tickSourceWatch() tea.Cmd {
	return tea.Tick(sourceWatchInterval, func(time.Time) tea.Msg {
		return tickSourceWatchMsg{}
	})
}

// autoReloadSourceEnabled reports whether the source-watch poll should
// run. nil Config (tests that bypass main.go) and a nil pointer-to-bool
// both default to true — disabling requires an explicit `false` in
// .mreview.toml.
func autoReloadSourceEnabled(c *Config) bool {
	if c == nil || c.AutoReloadSource == nil {
		return true
	}
	return *c.AutoReloadSource
}

// sourceWatchPaths returns the unique set of files whose mtime should
// be polled. m.Doc.File is the entry point; per-block File entries
// cover anything that ended up in the parsed doc via \input. Files
// that are present in blocks but not on disk are silently filtered by
// the stat call in handleSourceWatch.
func (m Model) sourceWatchPaths() []string {
	if m.Doc == nil {
		return nil
	}
	seen := map[string]struct{}{}
	var out []string
	add := func(p string) {
		if p == "" {
			return
		}
		if _, ok := seen[p]; ok {
			return
		}
		seen[p] = struct{}{}
		out = append(out, p)
	}
	add(m.Doc.File)
	for _, b := range m.Doc.Blocks {
		if b == nil {
			continue
		}
		add(b.File)
	}
	return out
}

// newestSourceMtime returns the most-recent mtime across watched files,
// or the zero value when no path stats successfully. Errors stat'ing
// individual files are intentionally swallowed: a renamed \input or a
// path that hasn't been built yet shouldn't block the watcher from
// catching real changes elsewhere.
func newestSourceMtime(paths []string) time.Time {
	var newest time.Time
	for _, p := range paths {
		info, err := os.Stat(p)
		if err != nil {
			continue
		}
		if info.ModTime().After(newest) {
			newest = info.ModTime()
		}
	}
	return newest
}

// handleSourceWatch is the body of the auto-reload tick. Returns the
// updated model + the command to drive the next cycle.
//
// The handler is conservative on purpose: it skips firing when a popup
// is open (the user is actively typing into a textarea or facing a
// y/N confirm — yanking the doc out from under them would be jarring)
// and when SourceMtime hasn't been seeded yet (the very first tick on
// program start treats whatever is on disk as the baseline rather than
// triggering an immediate redundant reload). Both cases still
// reschedule the next tick so a popup that closes after the change has
// already landed picks it up on the very next cycle.
//
// The "rebuild already in flight" case doesn't need an explicit guard
// because the mtime baseline is bumped to `newest` before startReload
// runs — subsequent ticks see no change and exit cleanly until the
// agent edits again.
func (m Model) handleSourceWatch() (Model, tea.Cmd) {
	if !autoReloadSourceEnabled(m.Config) {
		return m, nil
	}
	next := tickSourceWatch()

	// Sidecar poll runs first because it's a 3-way merge (cheap), not a
	// full source rebuild. This catches the agent-deletes-stale-annotations
	// flow without waiting for the user to trigger a save.
	m = m.pollSidecar()

	paths := m.sourceWatchPaths()
	newest := newestSourceMtime(paths)
	if newest.IsZero() {
		return m, next
	}
	if m.SourceMtime.IsZero() {
		// First tick: seed the baseline silently so we don't redundant-
		// reload on the file we just opened.
		m.SourceMtime = newest
		return m, next
	}
	if !newest.After(m.SourceMtime) {
		return m, next
	}
	if m.Popup != nil || m.Pending != nil {
		// Don't yank the doc out from under an open popup. The pending
		// change stays on disk; whichever tick fires after the popup
		// closes will see it and trigger.
		return m, next
	}

	m.SourceMtime = newest
	nm, cmd := m.startReload()
	if nm.Status == "rebuilding…" {
		nm.Status = "auto-reload: rebuilding…"
	}
	return nm, tea.Batch(cmd, next)
}

// pollSidecar checks whether the sidecar file on disk has been edited
// externally (typically by an agent removing addressed annotations)
// since our last sync. When it has, the disk view is reloaded, remapped,
// and 3-way-merged against the in-memory state — same path as
// saveSidecar's mtime guard, but driven by the watch tick instead of a
// save event so the merge happens proactively without requiring the
// user to take any action.
//
// Conservative early-outs match handleSourceWatch:
//   - SidecarPath empty (no persistence configured) or SidecarMtime zero
//     (first tick — saveSidecar / startup will seed it on the next event).
//   - Open popup or pending action: yanking annotation state out from
//     under the user mid-keystroke would be jarring; a later tick after
//     the popup closes will catch it.
//
// On success the sidecar mtime baseline and SidecarBase snapshot are
// advanced to match the merged state so subsequent saves (if any) take
// the merged view as common ancestor.
func (m Model) pollSidecar() Model {
	if m.SidecarPath == "" || m.SidecarMtime.IsZero() || m.Doc == nil {
		return m
	}
	if m.Popup != nil || m.Pending != nil {
		return m
	}
	info, err := os.Stat(m.SidecarPath)
	if err != nil || !info.ModTime().After(m.SidecarMtime) {
		return m
	}
	mp := &m
	merged, n, ok := mp.mergeWithDisk()
	if !ok {
		return *mp
	}
	mp.Sidecar = merged
	mp.SidecarMtime = info.ModTime()
	mp.SidecarBase = SnapshotSidecar(merged)
	if n > 0 {
		mp.Status = fmt.Sprintf("auto-reload: agent removed %d annotation(s)", n)
	}
	return *mp
}
