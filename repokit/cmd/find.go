package cmd

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/usuario/repokit/internal/config"
	"github.com/usuario/repokit/internal/output"
	"github.com/usuario/repokit/internal/store"
)

var (
	findKind string
	findTags []string
	findLimit int
)

var findCmd = &cobra.Command{
	Use:     "find",
	Aliases: []string{"f"},
	Short:   "Search the component registry",
}

var findComponentCmd = &cobra.Command{
	Use:   "component [query]",
	Short: "Find components matching a query",
	Long: `Search the indexed component registry by name, kind, or tags.

Examples:
  repokit find component "Button"
  repokit f component --kind hook
  repokit f component --tag modal
  repokit find component "Card" --tag layout`,
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

		query := ""
		if len(args) > 0 {
			query = args[0]
		}

		components, err := s.SearchComponents(ctx, query, findKind, findTags, findLimit)
		if err != nil {
			output.PrintError("Search failed: %v", err)
			return err
		}

		if len(components) == 0 {
			fmt.Println()
			if query != "" {
				output.PrintWarning("No components found matching \"%s\"", query)
				output.PrintHint("repokit find component \"\" to list all components")
			} else {
				output.PrintWarning("No components found. Is the project indexed?")
				output.PrintHint("repokit index --force to re-index")
			}
			fmt.Println()
			return nil
		}

		table := &output.Table{
			Headers: []string{"ID", "Name", "Kind", "Path", "Export", "Usage"},
		}
		for _, c := range components {
			tags := make([]string, 0)
			for _, t := range c.Tags {
				tags = append(tags, t.Name)
			}
			tagStr := ""
			if len(tags) > 0 {
				tagStr = " [" + strings.Join(tags, ", ") + "]"
			}

			table.Rows = append(table.Rows, []string{
				fmt.Sprintf("%d", c.ID),
				c.Name + tagStr,
				string(c.Kind),
				c.FilePath,
				c.ExportType,
				fmt.Sprintf("%d", c.UsageCount),
			})
		}

		fmt.Println()
		table.Print()
		fmt.Println()
		output.PrintSuccess("Found %d components", len(components))
		output.PrintHint("repokit show <id> to see component details")
		fmt.Println()
		return nil
	},
}

func init() {
	findComponentCmd.Flags().StringVar(&findKind, "kind", "", "Filter by kind (function_component, hook, etc.)")
	findComponentCmd.Flags().StringSliceVar(&findTags, "tag", nil, "Filter by tag (modal, form, table, etc.)")
	findComponentCmd.Flags().IntVar(&findLimit, "limit", 50, "Maximum results to return")

	findCmd.AddCommand(findComponentCmd)
	rootCmd.AddCommand(findCmd)
}
