package persist

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// StdoutFormat selects the quit-time emission shape.
type StdoutFormat int

const (
	// StdoutNone suppresses the emission entirely.
	StdoutNone StdoutFormat = iota
	// StdoutMarkdown emits the annotation body (no YAML frontmatter) — the
	// same per-annotation markdown format used in the sidecar, ready to be
	// piped into an LLM prompt.
	StdoutMarkdown
	// StdoutJSON emits a JSON array of annotation records.
	StdoutJSON
)

// ParseStdoutFormat maps the CLI flag values ("md", "json", "none") to a
// StdoutFormat. Unknown values return an error.
func ParseStdoutFormat(s string) (StdoutFormat, error) {
	switch strings.ToLower(s) {
	case "", "md", "markdown":
		return StdoutMarkdown, nil
	case "json":
		return StdoutJSON, nil
	case "none", "off":
		return StdoutNone, nil
	}
	return StdoutNone, fmt.Errorf("persist: unknown stdout format %q", s)
}

// Emit writes the sidecar's annotations in the requested format. A nil
// sidecar or StdoutNone is a no-op that returns nil.
func Emit(w io.Writer, s *Sidecar, f StdoutFormat) error {
	if s == nil || f == StdoutNone {
		return nil
	}
	switch f {
	case StdoutMarkdown:
		return EmitMarkdown(w, s)
	case StdoutJSON:
		return EmitJSON(w, s)
	}
	return nil
}

// EmitMarkdown writes the annotation body (attached + detached) to w in the
// same markdown shape Marshal uses, minus the YAML frontmatter. Detached
// annotations are preceded by a `## Detached` separator line when present.
func EmitMarkdown(w io.Writer, s *Sidecar) error {
	if s == nil {
		return nil
	}
	var buf strings.Builder
	for i, a := range s.Annotations {
		if i > 0 {
			buf.WriteString("\n")
		}
		buf.WriteString(formatAnnotation(a, false))
	}
	if len(s.Detached) > 0 {
		if len(s.Annotations) > 0 {
			buf.WriteString("\n")
		}
		buf.WriteString(DetachedMarker)
		buf.WriteString("\n")
		for _, a := range s.Detached {
			buf.WriteString("\n")
			buf.WriteString(formatAnnotation(a, false))
		}
	}
	_, err := io.WriteString(w, buf.String())
	return err
}

// stdoutAnnotation is the JSON shape emitted under --stdout=json. Field names
// match the plan spec (snake_case). Detached annotations have detached=true.
type stdoutAnnotation struct {
	BlockID     string `json:"block_id"`
	Breadcrumb  string `json:"breadcrumb"`
	File        string `json:"file"`
	StartLine   int    `json:"start_line"`
	EndLine     int    `json:"end_line"`
	SourceQuote string `json:"source_quote"`
	Note        string `json:"note"`
	Detached    bool   `json:"detached,omitempty"`
}

// EmitJSON writes a JSON array `[{block_id, breadcrumb, file, start_line,
// end_line, source_quote, note, detached?}]`. Encoding uses an indented
// two-space form followed by a trailing newline.
func EmitJSON(w io.Writer, s *Sidecar) error {
	if s == nil {
		if _, err := io.WriteString(w, "[]\n"); err != nil {
			return err
		}
		return nil
	}
	out := make([]stdoutAnnotation, 0, len(s.Annotations)+len(s.Detached))
	for _, a := range s.Annotations {
		out = append(out, stdoutAnnotation{
			BlockID:     a.BlockID,
			Breadcrumb:  a.Breadcrumb,
			File:        a.File,
			StartLine:   a.StartLine,
			EndLine:     a.EndLine,
			SourceQuote: a.SourceQuote,
			Note:        a.Note,
		})
	}
	for _, a := range s.Detached {
		out = append(out, stdoutAnnotation{
			BlockID:     a.BlockID,
			Breadcrumb:  a.Breadcrumb,
			File:        a.File,
			StartLine:   a.StartLine,
			EndLine:     a.EndLine,
			SourceQuote: a.SourceQuote,
			Note:        a.Note,
			Detached:    true,
		})
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(out)
}
