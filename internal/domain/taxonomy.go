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
		t.rules = append(t.rules, taxonomyRule{pattern: pattern, docType: docType})
	}
	return t, nil
}

// Empty reports whether the taxonomy says nothing.
func (t Taxonomy) Empty() bool { return len(t.rules) == 0 }

// TypeFor returns the canonical type a repository declares for path.
//
// Matching reuses RFC-0005's applies_to glob semantics, so a repository
// author learns one path syntax rather than two.
func (t Taxonomy) TypeFor(path string) (DocType, bool) {
	for _, r := range t.rules {
		if matchPath(r.pattern, path) {
			return r.docType, true
		}
	}
	return DocTypeUnknown, false
}
