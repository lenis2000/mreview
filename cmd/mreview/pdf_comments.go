package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/jessevdk/go-flags"

	"mreview/pkg/pdfreview"
)

type pdfCommentsOpts struct {
	Model string `long:"model" default:"opus" description:"value passed to claude --model"`
	Out   string `long:"out" description:"output JSON path (default: <PDF>.pdf-comments.json)"`

	Args struct {
		First  string `positional-arg-name:"REVIEW.md" description:"unstructured AI review comments (.md)"`
		Second string `positional-arg-name:"PAPER.pdf" description:"the preprint PDF (.pdf) — order with REVIEW.md is interchangeable"`
	} `positional-args:"yes" required:"yes"`
}

// classifyMDPDFArgs returns (mdPath, pdfPath) regardless of which positional
// slot held which file. Either order is accepted: `REVIEW.md PAPER.pdf` and
// `PAPER.pdf REVIEW.md` both work.
func classifyMDPDFArgs(a, b string) (md, pdf string, err error) {
	aExt := strings.ToLower(filepath.Ext(a))
	bExt := strings.ToLower(filepath.Ext(b))
	switch {
	case aExt == ".md" && bExt == ".pdf":
		return a, b, nil
	case aExt == ".pdf" && bExt == ".md":
		return b, a, nil
	default:
		return "", "", fmt.Errorf("expected one .md and one .pdf argument (got %q and %q)", a, b)
	}
}

// runPdfComments anchors each comment in REVIEW.md to a page + quote in
// PAPER.pdf via a single `claude -p` call. Output: PAPER.pdf.pdf-comments.json.
func runPdfComments(args []string, stdout, stderr io.Writer) int {
	var o pdfCommentsOpts
	parser := flags.NewParser(&o, flags.HelpFlag|flags.PassDoubleDash)
	parser.Name = "mreview pdf-comments"
	parser.Usage = "[OPTIONS]"
	if _, err := parser.ParseArgs(args); err != nil {
		var fe *flags.Error
		if errors.As(err, &fe) && fe.Type == flags.ErrHelp {
			fmt.Fprintln(stdout, err.Error())
			return 0
		}
		fmt.Fprintf(stderr, "mreview pdf-comments: %v\n", err)
		return 2
	}

	mdPath, pdfPath, err := classifyMDPDFArgs(o.Args.First, o.Args.Second)
	if err != nil {
		fmt.Fprintf(stderr, "mreview pdf-comments: %v\n", err)
		return 2
	}

	logf := func(format string, args ...any) {
		fmt.Fprintf(stderr, "mreview pdf-comments: "+format+"\n", args...)
	}

	mdBytes, err := os.ReadFile(mdPath)
	if err != nil {
		logf("read %q: %v", mdPath, err)
		return 1
	}
	if st, err := os.Stat(pdfPath); err != nil {
		logf("stat %q: %v", pdfPath, err)
		return 1
	} else {
		logf("inputs: md=%s (%d bytes), pdf=%s (%d bytes)",
			mdPath, len(mdBytes), pdfPath, st.Size())
	}

	if _, err := exec.LookPath("pdftotext"); err != nil {
		logf("pdftotext not found on PATH (install poppler)")
		return 1
	}
	if _, err := exec.LookPath("claude"); err != nil {
		logf("claude CLI not found on PATH")
		return 1
	}

	logf("extracting PDF text via pdftotext -layout…")
	t0 := time.Now()
	pagedText, err := extractPagedPDFText(pdfPath)
	if err != nil {
		logf("%v", err)
		return 1
	}
	pages := splitPages(pagedText)
	logf("extracted %d page(s), %d chars total in %s",
		len(pages), len(pagedText), time.Since(t0).Round(time.Millisecond))

	prompt := buildAnchoringPrompt(string(mdBytes), pagedText)

	logf("calling claude (model=%s, prompt=%d chars)…", o.Model, len(prompt))
	claudeStart := time.Now()
	rawResult, err := invokeClaudeVerbose(prompt, o.Model, stderr)
	if err != nil {
		logf("claude: %v", err)
		return 1
	}
	logf("claude returned %d chars in %s",
		len(rawResult), time.Since(claudeStart).Round(time.Second))

	comments, err := parseCommentsArray(rawResult)
	if err != nil {
		logf("parse claude output: %v", err)
		fmt.Fprintf(stderr, "----- raw claude result -----\n%s\n----- end -----\n", rawResult)
		return 1
	}
	logf("parsed %d item(s); anchoring against %d page(s)…", len(comments), len(pages))

	kindCounts := map[string]int{}
	anchored, unanchored := 0, 0
	for i := range comments {
		c := &comments[i]
		c.ID = i + 1
		// Default kind / status when the model omits them. We default to
		// "comment" rather than rejecting so older anchoring outputs still
		// load; the viewer can re-classify with `c`.
		if c.Kind == "" {
			c.Kind = pdfreview.KindComment
		}
		if !pdfreview.ValidKind(c.Kind) {
			fmt.Fprintf(stderr, "mreview pdf-comments: warn: unknown kind %q on item %d, defaulting to %q\n", c.Kind, c.ID, pdfreview.KindComment)
			c.Kind = pdfreview.KindComment
		}
		c.Status = pdfreview.StatusPending
		// Framing / meta items have no in-paper locus.
		switch c.Kind {
		case pdfreview.KindFramingIntro, pdfreview.KindFramingOutro, pdfreview.KindMeta:
			c.Page = 0
			c.Quote = ""
		}
		if c.Page > 0 && c.Page <= len(pages) && c.Quote != "" {
			if !strings.Contains(pages[c.Page-1], c.Quote) {
				c.Quote = ""
				c.Confidence = "low"
			}
		} else if c.Quote != "" && (c.Page <= 0 || c.Page > len(pages)) {
			c.Quote = ""
		}
		// QuoteFocus must also be a verbatim substring of the page text;
		// blank it (without touching the broader Quote anchor) if not.
		if c.QuoteFocus != "" {
			if c.Page <= 0 || c.Page > len(pages) || !strings.Contains(pages[c.Page-1], c.QuoteFocus) {
				c.QuoteFocus = ""
			}
		}
		if c.Page > 0 {
			anchored++
		} else {
			unanchored++
		}
		kindCounts[c.Kind]++
	}
	logf("kinds: comment=%d minor=%d framing-intro=%d framing-outro=%d meta=%d",
		kindCounts[pdfreview.KindComment],
		kindCounts[pdfreview.KindMinor],
		kindCounts[pdfreview.KindFramingIntro],
		kindCounts[pdfreview.KindFramingOutro],
		kindCounts[pdfreview.KindMeta])

	report := pdfreview.Report{
		SourceMD:  mdPath,
		SourcePDF: pdfPath,
		Generated: time.Now().UTC().Format(time.RFC3339),
		Model:     o.Model,
		Comments:  comments,
	}

	outPath := o.Out
	if outPath == "" {
		outPath = pdfreview.ReportPath(pdfPath)
	}
	if err := pdfreview.SaveReport(outPath, &report); err != nil {
		fmt.Fprintf(stderr, "mreview pdf-comments: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "%s: %d comments (%d anchored, %d unanchored)\n",
		outPath, len(comments), anchored, unanchored)
	return 0
}

// extractPagedPDFText runs `pdftotext -layout` and replaces form-feed
// separators with explicit `<<<PAGE N>>>` markers (1-indexed) so the LLM
// can map quotes to page numbers unambiguously.
func extractPagedPDFText(pdfPath string) (string, error) {
	cmd := exec.Command("pdftotext", "-layout", pdfPath, "-")
	out, err := cmd.Output()
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return "", fmt.Errorf("pdftotext %q: %w (stderr: %s)",
				pdfPath, err, strings.TrimSpace(string(ee.Stderr)))
		}
		return "", fmt.Errorf("pdftotext %q: %w", pdfPath, err)
	}
	pages := strings.Split(string(out), "\f")
	// pdftotext appends a trailing form-feed → empty final element. Drop it.
	if n := len(pages); n > 0 && strings.TrimSpace(pages[n-1]) == "" {
		pages = pages[:n-1]
	}
	var b strings.Builder
	for i, p := range pages {
		fmt.Fprintf(&b, "<<<PAGE %d>>>\n", i+1)
		b.WriteString(p)
		if !strings.HasSuffix(p, "\n") {
			b.WriteByte('\n')
		}
	}
	return b.String(), nil
}

// splitPages returns the per-page text bodies from the paged-marker form
// produced by extractPagedPDFText. Index i is page i+1.
func splitPages(paged string) []string {
	var pages []string
	var cur strings.Builder
	started := false
	for _, line := range strings.Split(paged, "\n") {
		if strings.HasPrefix(line, "<<<PAGE ") && strings.HasSuffix(line, ">>>") {
			if started {
				pages = append(pages, cur.String())
				cur.Reset()
			}
			started = true
			continue
		}
		if started {
			cur.WriteString(line)
			cur.WriteByte('\n')
		}
	}
	if started {
		pages = append(pages, cur.String())
	}
	return pages
}

const anchoringRole = `You are an anchoring assistant. You receive (1) a markdown file containing review comments about a mathematical preprint, written by another AI, and (2) the plain-text extraction of that preprint with explicit page markers. Your job is to split the markdown into discrete items, classify each item by SEMANTIC ROLE (kind), and for each substantive item identify the single most likely page and a short verbatim quote from the PDF text that anchors it. The input markdown's format is ARBITRARY — it may be a letter, a flat list, structured headings, mixed prose, or contain a private appendix the user does not intend to send. Do not assume any fixed shape; classify each item from its content, not its layout. You do not evaluate or rewrite the comments — only split, classify, and locate. Output strict JSON. No prose, no markdown fences, no commentary.`

const anchoringRules = `TASK
Produce a JSON array. Each element corresponds to one discrete item
extracted from the markdown above. Items must appear in the same order they
occur in the source markdown. RETAIN framing prose (greetings, sign-offs)
and any private appendix as items with the appropriate "kind" — DO NOT
discard them. The downstream viewer groups by kind and excludes meta /
framing from page anchoring.

ITEM SPLITTING
- A "discrete item" is one self-contained unit: a substantive criticism, a
  one-line typo nit, a greeting paragraph, a sign-off paragraph, a private
  appendix, etc.
- Split on these boundaries, in priority order:
    1. Explicit list markers (-, *, 1., (a), etc.).
    2. Headings or bolded labels ("Minor:", "p. 7:", "Section 3.2:").
    3. Blank-line-separated paragraphs that each make one distinct point.
    4. Within a paragraph, sentence boundaries IF the sentences clearly
       address different locations or different issues. Do not split a single
       multi-sentence argument that develops one point.
- Preserve the original wording verbatim in original_text. Include any
  inline math (e.g. $X_n$) and equation references (e.g. "(3.7)") as-is.
- Do NOT merge items. Do NOT paraphrase. Do NOT renumber the AI's own
  internal references.

KIND CLASSIFICATION (semantic, not layout-based)
Assign exactly one of these values to "kind":

  "comment"
    A substantive criticism, suggestion, or question about a specific
    place in the paper. Usually multi-sentence. Anchors to a page.
    Examples: "the proof of Theorem 3.4 has a gap because…",
              "Section 5 should expand the Lindeberg comparison…",
              "the m vs. n centering in Theorem 1.12 needs justification".

  "minor"
    A short, often one-line typo, wording, or cosmetic nit. Usually
    appears in a bulleted list near the end of the review. Anchors to a
    page. Examples: 'remove the extra period after "(3.7).".',
                   '"which the total" should be "bringing the total"'.

  "framing-intro"
    Opening / greeting prose addressed to the author or the reader.
    Examples: "Dear Zoe,",
              "I read it. My overall impression is positive…",
              "Thanks for sharing the draft.".
    Page is 0; quote is empty.

  "framing-outro"
    Sign-off or closing prose. Examples: "Best, Leo",
                                          "Looking forward to your reply.",
                                          "Overall I think this is a strong paper, …".
    Page is 0; quote is empty.

  "meta"
    Private / internal-to-the-reviewer notes the user is unlikely to send,
    including things explicitly labeled private, rankings of importance
    among the comments, TODOs to themselves, "do not send" markers, etc.
    Examples: "My private ranking of importance: …",
              "(Note to self: double-check the Conf⁰ point before sending.)",
              "Side question I won't include: …".
    Page is 0; quote is empty.

Some examples of how the same content can appear in different formats:
  - A letter-shape input might begin with "Dear X," (framing-intro), have
    several substantive paragraphs (comment), a bulleted typo list (minor),
    end with "Best, Leo" (framing-outro), and append "private ranking:"
    (meta).
  - A flat-list-shape input might be just numbered comments (all kind=
    comment) with no framing at all.
  - A mixed input might have a bold heading "Substantive comments:"
    followed by paragraphs (comment), then "Minor:" followed by bullets
    (minor), with no greeting or sign-off.
Treat layout cues as hints only; classify by what the text IS, not how it
is formatted.

ANCHORING — PAGE
- Only "comment" and "minor" items get page-anchored. For "framing-intro",
  "framing-outro", and "meta", set page: 0 and quote: "" — those items have
  no in-paper locus.
- For "comment" and "minor" items, determine the single page in the PDF
  where the item's target lives.
- Pages are 1-indexed and come from the <<<PAGE N>>> markers.
- Strong signals (use in this priority order):
    1. The comment explicitly names a page ("p. 7", "page 12", "on p.4").
    2. The comment names a labeled object (Theorem 2.3, eq. (3.7),
       Section 4.1, Lemma 5, Figure 2). Find that label in the PDF text and
       use its page.
    3. The comment quotes or near-quotes a phrase from the PDF. Locate the
       phrase and use that page.
    4. The comment references the bibliography / a citation key. Use the
       page where the bibliography entry appears.
- If the target spans a page break, prefer the page where the labeled
  object's STATEMENT begins (not where it is referenced).
- If the comment is global / structural and has no specific locus
  (e.g. "the introduction could motivate the model better"), use the page
  where that section starts (page of the abstract for a comment about the
  abstract; first page of the introduction for "the intro"; etc.).
- If no page can be determined with at least medium confidence, set
  page: 0.

ANCHORING — QUOTE
- quote must be a VERBATIM substring of the PDF text on the chosen page,
  <=120 characters, >=15 characters where possible.
- Prefer quotes that are surrounding PROSE rather than raw mathematical
  display. pdftotext mangles aligned equations and Greek letters, so a
  display-math quote will often fail to match downstream. Quote the
  sentence introducing the equation, or the sentence after it, instead.
- If the comment names a labeled object, the quote should be the label's
  declaration line ("Theorem 3.4. Let $X_n$ ..."), truncated to 120 chars,
  rather than text from inside the proof.
- Do not invent text. Do not normalize whitespace beyond collapsing runs of
  spaces/tabs/newlines into single spaces. If you cannot find a clean
  verbatim span, set quote: "".
- A non-empty quote with page: 0 is invalid — never produce that.

ANCHORING — QUOTE_FOCUS (optional, narrow highlight)
- quote_focus is a SHORT verbatim substring of the page text that
  pinpoints the EXACT phrase the comment is about. The viewer renders
  it as a strong highlight on top of the broader (faint) quote, so the
  reader's eye lands on the precise locus of the issue.
- Populate quote_focus only when the comment has a localized target —
  typically "minor" items: a typo, an extra/missing punctuation mark, a
  changed word, a single mistyped phrase. Examples:
    * "should lose the extra period" → quote_focus = ". bound."
    * "should be 'bringing the total'" → quote_focus = "which the total"
    * "missing 'by' in this clause" → quote_focus = "Ce−dT the preceding"
- Do NOT populate quote_focus for global / structural / multi-paragraph
  comments where there is no single phrase to point at; leave it "".
- quote_focus must be a verbatim substring of the page (same rule as
  quote). If you cannot find one, set quote_focus: "".
- Length: aim for <=60 characters. Shorter is better.
- It need not be a substring of quote, but usually is.

CONFIDENCE
- "high":   comment names an explicit page or a unique labeled object that
            appears exactly once in the PDF, and the quote was found verbatim.
- "medium": label/phrase match was ambiguous (multiple candidate pages) but
            one is clearly the best fit, OR the quote is non-empty but had to
            be loosened.
- "low":    no specific locus found; page is the section's start page, or
            page is 0 with empty quote.

OUTPUT FORMAT
Output ONLY a JSON array. Each element:
{
  "id":            <1-based integer matching position in the array>,
  "original_text": <verbatim item text, string>,
  "kind":          "comment" | "minor" | "framing-intro" | "framing-outro" | "meta",
  "page":          <integer; 0 for framing/meta or when not anchorable>,
  "quote":         <verbatim PDF substring or "">,
  "quote_focus":   <short verbatim PDF substring or "" — see rules above>,
  "confidence":    "high" | "medium" | "low",
  "status":        "pending"
}

The "status" field must always be the literal string "pending"; the viewer
overwrites it later. No trailing prose. No code fences. No top-level
object — array only. If the input contains zero items, output [].`

func buildAnchoringPrompt(md, pagedPDFText string) string {
	var b strings.Builder
	b.WriteString("ROLE\n")
	b.WriteString(anchoringRole)
	b.WriteString("\n\n=== REVIEW COMMENTS (markdown, unstructured) ===\n")
	b.WriteString(md)
	if !strings.HasSuffix(md, "\n") {
		b.WriteByte('\n')
	}
	b.WriteString("\n=== PDF TEXT (pdftotext -layout, page markers inserted) ===\n")
	b.WriteString(pagedPDFText)
	if !strings.HasSuffix(pagedPDFText, "\n") {
		b.WriteByte('\n')
	}
	b.WriteString("\n=== INSTRUCTIONS ===\n")
	b.WriteString(anchoringRules)
	b.WriteByte('\n')
	return b.String()
}

// invokeClaude shells out to `claude -p --model <m> --output-format json` with
// the prompt on stdin. Returns the assistant's `result` field (which itself
// must be a JSON array per the prompt's instructions).
func invokeClaude(prompt, model string) (string, error) {
	return invokeClaudeVerbose(prompt, model, io.Discard)
}

// invokeClaudeVerbose is invokeClaude with a periodic "still waiting" heartbeat
// written to progress so the user sees signs of life during the long call.
func invokeClaudeVerbose(prompt, model string, progress io.Writer) (string, error) {
	cmd := exec.Command("claude", "-p", "--model", model, "--output-format", "json")
	cmd.Stdin = strings.NewReader(prompt)
	var out, errBuf bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errBuf

	if err := cmd.Start(); err != nil {
		return "", err
	}

	done := make(chan struct{})
	go func() {
		ticker := time.NewTicker(15 * time.Second)
		defer ticker.Stop()
		start := time.Now()
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				fmt.Fprintf(progress, "mreview pdf-comments:   …still waiting on claude (%s elapsed)\n",
					time.Since(start).Round(time.Second))
			}
		}
	}()
	err := cmd.Wait()
	close(done)
	if err != nil {
		stderrTail := strings.TrimSpace(errBuf.String())
		if stderrTail != "" {
			return "", fmt.Errorf("%w (stderr: %s)", err, stderrTail)
		}
		return "", err
	}
	return extractClaudeResult(out.Bytes(), progress)
}

// extractClaudeResult pulls the assistant's `result` payload out of the
// `claude -p --output-format json` stdout. Two shapes are accepted:
//
//   - older CLI:   {"result": "..."}
//   - newer CLI:   [ {"type":"system",...}, {"type":"assistant",...},
//                    ..., {"type":"result", "result":"...", ...} ]
//
// When the event-array form is present, also reports a one-line summary of
// duration / cost / token usage to progress so the user gets a feel for what
// just happened.
func extractClaudeResult(raw []byte, progress io.Writer) (string, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) > 0 && trimmed[0] == '[' {
		var events []struct {
			Type       string  `json:"type"`
			Subtype    string  `json:"subtype"`
			Result     string  `json:"result"`
			IsError    bool    `json:"is_error"`
			StopReason string  `json:"stop_reason"`
			Duration   int64   `json:"duration_ms"`
			TotalCost  float64 `json:"total_cost_usd"`
			NumTurns   int     `json:"num_turns"`
			Usage      struct {
				InputTokens              int `json:"input_tokens"`
				OutputTokens             int `json:"output_tokens"`
				CacheReadInputTokens     int `json:"cache_read_input_tokens"`
				CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
			} `json:"usage"`
		}
		if err := json.Unmarshal(trimmed, &events); err != nil {
			return "", fmt.Errorf("claude output appears truncated (no closing `]`, %d bytes); the model probably hit CLAUDE_CODE_MAX_OUTPUT_TOKENS mid-array. Try a model that produces more compact output (e.g. opus). Underlying decode error: %v", len(raw), err)
		}
		for i := len(events) - 1; i >= 0; i-- {
			ev := events[i]
			if ev.Type != "result" {
				continue
			}
			if ev.IsError {
				return "", fmt.Errorf("claude reported error in result event (stop_reason=%q)", ev.StopReason)
			}
			fmt.Fprintf(progress,
				"mreview pdf-comments:   claude usage: turns=%d, in=%d, out=%d, cache_read=%d, cache_create=%d, cost=$%.4f, api_duration=%s, stop=%s\n",
				ev.NumTurns,
				ev.Usage.InputTokens, ev.Usage.OutputTokens,
				ev.Usage.CacheReadInputTokens, ev.Usage.CacheCreationInputTokens,
				ev.TotalCost,
				(time.Duration(ev.Duration) * time.Millisecond).Round(time.Second),
				ev.StopReason)
			if ev.StopReason == "max_tokens" {
				return "", fmt.Errorf("claude hit max output tokens (stop_reason=max_tokens) — response truncated. Either raise CLAUDE_CODE_MAX_OUTPUT_TOKENS (current ceiling 64000) or use a model whose output is more compact (opus has worked for this task)")
			}
			return ev.Result, nil
		}
		return "", fmt.Errorf("no result event in claude output (%d events parsed); the stream likely cut off before the final result", len(events))
	}
	var wrap struct {
		Result string `json:"result"`
	}
	if err := json.Unmarshal(trimmed, &wrap); err != nil {
		return "", fmt.Errorf("decode wrapper: %w; raw=%s", err, string(raw))
	}
	return wrap.Result, nil
}

// parseCommentsArray extracts the JSON array of comments from the assistant's
// reply. Tolerates accidental ```json fences or short preambles by hunting for
// the outer [ ... ] span.
func parseCommentsArray(text string) ([]pdfreview.Comment, error) {
	t := strings.TrimSpace(text)
	t = strings.TrimPrefix(t, "```json")
	t = strings.TrimPrefix(t, "```")
	t = strings.TrimSuffix(t, "```")
	t = strings.TrimSpace(t)

	i := strings.IndexByte(t, '[')
	j := strings.LastIndexByte(t, ']')
	if i < 0 || j < 0 || j < i {
		return nil, errors.New("no JSON array found in claude output")
	}
	var arr []pdfreview.Comment
	if err := json.Unmarshal([]byte(t[i:j+1]), &arr); err != nil {
		return nil, err
	}
	return arr, nil
}
