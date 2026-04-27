package pdfreview

// Keymap holds the literal `KeyMsg.String()` forms for the viewer's
// bindings. Field order mirrors the help table so a developer looking at
// either file sees the same shape.
type Keymap struct {
	Quit         []string // q, :q   — save + write letter + exit
	QuitNoLetter []string // Q       — save only, no letter

	Down    []string // j, down
	Up      []string // k, up
	NextBkt []string // J — next kind bucket
	PrevBkt []string // K — prev kind bucket
	First   []string // g
	Last    []string // G

	JumpPage []string // enter, space — jump PDF to current comment's page

	PageNext []string // ]
	PagePrev []string // [
	ZoomIn   []string // +
	ZoomOut  []string // -

	EditText  []string // e — edit original_text only
	EditYAML  []string // E — full re-anchor edit (text + page + quote + kind)
	MarkKept  []string // K (capital)
	MarkDrop  []string // D
	CycleKind []string // c
	NewItem   []string // n

	WriteNow []string // w — write PAPER.review.md without exiting
	Help     []string // ? — toggle help modal
}

// DefaultKeymap is the built-in set. Hard-coded in v1; if LP wants
// overrides, add a [pdf_review] section to config.toml later.
func DefaultKeymap() Keymap {
	return Keymap{
		Quit:         []string{"q"},
		QuitNoLetter: []string{"Q"},

		Down:    []string{"j", "down"},
		Up:      []string{"k", "up"},
		NextBkt: []string{"J"},
		PrevBkt: []string{"K"},
		First:   []string{"g"},
		Last:    []string{"G"},

		JumpPage: []string{"enter", " "},

		PageNext: []string{"]"},
		PagePrev: []string{"["},
		ZoomIn:   []string{"+", "="},
		ZoomOut:  []string{"-"},

		EditText:  []string{"e"},
		EditYAML:  []string{"E"},
		MarkKept:  []string{"K"},
		MarkDrop:  []string{"D"},
		CycleKind: []string{"c"},
		NewItem:   []string{"n"},

		WriteNow: []string{"w"},
		Help:     []string{"?"},
	}
}

func keyMatches(s string, bindings []string) bool {
	for _, b := range bindings {
		if s == b {
			return true
		}
	}
	return false
}
