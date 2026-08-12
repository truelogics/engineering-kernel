package normalizer

import (
	"context"
	"testing"
	"time"

	"github.com/truelogics/engineering-kernel/internal/domain"
)

func fixedClock(at time.Time) func() time.Time {
	return func() time.Time { return at }
}

func TestNormalizeFillsDefaults(t *testing.T) {
	doc := domain.CanonicalDocument{
		ID:           "doc-1",
		RepositoryID: "repo-1",
		Path:         "./docs/../docs/README.md",
	}
	now := time.Date(2026, 8, 2, 12, 0, 0, 0, time.UTC)
	n := &Normalizer{Now: fixedClock(now)}

	got, err := n.Normalize(context.Background(), doc)
	if err != nil {
		t.Fatalf("Normalize: unexpected error: %v", err)
	}
	if got.Path != "docs/README.md" {
		t.Errorf("Path = %q, want %q", got.Path, "docs/README.md")
	}
	if got.Title != got.Path {
		t.Errorf("Title = %q, want fallback to Path %q", got.Title, got.Path)
	}
	if got.Type != domain.DocTypeUnknown {
		t.Errorf("Type = %q, want %q", got.Type, domain.DocTypeUnknown)
	}
	if got.Metadata == nil {
		t.Error("expected non-nil Metadata")
	}
	if got.Tags == nil {
		t.Error("expected non-nil Tags")
	}
	if got.Relationships == nil {
		t.Error("expected non-nil Relationships")
	}
	if !got.GitUpdatedAt.Equal(now) {
		t.Errorf("GitUpdatedAt = %v, want %v (defaulted)", got.GitUpdatedAt, now)
	}
	if !got.IndexedAt.Equal(now) {
		t.Errorf("IndexedAt = %v, want %v", got.IndexedAt, now)
	}
}

func TestNormalizePreservesExistingGitUpdatedAt(t *testing.T) {
	original := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	doc := domain.CanonicalDocument{
		ID:           "doc-1",
		RepositoryID: "repo-1",
		Path:         "README.md",
		GitUpdatedAt: original,
	}
	n := &Normalizer{Now: fixedClock(time.Now())}
	got, err := n.Normalize(context.Background(), doc)
	if err != nil {
		t.Fatalf("Normalize: unexpected error: %v", err)
	}
	if !got.GitUpdatedAt.Equal(original) {
		t.Errorf("GitUpdatedAt = %v, want preserved %v", got.GitUpdatedAt, original)
	}
}

func TestNormalizeRejectsMissingIdentity(t *testing.T) {
	cases := []domain.CanonicalDocument{
		{ID: "", RepositoryID: "repo-1", Path: "README.md"},
		{ID: "doc-1", RepositoryID: "", Path: "README.md"},
		{ID: "doc-1", RepositoryID: "repo-1", Path: ""},
		{ID: "doc-1", RepositoryID: "repo-1", Path: "./"},
	}
	n := New()
	for i, doc := range cases {
		if _, err := n.Normalize(context.Background(), doc); err == nil {
			t.Errorf("case %d: expected error for %+v", i, doc)
		}
	}
}

func TestNormalizeDedupesTagsAndRelationships(t *testing.T) {
	tagA, _ := domain.NewTag("doc-1", "severity", "error")
	tagB, _ := domain.NewTag("doc-1", "severity", "error") // identical
	tagC, _ := domain.NewTag("doc-1", "severity", "warn")

	relA, _ := domain.NewRelationship("doc-1", "doc-2", domain.RelationshipReferences, domain.RelationshipExplicit)
	relB, _ := domain.NewRelationship("doc-1", "doc-2", domain.RelationshipReferences, domain.RelationshipExplicit) // identical

	doc := domain.CanonicalDocument{
		ID:            "doc-1",
		RepositoryID:  "repo-1",
		Path:          "README.md",
		Tags:          []domain.Tag{tagA, tagB, tagC},
		Relationships: []domain.Relationship{relA, relB},
	}

	got, err := New().Normalize(context.Background(), doc)
	if err != nil {
		t.Fatalf("Normalize: unexpected error: %v", err)
	}
	if len(got.Tags) != 2 {
		t.Errorf("Tags = %+v, want 2 deduplicated tags", got.Tags)
	}
	if len(got.Relationships) != 1 {
		t.Errorf("Relationships = %+v, want 1 deduplicated relationship", got.Relationships)
	}
}
