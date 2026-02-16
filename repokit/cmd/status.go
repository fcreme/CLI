package cmd

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/usuario/repokit/internal/config"
	"github.com/usuario/repokit/internal/output"
	"github.com/usuario/repokit/internal/store"
)

var statusCmd = &cobra.Command{
	Use:     "status",
	Aliases: []string{"st"},
	Short:   "Show repository connection status and index summary",
	RunE: func(cmd *cobra.Command, args []string) error {
		cwd, _ := os.Getwd()
		repoRoot, err := config.FindRepoRoot(cwd)
		if err != nil {
			output.PrintError("%v", err)
			return err
		}

		cfg, err := config.Load(repoRoot)
		if err != nil {
			output.PrintError("Loading config: %v", err)
			return err
		}

		s, err := store.Open(config.DBPath(repoRoot))
		if err != nil {
			output.PrintError("Opening index: %v", err)
			return err
		}
		defer s.Close()

		ctx := context.Background()

		fmt.Println()
		fmt.Printf("  %s %s\n", output.Bold("Repository:"), cfg.RepoPath)
		if cfg.RemoteURL != "" {
			fmt.Printf("  %s %s\n", output.Bold("Remote:"), cfg.RemoteURL)
		}

		lastIndexed, _ := s.GetMeta(ctx, "last_indexed_at")
		if lastIndexed != "" {
			fmt.Printf("  %s %s\n", output.Bold("Last indexed:"), lastIndexed)
		} else {
			fmt.Printf("  %s %s\n", output.Bold("Last indexed:"), output.Yellow("never (run 'repokit index')"))
		}

		// Counts
		var fileCount, componentCount, hookCount, importCount, typeCount int
		s.DB().QueryRowContext(ctx, "SELECT COUNT(*) FROM files").Scan(&fileCount)
		s.DB().QueryRowContext(ctx, "SELECT COUNT(*) FROM components WHERE kind != 'hook'").Scan(&componentCount)
		s.DB().QueryRowContext(ctx, "SELECT COUNT(*) FROM components WHERE kind = 'hook'").Scan(&hookCount)
		s.DB().QueryRowContext(ctx, "SELECT COUNT(*) FROM imports").Scan(&importCount)
		s.DB().QueryRowContext(ctx, "SELECT COUNT(*) FROM type_definitions").Scan(&typeCount)

		fmt.Println()
		fmt.Printf("  %s\n", output.Bold("Index Summary"))
		fmt.Printf("    Files:        %s\n", output.Cyan(fmt.Sprintf("%d", fileCount)))
		fmt.Printf("    Components:   %s\n", output.Cyan(fmt.Sprintf("%d", componentCount)))
		fmt.Printf("    Hooks:        %s\n", output.Cyan(fmt.Sprintf("%d", hookCount)))
		fmt.Printf("    Types:        %s\n", output.Cyan(fmt.Sprintf("%d", typeCount)))
		fmt.Printf("    Imports:      %s\n", output.Cyan(fmt.Sprintf("%d", importCount)))

		// Stack summary
		stack, _ := s.GetStack(ctx)
		if len(stack) > 0 {
			fmt.Println()
			fmt.Printf("  %s ", output.Bold("Stack:"))
			var names []string
			for _, item := range stack {
				if item.Category == "framework" || item.Category == "language" || item.Category == "styling" {
					names = append(names, item.Name)
				}
			}
			fmt.Println(output.Green(joinMax(names, " + ", 6)))
		}

		// Patterns
		var patternCount int
		s.DB().QueryRowContext(ctx, "SELECT COUNT(*) FROM patterns").Scan(&patternCount)
		if patternCount > 0 {
			fmt.Printf("  %s %d detected\n", output.Bold("Patterns:"), patternCount)
		}

		fmt.Println()
		return nil
	},
}

func joinMax(items []string, sep string, max int) string {
	if len(items) > max {
		items = items[:max]
		items = append(items, "...")
	}
	result := ""
	for i, item := range items {
		if i > 0 {
			result += sep
		}
		result += item
	}
	return result
}

func init() {
	rootCmd.AddCommand(statusCmd)
}
