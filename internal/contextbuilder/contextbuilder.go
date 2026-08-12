// Package contextbuilder implements kernel.ContextBuilder: packages a
// RetrievalBundle into text. v1's only consumer is a terminal
// (`eng context`/`eng ask`); a future Milestone (AI layer) packages the
// same bundle into a token-budget-aware prompt instead — Retriever never
// has to change either way. See ARCHITECTURE.md's Context Builder.
package contextbuilder

import (
	"context"
	"fmt"
	"strings"

	"github.com/truelogics/engineering-kernel/internal/kernel"
)

// ContextBuilder implements kernel.ContextBuilder.
type ContextBuilder struct{}

var _ kernel.ContextBuilder = (*ContextBuilder)(nil)

// New returns a text-formatting ContextBuilder.
func New() *ContextBuilder {
	return &ContextBuilder{}
}

// Build implements kernel.ContextBuilder.
func (c *ContextBuilder) Build(ctx context.Context, bundle kernel.RetrievalBundle) (kernel.AssembledContext, error) {
	var b strings.Builder
	for i, group := range bundle.Groups {
		if i > 0 {
			b.WriteString("\n")
		}
		fmt.Fprintf(&b, "%s:\n", group.Label)
		if len(group.Results) == 0 {
			b.WriteString("  (none indexed yet)\n")
			continue
		}
		for _, r := range group.Results {
			fmt.Fprintf(&b, "  %s (score %.2f)\n", r.Document.Path, r.Score)
		}
	}
	return kernel.AssembledContext{Task: bundle.Task, Body: b.String()}, nil
}
