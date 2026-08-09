// Package retriever implements kernel.Retriever: gathers everything
// relevant for a task by searching and grouping results by Knowledge
// Type. See ARCHITECTURE.md's Retriever and RFC-0003/the Step 8
// Definition of Done ("Review authentication PR" -> Architecture, ADRs,
// Rules, ...).
package retriever

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/truelogics/ai-memory/internal/domain"
	"github.com/truelogics/ai-memory/internal/kernel"
)

// labelByDocType maps a Knowledge Type to the section label its
// documents get grouped under.
const labelRules = "Rules"

var labelByDocType = map[domain.DocType]string{
	domain.DocTypeStandard: "Architecture",
	domain.DocTypeADR:      "Related ADRs",
	domain.DocTypeRule:     labelRules,
	domain.DocTypeRFC:      "Related RFCs",
	domain.DocTypeRoadmap:  "Roadmap",
	domain.DocTypeReadme:   "Documentation",
	// RFC-0007 canonical types with no prior name here.
	domain.DocTypeSpecification: "Specifications",
	domain.DocTypeGuide:         "Guides",
}

// alwaysShownEmpty are sections with no producer yet (RFC-0001's
// non-goals: no PR/Issue ingestion) — shown empty rather than omitted.
var alwaysShownEmpty = []string{"Related Issues", "Related PRs"}

// defaultPriority is the section order used when Retriever.Priority is
// unset — matches Step 8's own "Priority example" (Architecture, ADRs,
// ..., README) as closely as this kernel's actual Knowledge Types allow.
// Milestone 7: this used to be the only order; now it's just the default.
var defaultPriority = []string{
	"Architecture", "Related ADRs", "Rules", "Related RFCs", "Specifications",
	"Guides", "Roadmap", "Documentation", "Related Issues", "Related PRs",
}

// stopwords are dropped when turning a free-text task ("Review the
// authentication PR") into a search query — plain keyword search, not
// natural-language understanding, so noise words just hurt recall.
var stopwords = map[string]bool{
	"a": true, "an": true, "the": true, "review": true, "please": true,
	"for": true, "and": true, "or": true, "in": true, "of": true,
	"to": true, "is": true, "on": true, "about": true, "this": true,
	"that": true, "our": true, "we": true, "it": true,
}

// maxScopedRules bounds how many scope-selected rules one retrieval
// returns, so a large rulebook can't crowd out every other section.
const maxScopedRules = 10

// Retriever implements kernel.Retriever against a kernel.Search.
type Retriever struct {
	Search kernel.Search
	// Storage backs scope-selected rule retrieval (RFC-0005). Optional:
	// a Retriever without it falls back to filtering the text-search
	// results, which is what every caller did before RFC-0005.
	Storage kernel.Storage
	// Priority orders the returned groups (Milestone 7). Zero value uses
	// defaultPriority. A custom Priority only needs to name the sections
	// worth moving to the front — anything from defaultPriority it
	// doesn't mention still appears afterward; nothing is silently
	// dropped by a partial list.
	Priority []string
}

var _ kernel.Retriever = (*Retriever)(nil)

// New returns a Retriever backed by search, using defaultPriority.
func New(search kernel.Search) *Retriever {
	return &Retriever{Search: search}
}

// Retrieve implements kernel.Retriever.
func (r *Retriever) Retrieve(ctx context.Context, task string, opts kernel.RetrieveOptions) (kernel.RetrievalBundle, error) {
	results, err := r.Search.Search(ctx, keywords(task), kernel.SearchOptions{Limit: 20})
	if err != nil {
		return kernel.RetrievalBundle{}, fmt.Errorf("retriever: %w", err)
	}

	byLabel := make(map[string][]kernel.SearchResult, len(labelByDocType)+len(alwaysShownEmpty))
	var other []kernel.SearchResult
	for _, res := range results {
		if res.Document.Type == domain.DocTypeRule && !governs(res.Document, opts.ChangedPaths) {
			continue // RFC-0005: this rule doesn't govern the files in question
		}
		if label, ok := labelByDocType[res.Document.Type]; ok {
			byLabel[label] = append(byLabel[label], res)
			continue
		}
		other = append(other, res)
	}

	if rules, err := r.scopedRules(ctx, opts, byLabel[labelRules]); err != nil {
		return kernel.RetrievalBundle{}, err
	} else if rules != nil {
		byLabel[labelRules] = rules
	}
	for _, label := range alwaysShownEmpty {
		if _, ok := byLabel[label]; !ok {
			byLabel[label] = nil
		}
	}

	order := r.Priority
	if len(order) == 0 {
		order = defaultPriority
	}

	emitted := make(map[string]bool, len(byLabel))
	groups := make([]kernel.RetrievalGroup, 0, len(byLabel)+1)
	emit := func(label string) {
		if emitted[label] {
			return
		}
		emitted[label] = true
		groups = append(groups, kernel.RetrievalGroup{Label: label, Results: byLabel[label]})
	}
	for _, label := range order {
		emit(label)
	}
	// A partial custom Priority reorders what it names; anything from
	// the default set it left out still appears, just afterward.
	for _, label := range defaultPriority {
		emit(label)
	}
	if len(other) > 0 {
		groups = append(groups, kernel.RetrievalGroup{Label: "Other", Results: other})
	}

	return kernel.RetrievalBundle{Task: task, Groups: groups}, nil
}

// scopedRules answers "which rules govern these files" by scope rather
// than by text similarity, returning nil when no paths were supplied (so
// the caller keeps whatever the text search found).
//
// Filtering search results by scope — the first thing RFC-0005
// implemented — removes wrong rules but cannot surface right ones, and
// measuring it showed why that matters: reviewing a change to
// ai-review's Claude provider and Markdown formatter, the task's
// vocabulary was "category reported security severity decoder default
// float64", while the rule that governs it reads "a silent fallback
// produces output that looks exactly like the real thing". They share no
// words, so no text search would ever have connected them.
//
// **A violation rarely quotes the rule it violates.** Text similarity is
// the wrong selector for rules specifically, which is why rules — alone
// among Knowledge Types — are selected by declared scope and merely
// *ordered* by text relevance.
func (r *Retriever) scopedRules(ctx context.Context, opts kernel.RetrieveOptions, matched []kernel.SearchResult) ([]kernel.SearchResult, error) {
	if len(opts.ChangedPaths) == 0 || r.Storage == nil {
		return nil, nil
	}

	all, err := r.Storage.ListDocumentsByType(ctx, domain.DocTypeRule)
	if err != nil {
		return nil, fmt.Errorf("retriever: list rules: %w", err)
	}

	// A rule the text search already found keeps that score, so a rule
	// the change actually talks about still ranks above one that merely
	// governs the same file type.
	scoreByID := make(map[string]kernel.SearchResult, len(matched))
	for _, m := range matched {
		scoreByID[m.Document.ID] = m
	}

	var out []kernel.SearchResult
	for _, doc := range all {
		if !domain.ScopeOf(doc.Metadata).Matches(opts.ChangedPaths) {
			continue
		}
		if hit, ok := scoreByID[doc.ID]; ok {
			out = append(out, hit)
			continue
		}
		out = append(out, kernel.SearchResult{Document: doc, Snippet: snippetOf(doc)})
	}

	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Score != out[j].Score {
			return out[i].Score > out[j].Score
		}
		return out[i].Document.Path < out[j].Document.Path
	})
	if len(out) > maxScopedRules {
		out = out[:maxScopedRules]
	}
	return out, nil
}

// snippetOf gives a scope-selected rule the same kind of short excerpt
// search would have produced, so every entry in the Rules group looks
// alike to a consumer regardless of how it was selected.
func snippetOf(doc domain.CanonicalDocument) string {
	const max = 200
	body := strings.TrimSpace(doc.Content)
	if len(body) <= max {
		return body
	}
	return strings.TrimSpace(body[:max]) + "..."
}

// governs reports whether a rule applies to the files a task concerns
// (RFC-0005). Three cases, each resolved the way RFC-0005 argues:
//
//   - No paths supplied: no scoping. This is what keeps `eng ask` and
//     every pre-RFC-0005 caller behaving identically.
//   - A rule with no applies_to: kept. Unscoped means universal — a rule
//     whose author didn't think about scope is more likely a general
//     standard than a stack-specific one, and silently dropping it is
//     the worse failure.
//   - A rule whose applies_to matches nothing changed: dropped, even if
//     that empties the Rules group. An empty rules section honestly says
//     no rule governs these files; falling back to unfiltered results
//     would reintroduce exactly the defect this scoping exists to fix.
func governs(doc domain.CanonicalDocument, changedPaths []string) bool {
	if len(changedPaths) == 0 {
		return true
	}
	return domain.ScopeOf(doc.Metadata).Matches(changedPaths)
}

// keywords turns a free-text task into an FTS5 query: lowercase, drop
// stopwords and duplicates, OR the rest together — a bareword multi-term
// FTS5 MATCH is an implicit AND, which is too strict for a natural
// sentence like "Review authentication PR" (nothing indexes "PR" at all
// yet, and requiring it would zero out an otherwise-good match on
// "authentication"). Falls back to the raw task if every word was a
// stopword.
//
// Every kept term is quoted (FTS5 phrase syntax) before joining, not
// passed as a bareword: a term like "readme.md" or "modified:" contains
// characters FTS5's query grammar treats specially (a bare trailing
// colon is column-filter syntax; an embedded period breaks bareword
// parsing entirely) — real failures a consumer's deterministic,
// non-natural-language task strings hit (RFC-0002's TaskBuilder,
// ai-review). Quoting sidesteps FTS5 query-syntax parsing altogether;
// the content tokenizer still splits the quoted phrase the same way it
// would a bareword, so match behavior for ordinary words is unchanged.
func keywords(task string) string {
	words := strings.Fields(strings.ToLower(task))
	seen := make(map[string]bool, len(words))
	var kept []string
	for _, w := range words {
		w = strings.Trim(w, ".,!?#()[]{}\"':;")
		if w == "" || stopwords[w] || seen[w] {
			continue
		}
		seen[w] = true
		kept = append(kept, `"`+strings.ReplaceAll(w, `"`, `""`)+`"`)
	}
	if len(kept) == 0 {
		return task
	}
	return strings.Join(kept, " OR ")
}
