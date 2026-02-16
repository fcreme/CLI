package suggest

import (
	"fmt"
	"os"
	"path/filepath"
)

// SuggestRequest describes a code modification to suggest.
type SuggestRequest struct {
	Description string
	FilePath    string // repo-relative path
	RepoRoot    string
	Apply       bool // if true, apply the change; if false, dry-run
}

// SuggestResult holds the result of a suggestion.
type SuggestResult struct {
	FilePath    string
	Strategy    string
	UnifiedDiff string
	BackupPath  string
	Applied     bool
	Modified    string // the full modified source
}

// Suggest generates a code modification suggestion.
func Suggest(req SuggestRequest) (*SuggestResult, error) {
	// Read the file
	absPath := filepath.Join(req.RepoRoot, filepath.FromSlash(req.FilePath))
	content, err := os.ReadFile(absPath)
	if err != nil {
		return nil, fmt.Errorf("reading file %s: %w", req.FilePath, err)
	}
	original := string(content)

	// Parse parameters from description
	params := ParseParams(req.Description)

	// Select strategy
	strategy := SelectStrategy(req.Description)

	// Apply strategy
	modified, err := strategy.Apply(original, params)
	if err != nil {
		return nil, fmt.Errorf("strategy '%s': %w", strategy.Name(), err)
	}

	// Compute diff
	diff := ComputeUnifiedDiff(req.FilePath, original, modified)

	result := &SuggestResult{
		FilePath:    req.FilePath,
		Strategy:    strategy.Name(),
		UnifiedDiff: diff,
		Modified:    modified,
	}

	// Apply if requested
	if req.Apply {
		backupPath, err := BackupAndApply(req.RepoRoot, req.FilePath, modified)
		if err != nil {
			return result, fmt.Errorf("applying changes: %w", err)
		}
		result.BackupPath = backupPath
		result.Applied = true
	}

	return result, nil
}
