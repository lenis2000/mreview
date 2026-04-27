package pdfreview

import (
	"fmt"
	"strings"
)

// RenderLetter produces the final markdown letter from a comment list.
// Deterministic: skips dropped + meta items; orders framing-intro →
// substantive comments → minor (as a bulleted list) → framing-outro.
//
// Page-prefix rule: each comment / minor item is prepended with
// "(page N) " when Page > 0; unanchored items (page 0) are emitted
// verbatim. Framing entries are never prefixed.
func RenderLetter(items []Comment) string {
	var (
		intro  []Comment
		body   []Comment
		minors []Comment
		outro  []Comment
	)
	for _, c := range items {
		if c.Status == StatusDropped {
			continue
		}
		if c.Kind == KindMeta {
			continue
		}
		switch c.Kind {
		case KindFramingIntro:
			intro = append(intro, c)
		case KindMinor:
			minors = append(minors, c)
		case KindFramingOutro:
			outro = append(outro, c)
		case KindComment:
			body = append(body, c)
		default:
			// Unknown kind: include as a substantive comment so the user
			// at least sees the text. (Re-classify in the viewer with `c`.)
			body = append(body, c)
		}
	}

	var sb strings.Builder
	writeParas(&sb, intro, false)

	if len(body) > 0 {
		ensureBlank(&sb)
		writeParas(&sb, body, true)
	}

	if len(minors) > 0 {
		ensureBlank(&sb)
		for _, c := range minors {
			sb.WriteString("- ")
			sb.WriteString(prefixedText(c, true))
			sb.WriteByte('\n')
		}
	}

	if len(outro) > 0 {
		ensureBlank(&sb)
		writeParas(&sb, outro, false)
	}

	out := strings.TrimRight(sb.String(), "\n") + "\n"
	return out
}

func writeParas(sb *strings.Builder, items []Comment, withPrefix bool) {
	for i, c := range items {
		if i > 0 {
			sb.WriteString("\n\n")
		}
		sb.WriteString(prefixedText(c, withPrefix))
	}
	sb.WriteByte('\n')
}

func prefixedText(c Comment, withPrefix bool) string {
	text := strings.TrimSpace(c.OriginalText)
	if !withPrefix || c.Page <= 0 {
		return text
	}
	return fmt.Sprintf("(page %d) %s", c.Page, text)
}

// ensureBlank writes a blank-line separator unless we're at the top of the
// buffer or the buffer already ends with two newlines.
func ensureBlank(sb *strings.Builder) {
	s := sb.String()
	if s == "" {
		return
	}
	if !strings.HasSuffix(s, "\n") {
		sb.WriteByte('\n')
		sb.WriteByte('\n')
		return
	}
	if !strings.HasSuffix(s, "\n\n") {
		sb.WriteByte('\n')
	}
}
