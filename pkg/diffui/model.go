package diffui

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"

	"mreview/pkg/diffreview"
	"mreview/pkg/ui"
)

// Filter selects which semantic diff pairs are visible in the outline.
type Filter int

const (
	// FilterChanged is the default diff-review view.
	FilterChanged Filter = iota
	FilterAll
	FilterUnreviewed
	FilterAnnotated
	FilterIssues
)

// String returns the filter label shown in the status bar.
func (f Filter) String() string {
	switch f {
	case FilterAll:
		return "all"
	case FilterChanged:
		return "changed"
	case FilterUnreviewed:
		return "unreviewed"
	case FilterAnnotated:
		return "annotated"
	case FilterIssues:
		return "issues"
	default:
		return "changed"
	}
}

// CycleFilter rotates through the diff outline filters.
func CycleFilter(f Filter) Filter {
	switch f {
	case FilterAll:
		return FilterChanged
	case FilterChanged:
		return FilterUnreviewed
	case FilterUnreviewed:
		return FilterAnnotated
	case FilterAnnotated:
		return FilterIssues
	default:
		return FilterAll
	}
}

// Options configures a new diff TUI model. Reviewed, Annotations, and Issues
// are intentionally simple maps until Task 5 wires in the diff sidecar.
type Options struct {
	Styles             ui.Styles
	Filter             Filter
	Reviewed           map[string]bool
	Annotations        map[string]string
	Issues             map[string][]string
	AllowModifications bool
	Status             string
}

// Model is the Bubble Tea state for the semantic diff-review skeleton.
type Model struct {
	Review *diffreview.Review

	Cursor int
	Filter Filter

	Width, Height int
	Status        string
	Styles        ui.Styles

	Reviewed           map[string]bool
	Annotations        map[string]string
	Issues             map[string][]string
	AllowModifications bool

	ShowHelp bool
	pendingG bool
	quitting bool
}

// New constructs a diff TUI model with the changed filter selected by
// default and the cursor snapped to the first visible semantic pair.
func New(review *diffreview.Review, opts Options) Model {
	m := Model{
		Review:             review,
		Filter:             opts.Filter,
		Status:             opts.Status,
		Styles:             opts.Styles,
		Reviewed:           copyBoolMap(opts.Reviewed),
		Annotations:        copyStringMap(opts.Annotations),
		Issues:             copyIssueMap(opts.Issues),
		AllowModifications: opts.AllowModifications,
	}
	m.snapCursor()
	return m
}

// Init implements tea.Model.
func (m Model) Init() tea.Cmd { return nil }

// CurrentPair returns the selected semantic pair.
func (m Model) CurrentPair() *diffreview.Pair {
	if m.Review == nil || m.Cursor < 0 || m.Cursor >= len(m.Review.Pairs) {
		return nil
	}
	return &m.Review.Pairs[m.Cursor]
}

func (m Model) visibleIndices() []int {
	if m.Review == nil {
		return nil
	}
	visible := make([]int, 0, len(m.Review.Pairs))
	for i := range m.Review.Pairs {
		if pairMatchesFilter(m.Review.Pairs[i], m.Filter, m.Reviewed, m.Annotations, m.Issues) {
			visible = append(visible, i)
		}
	}
	return visible
}

func (m *Model) snapCursor() {
	if m.Review == nil || len(m.Review.Pairs) == 0 {
		m.Cursor = 0
		return
	}
	visible := m.visibleIndices()
	if len(visible) == 0 {
		if m.Cursor < 0 {
			m.Cursor = 0
		}
		if m.Cursor >= len(m.Review.Pairs) {
			m.Cursor = len(m.Review.Pairs) - 1
		}
		return
	}
	if containsIndex(visible, m.Cursor) {
		return
	}
	for _, idx := range visible {
		if idx >= m.Cursor {
			m.Cursor = idx
			return
		}
	}
	m.Cursor = visible[len(visible)-1]
}

func (m Model) statusText() string {
	pair := m.CurrentPair()
	selected := "-"
	if pair != nil {
		selected = pair.ID
	}
	stats := reviewStats(m.Review)
	base := fmt.Sprintf(
		"filter:%s pair:%s total:%d ~%d +%d -%d fmt%d %s%d",
		m.Filter.String(),
		selected,
		stats.Total,
		stats.Changed,
		stats.Added,
		stats.Deleted,
		stats.FormatOnly,
		StatusMarker(diffreview.Moved),
		stats.Moved,
	)
	if m.Status == "" {
		return base
	}
	return base + " | " + m.Status
}

func pairMatchesFilter(
	pair diffreview.Pair,
	filter Filter,
	reviewed map[string]bool,
	annotations map[string]string,
	issues map[string][]string,
) bool {
	switch filter {
	case FilterAll:
		return true
	case FilterChanged:
		return changedStatus(pair.Status)
	case FilterUnreviewed:
		return changedStatus(pair.Status) && !reviewed[pair.ID]
	case FilterAnnotated:
		return annotations[pair.ID] != ""
	case FilterIssues:
		return len(issues[pair.ID]) > 0
	default:
		return changedStatus(pair.Status)
	}
}

func changedStatus(status diffreview.PairStatus) bool {
	switch status {
	case diffreview.Changed, diffreview.Added, diffreview.Deleted, diffreview.Moved, diffreview.FormatOnly:
		return true
	default:
		return false
	}
}

func containsIndex(values []int, needle int) bool {
	for _, v := range values {
		if v == needle {
			return true
		}
	}
	return false
}

func copyBoolMap(in map[string]bool) map[string]bool {
	if len(in) == 0 {
		return map[string]bool{}
	}
	out := make(map[string]bool, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func copyStringMap(in map[string]string) map[string]string {
	if len(in) == 0 {
		return map[string]string{}
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func copyIssueMap(in map[string][]string) map[string][]string {
	if len(in) == 0 {
		return map[string][]string{}
	}
	out := make(map[string][]string, len(in))
	for k, v := range in {
		out[k] = append([]string(nil), v...)
	}
	return out
}
