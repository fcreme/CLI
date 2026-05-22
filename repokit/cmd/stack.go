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

		fmt.Println()
		fmt.Println(output.Banner("TECH STACK", fmt.Sprintf("%d detected", len(items))))
		fmt.Println()

		if len(items) == 0 {
			fmt.Println(output.Box("Stack", []string{" " + output.Dim("No technologies detected")}))
			fmt.Println()
			return nil
		}

		var lines []string
		for _, item := range items {
			version := item.Version
			if version == "" {
				version = "-"
			}
			lines = append(lines, fmt.Sprintf(" %-14s %-24s %s",
				output.Dim(item.Category),
				output.Cyan(item.Name),
				output.Dim(version)))
		}
		fmt.Println(output.Box("Technologies", lines))
		fmt.Println()
		return nil
	},
}

func init() {
	rootCmd.AddCommand(stackCmd)
}
