package ui

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"mreview/pkg/build"
)

// skimDisplaylinePath is the SyncTeX forward-search helper shipped with
// Skim.app — the same binary vimtex's `<leader>ld` invokes. Taking
// (line, pdf, tex) it tells Skim to jump to (and highlight) the region
// that SyncTeX maps the source line to.
const skimDisplaylinePath = "/Applications/Skim.app/Contents/SharedSupport/displayline"

// openInSkim runs the Skim forward-search then focuses Skim.app on the
// current PDF. Preference order:
//
//  1. Revert Skim's open document (if any) so it picks up the freshly
//     rebuilt PDF + SyncTeX on disk, then `displayline` with
//     (line, pdf, tex) — line-accurate, matches the vimtex `\ld`
//     workflow and highlights the target region. Without the revert,
//     displayline forward-searches against whatever SyncTeX mapping
//     Skim cached at open time, which after a rebuild points at stale
//     line/page coordinates — exactly the "Skim detached from SyncTeX"
//     symptom we used to hit whenever S rebuilt the paper.
//  2. AppleScript fallback modelled on docviewer: revert an already-open
//     copy so it picks up rebuilds, open the file, and jump to the page
//     SyncTeX maps the cursor line to. `activate` ensures focus lands
//     on Skim even if it was already running in the background.
//  3. Plain `open -a Skim` if SyncTeX isn't wired up.
func (m Model) openInSkim() (tea.Model, tea.Cmd) {
	if m.PDF == nil || m.PDF.Path() == "" {
		m.Status = "S: no PDF loaded"
		return m, nil
	}
	pdfPath := m.PDF.Path()

	line, lineOK := absoluteCursorLine(m)
	if lineOK && m.Doc != nil && m.Doc.File != "" {
		if _, err := os.Stat(skimDisplaylinePath); err == nil {
			texFile := m.Doc.File
			// Run the revert → displayline → activate chain off the
			// Update goroutine: osascript + displayline together take a
			// few hundred ms, and blocking Update for that long visibly
			// freezes input. The sequence itself must be sequential —
			// displayline has to run *after* the revert commits, or
			// Skim forward-searches against the pre-revert SyncTeX
			// index we were trying to invalidate.
			go func() {
				revert := fmt.Sprintf(
					`set theFile to POSIX file %q
tell application "Skim"
  try
    set theDocs to get documents whose path is (get POSIX path of theFile)
    if (count of theDocs) > 0 then
      revert theDocs
    end if
  end try
end tell`, pdfPath)
				_ = exec.Command("osascript", "-e", revert).Run()
				_ = exec.Command(skimDisplaylinePath,
					strconv.Itoa(line), pdfPath, texFile).Run()
				_ = exec.Command("osascript", "-e",
					`tell application "Skim" to activate`).Run()
			}()
			m.Status = ""
			return m, nil
		}
	}

	page := 0
	if lineOK && m.Synctex != nil && m.Doc != nil {
		b := m.Doc.ByID[m.CursorBlockID]
		if b != nil {
			if r := m.Synctex.RegionForLines(m.Doc.File, b.StartLine, b.EndLine); r != nil {
				page = r.Page
			}
		}
	}
	if page > 0 {
		script := fmt.Sprintf(
			`set theFile to POSIX file %q
tell application "Skim"
  activate
  set theDocs to get documents whose path is (get POSIX path of theFile)
  if (count of theDocs) > 0 then
    try
      revert theDocs
    end try
  end if
  open theFile
  set index of current page of document 1 to %d
end tell`, pdfPath, page)
		cmd := exec.Command("osascript", "-e", script)
		if err := cmd.Start(); err == nil {
			go cmd.Wait()
			m.Status = ""
			return m, nil
		}
	}

	cmd := exec.Command("open", "-a", "Skim", pdfPath)
	if err := cmd.Start(); err != nil {
		m.Status = "S: " + err.Error()
		return m, nil
	}
	go cmd.Wait()
	m.Status = ""
	return m, nil
}

// reloadSkim forces Skim to re-read the current PDF from disk — without
// the displayline forward-search S triggers. Useful after a background
// rebuild (lmkf) when Skim still shows a stale render and the user
// doesn't want the cursor to jump in the process.
//
// Two-step flow with a strict separation of responsibilities:
//
//  1. AppleScript revert pass — only operates when Skim already has the
//     document open. Iterates the matching documents one-by-one (Skim
//     has historically been finicky about reverting a list).
//  2. If no doc was reverted, fall back to `open -a Skim <abs-path>`,
//     which asks Launch Services to route the open through Skim
//     specifically. The earlier version did `open theFile` from inside
//     `tell application "Skim"`; if Skim hadn't finished launching, the
//     open verb would be re-routed via Launch Services to the default
//     PDF handler — which on some macOS setups is Safari, producing
//     phantom failed downloads in Safari's Downloads window.
//
// The path is also resolved to an absolute form before either step so
// AppleScript's `POSIX file` doesn't try to interpret a relative path
// against an unpredictable working directory.
func (m Model) reloadSkim() (tea.Model, tea.Cmd) {
	if m.PDF == nil || m.PDF.Path() == "" {
		m.Status = "R: no PDF loaded"
		return m, nil
	}
	pdfPath, err := filepath.Abs(m.PDF.Path())
	if err != nil {
		m.Status = "R: resolve path: " + err.Error()
		return m, nil
	}
	if _, statErr := os.Stat(pdfPath); statErr != nil {
		m.Status = "R: " + statErr.Error()
		return m, nil
	}

	// Step 1: revert any already-open document with this path. Returning
	// "true" / "false" lets us decide whether step 2 is needed without a
	// second osascript round-trip.
	script := fmt.Sprintf(`tell application "Skim"
  set didRevert to false
  try
    set theDocs to documents whose path is %q
    repeat with d in theDocs
      revert d
      set didRevert to true
    end repeat
  end try
end tell
return didRevert`, pdfPath)
	out, _ := exec.Command("osascript", "-e", script).Output()
	reverted := strings.TrimSpace(string(out)) == "true"
	if !reverted {
		// Step 2: not currently open. Use the shell `open -a Skim` so
		// Launch Services hands the file to Skim explicitly, never to
		// the default PDF handler.
		cmd := exec.Command("open", "-a", "Skim", pdfPath)
		if err := cmd.Start(); err != nil {
			m.Status = "R: " + err.Error()
			return m, nil
		}
		go cmd.Wait()
	}

	// Step 3: piggyback on the pdf-watch reopen pipeline so the in-TUI
	// pane picks up the same on-disk PDF + SyncTeX Skim was just told
	// to revert against. Without this, R refreshed Skim but left the
	// embedded preview frozen on the previous build's render.
	cmd := m.forcePDFReopen()
	m.Status = "Skim reloaded"
	return m, cmd
}

// forcePDFReopen kicks off the pdf-watch async reopen path so the
// in-TUI pane catches up to the on-disk PDF + SyncTeX. Gated on the
// same mtime-advanced check handlePDFWatch uses: when neither artefact
// has changed, this is a no-op.
//
// The gate is load-bearing — reopening an unchanged pair would still
// route through applyPDFWatchResult, which unconditionally clears
// BuildStale. After a failed rebuild that flag is what suppresses auto
// renders against the now-mismatched SyncTeX index; clobbering it lets
// the next cursor move feed new line numbers into the stale index and
// render a wrong region.
func (m *Model) forcePDFReopen() tea.Cmd {
	if m.PDF == nil || m.Doc == nil || m.Doc.File == "" {
		return nil
	}
	buildRes := build.ResolveBuildOutputsOnDisk(m.Doc.File)
	pdfPath := buildRes.PDFPath
	sxPath := buildRes.SyncTeXPath
	pdfStat, err := os.Stat(pdfPath)
	if err != nil {
		return nil
	}
	sxStat, err := os.Stat(sxPath)
	if err != nil {
		return nil
	}
	pdfChanged := pdfStat.ModTime().After(m.PDF.Mtime())
	sxChanged := sxStat.ModTime().After(m.SyncTeXMtime)
	if !pdfChanged && !sxChanged {
		return nil
	}
	if m.BuildStale {
		// A source-watch reload is mid-flight (phase 1 installed the new
		// doc, phase 2 still owes us the rebuilt artefacts). Let it own
		// the swap so we don't double-apply against the same on-disk pair.
		return nil
	}
	m.pdfWatchGen++
	gen := m.pdfWatchGen
	oldPDF := m.PDF
	return func() tea.Msg {
		return performPDFWatchReopen(pdfPath, sxPath, gen, oldPDF)
	}
}
