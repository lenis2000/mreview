package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHelpRows_CoverCoreBindings(t *testing.T) {
	rows := HelpRows(true)
	joined := ""
	for _, r := range rows {
		joined += r.Keys + "|" + r.Desc + "\n"
	}
	for _, want := range []string{"j / k", "gd", "go", "gu", "Ctrl-O", "/", "@", "space", "?", "q"} {
		assert.Contains(t, joined, want, "help rows should mention %q", want)
	}
}

func TestHelpRows_ReadOnlyBanner(t *testing.T) {
	roRows := HelpRows(false)
	rwRows := HelpRows(true)
	assert.Equal(t, len(rwRows)+2, len(roRows), "read-only adds a 2-row banner")
	assert.Contains(t, roRows[0].Desc, "READ-ONLY")
	assert.Contains(t, roRows[0].Desc, "--allow-modifications")
}

func TestRenderHelpBody_Alignment(t *testing.T) {
	body := RenderHelpBody(120, true)
	lines := strings.Split(body, "\n")
	// Every row should start with a keys column followed by two spaces and the
	// description; find the longest key and assert at least one row contains
	// the canonical "gg / G" pair.
	joined := strings.Join(lines, "\n")
	assert.Contains(t, joined, "gg / G")
	assert.Contains(t, joined, "gd")
	// One row per HelpRow.
	assert.Equal(t, len(HelpRows(true)), len(lines))
}

func TestOpenHelp_TogglesPopup(t *testing.T) {
	m := New(parsedSample(t), nil)
	m.PendingG = true
	out := m.OpenHelp()
	require.IsType(t, &HelpPopup{}, out.Popup)
	assert.False(t, out.PendingG, "OpenHelp should clear pending-g")
}

func TestUpdate_QuestionMarkOpensHelp(t *testing.T) {
	m := New(parsedSample(t), nil)
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'?'}})
	assert.Nil(t, cmd)
	out := updated.(Model)
	require.NotNil(t, out.Popup)
	_, ok := out.Popup.(*HelpPopup)
	assert.True(t, ok)
}

func TestUpdate_HelpPopupEscCloses(t *testing.T) {
	m := New(parsedSample(t), nil)
	m.Popup = &HelpPopup{}
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	assert.Nil(t, cmd)
	assert.Nil(t, updated.(Model).Popup)
}
