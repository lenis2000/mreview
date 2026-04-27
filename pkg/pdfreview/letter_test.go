package pdfreview

import (
	"strings"
	"testing"
)

func TestRenderLetter_OrderAndPrefixes(t *testing.T) {
	items := []Comment{
		{ID: 1, OriginalText: "Dear Zoe,", Kind: KindFramingIntro, Status: StatusKept},
		{ID: 2, OriginalText: "First substantive paragraph.", Kind: KindComment, Page: 12, Status: StatusKept},
		{ID: 3, OriginalText: "private ranking", Kind: KindMeta, Status: StatusKept},
		{ID: 4, OriginalText: "Stray period after (3.7).", Kind: KindMinor, Page: 21, Status: StatusKept},
		{ID: 5, OriginalText: "Second substantive paragraph.", Kind: KindComment, Page: 18, Status: StatusEdited},
		{ID: 6, OriginalText: "dropped item", Kind: KindComment, Page: 9, Status: StatusDropped},
		{ID: 7, OriginalText: "Best, Leo", Kind: KindFramingOutro, Status: StatusKept},
		{ID: 8, OriginalText: "Wording typo on the page.", Kind: KindMinor, Page: 18, Status: StatusKept},
		{ID: 9, OriginalText: "Unanchored remark.", Kind: KindComment, Page: 0, Status: StatusKept},
	}
	out := RenderLetter(items)

	// Order: intro → body comments → minor list → outro.
	idxIntro := strings.Index(out, "Dear Zoe,")
	idxBody1 := strings.Index(out, "First substantive")
	idxBody2 := strings.Index(out, "Second substantive")
	idxBodyU := strings.Index(out, "Unanchored remark")
	idxMinor1 := strings.Index(out, "Stray period")
	idxOutro := strings.Index(out, "Best, Leo")
	if !(idxIntro >= 0 && idxIntro < idxBody1 && idxBody1 < idxBody2 && idxBody2 < idxBodyU && idxBodyU < idxMinor1 && idxMinor1 < idxOutro) {
		t.Fatalf("ordering wrong:\n%s", out)
	}

	// Page prefixes on body and minor; none on framing.
	if !strings.Contains(out, "(page 12) First substantive paragraph.") {
		t.Errorf("missing (page 12) prefix on body comment 2")
	}
	if !strings.Contains(out, "(page 18) Second substantive paragraph.") {
		t.Errorf("missing (page 18) prefix on body comment 5")
	}
	if !strings.Contains(out, "- (page 21) Stray period after (3.7).") {
		t.Errorf("missing minor bullet with (page 21)")
	}
	if !strings.Contains(out, "- (page 18) Wording typo on the page.") {
		t.Errorf("missing minor bullet with (page 18)")
	}

	// Unanchored body comment: no (page N) prefix, just the text.
	if !strings.Contains(out, "\nUnanchored remark.\n") && !strings.Contains(out, "\n\nUnanchored remark.") {
		t.Errorf("unanchored comment should appear without page prefix\n--- got ---\n%s", out)
	}
	if strings.Contains(out, "(page 0)") {
		t.Errorf("page 0 should never produce (page 0) prefix\n%s", out)
	}

	// Excluded: meta, dropped.
	if strings.Contains(out, "private ranking") {
		t.Errorf("meta items should not be exported")
	}
	if strings.Contains(out, "dropped item") {
		t.Errorf("dropped items should not be exported")
	}
}

func TestRenderLetter_Empty(t *testing.T) {
	if got := RenderLetter(nil); got != "\n" && got != "" {
		t.Errorf("empty input → %q, want empty/newline-only", got)
	}
}

func TestRenderLetter_OnlyMinor(t *testing.T) {
	items := []Comment{
		{Kind: KindMinor, OriginalText: "fix typo", Page: 3, Status: StatusKept},
	}
	got := RenderLetter(items)
	if !strings.HasPrefix(strings.TrimSpace(got), "- (page 3) fix typo") {
		t.Errorf("unexpected single-minor output: %q", got)
	}
}
