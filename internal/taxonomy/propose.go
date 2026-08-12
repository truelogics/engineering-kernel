// Package taxonomy proposes a repository taxonomy from what a repository
// already contains.
//
// It is a kernel capability rather than CLI code because it is the same
// question RFC-0007 answers by hand — what does this directory hold? —
// and because a proposal has to agree exactly with what indexing will
// later do. The impact figures here are not estimated: the proposal is
// rendered to YAML, parsed by domain.ParseTaxonomy, and applied with
// domain.Taxonomy.TypeFor, which is the code that runs at index time.
//
// Deterministic and local. No provider, no model, no network: the same
// repository state produces the same proposal, which is what makes a
// proposal reviewable.
package taxonomy

import (
	"fmt"
	"path"
	"sort"
	"strings"

	"github.com/truelogics/ai-memory/internal/domain"
)

// directoryNames maps an unambiguous directory name to the canonical type
// a directory of that name holds.
//
// Two deliberate absences.
//
// `adr`, `rfc`, `rules` and `roadmap` are missing because the markdown
// parser already classifies those path segments itself. A proposal for
// them would classify nothing, and Propose only offers a mapping that
// rescues a document that is currently unknown — so they fall out
// without needing a special case.
//
// `docs`, `design`, `notes` and `misc` are missing because they are
// ambiguous. `docs/` holds whatever a project puts in it, and `design`
// is as likely to be product design as software architecture. A wrong
// mapping returns confidently mislabelled documents, which is worse than
// leaving them unknown — so an ambiguous name is not a signal, and a
// directory that has one is proposed for only when its documents' own
// front matter says what it holds.
var directoryNames = map[string]string{
	"decisions":      "Decision",
	"decision":       "Decision",
	"architecture":   "Architecture",
	"arch":           "Architecture",
	"specs":          "Specification",
	"spec":           "Specification",
	"specifications": "Specification",
	"specification":  "Specification",
	"requirements":   "Specification",
	"guides":         "Guide",
	"guide":          "Guide",
	"handbook":       "Guide",
	"handbooks":      "Guide",
	"playbook":       "Guide",
	"playbooks":      "Guide",
	"runbook":        "Guide",
	"runbooks":       "Guide",
	"tutorials":      "Guide",
	"tutorial":       "Guide",
	"howto":          "Guide",
	"how-to":         "Guide",
	"plans":          "Planning",
	"plan":           "Planning",
	"planning":       "Planning",
	"roadmaps":       "Planning",
	"backlog":        "Planning",
	"milestones":     "Planning",
	"reference":      "Reference",
	"references":     "Reference",
	"glossary":       "Reference",
	"api":            "Reference",
}

// Confidence thresholds for the front-matter signal. A directory whose
// documents mostly declare what they are is evidence about its
// unclassified siblings; one document that happens to be typed is not.
const (
	minTypedDocuments = 2
	majorityShare     = 2.0 / 3.0
)

// Mapping is one proposed line of .engineering.yaml, with what it rests
// on and what it would do.
type Mapping struct {
	Pattern string
	// Name is the canonical vocabulary word, as it will be written.
	Name string
	Type domain.DocType
	// Evidence is why this is being proposed, in the reader's terms. A
	// proposal a reader cannot evaluate is not a proposal.
	Evidence string
	// Classifies counts the currently-unknown documents this would
	// classify, computed by applying the rendered file.
	Classifies int
}

// Skipped is a directory that held unclassified documents and was not
// proposed for, with the reason.
//
// Reported rather than dropped: a developer looking at a proposal that
// covers half their repository needs to know the other half was
// considered and passed over, not overlooked.
type Skipped struct {
	Pattern string
	Reason  string
	Unknown int
}

// Proposal is a candidate taxonomy and its measured effect.
type Proposal struct {
	// Mappings are the new lines being proposed.
	Mappings []Mapping
	// Existing are the repository's own lines, carried through unchanged.
	//
	// An update merges rather than replaces. A proposal has evidence for
	// what it can see and none for what it cannot, and treating "no
	// evidence" as "delete this line" would discard a statement its author
	// made deliberately — the same reasoning that makes EOF a no, with
	// more at stake, because a decline writes nothing and a replacement
	// destroys something.
	Existing []domain.Mapping
	Skipped  []Skipped
	// Documents is how many documents were examined.
	Documents int
	// UnknownBefore and UnknownAfter are counted by applying the rendered
	// YAML, not predicted.
	UnknownBefore, UnknownAfter int
	// Overridden are documents a pattern matches whose own front matter
	// already classifies them differently. Front matter wins (RFC-0007),
	// so these are places the file and the documents disagree.
	Overridden []string
	// RootUnknown counts unclassified documents sitting directly in the
	// repository root, which no directory pattern can reach.
	//
	// Reported because the alternative is a reader doing subtraction: a
	// repository with 18 unclassified documents and 6 accounted for in
	// Skipped leaves 12 unexplained, and unexplained reads as overlooked.
	RootUnknown int
}

// Empty reports whether there is nothing new to propose. An update that
// finds nothing to add is empty even though the merged file would not be.
func (p Proposal) Empty() bool { return len(p.Mappings) == 0 }

// YAML renders the proposal as the .engineering.yaml it would write.
//
// Patterns are sorted, so the same repository state renders byte for
// byte the same file — a proposal that shuffled between runs could not
// be reviewed in a diff.
func (p Proposal) YAML() string {
	lines := p.merged()
	if len(lines) == 0 {
		return ""
	}
	width := 0
	for _, m := range lines {
		if n := len(m.Pattern) + 1; n > width {
			width = n
		}
	}

	var b strings.Builder
	b.WriteString("# What this repository's directories hold.\n")
	b.WriteString("# Proposed by `eng taxonomy auto`; edit freely — this file is yours.\n")
	b.WriteString("# A document's own `doc:` front matter always wins over these lines.\n")
	b.WriteString("taxonomy:\n")
	for _, m := range lines {
		fmt.Fprintf(&b, "  %-*s %s\n", width, m.Pattern+":", m.Declared)
	}
	return b.String()
}

// merged is the file that would be written: the repository's own lines
// plus the new ones, sorted. A pattern the repository already declares
// keeps its own wording — nothing here overrules a line its author wrote.
func (p Proposal) merged() []domain.Mapping {
	seen := map[string]bool{}
	out := make([]domain.Mapping, 0, len(p.Existing)+len(p.Mappings))
	for _, m := range p.Existing {
		if seen[m.Pattern] {
			continue
		}
		seen[m.Pattern] = true
		out = append(out, m)
	}
	for _, m := range p.Mappings {
		if seen[m.Pattern] {
			continue
		}
		seen[m.Pattern] = true
		out = append(out, domain.Mapping{Pattern: m.Pattern, Declared: m.Name, Type: m.Type})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Pattern < out[j].Pattern })
	return out
}

// observation is one directory's evidence.
//
// docs is everything beneath dir, because a proposed pattern is `dir/**`
// and that is what it would claim. direct is only the documents sitting
// immediately in dir, because that is the only evidence about dir itself.
//
// The distinction is load-bearing. Reading front matter from the whole
// subtree let `docs/`, whose name this deliberately treats as ambiguous,
// inherit the declarations of `docs/architecture/` and then silently
// classify `docs/product/` as Architecture — while printing
// `docs/product/**` to the developer as considered and not proposed. The
// report contradicted the file, in exactly the ambiguous-name case the
// conservatism argument rests on. Found in review.
type observation struct {
	dir     string
	docs    []domain.CanonicalDocument
	direct  []domain.CanonicalDocument
	unknown int
}

// Propose reads a repository's parsed documents and offers mappings for
// the directories whose contents it can identify.
//
// docs are documents as the pipeline sees them *before* any taxonomy is
// applied: Collector -> Parser -> Normalizer. Their Type is therefore
// what front matter and the parser's own path rules established, which is
// exactly the evidence a taxonomy is meant to supplement.
func Propose(docs []domain.CanonicalDocument, existing domain.Taxonomy) Proposal {
	p := Proposal{Documents: len(docs), Existing: existing.Mappings()}

	// The baseline is what the repository achieves *today*, which on an
	// update includes its own taxonomy. Counting from the raw parse
	// instead would credit this proposal for everything the developer's
	// existing file already does, under a heading that says the number is
	// measured — honest about the mechanism and wrong about the delta,
	// which is the one thing an update exists to show.
	effective := make([]domain.CanonicalDocument, len(docs))
	copy(effective, docs)
	if !existing.Empty() {
		for i := range effective {
			if effective[i].Type != domain.DocTypeUnknown {
				continue
			}
			if t, ok := existing.TypeFor(effective[i].Path); ok {
				effective[i].Type = t
			}
		}
	}

	for _, d := range effective {
		if d.Type == domain.DocTypeUnknown {
			p.UnknownBefore++
			if path.Dir(d.Path) == "." {
				p.RootUnknown++
			}
		}
	}
	if p.UnknownBefore == 0 {
		return p
	}

	byDir := groupByDirectory(effective)
	claimed := map[string]string{} // dir -> proposed canonical name

	for _, obs := range byDir {
		if obs.unknown == 0 {
			// Nothing to gain. This is also what keeps the proposer from
			// duplicating the parser's own built-in path rules: a `rules/`
			// directory is already classified, so it is never proposed for.
			continue
		}

		name, evidence, ok := identify(obs)
		if !ok {
			p.Skipped = append(p.Skipped, Skipped{
				Pattern: pattern(obs.dir), Reason: evidence, Unknown: obs.unknown,
			})
			continue
		}
		if parent, parentName, redundant := coveredByAncestor(obs.dir, claimed); redundant && parentName == name {
			p.Skipped = append(p.Skipped, Skipped{
				Pattern: pattern(obs.dir),
				Reason:  fmt.Sprintf("already covered by %s", pattern(parent)),
				Unknown: obs.unknown,
			})
			continue
		}

		docType, valid := domain.CanonicalType(name)
		if !valid {
			// Unreachable unless the table above drifts from the canonical
			// set, which TestEveryProposableNameIsCanonical pins.
			continue
		}
		claimed[obs.dir] = name
		p.Mappings = append(p.Mappings, Mapping{
			Pattern: pattern(obs.dir), Name: name, Type: docType, Evidence: evidence,
		})
	}

	sort.Slice(p.Mappings, func(i, j int) bool { return p.Mappings[i].Pattern < p.Mappings[j].Pattern })
	sort.Slice(p.Skipped, func(i, j int) bool { return p.Skipped[i].Pattern < p.Skipped[j].Pattern })

	measure(&p, docs, effective)
	return p
}

// measure computes the proposal's effect by rendering it and applying it
// exactly as indexing will: the same parser, the same matcher.
//
// Nothing here estimates. A number a developer is asked to approve has to
// be the number they will get, and the only way to be sure of that is to
// run the real thing.
func measure(p *Proposal, docs, effective []domain.CanonicalDocument) {
	tax, err := domain.ParseTaxonomy([]byte(p.YAML()))
	if err != nil {
		// The proposal does not parse, so it classifies nothing. Reported
		// as no effect rather than as a crash, and pinned by
		// TestProposalAlwaysParses.
		p.Mappings = nil
		p.UnknownAfter = p.UnknownBefore
		return
	}

	// effective for the counts: the baseline is what the repository
	// achieves today, so a document its existing file already classifies
	// is not something this proposal gets to claim.
	classified := map[string]bool{}
	for _, d := range effective {
		if d.Type != domain.DocTypeUnknown {
			continue
		}
		if _, claims := tax.TypeFor(d.Path); claims {
			classified[d.Path] = true
			continue
		}
		p.UnknownAfter++
	}

	// docs for the overrides: front-matter precedence is about what a
	// document declares about itself, which no taxonomy changes.
	for _, d := range docs {
		if d.Type == domain.DocTypeUnknown {
			continue
		}
		if declared, claims := tax.TypeFor(d.Path); claims && declared != d.Type {
			p.Overridden = append(p.Overridden, d.Path)
		}
	}
	sort.Strings(p.Overridden)

	// Attributed to the pattern that actually won, so two overlapping
	// patterns do not both claim credit for the same document.
	credit := map[string]int{}
	for docPath := range classified {
		if m, ok := tax.MappingFor(docPath); ok {
			credit[m.Pattern]++
		}
	}
	for i, m := range p.Mappings {
		p.Mappings[i].Classifies = credit[m.Pattern]
	}
}

// identify decides what a directory holds, or says why it could not.
func identify(obs observation) (name, evidence string, ok bool) {
	nameGuess, nameOK := directoryNames[strings.ToLower(path.Base(obs.dir))]
	fmGuess, typed, fmOK := frontMatterMajority(obs.direct)

	switch {
	case nameOK && fmOK && nameGuess == fmGuess:
		return nameGuess, fmt.Sprintf("named %q, and %d of its documents already declare %s",
			path.Base(obs.dir), typed, fmGuess), true

	case nameOK && fmOK:
		// The directory's name and its documents disagree. Either could be
		// right and this cannot tell which, so it says so and proposes
		// nothing — the objective is useful proposals, not coverage.
		return "", fmt.Sprintf("its name suggests %s but %d of its documents declare %s",
			nameGuess, typed, fmGuess), false

	case fmOK:
		return fmGuess, fmt.Sprintf("%d of its documents already declare %s", typed, fmGuess), true

	case nameOK:
		return nameGuess, fmt.Sprintf("named %q", path.Base(obs.dir)), true

	default:
		return "", "no signal: an ambiguous name and no document declaring what it is", false
	}
}

// frontMatterMajority reads what a directory's already-classified
// documents say about it.
//
// Requires both a majority and a floor, because one typed document among
// twenty unknown ones is a coincidence, not a pattern — and a proposal
// resting on a coincidence is exactly the mislabelling this is meant to
// avoid.
func frontMatterMajority(docs []domain.CanonicalDocument) (name string, typed int, ok bool) {
	counts := map[domain.DocType]int{}
	for _, d := range docs {
		if d.Type == domain.DocTypeUnknown {
			continue
		}
		counts[d.Type]++
		typed++
	}
	if typed < minTypedDocuments {
		return "", typed, false
	}

	// Deterministic: iterate the counted types in a fixed order rather
	// than a map's.
	types := make([]domain.DocType, 0, len(counts))
	for t := range counts {
		types = append(types, t)
	}
	sort.Slice(types, func(i, j int) bool {
		if counts[types[i]] != counts[types[j]] {
			return counts[types[i]] > counts[types[j]]
		}
		return types[i] < types[j]
	})

	best := types[0]
	if float64(counts[best]) < majorityShare*float64(typed) {
		return "", typed, false
	}
	canonical, nameable := domain.CanonicalNameFor(best)
	if !nameable {
		// e.g. DocTypeRFC, which the parser infers from a path and the
		// canonical vocabulary has no separate word for.
		return "", typed, false
	}
	return canonical, typed, true
}

// groupByDirectory collects every directory that contains documents, at
// every depth, with everything beneath it — because a proposed pattern is
// `dir/**`, which is recursive.
func groupByDirectory(docs []domain.CanonicalDocument) []observation {
	members := map[string][]domain.CanonicalDocument{}
	direct := map[string][]domain.CanonicalDocument{}
	for _, d := range docs {
		for _, dir := range ancestors(d.Path) {
			members[dir] = append(members[dir], d)
		}
		if parent := path.Dir(d.Path); parent != "." {
			direct[parent] = append(direct[parent], d)
		}
	}

	dirs := make([]string, 0, len(members))
	for dir := range members {
		dirs = append(dirs, dir)
	}
	// Shallowest first, so an ancestor claims a type before its children
	// are considered and a child can be recognized as redundant.
	sort.Slice(dirs, func(i, j int) bool {
		di, dj := strings.Count(dirs[i], "/"), strings.Count(dirs[j], "/")
		if di != dj {
			return di < dj
		}
		return dirs[i] < dirs[j]
	})

	out := make([]observation, 0, len(dirs))
	for _, dir := range dirs {
		obs := observation{dir: dir, docs: members[dir], direct: direct[dir]}
		for _, d := range obs.docs {
			if d.Type == domain.DocTypeUnknown {
				obs.unknown++
			}
		}
		out = append(out, obs)
	}
	return out
}

// ancestors lists the directories containing p, excluding the repository
// root: a mapping of `**` would claim the entire repository on the
// strength of whatever its most common directory happened to hold.
func ancestors(p string) []string {
	var dirs []string
	dir := path.Dir(p)
	for dir != "." && dir != "/" && dir != "" {
		dirs = append(dirs, dir)
		dir = path.Dir(dir)
	}
	return dirs
}

func coveredByAncestor(dir string, claimed map[string]string) (ancestor, name string, ok bool) {
	for parent := path.Dir(dir); parent != "." && parent != "/" && parent != ""; parent = path.Dir(parent) {
		if n, found := claimed[parent]; found {
			return parent, n, true
		}
	}
	return "", "", false
}

func pattern(dir string) string { return dir + "/**" }
