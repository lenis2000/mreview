package diffui

import (
	"os/exec"
	"reflect"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"mreview/pkg/diffreview"
)

func TestCompareEditorArgvBuilderForMatchedAddedAndDeletedPairs(t *testing.T) {
	m := New(fixtureReviewWithPaths(), Options{Filter: FilterAll})

	m.Cursor = 1
	m.SourceLineCursor = 2
	target, err := m.compareTarget()
	if err != nil {
		t.Fatalf("matched compare target: %v", err)
	}
	assertArgv(t, buildCompareEditorArgv("zed", nil, target), []string{
		"/repo/.mreview-diff/session/paper.old.tex:4",
		"/repo/paper.tex:4",
	})

	m.Cursor = 2
	m.SourceLineCursor = 1
	target, err = m.compareTarget()
	if err != nil {
		t.Fatalf("added compare target: %v", err)
	}
	assertArgv(t, buildCompareEditorArgv("zed", nil, target), []string{
		"/repo/.mreview-diff/session/paper.old.tex:4",
		"/repo/paper.tex:6",
	})

	m.Cursor = 3
	target, err = m.compareTarget()
	if err != nil {
		t.Fatalf("deleted compare target: %v", err)
	}
	assertArgv(t, buildCompareEditorArgv("zed", nil, target), []string{
		"/repo/.mreview-diff/session/paper.old.tex:9",
		"/repo/paper.tex:12",
	})
}

func TestCompareEditorArgvDefaultsToPlainPathsForNonZedCommand(t *testing.T) {
	target := compareTarget{
		OldPath: "/repo/old.tex",
		NewPath: "/repo/new.tex",
		OldLine: 10,
		NewLine: 20,
	}
	assertArgv(t, buildCompareEditorArgv("/bin/true", []string{"--flag"}, target), []string{
		"--flag",
		"/repo/old.tex",
		"/repo/new.tex",
	})
}

func TestOpenCompareEditorMissingZedGivesStatus(t *testing.T) {
	t.Setenv("MREVIEW_COMPARE_EDITOR", "")
	t.Setenv("PATH", t.TempDir())

	m := New(fixtureReviewWithPaths(), Options{})
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("Z")})
	if cmd != nil {
		t.Fatalf("expected no command when compare editor is missing")
	}
	nm, ok := next.(Model)
	if !ok {
		t.Fatalf("unexpected model type %T", next)
	}
	if !strings.Contains(nm.Status, "no compare editor found") {
		t.Fatalf("missing-editor status = %q", nm.Status)
	}
}

func TestOpenZedInitSchedulesOneOpenCommand(t *testing.T) {
	truePath, err := exec.LookPath("true")
	if err != nil {
		t.Fatalf("true not found: %v", err)
	}
	t.Setenv("MREVIEW_COMPARE_EDITOR", truePath)
	m := New(fixtureReviewWithPaths(), Options{OpenZed: true})

	saved := runDiffCompareProcess
	var calls int
	var gotArgs []string
	runDiffCompareProcess = func(cmd *exec.Cmd) tea.Cmd {
		calls++
		gotArgs = append([]string{}, cmd.Args...)
		return func() tea.Msg { return diffCompareFinishedMsg{} }
	}
	t.Cleanup(func() { runDiffCompareProcess = saved })

	cmd := m.Init()
	if cmd == nil {
		t.Fatalf("expected --open-zed to schedule a command")
	}
	if calls != 1 {
		t.Fatalf("scheduled compare commands = %d, want 1", calls)
	}
	if len(gotArgs) != 3 || gotArgs[0] != truePath ||
		gotArgs[1] != "/repo/.mreview-diff/session/paper.old.tex" ||
		gotArgs[2] != "/repo/paper.tex" {
		t.Fatalf("unexpected command args: %#v", gotArgs)
	}

	next, _ := m.Update(cmd())
	nm, ok := next.(Model)
	if !ok {
		t.Fatalf("unexpected model type %T", next)
	}
	if nm.Status != "opened old+new in Zed" {
		t.Fatalf("status after command = %q", nm.Status)
	}
}

func assertArgv(t *testing.T, got, want []string) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("argv = %#v, want %#v", got, want)
	}
}

func fixtureReviewWithPaths() *diffreview.Review {
	review := fixtureReview()
	review.Old = diffreview.Endpoint{
		Kind:         diffreview.GitBlob,
		Spec:         "HEAD:paper.tex",
		Path:         "/repo/.mreview-diff/session/paper.old.tex",
		Materialized: true,
	}
	review.New = diffreview.Endpoint{
		Kind: diffreview.WorkingFile,
		Spec: "paper.tex",
		Path: "/repo/paper.tex",
	}
	return review
}
