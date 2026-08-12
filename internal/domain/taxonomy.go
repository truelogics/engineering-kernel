package domain

import (
	"fmt"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// TaxonomyFile is the name a repository uses to describe how it
// organizes knowledge. It lives in the repository, not in the workspace:
// the answer belongs to the project, and should travel with it, be
// reviewed with it, and change with it (RFC-0007).
const TaxonomyFile = ".engineering.yaml"

// canonicalTypes maps the vocabulary a repository writes onto the
// Knowledge Types the kernel reasons over. Small and closed on purpose —
// large enough to tell a rule from a decision from a guide, small enough
// that any repository can map onto it. Repository directory names are
// open; this list is not.
var canonicalTypes = map[string]DocType{
	"rule":          DocTypeRule,
	"decision":      DocTypeADR,
	"architecture":  DocTypeStandard,
	"specification": DocTypeSpecification,
	"guide":         DocTypeGuide,
	"planning":      DocTypeRoadmap,
	"reference":     DocTypeReadme,
	"other":         DocTypeUnknown,
}

// CanonicalType resolves a canonical type name, however cased, to the
// Knowledge Type it means.
//
// Exported so that anything generating a taxonomy — a proposal, a
// template, a test — validates its vocabulary against the same table
// ParseTaxonomy uses, rather than carrying a second copy that can fall
// out of step with it.
func CanonicalType(name string) (DocType, bool) {
	t, ok := canonicalTypes[strings.ToLower(strings.TrimSpace(name))]
	return t, ok
}

// CanonicalNameFor is the inverse: the vocabulary word a repository
// would write to declare docType.
//
// Not every DocType has one. The kernel infers DocTypeRFC from a path,
// and the canonical set has no separate word for it — an RFC is a
// Decision — so a caller proposing a mapping from observed types must
// handle the false and propose nothing rather than invent a word the
// parser would reject.
func CanonicalNameFor(docType DocType) (string, bool) {
	for _, name := range CanonicalTypeNames() {
		if canonicalTypes[name] == docType {
			// Title case: the canonical types are written capitalized
			// everywhere they are documented, and ParseTaxonomy lowercases
			// before matching, so this round-trips.
			return strings.ToUpper(name[:1]) + name[1:], true
		}
	}
	return "", false
}

// CanonicalTypeNames lists the accepted canonical types, sorted. Used to
// tell an author what they may write when they write something else.
func CanonicalTypeNames() []string {
	names := make([]string, 0, len(canonicalTypes))
	for n := range canonicalTypes {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// Taxonomy is a repository's own statement about what its directories
// hold: path glob -> canonical type.
type Taxonomy struct {
	rules []taxonomyRule
}

type taxonomyRule struct {
	pattern string
	docType DocType
	// declared is the canonical name as the repository wrote it. Several
	// canonical names collapse onto one DocType — Reference and Planning
	// are stored as readme and roadmap — so showing a reader the DocType
	// would show them a word they did not write and cannot look up.
	declared string
}

// ParseTaxonomy reads a .engineering.yaml body.
//
// A malformed file is an error rather than an empty taxonomy. It is a
// statement the repository is trying to make, and silently ignoring it
// would leave every document unclassified with nothing to explain why
// (engineering:rules/no-silent-fallback.md).
func ParseTaxonomy(content []byte) (Taxonomy, error) {
	var file struct {
		Taxonomy map[string]string `yaml:"taxonomy"`
	}
	if err := yaml.Unmarshal(content, &file); err != nil {
		return Taxonomy{}, fmt.Errorf("taxonomy: %w", err)
	}

	patterns := make([]string, 0, len(file.Taxonomy))
	for pattern := range file.Taxonomy {
		patterns = append(patterns, pattern)
	}
	// Deterministic order: a map iterates randomly, and two patterns can
	// match one path. Longest pattern first, so the more specific
	// statement wins; ties broken by name so a run is reproducible.
	sort.Slice(patterns, func(i, j int) bool {
		if len(patterns[i]) != len(patterns[j]) {
			return len(patterns[i]) > len(patterns[j])
		}
		return patterns[i] < patterns[j]
	})

	var t Taxonomy
	for _, pattern := range patterns {
		name := strings.ToLower(strings.TrimSpace(file.Taxonomy[pattern]))
		docType, ok := canonicalTypes[name]
		if !ok {
			return Taxonomy{}, fmt.Errorf("taxonomy: %q maps to unknown type %q — expected one of %s",
				pattern, file.Taxonomy[pattern], strings.Join(CanonicalTypeNames(), ", "))
		}
		if strings.TrimSpace(pattern) == "" {
			return Taxonomy{}, fmt.Errorf("taxonomy: empty path pattern")
		}
		t.rules = append(t.rules, taxonomyRule{pattern: pattern, docType: docType, declared: strings.TrimSpace(file.Taxonomy[pattern])})
	}
	return t, nil
}

// Empty reports whether the taxonomy says nothing.
func (t Taxonomy) Empty() bool { return len(t.rules) == 0 }

// Mapping is one declared pattern and the type it claims, in both the
// vocabulary the repository wrote and the one the kernel stores.
type Mapping struct {
	Pattern string
	// Declared is the canonical name from the file, e.g. "Reference".
	Declared string
	// Type is what it resolves to internally, e.g. DocTypeReadme.
	Type DocType
}

// Mappings returns what the repository declared, most specific first —
// the same order TypeFor consults, so a reader sees the order that
// actually decides rather than the order the file was written in.
//
// Exists so a caller can display a taxonomy without reconstructing the
// pattern-to-type relation from the outside. A second implementation of
// that could disagree with the one indexing uses, and the disagreement
// would surface as a command that explains the wrong thing.
func (t Taxonomy) Mappings() []Mapping {
	out := make([]Mapping, 0, len(t.rules))
	for _, r := range t.rules {
		out = append(out, Mapping{Pattern: r.pattern, Declared: r.declared, Type: r.docType})
	}
	return out
}

// TypeFor returns the canonical type a repository declares for path.
//
// Matching reuses RFC-0005's applies_to glob semantics, so a repository
// author learns one path syntax rather than two.
func (t Taxonomy) TypeFor(path string) (DocType, bool) {
	m, ok := t.MappingFor(path)
	if !ok {
		// DocTypeUnknown, not the zero Mapping's empty string. Every
		// current caller checks the boolean, so nothing was broken — but
		// routing this through MappingFor silently narrowed a contract
		// that used to be safe to ignore, and the next caller to ignore it
		// would get "" where the package's own vocabulary says "unknown".
		return DocTypeUnknown, false
	}
	return m.Type, true
}

// MappingFor returns the mapping that decides path — which pattern won,
// not just what it decided.
//
// Two patterns can match one path, and a caller reporting what a taxonomy
// achieves has to attribute each document to the line that actually
// claimed it. Answering that here rather than in the caller keeps one
// definition of "wins": the rules are consulted most-specific-first, and
// the first match decides.
func (t Taxonomy) MappingFor(path string) (Mapping, bool) {
	for _, r := range t.rules {
		if matchPath(r.pattern, path) {
			return Mapping{Pattern: r.pattern, Declared: r.declared, Type: r.docType}, true
		}
	}
	return Mapping{}, false
}
