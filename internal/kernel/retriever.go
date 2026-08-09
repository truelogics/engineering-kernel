package kernel

import "context"

// RetrievalGroup is one labeled section of a RetrievalBundle — e.g.
// "Architecture docs", "ADRs", "Related PRs" (empty until Milestone 2
// ingests PRs).
type RetrievalGroup struct {
	Label   string
	Results []SearchResult
}

// RetrievalBundle is Retriever's output — grouped, de-duplicated results
// for a task, not a flat list.
type RetrievalBundle struct {
	Task   string
	Groups []RetrievalGroup
}

// Retriever gathers everything relevant for a task — not just a search
// query. v1's only task shape is a natural-language question (`eng ask`);
// "review PR #123" (KNOWLEDGE_MODEL.md, INTERFACES.md) is illustrative of
// where this is headed, not implemented in v1 (no PR ingestion yet). Must
// not format output for a specific consumer (Context Builder's job) or
// generate prose — assembly only.
type Retriever interface {
	Retrieve(ctx context.Context, task string, opts RetrieveOptions) (RetrievalBundle, error)
}

// RetrieveOptions narrows what a retrieval returns (RFC-0005).
type RetrieveOptions struct {
	// ChangedPaths are the repository-relative files this task concerns.
	// When set, the Rules group is filtered to rules whose applies_to
	// governs at least one of them. Empty means no path information is
	// available and no scoping is applied — `eng ask "how does indexing
	// work?"` has no changed paths and must behave exactly as before.
	ChangedPaths []string
}
