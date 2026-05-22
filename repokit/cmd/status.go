package cmd

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/fcreme/CLI/repokit/internal/config"
	"github.com/fcreme/CLI/repokit/internal/output"
	"github.com/fcreme/CLI/repokit/internal/store"
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
		fmt.Println(output.Banner("REPOSITORY STATUS", "repokit v"+Version))
		fmt.Println()

		// Connection box
		connLines := []string{
			fmt.Sprintf(" %-13s %s", output.Dim("Repository:"), cfg.RepoPath),
		}
		if cfg.RemoteURL != "" {
			connLines = append(connLines, fmt.Sprintf(" %-13s %s", output.Dim("Remote:"), cfg.RemoteURL))
		}
		lastIndexed, _ := s.GetMeta(ctx, "last_indexed_at")
		if lastIndexed != "" {
			connLines = append(connLines, fmt.Sprintf(" %-13s %s", output.Dim("Last indexed:"), lastIndexed))
		} else {
			connLines = append(connLines, fmt.Sprintf(" %-13s %s", output.Dim("Last indexed:"), output.Yellow("never (run 'repokit index')")))
		}
		fmt.Println(output.Box("Connection", connLines))

		// Index Summary box
		var fileCount, componentCount, hookCount, importCount, typeCount int
		s.DB().QueryRowContext(ctx, "SELECT COUNT(*) FROM files").Scan(&fileCount)
		s.DB().QueryRowContext(ctx, "SELECT COUNT(*) FROM components WHERE kind != 'hook'").Scan(&componentCount)
		s.DB().QueryRowContext(ctx, "SELECT COUNT(*) FROM components WHERE kind = 'hook'").Scan(&hookCount)
		s.DB().QueryRowContext(ctx, "SELECT COUNT(*) FROM imports").Scan(&importCount)
		s.DB().QueryRowContext(ctx, "SELECT COUNT(*) FROM type_definitions").Scan(&typeCount)

		idxLines := []string{
			fmt.Sprintf(" %-13s %s", output.Dim("Files:"), output.Cyan(fmt.Sprintf("%d", fileCount))),
			fmt.Sprintf(" %-13s %s", output.Dim("Components:"), output.Cyan(fmt.Sprintf("%d", componentCount))),
			fmt.Sprintf(" %-13s %s", output.Dim("Hooks:"), output.Cyan(fmt.Sprintf("%d", hookCount))),
			fmt.Sprintf(" %-13s %s", output.Dim("Types:"), output.Cyan(fmt.Sprintf("%d", typeCount))),
			fmt.Sprintf(" %-13s %s", output.Dim("Imports:"), output.Cyan(fmt.Sprintf("%d", importCount))),
		}
		fmt.Println()
		fmt.Println(output.Box("Index Summary", idxLines))

		// Stack box (only if items detected)
		stack, _ := s.GetStack(ctx)
		var patternCount int
		s.DB().QueryRowContext(ctx, "SELECT COUNT(*) FROM patterns").Scan(&patternCount)
		if len(stack) > 0 || patternCount > 0 {
			var extraLines []string
			if len(stack) > 0 {
				var names []string
				for _, item := range stack {
					if item.Category == "framework" || item.Category == "language" || item.Category == "styling" {
						names = append(names, item.Name)
					}
				}
				extraLines = append(extraLines, fmt.Sprintf(" %-13s %s", output.Dim("Stack:"), output.Green(joinMax(names, " + ", 6))))
			}
			if patternCount > 0 {
				extraLines = append(extraLines, fmt.Sprintf(" %-13s %d detected", output.Dim("Patterns:"), patternCount))
			}
			fmt.Println()
			fmt.Println(output.Box("Project", extraLines))
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
