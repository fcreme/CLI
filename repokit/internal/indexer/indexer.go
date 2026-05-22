package indexer

import (
	"context"
	"fmt"
	"os"

	"github.com/fcreme/CLI/repokit/internal/detector"
	"github.com/fcreme/CLI/repokit/internal/store"
	"github.com/fcreme/CLI/repokit/pkg/models"
)

const maxFileSize = 1024 * 1024 // 1MB - skip minified/generated files

// IndexResult holds statistics from an indexing run.
type IndexResult struct {
	FilesIndexed int
	FilesSkipped int
	FilesFailed  int
	TotalFiles   int
	Components   int
	Hooks        int
	TypeDefs     int
	Imports      int
	Exports      int
	TagsAssigned int
	Errors       []IndexError
}

// IndexError tracks a specific indexing failure.
type IndexError struct {
	File    string
	Message string
}

// IndexOptions configures the indexing behavior.
type IndexOptions struct {
	Force      bool
	OnProgress func(indexed, total int, file string)
}

// Index performs full AST-based indexing of the repository.
func Index(ctx context.Context, repoRoot string, s *store.Store, opts IndexOptions) (*IndexResult, error) {
	result := &IndexResult{}

	// Walk the repository
	files, err := WalkRepo(repoRoot)
	if err != nil {
		return nil, fmt.Errorf("walking repository: %w", err)
	}
	result.TotalFiles = len(files)

	// Load existing file hashes for incremental check (single query, not per-file)
	existingHashes, err := s.GetAllFileHashes(ctx)
	if err != nil {
		existingHashes = make(map[string]string) // fresh index
	}

	// Begin transaction for the entire indexing operation
	txs, err := s.BeginTx(ctx)
	if err != nil {
		return nil, fmt.Errorf("starting transaction: %w", err)
	}
	defer txs.Rollback() // no-op if committed

	analyzer := NewReactAnalyzer()
	resolver := NewImportResolver(repoRoot, files)
	analyzer.SetResolver(resolver)

	for _, f := range files {
		// Skip oversized files (minified bundles, etc.)
		if f.Size > maxFileSize {
			result.FilesSkipped++
			continue
		}

		// Incremental check
		if !opts.Force {
			if existingHash, ok := existingHashes[f.Path]; ok && existingHash == f.Hash {
				result.FilesSkipped++
				continue
			}
		}

		// Upsert file record
		fileRecord := &models.FileRecord{
			Path:      f.Path,
			Hash:      f.Hash,
			Language:  f.Language,
			SizeBytes: f.Size,
		}
		fileID, err := txs.UpsertFile(ctx, fileRecord)
		if err != nil {
			result.FilesFailed++
			result.Errors = append(result.Errors, IndexError{File: f.Path, Message: fmt.Sprintf("upsert: %v", err)})
			continue
		}

		// Clear existing data for this file
		if err := txs.ClearFileData(ctx, fileID); err != nil {
			result.FilesFailed++
			result.Errors = append(result.Errors, IndexError{File: f.Path, Message: fmt.Sprintf("clear: %v", err)})
			continue
		}

		// Only analyze supported languages
		if !isSupportedLanguage(f.Language) {
			result.FilesIndexed++
			reportProgress(opts, result)
			continue
		}

		// Read file content
		content, err := os.ReadFile(f.AbsPath)
		if err != nil {
			result.FilesFailed++
			result.Errors = append(result.Errors, IndexError{File: f.Path, Message: fmt.Sprintf("read: %v", err)})
			continue
		}

		// Run analyzer
		analysis, err := analyzer.Analyze(ctx, f.Path, content, f.Language)
		if err != nil {
			result.FilesFailed++
			result.Errors = append(result.Errors, IndexError{File: f.Path, Message: fmt.Sprintf("analyze: %v", err)})
			continue
		}

		// Store components
		for _, comp := range analysis.Components {
			comp.FileID = fileID
			compID, err := txs.InsertComponent(ctx, &comp)
			if err != nil {
				continue
			}
			result.Components++
			comp.ID = compID
			tags := detector.AutoTag(&comp)
			for _, tag := range tags {
				if err := txs.TagComponent(ctx, compID, tag.Tag, tag.Confidence, "auto"); err == nil {
					result.TagsAssigned++
				}
			}
		}

		// Store hooks
		for _, hook := range analysis.Hooks {
			hook.FileID = fileID
			hookID, err := txs.InsertComponent(ctx, &hook)
			if err != nil {
				continue
			}
			result.Hooks++
			hook.ID = hookID
			tags := detector.AutoTag(&hook)
			for _, tag := range tags {
				if txs.TagComponent(ctx, hookID, tag.Tag, tag.Confidence, "auto") == nil {
					result.TagsAssigned++
				}
			}
		}

		// Store type definitions
		for _, td := range analysis.TypeDefinitions {
			td.FileID = fileID
			if _, err := txs.InsertTypeDef(ctx, &td); err == nil {
				result.TypeDefs++
			}
		}

		// Store imports
		for _, imp := range analysis.Imports {
			imp.FileID = fileID
			if err := txs.InsertImport(ctx, &imp); err == nil {
				result.Imports++
			}
		}

		// Store exports
		for _, exp := range analysis.Exports {
			exp.FileID = fileID
			if err := txs.InsertExport(ctx, &exp); err == nil {
				result.Exports++
			}
		}

		result.FilesIndexed++
		reportProgress(opts, result)

		// Check context cancellation
		select {
		case <-ctx.Done():
			return result, ctx.Err()
		default:
		}
	}

	// Commit the transaction
	if err := txs.Commit(); err != nil {
		return result, fmt.Errorf("committing index transaction: %w", err)
	}

	// WAL checkpoint for read performance
	s.Checkpoint()

	// Store index metadata (outside transaction, non-critical)
	s.SetMeta(ctx, "components_count", fmt.Sprintf("%d", result.Components))
	s.SetMeta(ctx, "hooks_count", fmt.Sprintf("%d", result.Hooks))

	return result, nil
}

func reportProgress(opts IndexOptions, result *IndexResult) {
	if opts.OnProgress != nil {
		total := result.FilesIndexed + result.FilesSkipped + result.FilesFailed
		opts.OnProgress(total, result.TotalFiles, "")
	}
}

func isSupportedLanguage(lang string) bool {
	switch lang {
	case "tsx", "ts", "jsx", "js":
		return true
	default:
		return false
	}
}
