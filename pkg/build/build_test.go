package build

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolveBuildOutputs(t *testing.T) {
	got := ResolveBuildOutputs("/tmp/papers/foo.tex")
	assert.Equal(t, "/tmp/papers/foo.pdf", got.PDFPath)
	assert.Equal(t, "/tmp/papers/foo.synctex.gz", got.SyncTeXPath)
	assert.Equal(t, "/tmp/papers/foo.aux", got.AuxPath)
	assert.Equal(t, "/tmp/papers/foo.bbl", got.BBLPath)
	assert.Equal(t, "/tmp/papers/foo.log", got.LogPath)
}

func TestResolveBuildOutputs_NoExtension(t *testing.T) {
	got := ResolveBuildOutputs("paper.tex")
	assert.Equal(t, "paper.pdf", filepath.Base(got.PDFPath))
}

func TestScanLogForErrors(t *testing.T) {
	cases := []struct {
		name    string
		content string
		want    string
	}{
		{
			name:    "clean log",
			content: "(./foo.tex\nLaTeX2e <2024-06-01>\nOutput written on foo.pdf (1 page).\n",
			want:    "",
		},
		{
			name:    "tex bang error",
			content: "(./foo.tex\n! Undefined control sequence.\nl.5 \\foo\n",
			want:    "! Undefined control sequence.",
		},
		{
			name:    "undefined reference warning",
			content: "Output written on foo.pdf\nLaTeX Warning: Reference `thm:nope' on page 1 undefined on input line 12.\n",
			want:    "LaTeX Warning: Reference `thm:nope' on page 1 undefined on input line 12.",
		},
		{
			name:    "undefined citation warning",
			content: "Package natbib Warning: Citation `Bar2024' on page 1 undefined on input line 9.\n",
			want:    "Package natbib Warning: Citation `Bar2024' on page 1 undefined on input line 9.",
		},
		{
			name:    "ignored unrelated warning",
			content: "LaTeX Warning: Overfull hbox on page 2.\n",
			want:    "",
		},
		{
			name:    "first error wins over later warnings",
			content: "! Missing $ inserted.\nLaTeX Warning: Reference `x' on page 1 undefined on input line 1.\n",
			want:    "! Missing $ inserted.",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := writeTempFile(t, "log", tc.content)
			got := scanLogForErrors(path)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestScanLogForErrors_MissingFile(t *testing.T) {
	assert.Equal(t, "", scanLogForErrors(filepath.Join(t.TempDir(), "absent.log")))
}

func TestTailLines(t *testing.T) {
	content := strings.Join([]string{"a", "b", "c", "d", "e"}, "\n") + "\n"
	path := writeTempFile(t, "log", content)
	tail, err := tailLines(path, 3)
	require.NoError(t, err)
	assert.Equal(t, []string{"c", "d", "e"}, tail)

	all, err := tailLines(path, 10)
	require.NoError(t, err)
	assert.Equal(t, []string{"a", "b", "c", "d", "e"}, all)
}

func TestTailLines_MissingFile(t *testing.T) {
	_, err := tailLines(filepath.Join(t.TempDir(), "absent.log"), 5)
	assert.Error(t, err)
}

// TestRun_MockSuccess uses a fake build command that simply writes a clean
// .log file and exits 0. It verifies that Run returns nil error and the
// expected paths.
func TestRun_MockSuccess(t *testing.T) {
	dir := t.TempDir()
	tex := filepath.Join(dir, "paper.tex")
	require.NoError(t, os.WriteFile(tex, []byte("\\documentclass{article}\\begin{document}hi\\end{document}"), 0o644))

	// fake command: write a clean log + a fake pdf
	cmd := "printf 'Output written on paper.pdf (1 page).\\n' > paper.log && touch paper.pdf"
	res, err := Run(tex, cmd)
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(dir, "paper.pdf"), res.PDFPath)
	assert.Equal(t, filepath.Join(dir, "paper.log"), res.LogPath)
}

// TestResolveBuildOutputsOnDisk_FindsOutdir asserts that when <base>.pdf
// is missing next to the source but present in a build/ subdir, the
// returned Result points at the subdir for every artefact path. This
// covers the layout produced by `latexmk -outdir=build`.
func TestResolveBuildOutputsOnDisk_FindsOutdir(t *testing.T) {
	dir := t.TempDir()
	tex := filepath.Join(dir, "paper.tex")
	require.NoError(t, os.WriteFile(tex, []byte("x"), 0o644))

	outdir := filepath.Join(dir, "build")
	require.NoError(t, os.MkdirAll(outdir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(outdir, "paper.pdf"), []byte("%PDF"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(outdir, "paper.synctex.gz"), []byte{}, 0o644))

	res := ResolveBuildOutputsOnDisk(tex)
	assert.Equal(t, filepath.Join(outdir, "paper.pdf"), res.PDFPath)
	assert.Equal(t, filepath.Join(outdir, "paper.synctex.gz"), res.SyncTeXPath)
	assert.Equal(t, filepath.Join(outdir, "paper.aux"), res.AuxPath)
}

// TestResolveBuildOutputsOnDisk_PrefersConventional asserts that when
// the PDF is next to the source the conventional location wins, even
// if a stale build/ subdir contains an older PDF.
func TestResolveBuildOutputsOnDisk_PrefersConventional(t *testing.T) {
	dir := t.TempDir()
	tex := filepath.Join(dir, "paper.tex")
	require.NoError(t, os.WriteFile(tex, []byte("x"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "paper.pdf"), []byte("%PDF"), 0o644))

	outdir := filepath.Join(dir, "build")
	require.NoError(t, os.MkdirAll(outdir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(outdir, "paper.pdf"), []byte("%PDF-stale"), 0o644))

	res := ResolveBuildOutputsOnDisk(tex)
	assert.Equal(t, filepath.Join(dir, "paper.pdf"), res.PDFPath)
}

// TestRunWith_CustomCmd_DiscoversOutdir wires the rediscovery path: a
// BuildCmd that writes outputs into build/ must yield a Result with
// build/ paths so downstream consumers (PDF-pane open, SyncTeX, sidecar)
// look in the right place.
func TestRunWith_CustomCmd_DiscoversOutdir(t *testing.T) {
	dir := t.TempDir()
	tex := filepath.Join(dir, "paper.tex")
	require.NoError(t, os.WriteFile(tex, []byte("\\documentclass{article}\\begin{document}hi\\end{document}"), 0o644))

	// Custom command writes the artefacts into ./build/. Use printf
	// rather than touch so the discovery's non-zero-size check passes.
	cmd := "mkdir -p build && printf 'Output written on build/paper.pdf (1 page).\\n' > build/paper.log && printf '%%PDF\\n' > build/paper.pdf && printf 'x' > build/paper.synctex.gz && printf 'x' > build/paper.aux && printf 'x' > build/paper.bbl"
	res, err := RunWith(Options{TexPath: tex, BuildCmd: cmd})
	require.NoError(t, err)

	assert.Equal(t, filepath.Join(dir, "build", "paper.pdf"), res.PDFPath)
	assert.Equal(t, filepath.Join(dir, "build", "paper.synctex.gz"), res.SyncTeXPath)
	assert.Equal(t, filepath.Join(dir, "build", "paper.log"), res.LogPath)
}

func TestRun_MockNonZeroExit(t *testing.T) {
	dir := t.TempDir()
	tex := filepath.Join(dir, "paper.tex")
	require.NoError(t, os.WriteFile(tex, []byte("x"), 0o644))

	// fake command: write a clean log but exit non-zero
	cmd := "printf 'something happened\\n' > paper.log && exit 7"
	_, err := Run(tex, cmd)
	require.Error(t, err)
	var be *BuildError
	require.True(t, errors.As(err, &be))
	assert.Contains(t, be.Reason, "command failed")
	assert.NotEmpty(t, be.LogTail)
}

func TestRun_MockLogError(t *testing.T) {
	dir := t.TempDir()
	tex := filepath.Join(dir, "paper.tex")
	require.NoError(t, os.WriteFile(tex, []byte("x"), 0o644))

	// fake command: exit 0 but log contains a `!` error line
	cmd := "printf '! Undefined control sequence.\\nl.5 \\\\foo\\n' > paper.log"
	_, err := Run(tex, cmd)
	require.Error(t, err)
	var be *BuildError
	require.True(t, errors.As(err, &be))
	assert.Equal(t, "! Undefined control sequence.", be.LogIssue)
}

func TestRun_MockUndefinedRefWarning(t *testing.T) {
	dir := t.TempDir()
	tex := filepath.Join(dir, "paper.tex")
	require.NoError(t, os.WriteFile(tex, []byte("x"), 0o644))

	cmd := "printf \"LaTeX Warning: Reference \\`thm:nope' on page 1 undefined on input line 12.\\n\" > paper.log"
	_, err := Run(tex, cmd)
	require.Error(t, err)
	var be *BuildError
	require.True(t, errors.As(err, &be))
	assert.Contains(t, be.LogIssue, "Reference")
	assert.Contains(t, be.LogIssue, "undefined")
}

func TestRun_EmptyTex(t *testing.T) {
	_, err := Run("", "")
	assert.Error(t, err)
}

func TestRun_BuildErrorString(t *testing.T) {
	e := &BuildError{
		TexPath:   "foo.tex",
		Reason:    "command failed",
		LogIssue: "! oops",
		LogTail:   []string{"line1", "line2"},
	}
	s := e.Error()
	assert.Contains(t, s, "foo.tex")
	assert.Contains(t, s, "command failed")
	assert.Contains(t, s, "! oops")
	assert.Contains(t, s, "line1")
	assert.Contains(t, s, "line2")
}

// TestRun_RealLatexmk is gated by testing.Short(); it only runs when latexmk
// is available and -short is not set, providing an end-to-end smoke test.
func TestRun_RealLatexmk(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping real-latexmk test under -short")
	}
	if _, err := exec.LookPath("latexmk"); err != nil {
		t.Skip("latexmk not in PATH")
	}
	dir := t.TempDir()
	tex := filepath.Join(dir, "tiny.tex")
	body := "\\documentclass{article}\\begin{document}hello world\\end{document}\n"
	require.NoError(t, os.WriteFile(tex, []byte(body), 0o644))
	res, err := Run(tex, "")
	require.NoError(t, err)
	_, err = os.Stat(res.PDFPath)
	require.NoError(t, err)
}

func TestShellQuote(t *testing.T) {
	assert.Equal(t, "'foo'", shellQuote("foo"))
	assert.Equal(t, `'it'\''s'`, shellQuote("it's"))
}

// writeTempFile writes content to a new file under t.TempDir() and returns
// its absolute path.
func writeTempFile(t *testing.T, name, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	require.NoError(t, os.WriteFile(p, []byte(content), 0o644))
	return p
}
