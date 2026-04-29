package ui

import (
	"sort"

	"mreview/pkg/persist"
)

// annotKey identifies an annotation across base / disk / mem snapshots.
// (BlockID, LineOffset) is the same compound key upsertAnnotation uses,
// so a block-level note (LineOffset=0) and a line-pinned note on the
// same block are tracked independently.
type annotKey struct {
	BlockID    string
	LineOffset int
}

func annotKeyOf(a persist.Annotation) annotKey {
	return annotKey{BlockID: a.BlockID, LineOffset: a.LineOffset}
}

// MergeSidecar reconciles three views of a sidecar:
//
//   - base: what we believed was on disk at the last sync (load / save /
//     reload-remap).
//   - disk: what's on disk right now (after an external editor — typically
//     an agent deleting addressed annotations — has run).
//   - mem:  the live in-memory sidecar, including any user edits since
//     the base snapshot.
//
// The result starts from disk (so the agent's deletions survive) and
// applies the user's mem-vs-base delta on top:
//
//   - annotations the user added (in mem, not in base) are upserted;
//   - annotations whose Note the user edited (in mem and base, Note
//     differs) are upserted (resurrects if the agent deleted them
//     concurrently — better to keep an in-flight user edit than to
//     silently drop it);
//   - annotations the user deleted (in base, not in mem) are removed;
//   - reviewed-list toggles are merged as set arithmetic
//     ((disk ∪ userAdded) - userRemoved);
//   - Cursor is taken from cursor (the in-memory CursorBlockID);
//   - Paper / PDF are taken from disk so the merge result reflects
//     whatever the canonical on-disk identity is.
//
// Detached follows the same annotation-merge logic so a user's
// hand-typed detached entry isn't lost if the agent rewrote the file.
//
// Reports the count of agent-side deletions detected (annotations in
// base but not in disk) so the caller can surface a status message.
func MergeSidecar(base SidecarBase, disk *persist.Sidecar, mem *persist.Sidecar, cursor string) (*persist.Sidecar, int) {
	if disk == nil {
		disk = &persist.Sidecar{}
	}
	if mem == nil {
		mem = &persist.Sidecar{}
	}
	out := &persist.Sidecar{
		Paper:  disk.Paper,
		PDF:    disk.PDF,
		Cursor: cursor,
	}
	out.Annotations = mergeAnnotations(base.Annotations, disk.Annotations, mem.Annotations)
	out.Detached = mergeAnnotations(base.Detached, disk.Detached, mem.Detached)
	out.Reviewed = mergeReviewed(base.Reviewed, disk.Reviewed, mem.Reviewed)
	return out, agentDeletionCount(base.Annotations, disk.Annotations)
}

// mergeAnnotations is the per-list merge described in MergeSidecar.
// Annotation order in the result is: disk's order first (preserving the
// agent's view), then any user-added entries appended. Resurrected
// entries land back in their original disk position when possible, else
// at the end.
func mergeAnnotations(base, disk, mem []persist.Annotation) []persist.Annotation {
	baseByKey := indexAnnotations(base)
	memByKey := indexAnnotations(mem)

	out := append([]persist.Annotation(nil), disk...)
	pos := indexPositions(out)

	upsert := func(a persist.Annotation) {
		k := annotKeyOf(a)
		if i, ok := pos[k]; ok {
			out[i] = a
			return
		}
		pos[k] = len(out)
		out = append(out, a)
	}
	remove := func(k annotKey) {
		i, ok := pos[k]
		if !ok {
			return
		}
		out = append(out[:i], out[i+1:]...)
		// rebuild positions because indices shifted
		pos = indexPositions(out)
	}

	// User additions and edits.
	for _, a := range mem {
		k := annotKeyOf(a)
		baseA, hadBase := baseByKey[k]
		switch {
		case !hadBase:
			upsert(a) // userAdded
		case a.Note != baseA.Note:
			upsert(a) // userEdited (resurrects if agent deleted)
		}
	}
	// User deletions: items in base that the user has removed from mem.
	// We only drop them from the result if the agent didn't *also* edit
	// them — but since base and disk both lack a user-deleted edit case
	// here, just remove unconditionally.
	for k := range baseByKey {
		if _, stillInMem := memByKey[k]; !stillInMem {
			// If the user added something with the same key after deleting,
			// memByKey still has it so we won't reach this branch — guard
			// is unnecessary but the comment makes the intent explicit.
			remove(k)
		}
	}
	return out
}

// mergeReviewed is set arithmetic over the three reviewed-ID lists.
// Result: (disk ∪ userAdded) - userRemoved, sorted for stable output.
func mergeReviewed(base, disk, mem []string) []string {
	baseSet := stringSet(base)
	memSet := stringSet(mem)

	out := stringSet(disk)
	// userAdded: in mem, not in base.
	for id := range memSet {
		if _, ok := baseSet[id]; !ok {
			out[id] = struct{}{}
		}
	}
	// userRemoved: in base, not in mem.
	for id := range baseSet {
		if _, ok := memSet[id]; !ok {
			delete(out, id)
		}
	}
	keys := make([]string, 0, len(out))
	for id := range out {
		keys = append(keys, id)
	}
	sort.Strings(keys)
	return keys
}

// agentDeletionCount counts annotations present in base but absent from
// disk — i.e., what the external editor removed since our last sync.
// Used purely for the status-line message.
func agentDeletionCount(base, disk []persist.Annotation) int {
	if len(base) == 0 {
		return 0
	}
	diskSet := make(map[annotKey]struct{}, len(disk))
	for _, a := range disk {
		diskSet[annotKeyOf(a)] = struct{}{}
	}
	n := 0
	for _, a := range base {
		if _, ok := diskSet[annotKeyOf(a)]; !ok {
			n++
		}
	}
	return n
}

func indexAnnotations(xs []persist.Annotation) map[annotKey]persist.Annotation {
	m := make(map[annotKey]persist.Annotation, len(xs))
	for _, a := range xs {
		m[annotKeyOf(a)] = a
	}
	return m
}

func indexPositions(xs []persist.Annotation) map[annotKey]int {
	m := make(map[annotKey]int, len(xs))
	for i, a := range xs {
		m[annotKeyOf(a)] = i
	}
	return m
}

func stringSet(xs []string) map[string]struct{} {
	m := make(map[string]struct{}, len(xs))
	for _, x := range xs {
		m[x] = struct{}{}
	}
	return m
}
