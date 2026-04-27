package pdfreview

// Kind values: the semantic role of a comment item, set by the anchoring
// LLM (and rewritable by the user via `c` in the viewer).
const (
	KindComment      = "comment"
	KindMinor        = "minor"
	KindFramingIntro = "framing-intro"
	KindFramingOutro = "framing-outro"
	KindMeta         = "meta"
)

// Status values: written by the viewer; the writer initializes to
// StatusPending.
const (
	StatusPending = "pending"
	StatusKept    = "kept"
	StatusEdited  = "edited"
	StatusDropped = "dropped"
)

// Comment is one item in the anchored-comments JSON. The schema is the
// contract between `pdf-comments` (writer) and `pdf-review` (reader).
type Comment struct {
	ID           int    `json:"id"`
	OriginalText string `json:"original_text"`
	Page         int    `json:"page"`
	Quote        string `json:"quote"`
	Confidence   string `json:"confidence"`
	Kind         string `json:"kind"`
	Status       string `json:"status"`
}

// Report is the on-disk envelope written next to the PDF.
type Report struct {
	SourceMD  string    `json:"source_md"`
	SourcePDF string    `json:"source_pdf"`
	Generated string    `json:"generated"`
	Model     string    `json:"model"`
	Comments  []Comment `json:"comments"`
}

// ValidKind reports whether s is a recognised kind.
func ValidKind(s string) bool {
	switch s {
	case KindComment, KindMinor, KindFramingIntro, KindFramingOutro, KindMeta:
		return true
	}
	return false
}

// AllKinds is the canonical bucket order used by the viewer's list pane
// and the letter renderer.
var AllKinds = []string{KindComment, KindMinor, KindFramingIntro, KindFramingOutro, KindMeta}
