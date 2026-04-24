package ui

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"

	tea "github.com/charmbracelet/bubbletea"
)

// skimDisplaylinePath is the SyncTeX forward-search helper shipped with
// Skim.app — the same binary vimtex's `<leader>ld` invokes. Taking
// (line, pdf, tex) it tells Skim to jump to (and highlight) the region
// that SyncTeX maps the source line to.
const skimDisplaylinePath = "/Applications/Skim.app/Contents/SharedSupport/displayline"

// openInSkim runs the Skim forward-search then focuses Skim.app on the
// current PDF. Preference order:
//
//  1. `displayline` with (line, pdf, tex) — line-accurate, matches the
//     vimtex `\ld` workflow and highlights the target region.
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
			cmd := exec.Command(skimDisplaylinePath,
				strconv.Itoa(line), pdfPath, m.Doc.File)
			if err := cmd.Start(); err == nil {
				go cmd.Wait()
				_ = exec.Command("osascript", "-e",
					`tell application "Skim" to activate`).Start()
				m.Status = ""
				return m, nil
			}
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
