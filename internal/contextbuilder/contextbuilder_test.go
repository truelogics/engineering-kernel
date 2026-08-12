package contextbuilder

import (
	"context"
	"strings"
	"testing"

	"github.com/truelogics/engineering-kernel/internal/domain"
	"github.com/truelogics/engineering-kernel/internal/kernel"
)

func TestBuildFormatsGroupsAndEmptySections(t *testing.T) {
	bundle := kernel.RetrievalBundle{
		Task: "Review authentication PR",
		Groups: []kernel.RetrievalGroup{
			{
				Label: "Architecture",
				Results: []kernel.SearchResult{
					{Document: domain.CanonicalDocument{Path: "ARCHITECTURE.md"}, Score: 0.91},
				},
			},
			{Label: "Related Issues"}, // empty — nothing ingests these yet
		},
	}

	assembled, err := New().Build(context.Background(), bundle)
	if err != nil {
		t.Fatalf("Build: unexpected error: %v", err)
	}
	if assembled.Task != "Review authentication PR" {
		t.Fatalf("assembled.Task = %q, want %q", assembled.Task, "Review authentication PR")
	}
	if !strings.Contains(assembled.Body, "Architecture:") || !strings.Contains(assembled.Body, "ARCHITECTURE.md") {
		t.Fatalf("Body = %q, want it to contain the Architecture section and its result", assembled.Body)
	}
	if !strings.Contains(assembled.Body, "Related Issues:") || !strings.Contains(assembled.Body, "none indexed yet") {
		t.Fatalf("Body = %q, want the empty Related Issues section to say 'none indexed yet', not be omitted", assembled.Body)
	}
}

func TestBuildEmptyBundle(t *testing.T) {
	assembled, err := New().Build(context.Background(), kernel.RetrievalBundle{Task: "anything"})
	if err != nil {
		t.Fatalf("Build: unexpected error: %v", err)
	}
	if assembled.Body != "" {
		t.Fatalf("Body = %q, want empty for a bundle with no groups", assembled.Body)
	}
}
