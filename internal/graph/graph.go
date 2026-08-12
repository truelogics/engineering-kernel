package graph

import (
	"context"
	"fmt"

	"github.com/truelogics/engineering-kernel/internal/domain"
	"github.com/truelogics/engineering-kernel/internal/kernel"
)

// Subgraph is a bounded traversal result — the edges among Root and
// whatever's reachable within the requested depth.
type Subgraph struct {
	Root  string
	Edges []domain.Relationship
}

// Graph exposes traversal on top of kernel.Storage — Milestone 2. Not a
// second storage backend: Neighbors is exactly Storage.ListRelationships;
// Traverse is Storage.TraverseRelationships plus edge hydration.
type Graph struct {
	Storage kernel.Storage
}

// New returns a Graph backed by storage.
func New(storage kernel.Storage) *Graph {
	return &Graph{Storage: storage}
}

// Neighbors returns documentID's one-hop Relationships.
func (g *Graph) Neighbors(ctx context.Context, documentID string) ([]domain.Relationship, error) {
	rels, err := g.Storage.ListRelationships(ctx, documentID)
	if err != nil {
		return nil, fmt.Errorf("graph: neighbors of %s: %w", documentID, err)
	}
	return rels, nil
}

// Traverse returns the Subgraph reachable from documentID within depth
// hops: every edge where both endpoints are within the reachable set, so
// the result doesn't include edges that step one hop beyond depth.
func (g *Graph) Traverse(ctx context.Context, documentID string, depth int) (Subgraph, error) {
	ids, err := g.Storage.TraverseRelationships(ctx, documentID, depth)
	if err != nil {
		return Subgraph{}, fmt.Errorf("graph: traverse from %s: %w", documentID, err)
	}

	reachable := make(map[string]bool, len(ids)+1)
	reachable[documentID] = true
	for _, id := range ids {
		reachable[id] = true
	}

	seenEdge := map[string]bool{}
	var edges []domain.Relationship
	for id := range reachable {
		rels, err := g.Storage.ListRelationships(ctx, id)
		if err != nil {
			return Subgraph{}, fmt.Errorf("graph: traverse from %s: %w", documentID, err)
		}
		for _, r := range rels {
			if seenEdge[r.ID] {
				continue
			}
			if !reachable[r.FromDocumentID] || !reachable[r.ToDocumentID] {
				continue // steps one hop beyond the requested depth
			}
			seenEdge[r.ID] = true
			edges = append(edges, r)
		}
	}

	return Subgraph{Root: documentID, Edges: edges}, nil
}
