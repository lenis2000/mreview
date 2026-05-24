package diffui

import (
	"strings"
	"testing"

	"mreview/pkg/diffreview"
)

func TestBuildOutlineMarkersAndStats(t *testing.T) {
	review := fixtureReview()

	rows := BuildOutline(review, FilterAll, nil, map[string]string{"fmt": "note"}, map[string][]string{"deleted": []string{"issue"}})
	markers := map[string]string{}
	for _, row := range rows {
		markers[row.PairID] = row.Marker
	}

	want := map[string]string{
		"same":    "≡",
		"changed": "~",
		"added":   "+",
		"deleted": "-",
		"fmt":     "fmt",
		"moved":   "↷",
	}
	for id, marker := range want {
		if markers[id] != marker {
			t.Fatalf("marker for %s: got %q want %q", id, markers[id], marker)
		}
	}
	if !rows[4].Annotated {
		t.Fatalf("expected annotated marker on fmt row")
	}
	if !rows[3].Issues {
		t.Fatalf("expected issues marker on deleted row")
	}

	m := New(review, Options{Filter: FilterAll, Annotations: map[string]string{"fmt": "note"}, Issues: map[string][]string{"deleted": {"issue"}}})
	outline := m.renderOutline(120, 10)
	for _, needle := range []string{"stats total:6", "≡:1", "~:1", "+:1", "-:1", "fmt:1", "↷:1"} {
		if !strings.Contains(outline, needle) {
			t.Fatalf("outline stats missing %q in:\n%s", needle, outline)
		}
	}
}

func TestFilterBehavior(t *testing.T) {
	review := fixtureReview()

	m := New(review, Options{})
	if m.Filter != FilterChanged {
		t.Fatalf("default filter: got %s want changed", m.Filter)
	}
	if got := visibleIDs(m); strings.Join(got, ",") != "changed,added,deleted,fmt,moved" {
		t.Fatalf("changed filter visible ids = %v", got)
	}

	m.Filter = FilterUnreviewed
	m.Reviewed["changed"] = true
	if got := visibleIDs(m); strings.Join(got, ",") != "added,deleted,fmt,moved" {
		t.Fatalf("unreviewed filter visible ids = %v", got)
	}

	m.Filter = FilterAnnotated
	m.Annotations["fmt"] = "format note"
	if got := visibleIDs(m); strings.Join(got, ",") != "fmt" {
		t.Fatalf("annotated filter visible ids = %v", got)
	}

	m.Filter = FilterIssues
	m.Issues["deleted"] = []string{"needs decision"}
	if got := visibleIDs(m); strings.Join(got, ",") != "deleted" {
		t.Fatalf("issues filter visible ids = %v", got)
	}
}

func visibleIDs(m Model) []string {
	indices := m.visibleIndices()
	out := make([]string, 0, len(indices))
	for _, idx := range indices {
		out = append(out, m.Review.Pairs[idx].ID)
	}
	return out
}

func fixtureReview() *diffreview.Review {
	pairs := []diffreview.Pair{
		{
			ID:             "same",
			Status:         diffreview.Unchanged,
			Old:            fixtureBlock("old-same", 1, "Same paragraph."),
			New:            fixtureBlock("new-same", 1, "Same paragraph."),
			OldIndex:       0,
			NewIndex:       0,
			SectionPathOld: []string{"Intro"},
			SectionPathNew: []string{"Intro"},
		},
		{
			ID:             "changed",
			Status:         diffreview.Changed,
			Old:            fixtureBlock("old-changed", 3, "Alpha\nold beta"),
			New:            fixtureBlock("new-changed", 3, "Alpha\nnew beta"),
			OldIndex:       1,
			NewIndex:       1,
			SectionPathOld: []string{"Intro"},
			SectionPathNew: []string{"Intro"},
		},
		{
			ID:             "added",
			Status:         diffreview.Added,
			New:            fixtureBlock("new-added", 6, "Added line one.\nAdded line two."),
			OldIndex:       -1,
			NewIndex:       2,
			SectionPathNew: []string{"Intro"},
		},
		{
			ID:             "deleted",
			Status:         diffreview.Deleted,
			Old:            fixtureBlock("old-deleted", 9, "Deleted line one.\nDeleted line two."),
			OldIndex:       2,
			NewIndex:       -1,
			SectionPathOld: []string{"Methods"},
		},
		{
			ID:             "fmt",
			Status:         diffreview.FormatOnly,
			Old:            fixtureBlock("old-fmt", 12, "A  B"),
			New:            fixtureBlock("new-fmt", 12, "A B"),
			OldIndex:       3,
			NewIndex:       3,
			SectionPathOld: []string{"Methods"},
			SectionPathNew: []string{"Methods"},
		},
		{
			ID:             "moved",
			Status:         diffreview.Moved,
			Old:            fixtureBlock("old-moved", 14, "\\begin{theorem}\\label{thm:moved}\nOld section.\n\\end{theorem}"),
			New:            fixtureBlock("new-moved", 14, "\\begin{theorem}\\label{thm:moved}\nNew section.\n\\end{theorem}"),
			OldIndex:       4,
			NewIndex:       4,
			SectionPathOld: []string{"Old section"},
			SectionPathNew: []string{"New section"},
		},
	}
	return &diffreview.Review{Pairs: pairs}
}
