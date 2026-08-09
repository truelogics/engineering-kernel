package memory

import (
	"strings"
)

// Confidence grades how an Evidence excerpt matched its source
// (RFC-0006). It describes the match, not anyone's feelings about the
// claim.
type Confidence string

const (
	// ConfidenceHigh: the excerpt was found inside the retrieved content.
	ConfidenceHigh Confidence = "high"
	// ConfidenceMedium: the retrieved content was found inside a longer
	// excerpt — the quote is real but extends past what was retrieved.
	ConfidenceMedium Confidence = "medium"
	// ConfidenceLow exists for consumers grading their own non-evidence
	// claims. VerifyEvidence never returns it: an excerpt either matched
	// or is not evidence at all.
	ConfidenceLow Confidence = "low"
)

// Evidence is a verified claim that a document says a specific thing
// (RFC-0006). It is only ever produced by VerifyEvidence — an
// unverified quote is an assertion, and the difference between the two
// is the entire point of this type existing.
type Evidence struct {
	// Document is the path as retrieval returned it.
	Document string
	// Repository names which registered repository Document came from.
	Repository string
	// Excerpt is the quoted text, with retrieval's highlight markup
	// removed and whitespace collapsed.
	Excerpt string
	// Confidence describes how the match was made.
	Confidence Confidence
}

// Qualified returns the document as "repository:path", the form that is
// unambiguous across a multi-repository workspace.
func (e Evidence) Qualified() string {
	if e.Repository == "" {
		return e.Document
	}
	return e.Repository + ":" + e.Document
}

// VerifyEvidence reports whether excerpt genuinely appears in the
// content this ContextPackage returned for document (RFC-0006).
//
// document may be a bare path ("rules/logging.md") or repository-
// qualified ("engineering:rules/logging.md"). A bare path that names
// documents in more than one repository fails rather than guessing,
// since guessing would attribute a quote to a document nobody chose.
//
// Verification is bounded by what retrieval returned, which is a search
// highlight of roughly 40–200 characters rather than the document
// itself (see ai-review/KERNEL_REQUIREMENTS.md #15). A true quote from
// elsewhere in the same file will not verify. Checking against the file
// on disk instead was considered and rejected: it would make evidence
// depend on the working tree rather than on the index, and would report
// success for text the consumer was never shown.
func (p ContextPackage) VerifyEvidence(document, excerpt string) (Evidence, bool) {
	if strings.TrimSpace(document) == "" || strings.TrimSpace(excerpt) == "" {
		return Evidence{}, false
	}

	matches := p.lookup(document)
	if len(matches) != 1 {
		// Zero: not a document retrieval returned. More than one: the
		// same path in several repositories, and no basis to choose.
		return Evidence{}, false
	}
	source := matches[0]

	confidence, ok := matchExcerpt(excerpt, source.Snippet)
	if !ok {
		return Evidence{}, false
	}
	return Evidence{
		Document:   source.Path,
		Repository: source.Repository,
		Excerpt:    CleanExcerpt(excerpt),
		Confidence: confidence,
	}, true
}

// Documents returns every FileContext in the package, across sections —
// the set of documents any Evidence may cite.
func (p ContextPackage) Documents() []FileContext {
	var out []FileContext
	out = append(out, p.Rules...)
	out = append(out, p.ADRs...)
	out = append(out, p.RelevantFiles...)
	out = append(out, p.RelatedIssues...)
	out = append(out, p.RelatedPRs...)
	return out
}

// lookup finds the entries a document reference names. A qualified
// reference ("repo:path") matches exactly one repository; a bare path
// may match several, which is the ambiguity VerifyEvidence refuses.
// Duplicate chunks of the same document in the same repository collapse
// to the first, since they are the same document.
func (p ContextPackage) lookup(document string) []FileContext {
	wantRepo, wantPath := splitQualified(document)

	seen := map[string]bool{}
	var out []FileContext
	for _, f := range p.Documents() {
		if f.Path != wantPath {
			continue
		}
		if wantRepo != "" && f.Repository != wantRepo {
			continue
		}
		if seen[f.Repository] {
			continue
		}
		seen[f.Repository] = true
		out = append(out, f)
	}
	return out
}

// splitQualified splits "repository:path" into its parts. A reference
// with no colon, or whose text after the colon is empty, is treated as
// a bare path — Windows-style "C:\..." and a stray trailing colon both
// stay paths rather than becoming a repository named "C".
func splitQualified(document string) (repository, path string) {
	i := strings.Index(document, ":")
	if i <= 0 || i == len(document)-1 {
		return "", document
	}
	return document[:i], document[i+1:]
}

// matchExcerpt reports whether excerpt is present in snippet, and which
// direction the containment ran.
func matchExcerpt(excerpt, snippet string) (Confidence, bool) {
	normExcerpt := normalizeForMatch(excerpt)
	normSnippet := normalizeForMatch(snippet)
	if normExcerpt == "" || normSnippet == "" {
		return "", false
	}
	if strings.Contains(normSnippet, normExcerpt) {
		return ConfidenceHigh, true
	}
	if strings.Contains(normExcerpt, normSnippet) {
		return ConfidenceMedium, true
	}
	return "", false
}

// CleanExcerpt strips the highlight markup retrieval adds for a
// terminal's benefit and collapses whitespace, so a quote reaches a
// reader as text rather than as markdown asterisks and hard line
// breaks. Exported because a consumer rendering its own citations needs
// the same cleaning the kernel applies to verified evidence.
func CleanExcerpt(s string) string {
	return strings.Join(strings.Fields(strings.ReplaceAll(s, "**", "")), " ")
}

// normalizeForMatch removes everything that differs between a quote and
// its source for reasons neither party chose: case, whitespace, the
// "**" highlight markers, and the "..." elision retrieval inserts.
func normalizeForMatch(s string) string {
	s = strings.ToLower(s)
	s = strings.ReplaceAll(s, "**", "")
	s = strings.ReplaceAll(s, "...", " ")
	return strings.Join(strings.Fields(s), " ")
}
