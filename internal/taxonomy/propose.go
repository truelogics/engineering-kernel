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
	Mappings []Mapping
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

// Empty reports whether there is nothing to propose.
func (p Proposal) Empty() bool { return len(p.Mappings) == 0 }

// YAML renders the proposal as the .engineering.yaml it would write.
//
// Patterns are sorted, so the same repository state renders byte for
// byte the same file — a proposal that shuffled between runs could not
// be reviewed in a diff.
func (p Proposal) YAML() string {
	if len(p.Mappings) == 0 {
		return ""
	}
	width := 0
	for _, m := range p.Mappings {
		if n := len(m.Pattern) + 1; n > width {
			width = n
		}
	}

	var b strings.Builder
	b.WriteString("# What this repository's directories hold.\n")
	b.WriteString("# Proposed by `eng taxonomy auto`; edit freely — this file is yours.\n")
	b.WriteString("# A document's own `doc:` front matter always wins over these lines.\n")
	b.WriteString("taxonomy:\n")
	for _, m := range p.Mappings {
		fmt.Fprintf(&b, "  %-*s %s\n", width, m.Pattern+":", m.Name)
	}
	return b.String()
}

// observation is one directory's evidence.
type observation struct {
	dir     string
	docs    []domain.CanonicalDocument
	unknown int
}

// Propose reads a repository's parsed documents and offers mappings for
// the directories whose contents it can identify.
//
// docs are documents as the pipeline sees them *before* any taxonomy is
// applied: Collector -> Parser -> Normalizer. Their Type is therefore
// what front matter and the parser's own path rules established, which is
// exactly the evidence a taxonomy is meant to supplement.
func Propose(docs []domain.CanonicalDocument) Proposal {
	p := Proposal{Documents: len(docs)}
	for _, d := range docs {
		if d.Type == domain.DocTypeUnknown {
			p.UnknownBefore++
		}
	}
	if p.UnknownBefore == 0 {
		return p
	}

	for _, d := range docs {
		if d.Type == domain.DocTypeUnknown && path.Dir(d.Path) == "." {
			p.RootUnknown++
		}
	}

	byDir := groupByDirectory(docs)
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

	measure(&p, docs)
	return p
}

// measure computes the proposal's effect by rendering it and applying it
// exactly as indexing will: the same parser, the same matcher.
//
// Nothing here estimates. A number a developer is asked to approve has to
// be the number they will get, and the only way to be sure of that is to
// run the real thing.
func measure(p *Proposal, docs []domain.CanonicalDocument) {
	tax, err := domain.ParseTaxonomy([]byte(p.YAML()))
	if err != nil {
		// The proposal does not parse, so it classifies nothing. Reported
		// as no effect rather than as a crash, and pinned by
		// TestProposalAlwaysParses.
		p.Mappings = nil
		p.UnknownAfter = p.UnknownBefore
		return
	}

	classified := map[string]int{}
	for _, d := range docs {
		declared, claims := tax.TypeFor(d.Path)
		switch {
		case d.Type == domain.DocTypeUnknown && claims:
			classified[d.Path] = 1
		case d.Type == domain.DocTypeUnknown:
			p.UnknownAfter++
		case claims && declared != d.Type:
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
	fmGuess, typed, fmOK := frontMatterMajority(obs.docs)

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
	for _, d := range docs {
		for _, dir := range ancestors(d.Path) {
			members[dir] = append(members[dir], d)
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
		obs := observation{dir: dir, docs: members[dir]}
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
