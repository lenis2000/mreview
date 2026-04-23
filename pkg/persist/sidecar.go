// Package persist implements the sidecar `<paper>.mreview.md` file used to
// record annotations, reviewed checkboxes, and cursor position across
// mreview sessions.
//
// The on-disk format is a YAML frontmatter block (delimited by `---`) followed
// by markdown annotation sections, each with the heading
//
//	## <Breadcrumb> — `<BlockID>` (<File>:L<start>-L<end>)
//
// a blockquoted source snippet (at most 6 lines, with a middle-ellipsis `…`
// line when the original is longer), a blank line, and a free-text note body.
package persist

import (
	"bufio"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// Annotation is a single user note attached to a parser block.
type Annotation struct {
	BlockID     string
	Breadcrumb  string
	File        string
	StartLine   int
	EndLine     int
	SourceQuote string
	Note        string
}

// Sidecar holds the parsed contents of a `.mreview.md` file.
//
// Detached carries annotations from a previous review session that no longer
// map to any block in the current document — they are kept in a separate
// `## Detached` section so the user can see and salvage them.
type Sidecar struct {
	Paper       string
	PDF         string
	Cursor      string
	Reviewed    []string
	Annotations []Annotation
	Detached    []Annotation
}

// DetachedMarker is the literal heading used to separate the detached
// annotations section from the main annotation body.
const DetachedMarker = "## Detached"

// MaxQuoteLines is the maximum number of lines (including any middle-ellipsis
// marker) emitted in a blockquoted source snippet.
const MaxQuoteLines = 6

// EllipsisLine is the single-line sentinel inserted in the middle of a
// truncated source quote.
const EllipsisLine = "…"

// frontmatter is the YAML shape written to the top of the sidecar.
type frontmatter struct {
	Paper    string   `yaml:"paper"`
	PDF      string   `yaml:"pdf"`
	Cursor   string   `yaml:"cursor,omitempty"`
	Reviewed []string `yaml:"reviewed,omitempty"`
}

// headingRe matches the annotation heading format.
//
// The separator between the breadcrumb and the block-ID is an em-dash (U+2014)
// surrounded by ASCII spaces — note that the em-dash is multi-byte so we use
// a character class in the regex.
var headingRe = regexp.MustCompile(`^## (.+?) \x{2014} ` + "`" + `([^` + "`" + `]+)` + "`" + ` \(([^()]*):L(\d+)-L(\d+)\)\s*$`)

// Load reads a sidecar file from disk. A missing file is not an error: a
// zero-value *Sidecar with empty fields is returned instead so callers can
// proceed with a fresh review session.
func Load(path string) (*Sidecar, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &Sidecar{}, nil
		}
		return nil, err
	}
	return parse(data)
}

// Save writes the sidecar to disk with a deterministic ordering: frontmatter
// first, then annotations in their slice order. Reviewed IDs and frontmatter
// fields are written as given — callers are expected to sort them before
// passing the sidecar in if stable order matters.
func Save(path string, s *Sidecar) error {
	out, err := Marshal(s)
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, out, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// Marshal serialises a *Sidecar into its markdown representation.
func Marshal(s *Sidecar) ([]byte, error) {
	fm := frontmatter{
		Paper:    s.Paper,
		PDF:      s.PDF,
		Cursor:   s.Cursor,
		Reviewed: s.Reviewed,
	}
	yml, err := yaml.Marshal(fm)
	if err != nil {
		return nil, err
	}
	var buf strings.Builder
	buf.WriteString("---\n")
	buf.Write(yml)
	buf.WriteString("---\n")
	for _, a := range s.Annotations {
		buf.WriteString("\n")
		buf.WriteString(formatAnnotation(a))
	}
	if len(s.Detached) > 0 {
		buf.WriteString("\n")
		buf.WriteString(DetachedMarker)
		buf.WriteString("\n")
		for _, a := range s.Detached {
			buf.WriteString("\n")
			buf.WriteString(formatAnnotation(a))
		}
	}
	return []byte(buf.String()), nil
}

func formatAnnotation(a Annotation) string {
	var b strings.Builder
	fmt.Fprintf(&b, "## %s — `%s` (%s:L%d-L%d)\n\n",
		a.Breadcrumb, a.BlockID, a.File, a.StartLine, a.EndLine)
	for _, line := range truncateQuote(a.SourceQuote) {
		if line == "" {
			b.WriteString(">\n")
		} else {
			b.WriteString("> ")
			b.WriteString(line)
			b.WriteString("\n")
		}
	}
	b.WriteString("\n")
	note := strings.TrimRight(a.Note, "\n")
	if note != "" {
		b.WriteString(note)
		b.WriteString("\n")
	}
	return b.String()
}

// truncateQuote returns the lines to render in the blockquote. At most
// MaxQuoteLines are returned; inputs longer than that are collapsed into
// "first N/2 lines", an EllipsisLine, and "last N/2 lines" so that the total
// equals MaxQuoteLines.
func truncateQuote(src string) []string {
	if src == "" {
		return nil
	}
	src = strings.TrimRight(src, "\n")
	lines := strings.Split(src, "\n")
	if len(lines) <= MaxQuoteLines {
		return lines
	}
	// 6-line budget: 3 lines, ellipsis, 2 lines = 6 total.
	head := MaxQuoteLines - 1 - (MaxQuoteLines-1)/2 // 3 for MaxQuoteLines=6
	tail := MaxQuoteLines - 1 - head                // 2 for MaxQuoteLines=6
	out := make([]string, 0, MaxQuoteLines)
	out = append(out, lines[:head]...)
	out = append(out, EllipsisLine)
	out = append(out, lines[len(lines)-tail:]...)
	return out
}

func parse(data []byte) (*Sidecar, error) {
	s := &Sidecar{}
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
	var lines []string
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}

	// Frontmatter: optional, but if present must start on line 0.
	idx := 0
	if len(lines) > 0 && lines[0] == "---" {
		end := -1
		for i := 1; i < len(lines); i++ {
			if lines[i] == "---" {
				end = i
				break
			}
		}
		if end < 0 {
			return nil, fmt.Errorf("persist: unterminated YAML frontmatter")
		}
		yml := strings.Join(lines[1:end], "\n")
		var fm frontmatter
		if err := yaml.Unmarshal([]byte(yml), &fm); err != nil {
			return nil, fmt.Errorf("persist: frontmatter yaml: %w", err)
		}
		s.Paper = fm.Paper
		s.PDF = fm.PDF
		s.Cursor = fm.Cursor
		s.Reviewed = fm.Reviewed
		idx = end + 1
	}

	// Body: sequence of annotation sections. The `## Detached` marker, when
	// encountered, flips the destination to s.Detached for all subsequent
	// annotation sections until EOF.
	detached := false
	for idx < len(lines) {
		line := lines[idx]
		if strings.TrimSpace(line) == "" {
			idx++
			continue
		}
		if strings.TrimSpace(line) == DetachedMarker {
			detached = true
			idx++
			continue
		}
		m := headingRe.FindStringSubmatch(line)
		if m == nil {
			// Unknown content outside an annotation — skip it silently so a
			// manually edited sidecar with stray commentary does not crash
			// the loader.
			idx++
			continue
		}
		a := Annotation{
			Breadcrumb: m[1],
			BlockID:    m[2],
			File:       m[3],
		}
		a.StartLine, _ = strconv.Atoi(m[4])
		a.EndLine, _ = strconv.Atoi(m[5])
		idx++

		// Skip optional blank after heading.
		for idx < len(lines) && strings.TrimSpace(lines[idx]) == "" {
			idx++
		}
		// Blockquote (consecutive `> `-prefixed lines).
		var quote []string
		for idx < len(lines) && strings.HasPrefix(lines[idx], ">") {
			q := strings.TrimPrefix(lines[idx], ">")
			quote = append(quote, strings.TrimPrefix(q, " "))
			idx++
		}
		a.SourceQuote = strings.Join(quote, "\n")

		// Skip blank separators before the note body.
		for idx < len(lines) && strings.TrimSpace(lines[idx]) == "" {
			idx++
		}
		// Note body: everything up to the next `## ` heading or EOF.
		var note []string
		for idx < len(lines) && !strings.HasPrefix(lines[idx], "## ") {
			note = append(note, lines[idx])
			idx++
		}
		a.Note = strings.TrimRight(strings.Join(note, "\n"), "\n")
		if detached {
			s.Detached = append(s.Detached, a)
		} else {
			s.Annotations = append(s.Annotations, a)
		}
	}
	return s, nil
}
