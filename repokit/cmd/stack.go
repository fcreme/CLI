package cmd

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/fcreme/CLI/repokit/internal/config"
	"github.com/fcreme/CLI/repokit/internal/detector"
	"github.com/fcreme/CLI/repokit/internal/output"
	"github.com/fcreme/CLI/repokit/internal/store"
)

var stackCmd = &cobra.Command{
	Use:   "stack",
	Short: "Detect and display the project's tech stack",
	Long: `Analyze the connected repository to detect its technology stack.

Reads package.json, tsconfig.json, configuration files, and other
indicators to identify frameworks, languages, styling libraries,
state management, testing tools, and bundlers.`,
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
		output.PrintInfo("Detecting stack for %s...", repoRoot)

		items, err := detector.DetectStack(ctx, repoRoot, s)
		if err != nil {
			output.PrintError("Stack detection failed: %v", err)
			return err
		}

		if len(items) == 0 {
			output.PrintWarning("No technologies detected")
			return nil
		}

		table := &output.Table{
			Headers: []string{"Category", "Technology", "Version"},
		}
		for _, item := range items {
			version := item.Version
			if version == "" {
				version = "-"
			}
			table.Rows = append(table.Rows, []string{
				item.Category,
				item.Name,
				version,
			})
		}

		fmt.Println()
		table.Print()
		fmt.Println()
		output.PrintSuccess("Detected %d technologies", len(items))
		return nil
	},
}

func init() {
	rootCmd.AddCommand(stackCmd)
}
