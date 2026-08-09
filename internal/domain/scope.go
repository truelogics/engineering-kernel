package domain

import "strings"

// AppliesToKey is the front-matter field a rule uses to declare which
// files it governs (RFC-0005).
const AppliesToKey = "applies_to"

// Scope is a rule's declared file scope — the parsed form of its
// applies_to field. A zero Scope is universal: it matches every path.
type Scope struct {
	patterns []string
}

// ParseScope parses an applies_to value: a comma-separated list of
// patterns. An empty or whitespace-only value yields a universal Scope,
// because a rule whose author did not think about scope is far more
// likely to be a general standard than a silently inapplicable one
// (RFC-0005).
func ParseScope(appliesTo string) Scope {
	var patterns []string
	for _, raw := range strings.Split(appliesTo, ",") {
		p := strings.TrimSpace(raw)
		if p == "" {
			continue
		}
		patterns = append(patterns, expandShorthand(p))
	}
	return Scope{patterns: patterns}
}

// ScopeOf reads a document's declared scope from its metadata.
func ScopeOf(meta Metadata) Scope {
	v, _ := meta.Get(AppliesToKey)
	return ParseScope(v)
}

// Universal reports whether s applies to every path.
func (s Scope) Universal() bool {
	return len(s.patterns) == 0
}

// Patterns returns the expanded patterns, for tests and diagnostics.
func (s Scope) Patterns() []string {
	return append([]string(nil), s.patterns...)
}

// Matches reports whether s governs any of paths. A universal Scope
// matches everything, including an empty path list.
func (s Scope) Matches(paths []string) bool {
	if s.Universal() {
		return true
	}
	for _, p := range paths {
		for _, pattern := range s.patterns {
			if matchPath(pattern, p) {
				return true
			}
		}
	}
	return false
}

// expandShorthand turns a bare token into a glob. `applies_to: go` is
// what rule authors actually write, and by a wide margin the common
// case; requiring `**/*.go` would mean most rules carry a pattern whose
// syntax matters more than its content (RFC-0005). A token carrying any
// path or glob punctuation is already a pattern and passes through.
func expandShorthand(token string) string {
	if strings.ContainsAny(token, "/*?.") {
		return token
	}
	return "**/*." + token
}

// matchPath reports whether pattern matches path. Implements RFC-0005's
// glob semantics — `*` and `?` stop at a separator, `**` does not —
// because path.Match supports neither `**` nor the base-name fallback
// below.
func matchPath(pattern, path string) bool {
	if globMatch(pattern, path) {
		return true
	}
	// A pattern naming no directory is matched against the base name
	// too, so `*.go` behaves the way an author expects against
	// `internal/store/user.go`.
	if !strings.Contains(pattern, "/") {
		if i := strings.LastIndex(path, "/"); i >= 0 {
			return globMatch(pattern, path[i+1:])
		}
	}
	return false
}

// globMatch reports whether pattern matches name, recursively. Written
// as recursion rather than the usual backtracking loop because `**` and
// `*` have different separator rules, and the loop form of that is easy
// to write and hard to read — the first version of this function was
// wrong on four of the eleven cases in scope_test.go.
func globMatch(pattern, name string) bool {
	if pattern == "" {
		return name == ""
	}

	if strings.HasPrefix(pattern, "**") {
		rest := pattern[2:]
		if rest == "" {
			return true // `**` at the end matches everything below it
		}
		if rest[0] == '/' {
			tail := rest[1:]
			// `**/` matches zero directories as well as many, so
			// `**/*.go` matches a file at the repository root.
			if globMatch(tail, name) {
				return true
			}
			for i := 0; i < len(name); i++ {
				if name[i] == '/' && globMatch(tail, name[i+1:]) {
					return true
				}
			}
			return false
		}
		for i := 0; i <= len(name); i++ {
			if globMatch(rest, name[i:]) {
				return true
			}
		}
		return false
	}

	switch pattern[0] {
	case '*':
		// A single `*` matches within one path segment only.
		for i := 0; i <= len(name); i++ {
			if globMatch(pattern[1:], name[i:]) {
				return true
			}
			if i < len(name) && name[i] == '/' {
				break
			}
		}
		return false
	case '?':
		if name == "" || name[0] == '/' {
			return false
		}
		return globMatch(pattern[1:], name[1:])
	default:
		if name == "" || pattern[0] != name[0] {
			return false
		}
		return globMatch(pattern[1:], name[1:])
	}
}
