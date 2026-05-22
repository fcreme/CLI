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

var patternsCmd = &cobra.Command{
	Use:   "patterns",
	Short: "Show detected project conventions and patterns",
	Long: `Display conventions detected during indexing, including folder structure,
naming patterns, import styles, testing conventions, and component architecture.

Patterns are detected automatically during 'repokit index'.`,
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

		rows, err := s.DB().QueryContext(ctx,
			"SELECT kind, description, confidence FROM patterns ORDER BY kind, confidence DESC")
		if err != nil {
			output.PrintError("Querying patterns: %v", err)
			return err
		}
		defer rows.Close()

		var lines []string
		count := 0
		for rows.Next() {
			var kind, description string
			var confidence float64
			if err := rows.Scan(&kind, &description, &confidence); err != nil {
				continue
			}
			if len(description) > 40 {
				description = description[:37] + "..."
			}
			lines = append(lines, fmt.Sprintf(" %-14s %-40s %s",
				output.Dim(kind),
				description,
				output.Dim(fmt.Sprintf("(%.0f%%)", confidence*100))))
			count++
		}

		fmt.Println()
		fmt.Println(output.Banner("PROJECT PATTERNS", fmt.Sprintf("%d detected", count)))
		fmt.Println()

		if count == 0 {
			fmt.Println(output.Box("Patterns", []string{
				" " + output.Dim("No patterns detected."),
				" " + output.Dim("Run 'repokit index' first."),
			}))
			fmt.Println()
			return nil
		}

		fmt.Println(output.Box("Detected Conventions", lines))
		fmt.Println()
		return nil
	},
}

func init() {
	rootCmd.AddCommand(patternsCmd)
}
