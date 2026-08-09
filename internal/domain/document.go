package domain

import (
	"errors"
	"strings"
	"time"
)

// DocType is a Knowledge Type (KNOWLEDGE_MODEL.md §2) classifying what a
// Document represents, independent of its Source format.
type DocType string

const (
	DocTypeADR      DocType = "adr"
	DocTypeRule     DocType = "rule"
	DocTypeStandard DocType = "standard"
	DocTypeRFC      DocType = "rfc"
	DocTypeRoadmap  DocType = "roadmap"
	DocTypeReadme   DocType = "readme"
	DocTypeUnknown  DocType = "unknown"

	// Added by RFC-0007 for canonical types this organization's own
	// vocabulary had no name for. Additive: no existing document
	// changes type.
	DocTypeGuide         DocType = "guide"
	DocTypeSpecification DocType = "specification"
)

// RawDocument is a Collector's output and a Parser's input — bytes plus
// just enough provenance to trace them back to a Source. See
// INTERFACES.md.
type RawDocument struct {
	Path      string
	SourceID  string
	Bytes     []byte
	FetchedAt time.Time
}

// NewRawDocument validates and constructs a RawDocument.
func NewRawDocument(sourceID, path string, contentBytes []byte) (RawDocument, error) {
	sourceID = strings.TrimSpace(sourceID)
	path = strings.TrimSpace(path)
	if sourceID == "" {
		return RawDocument{}, errors.New("domain: raw document requires a source id")
	}
	if path == "" {
		return RawDocument{}, errors.New("domain: raw document requires a path")
	}
	return RawDocument{
		Path:      path,
		SourceID:  sourceID,
		Bytes:     contentBytes,
		FetchedAt: time.Now().UTC(),
	}, nil
}

// CanonicalDocument is the one shape every Parser must eventually produce
// (via Normalizer, if not directly) and every Storage must be able to
// persist. See INTERFACES.md's "canonical document model".
type CanonicalDocument struct {
	ID            string
	RepositoryID  string
	SourceID      string
	Path          string
	Type          DocType
	Title         string
	Content       string
	Metadata      Metadata
	Tags          []Tag
	Relationships []Relationship
	ContentHash   string
	GitAuthor     string
	GitUpdatedAt  time.Time
	IndexedAt     time.Time
}

// DocumentID computes the same deterministic id NewCanonicalDocument
// assigns, without constructing a document — for callers (internal/graph's
// reference resolver, internal/indexer's delete-by-path on sync) that only
// need to know a document's id given its repository and path.
func DocumentID(repositoryID, path string) string {
	return contentID(strings.TrimSpace(repositoryID), strings.TrimSpace(path))
}

// NewCanonicalDocument validates and constructs a CanonicalDocument. ID is
// derived from (repositoryID, path) — v1's answer to DATABASE.md's open
// question on document identity: path-based, not content-hash-based, so a
// renamed file becomes a new row rather than an edited one changing its id.
func NewCanonicalDocument(repositoryID, sourceID, path string) (CanonicalDocument, error) {
	repositoryID = strings.TrimSpace(repositoryID)
	path = strings.TrimSpace(path)
	if repositoryID == "" {
		return CanonicalDocument{}, errors.New("domain: document requires a repository id")
	}
	if path == "" {
		return CanonicalDocument{}, errors.New("domain: document requires a path")
	}
	return CanonicalDocument{
		ID:           contentID(repositoryID, path),
		RepositoryID: repositoryID,
		SourceID:     strings.TrimSpace(sourceID),
		Path:         path,
		Type:         DocTypeUnknown,
		Metadata:     NewMetadata(),
	}, nil
}
