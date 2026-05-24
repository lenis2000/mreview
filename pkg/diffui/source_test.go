package diffui

import (
	"strings"
	"testing"

	"mreview/pkg/diffreview"
	"mreview/pkg/parser"
)

func TestRenderPairSourceForAddedDeletedChangedAndFormatOnly(t *testing.T) {
	review := fixtureReview()

	added := RenderPairSource(pairByID(t, review, "added"), 100, 8)
	if !strings.Contains(added, "(added in new)") || !strings.Contains(added, "+    6 Added line one.") {
		t.Fatalf("added source rendering missing placeholder or added marker:\n%s", added)
	}

	deleted := RenderPairSource(pairByID(t, review, "deleted"), 100, 8)
	if !strings.Contains(deleted, "(deleted from new)") || !strings.Contains(deleted, "-    9 Deleted line one.") {
		t.Fatalf("deleted source rendering missing placeholder or deleted marker:\n%s", deleted)
	}

	changed := RenderPairSource(pairByID(t, review, "changed"), 100, 8)
	if !strings.Contains(changed, "~    4 old beta") || !strings.Contains(changed, "~    4 new beta") {
		t.Fatalf("changed source rendering missing changed line markers:\n%s", changed)
	}

	formatOnly := RenderPairSource(pairByID(t, review, "fmt"), 100, 8)
	if !strings.Contains(formatOnly, "~   12 A  B") || !strings.Contains(formatOnly, "~   12 A B") {
		t.Fatalf("format-only source rendering should show raw line difference:\n%s", formatOnly)
	}
}

func pairByID(t *testing.T, review *diffreview.Review, id string) *diffreview.Pair {
	t.Helper()
	for i := range review.Pairs {
		if review.Pairs[i].ID == id {
			return &review.Pairs[i]
		}
	}
	t.Fatalf("missing pair %q", id)
	return nil
}

func fixtureBlock(id string, startLine int, source string) *parser.Block {
	return &parser.Block{
		ID:        id,
		Kind:      parser.KindParagraph,
		StartLine: startLine,
		EndLine:   startLine + blockLineCount(source) - 1,
		Source:    source,
	}
}
