package cmd

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/fcreme/CLI/repokit/internal/config"
	"github.com/fcreme/CLI/repokit/internal/detector"
	"github.com/fcreme/CLI/repokit/internal/indexer"
	"github.com/fcreme/CLI/repokit/internal/output"
	"github.com/fcreme/CLI/repokit/internal/store"
)

var forceReindex bool

var indexCmd = &cobra.Command{
	Use:   "index",
	Short: "Index the connected repository",
	Long: `Walk the repository, parse source files, and build an index of
components, hooks, imports, exports, and type definitions.

Uses incremental indexing by default - only files that have changed
since the last index will be re-parsed. Use --force to rebuild from scratch.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		cwd, _ := os.Getwd()
		repoRoot, err := config.FindRepoRoot(cwd)
		if err != nil {
			output.PrintError("%v", err)
			return err
		}

		s, err := store.Open(config.DBPath(repoRoot))
		if err != nil {
			output.PrintError("Opening index: %v", err)
			return err
		}
		defer s.Close()

		ctx := context.Background()
		start := time.Now()

		output.PrintInfo("Indexing repository at %s", repoRoot)

		opts := indexer.IndexOptions{
			Force: forceReindex,
			OnProgress: func(current, total int, file string) {
				if current%10 == 0 || current == total {
					fmt.Printf("\r  [%d/%d] %s", current, total, truncatePath(file, 60))
				}
			},
		}

		result, err := indexer.Index(ctx, repoRoot, s, opts)
		if err != nil {
			output.PrintError("Indexing failed: %v", err)
			return err
		}

		fmt.Println() // clear progress line

		// Detect patterns
		patterns, _ := detector.DetectPatterns(ctx, repoRoot, s)

		// Update metadata
		s.SetMeta(ctx, "last_indexed_at", time.Now().UTC().Format(time.RFC3339))
		_ = patterns

		elapsed := time.Since(start)
		fmt.Println()
		output.PrintSuccess("Indexing complete in %s", elapsed.Round(time.Millisecond))
		fmt.Println()

		rows := [][]string{
			{"Files indexed", fmt.Sprintf("%d", result.FilesIndexed)},
			{"Files skipped (unchanged)", fmt.Sprintf("%d", result.FilesSkipped)},
			{"Total files", fmt.Sprintf("%d", result.TotalFiles)},
			{"Components found", fmt.Sprintf("%d", result.Components)},
			{"Hooks found", fmt.Sprintf("%d", result.Hooks)},
			{"Type definitions", fmt.Sprintf("%d", result.TypeDefs)},
			{"Imports tracked", fmt.Sprintf("%d", result.Imports)},
			{"Exports tracked", fmt.Sprintf("%d", result.Exports)},
			{"Tags assigned", fmt.Sprintf("%d", result.TagsAssigned)},
		}
		if result.FilesFailed > 0 {
			rows = append(rows, []string{"Files failed", fmt.Sprintf("%d", result.FilesFailed)})
		}

		table := &output.Table{
			Headers: []string{"Metric", "Count"},
			Rows:    rows,
		}
		table.Print()

		// Report errors
		if len(result.Errors) > 0 {
			fmt.Println()
			output.PrintWarning("Indexing errors (%d):", len(result.Errors))
			max := 5
			if len(result.Errors) < max {
				max = len(result.Errors)
			}
			for _, e := range result.Errors[:max] {
				fmt.Printf("    %s: %s\n", e.File, e.Message)
			}
			if len(result.Errors) > max {
				fmt.Printf("    ... and %d more\n", len(result.Errors)-max)
			}
		}

		// Next step hints
		output.PrintHint("repokit find component \"\"")
		output.PrintHint("repokit status")
		output.PrintHint("repokit deps")
		fmt.Println()

		return nil
	},
}

func truncatePath(path string, maxLen int) string {
	if len(path) <= maxLen {
		return path + strings.Repeat(" ", maxLen-len(path))
	}
	return "..." + path[len(path)-maxLen+3:]
}

func init() {
	indexCmd.Flags().BoolVar(&forceReindex, "force", false, "Force full re-index (ignore cache)")
	rootCmd.AddCommand(indexCmd)
}
