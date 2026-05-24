package diffreview

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"mreview/pkg/persist"
)

// Annotation is one diff-mode note attached to a semantic old/new pair.
type Annotation struct {
	PairID      string `json:"pair_id" yaml:"pair_id"`
	Status      string `json:"status" yaml:"status,omitempty"`
	Side        string `json:"side" yaml:"side,omitempty"`
	File        string `json:"file" yaml:"file,omitempty"`
	StartLine   int    `json:"start_line" yaml:"start_line,omitempty"`
	EndLine     int    `json:"end_line" yaml:"end_line,omitempty"`
	SourceQuote string `json:"source_quote" yaml:"source_quote,omitempty"`
	Note        string `json:"note" yaml:"note,omitempty"`
}

// Sidecar is the persisted state for one semantic diff-review session.
type Sidecar struct {
	OldSpec      string       `json:"old_spec" yaml:"old_spec,omitempty"`
	OldLabel     string       `json:"old_label" yaml:"old_label,omitempty"`
	NewSpec      string       `json:"new_spec" yaml:"new_spec,omitempty"`
	NewPath      string       `json:"new_path" yaml:"new_path,omitempty"`
	CursorPairID string       `json:"cursor_pair_id" yaml:"cursor_pair_id,omitempty"`
	Reviewed     []string     `json:"reviewed" yaml:"reviewed,omitempty"`
	Annotations  []Annotation `json:"annotations" yaml:"annotations,omitempty"`
	Detached     []Annotation `json:"detached" yaml:"detached,omitempty"`
}

// StdoutFormat selects the quit-time diff summary emission shape.
type StdoutFormat int

const (
	StdoutNone StdoutFormat = iota
	StdoutMarkdown
	StdoutJSON
)

// LoadSidecar reads a diff sidecar from disk. Missing files return an empty
// sidecar so first-time reviews can proceed without a separate existence check.
func LoadSidecar(path string) (*Sidecar, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return &Sidecar{}, nil
		}
		return nil, err
	}
	return ParseSidecar(data)
}

// ParseSidecar decodes the YAML frontmatter from a markdown diff sidecar.
func ParseSidecar(data []byte) (*Sidecar, error) {
	text := strings.TrimLeft(string(data), "\ufeff")
	if strings.TrimSpace(text) == "" {
		return &Sidecar{}, nil
	}
	lines := strings.Split(text, "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return &Sidecar{}, nil
	}
	end := -1
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "---" {
			end = i
			break
		}
	}
	if end < 0 {
		return nil, fmt.Errorf("diffreview: unterminated sidecar frontmatter")
	}
	var side Sidecar
	if err := yaml.Unmarshal([]byte(strings.Join(lines[1:end], "\n")), &side); err != nil {
		return nil, fmt.Errorf("diffreview: sidecar yaml: %w", err)
	}
	return &side, nil
}

// SaveSidecar writes a diff sidecar atomically.
func SaveSidecar(path string, side *Sidecar) error {
	out, err := MarshalSidecar(side)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return persist.WriteFileAtomic(path, out)
}

// MarshalSidecar serializes a sidecar as markdown with YAML frontmatter.
func MarshalSidecar(side *Sidecar) ([]byte, error) {
	if side == nil {
		side = &Sidecar{}
	}
	copySide := *side
	copySide.Reviewed = append([]string(nil), side.Reviewed...)
	sort.Strings(copySide.Reviewed)
	yml, err := yaml.Marshal(&copySide)
	if err != nil {
		return nil, err
	}
	var b strings.Builder
	b.WriteString("---\n")
	b.Write(yml)
	b.WriteString("---\n\n")
	writeSidecarMarkdown(&b, &copySide, nil, false)
	return []byte(b.String()), nil
}

// NewSidecar returns the current metadata shell for review.
func NewSidecar(review *Review) *Sidecar {
	if review == nil {
		return &Sidecar{}
	}
	return &Sidecar{
		OldSpec:  review.Old.Spec,
		OldLabel: review.Old.Label,
		NewSpec:  review.New.Spec,
		NewPath:  endpointDisplayPath(review.New),
	}
}

// RemapSidecar carries a previously loaded sidecar onto the current review.
// Annotations and detached notes whose pair IDs no longer exist remain
// detached instead of being dropped.
func RemapSidecar(loaded *Sidecar, review *Review) *Sidecar {
	if loaded == nil {
		loaded = &Sidecar{}
	}
	out := NewSidecar(review)
	if review == nil {
		out.Reviewed = append([]string(nil), loaded.Reviewed...)
		out.Annotations = append([]Annotation(nil), loaded.Annotations...)
		out.Detached = append([]Annotation(nil), loaded.Detached...)
		out.CursorPairID = loaded.CursorPairID
		return out
	}

	if _, ok := review.ByID[loaded.CursorPairID]; ok {
		out.CursorPairID = loaded.CursorPairID
	}

	seenReviewed := map[string]bool{}
	for _, id := range loaded.Reviewed {
		if id == "" || seenReviewed[id] {
			continue
		}
		if _, ok := review.ByID[id]; !ok {
			continue
		}
		seenReviewed[id] = true
		out.Reviewed = append(out.Reviewed, id)
	}

	tryAttach := func(a Annotation) {
		pair := review.ByID[a.PairID]
		if pair == nil {
			out.Detached = append(out.Detached, a)
			return
		}
		note := strings.TrimSpace(a.Note)
		if note == "" {
			return
		}
		out.UpsertAnnotation(AnnotationForPair(review, pair, note))
	}
	for _, a := range loaded.Annotations {
		tryAttach(a)
	}
	for _, a := range loaded.Detached {
		tryAttach(a)
	}
	return out
}

// DefaultSidecarPath returns the default sidecar path for a diff review.
func DefaultSidecarPath(review *Review) string {
	if review == nil {
		return "mreview-diff.md"
	}
	newPath := defaultSidecarNewPath(review.New)
	if newPath == "" {
		newPath = "mreview-diff"
	}
	base := review.Old.Spec
	if rev, _, ok := splitGitEndpoint(review.Old.Spec); ok {
		base = rev
	} else if base != "" {
		base = filepath.Base(base)
	}
	base = safePathComponent(base)
	if base == "" {
		base = "old"
	}
	return newPath + ".mreview-diff." + base + ".md"
}

func defaultSidecarNewPath(endpoint Endpoint) string {
	if endpoint.Kind == WorkingFile && !endpoint.Materialized && endpoint.Path != "" {
		return endpoint.Path
	}
	if endpoint.RelPath != "" {
		return endpoint.RelPath
	}
	return safePathComponent(endpoint.Spec)
}

// AnnotationForPair builds a block-level annotation record for pair.
func AnnotationForPair(review *Review, pair *Pair, note string) Annotation {
	if pair == nil {
		return Annotation{Note: strings.TrimSpace(note)}
	}
	side := "new"
	block := pair.New
	endpoint := Endpoint{}
	if review != nil {
		endpoint = review.New
	}
	if block == nil {
		side = "old"
		block = pair.Old
		if review != nil {
			endpoint = review.Old
		}
	}
	a := Annotation{
		PairID: pair.ID,
		Status: pair.Status.String(),
		Side:   side,
		Note:   strings.TrimSpace(note),
	}
	if block != nil {
		a.File = block.File
		if a.File == "" {
			a.File = endpointDisplayPath(endpoint)
		}
		a.StartLine = block.StartLine
		a.EndLine = block.EndLine
		a.SourceQuote = block.Source
	}
	return a
}

// ReviewedSet returns the sidecar reviewed IDs as a map.
func (s *Sidecar) ReviewedSet() map[string]bool {
	out := map[string]bool{}
	if s == nil {
		return out
	}
	for _, id := range s.Reviewed {
		if id != "" {
			out[id] = true
		}
	}
	return out
}

// AnnotationNotes returns attached annotations keyed by pair ID.
func (s *Sidecar) AnnotationNotes() map[string]string {
	out := map[string]string{}
	if s == nil {
		return out
	}
	for _, a := range s.Annotations {
		if a.PairID != "" && a.Note != "" {
			out[a.PairID] = a.Note
		}
	}
	return out
}

// SetReviewed updates one pair's reviewed state.
func (s *Sidecar) SetReviewed(pairID string, reviewed bool) {
	if s == nil || pairID == "" {
		return
	}
	out := s.Reviewed[:0]
	for _, id := range s.Reviewed {
		if id == pairID {
			continue
		}
		out = append(out, id)
	}
	if reviewed {
		out = append(out, pairID)
	}
	s.Reviewed = out
}

// UpsertAnnotation inserts or replaces one attached annotation.
func (s *Sidecar) UpsertAnnotation(a Annotation) {
	if s == nil || a.PairID == "" {
		return
	}
	for i := range s.Annotations {
		if s.Annotations[i].PairID == a.PairID {
			s.Annotations[i] = a
			return
		}
	}
	s.Annotations = append(s.Annotations, a)
}

// DeleteAnnotation removes an attached annotation by pair ID.
func (s *Sidecar) DeleteAnnotation(pairID string) bool {
	if s == nil || pairID == "" {
		return false
	}
	for i := range s.Annotations {
		if s.Annotations[i].PairID == pairID {
			s.Annotations = append(s.Annotations[:i], s.Annotations[i+1:]...)
			return true
		}
	}
	return false
}

// ParseStdoutFormat maps the CLI stdout flag to a diff stdout format.
func ParseStdoutFormat(s string) (StdoutFormat, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "md", "markdown":
		return StdoutMarkdown, nil
	case "json":
		return StdoutJSON, nil
	case "none", "off":
		return StdoutNone, nil
	default:
		return StdoutNone, fmt.Errorf("diffreview: unknown stdout format %q", s)
	}
}

// Emit writes the quit-time diff review summary in the selected format.
func Emit(w io.Writer, side *Sidecar, review *Review, format StdoutFormat) error {
	switch format {
	case StdoutNone:
		return nil
	case StdoutMarkdown:
		return EmitMarkdown(w, side, review)
	case StdoutJSON:
		return EmitJSON(w, side, review)
	default:
		return nil
	}
}

// EmitMarkdown writes a concise markdown summary and annotations.
func EmitMarkdown(w io.Writer, side *Sidecar, review *Review) error {
	if side == nil {
		side = NewSidecar(review)
	}
	var b strings.Builder
	writeSidecarMarkdown(&b, side, review, true)
	_, err := io.WriteString(w, b.String())
	return err
}

// EmitJSON writes a machine-readable diff review summary.
func EmitJSON(w io.Writer, side *Sidecar, review *Review) error {
	if side == nil {
		side = NewSidecar(review)
	}
	payload := struct {
		OldSpec      string       `json:"old_spec"`
		OldLabel     string       `json:"old_label,omitempty"`
		NewSpec      string       `json:"new_spec"`
		NewPath      string       `json:"new_path,omitempty"`
		CursorPairID string       `json:"cursor_pair_id,omitempty"`
		Reviewed     []string     `json:"reviewed,omitempty"`
		Pairs        []stdoutPair `json:"pairs,omitempty"`
		Annotations  []Annotation `json:"annotations,omitempty"`
		Detached     []Annotation `json:"detached,omitempty"`
	}{
		OldSpec:      side.OldSpec,
		OldLabel:     side.OldLabel,
		NewSpec:      side.NewSpec,
		NewPath:      side.NewPath,
		CursorPairID: side.CursorPairID,
		Reviewed:     append([]string(nil), side.Reviewed...),
		Pairs:        stdoutPairs(review),
		Annotations:  append([]Annotation(nil), side.Annotations...),
		Detached:     append([]Annotation(nil), side.Detached...),
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(payload)
}

type stdoutPair struct {
	ID     string `json:"id"`
	Status string `json:"status"`
}

func writeSidecarMarkdown(b *strings.Builder, side *Sidecar, review *Review, includeAll bool) {
	b.WriteString("# mreview diff review\n\n")
	fmt.Fprintf(b, "Old: %s", side.OldSpec)
	if side.OldLabel != "" && side.OldLabel != side.OldSpec {
		fmt.Fprintf(b, " (%s)", side.OldLabel)
	}
	b.WriteString("\n")
	fmt.Fprintf(b, "New: %s", side.NewSpec)
	if side.NewPath != "" && side.NewPath != side.NewSpec {
		fmt.Fprintf(b, " (%s)", side.NewPath)
	}
	b.WriteString("\n\n")

	if includeAll || review != nil {
		b.WriteString("## Pair statuses\n\n")
		pairs := stdoutPairs(review)
		if len(pairs) == 0 {
			b.WriteString("(none)\n\n")
		} else {
			for _, pair := range pairs {
				fmt.Fprintf(b, "- %s %s\n", pair.Status, pair.ID)
			}
			b.WriteString("\n")
		}
	}

	b.WriteString("## Annotations\n\n")
	if len(side.Annotations) == 0 {
		b.WriteString("(none)\n")
	} else {
		for _, a := range side.Annotations {
			writeAnnotationMarkdown(b, a)
		}
	}
	if len(side.Detached) > 0 {
		b.WriteString("\n## Detached\n\n")
		for _, a := range side.Detached {
			writeAnnotationMarkdown(b, a)
		}
	}
}

func writeAnnotationMarkdown(b *strings.Builder, a Annotation) {
	fmt.Fprintf(b, "### %s", a.PairID)
	if a.Status != "" {
		fmt.Fprintf(b, " [%s]", a.Status)
	}
	b.WriteString("\n\n")
	if a.Side != "" {
		fmt.Fprintf(b, "Side: %s\n", a.Side)
	}
	if a.File != "" {
		fmt.Fprintf(b, "Source: %s:L%d-L%d\n", a.File, a.StartLine, a.EndLine)
	}
	if a.Side != "" || a.File != "" {
		b.WriteString("\n")
	}
	writeQuote(b, a.SourceQuote)
	note := strings.TrimSpace(a.Note)
	if note != "" {
		b.WriteString(note)
		b.WriteString("\n")
	}
	b.WriteString("\n")
}

func writeQuote(b *strings.Builder, quote string) {
	quote = strings.TrimRight(quote, "\n")
	if quote == "" {
		return
	}
	for _, line := range truncateQuoteLines(quote, 6) {
		if line == "" {
			b.WriteString(">\n")
		} else {
			b.WriteString("> ")
			b.WriteString(line)
			b.WriteString("\n")
		}
	}
	b.WriteString("\n")
}

func truncateQuoteLines(src string, maxLines int) []string {
	if maxLines <= 0 {
		return nil
	}
	lines := strings.Split(src, "\n")
	if len(lines) <= maxLines {
		return lines
	}
	head := maxLines - 1 - (maxLines-1)/2
	tail := maxLines - 1 - head
	out := make([]string, 0, maxLines)
	out = append(out, lines[:head]...)
	out = append(out, "...")
	out = append(out, lines[len(lines)-tail:]...)
	return out
}

func stdoutPairs(review *Review) []stdoutPair {
	if review == nil {
		return nil
	}
	out := make([]stdoutPair, 0, len(review.Pairs))
	for _, pair := range review.Pairs {
		out = append(out, stdoutPair{ID: pair.ID, Status: pair.Status.String()})
	}
	return out
}

func endpointDisplayPath(endpoint Endpoint) string {
	switch {
	case endpoint.Path != "":
		return endpoint.Path
	case endpoint.RelPath != "":
		return endpoint.RelPath
	default:
		return endpoint.Spec
	}
}
