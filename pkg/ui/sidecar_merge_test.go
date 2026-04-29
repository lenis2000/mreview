package ui

import (
	"reflect"
	"sort"
	"testing"

	"mreview/pkg/persist"
)

// ann constructs a persist.Annotation tersely so the table-driven tests
// below stay readable. Only fields the merge logic actually consults are
// settable; the rest stay at their zero values.
func ann(blockID string, lineOffset int, note string) persist.Annotation {
	return persist.Annotation{BlockID: blockID, LineOffset: lineOffset, Note: note}
}

func TestMergeAnnotations(t *testing.T) {
	cases := []struct {
		name string
		base []persist.Annotation
		disk []persist.Annotation
		mem  []persist.Annotation
		want []persist.Annotation
	}{
		{
			name: "empty everywhere",
			want: nil,
		},
		{
			name: "agent deletes one, user untouched",
			base: []persist.Annotation{ann("a", 0, "n1"), ann("b", 0, "n2")},
			disk: []persist.Annotation{ann("a", 0, "n1")},
			mem:  []persist.Annotation{ann("a", 0, "n1"), ann("b", 0, "n2")},
			want: []persist.Annotation{ann("a", 0, "n1")},
		},
		{
			name: "user adds while agent deletes a different one",
			base: []persist.Annotation{ann("a", 0, "n1"), ann("b", 0, "n2")},
			disk: []persist.Annotation{ann("a", 0, "n1")},
			mem:  []persist.Annotation{ann("a", 0, "n1"), ann("b", 0, "n2"), ann("c", 0, "n3")},
			want: []persist.Annotation{ann("a", 0, "n1"), ann("c", 0, "n3")},
		},
		{
			name: "user edits note while agent deletes a different one",
			base: []persist.Annotation{ann("a", 0, "n1"), ann("b", 0, "n2")},
			disk: []persist.Annotation{ann("a", 0, "n1")},
			mem:  []persist.Annotation{ann("a", 0, "n1-edited"), ann("b", 0, "n2")},
			want: []persist.Annotation{ann("a", 0, "n1-edited")},
		},
		{
			name: "agent deletes annotation user just edited (resurrect — user wins)",
			base: []persist.Annotation{ann("a", 0, "n1")},
			disk: []persist.Annotation{},
			mem:  []persist.Annotation{ann("a", 0, "n1-edited")},
			want: []persist.Annotation{ann("a", 0, "n1-edited")},
		},
		{
			name: "user deletes one (mem missing it), agent untouched",
			base: []persist.Annotation{ann("a", 0, "n1"), ann("b", 0, "n2")},
			disk: []persist.Annotation{ann("a", 0, "n1"), ann("b", 0, "n2")},
			mem:  []persist.Annotation{ann("a", 0, "n1")},
			want: []persist.Annotation{ann("a", 0, "n1")},
		},
		{
			name: "block-level and line-pinned coexist on same block",
			base: []persist.Annotation{ann("a", 0, "block"), ann("a", 3, "line3")},
			disk: []persist.Annotation{ann("a", 0, "block")}, // agent deleted line3
			mem:  []persist.Annotation{ann("a", 0, "block"), ann("a", 3, "line3")},
			want: []persist.Annotation{ann("a", 0, "block")},
		},
		{
			name: "user adds two while agent deletes one — order: disk first, then user adds",
			base: []persist.Annotation{ann("a", 0, "n1"), ann("b", 0, "n2")},
			disk: []persist.Annotation{ann("a", 0, "n1")},
			mem:  []persist.Annotation{ann("a", 0, "n1"), ann("b", 0, "n2"), ann("c", 0, "n3"), ann("d", 0, "n4")},
			want: []persist.Annotation{ann("a", 0, "n1"), ann("c", 0, "n3"), ann("d", 0, "n4")},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := mergeAnnotations(tc.base, tc.disk, tc.mem)
			if len(got) == 0 && len(tc.want) == 0 {
				return
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("mergeAnnotations\nbase: %+v\ndisk: %+v\nmem:  %+v\nwant: %+v\ngot:  %+v",
					tc.base, tc.disk, tc.mem, tc.want, got)
			}
		})
	}
}

func TestMergeReviewed(t *testing.T) {
	cases := []struct {
		name              string
		base, disk, mem   []string
		want              []string
	}{
		{
			name: "all empty",
			want: []string{},
		},
		{
			name: "agent removed one, user untouched",
			base: []string{"a", "b"},
			disk: []string{"a"},
			mem:  []string{"a", "b"},
			want: []string{"a"},
		},
		{
			name: "user toggled new id while agent deleted a different one",
			base: []string{"a", "b"},
			disk: []string{"a"},
			mem:  []string{"a", "b", "c"},
			want: []string{"a", "c"},
		},
		{
			name: "user un-toggled (removed in mem) wins over disk",
			base: []string{"a", "b"},
			disk: []string{"a", "b"},
			mem:  []string{"a"},
			want: []string{"a"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := mergeReviewed(tc.base, tc.disk, tc.mem)
			// Both empty-slice and nil are acceptable for the "all empty" case.
			if len(got) == 0 && len(tc.want) == 0 {
				return
			}
			sort.Strings(got)
			sort.Strings(tc.want)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("mergeReviewed: want %v got %v", tc.want, got)
			}
		})
	}
}

func TestMergeSidecar_FullObject(t *testing.T) {
	base := SidecarBase{
		Annotations: []persist.Annotation{ann("a", 0, "n1"), ann("b", 0, "n2")},
		Reviewed:    []string{"a"},
	}
	disk := &persist.Sidecar{
		Paper:       "paper.tex",
		PDF:         "paper.pdf",
		Annotations: []persist.Annotation{ann("a", 0, "n1")},          // agent deleted "b"
		Reviewed:    []string{"a"},
	}
	mem := &persist.Sidecar{
		Annotations: []persist.Annotation{ann("a", 0, "n1"), ann("b", 0, "n2"), ann("c", 0, "n3")}, // user added "c"
		Reviewed:    []string{"a", "d"},                                                               // user toggled "d"
	}

	got, nDeleted := MergeSidecar(base, disk, mem, "cursor-x")
	if nDeleted != 1 {
		t.Fatalf("agent deletion count: want 1 got %d", nDeleted)
	}
	if got.Cursor != "cursor-x" {
		t.Fatalf("Cursor: want cursor-x got %q", got.Cursor)
	}
	if got.Paper != "paper.tex" || got.PDF != "paper.pdf" {
		t.Fatalf("Paper/PDF should come from disk, got Paper=%q PDF=%q", got.Paper, got.PDF)
	}
	wantAnnots := []persist.Annotation{ann("a", 0, "n1"), ann("c", 0, "n3")}
	if !reflect.DeepEqual(got.Annotations, wantAnnots) {
		t.Fatalf("Annotations: want %+v got %+v", wantAnnots, got.Annotations)
	}
	wantReviewed := []string{"a", "d"}
	sort.Strings(got.Reviewed)
	if !reflect.DeepEqual(got.Reviewed, wantReviewed) {
		t.Fatalf("Reviewed: want %v got %v", wantReviewed, got.Reviewed)
	}
}

func TestSnapshotSidecar_DeepEnough(t *testing.T) {
	s := &persist.Sidecar{
		Annotations: []persist.Annotation{ann("a", 0, "n1")},
		Reviewed:    []string{"a", "b"},
	}
	snap := SnapshotSidecar(s)
	// Mutate the original; snapshot must not change.
	s.Annotations[0].Note = "changed"
	s.Reviewed[0] = "z"
	if snap.Annotations[0].Note != "n1" {
		t.Fatalf("snapshot Annotation aliased: got Note=%q", snap.Annotations[0].Note)
	}
	if snap.Reviewed[0] != "a" {
		t.Fatalf("snapshot Reviewed aliased: got %q", snap.Reviewed[0])
	}
}
