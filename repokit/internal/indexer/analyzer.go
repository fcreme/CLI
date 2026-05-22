package indexer

import (
	"context"

	"github.com/fcreme/CLI/repokit/pkg/models"
)

// AnalysisResult holds everything extracted from a single file.
type AnalysisResult struct {
	Components      []models.Component
	TypeDefinitions []models.TypeDefinition
	Imports         []models.Import
	Exports         []models.Export
	Hooks           []models.Component // hooks stored as components with kind=hook
}

// Analyzer extracts structured entities from source files.
type Analyzer interface {
	// SupportedExtensions returns file extensions this analyzer handles.
	SupportedExtensions() []string

	// Analyze parses a file and returns all extracted entities.
	Analyze(ctx context.Context, filePath string, content []byte, language string) (*AnalysisResult, error)
}
