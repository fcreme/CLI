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

		table := &output.Table{
			Headers: []string{"Category", "Pattern", "Confidence"},
		}

		count := 0
		for rows.Next() {
			var kind, description string
			var confidence float64
			if err := rows.Scan(&kind, &description, &confidence); err != nil {
				continue
			}
			table.Rows = append(table.Rows, []string{
				kind,
				description,
				fmt.Sprintf("%.0f%%", confidence*100),
			})
			count++
		}

		if count == 0 {
			output.PrintWarning("No patterns detected. Run 'repokit index' first.")
			return nil
		}

		fmt.Println()
		table.Print()
		fmt.Println()
		output.PrintSuccess("Detected %d patterns", count)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(patternsCmd)
}
