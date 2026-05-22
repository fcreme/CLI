package cmd

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/fcreme/CLI/repokit/internal/config"
	"github.com/fcreme/CLI/repokit/internal/output"
	"github.com/fcreme/CLI/repokit/internal/store"
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

		subtitle := query
		if subtitle == "" {
			subtitle = "all"
		}
		fmt.Println()
		fmt.Println(output.Banner("COMPONENT SEARCH", subtitle))
		fmt.Println()

		if len(components) == 0 {
			fmt.Println(output.Box("Results", []string{
				" " + output.Dim("No components match this query."),
			}))
			fmt.Println()
			if query != "" {
				output.PrintHint("repokit find component \"\" to list all components")
			} else {
				output.PrintHint("repokit index --force to re-index")
			}
			fmt.Println()
			return nil
		}

		var lines []string
		for _, c := range components {
			tagStr := ""
			if len(c.Tags) > 0 {
				var tags []string
				for _, t := range c.Tags {
					tags = append(tags, t.Name)
				}
				tagStr = output.Dim(" [" + strings.Join(tags, ",") + "]")
			}
			path := c.FilePath
			if len(path) > 30 {
				path = "..." + path[len(path)-27:]
			}
			lines = append(lines, fmt.Sprintf(" %s  %-18s %-14s %s%s",
				output.Cyan(fmt.Sprintf("#%-4d", c.ID)),
				c.Name,
				output.Dim(string(c.Kind)),
				output.Dim(path),
				tagStr))
		}
		fmt.Println(output.Box(fmt.Sprintf("Results (%d)", len(components)), lines))
		fmt.Println()
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
