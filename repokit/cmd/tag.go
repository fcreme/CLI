package cmd

import (
	"context"
	"fmt"
	"os"
	"strconv"

	"github.com/spf13/cobra"
	"github.com/usuario/repokit/internal/config"
	"github.com/usuario/repokit/internal/output"
	"github.com/usuario/repokit/internal/store"
)

var tagCmd = &cobra.Command{
	Use:   "tag",
	Short: "Manage component tags",
	Long: `Add, remove, or list tags on components.

Tags help you organize and track components (e.g., "needs-refactor",
"deprecated", "wip", "reviewed").

Examples:
  repokit tag add 22 needs-refactor
  repokit tag add 22 deprecated
  repokit tag remove 22 needs-refactor
  repokit tag list
  repokit tag list --filter needs-refactor`,
}

var tagAddCmd = &cobra.Command{
	Use:   "add <component-id> <tag>",
	Short: "Add a tag to a component",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		id, err := strconv.ParseInt(args[0], 10, 64)
		if err != nil {
			output.PrintError("Invalid component ID: %s", args[0])
			return err
		}
		tag := args[1]

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

		// Verify component exists
		comp, err := s.GetComponentByID(ctx, id)
		if err != nil {
			output.PrintError("Component #%d not found", id)
			output.PrintHint("repokit find component \"\" to list all components")
			return err
		}

		if err := s.TagComponent(ctx, id, tag, 1.0, "manual"); err != nil {
			output.PrintError("Failed to add tag: %v", err)
			return err
		}

		fmt.Println()
		output.PrintSuccess("Tagged %s with '%s'", output.Cyan(comp.Name), output.Green(tag))
		fmt.Println()
		return nil
	},
}

var tagRemoveCmd = &cobra.Command{
	Use:   "remove <component-id> <tag>",
	Short: "Remove a tag from a component",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		id, err := strconv.ParseInt(args[0], 10, 64)
		if err != nil {
			output.PrintError("Invalid component ID: %s", args[0])
			return err
		}
		tag := args[1]

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

		_, err = s.DB().ExecContext(ctx, `
			DELETE FROM component_tags
			WHERE component_id = ?
			AND tag_id = (SELECT id FROM tags WHERE name = ?)
		`, id, tag)
		if err != nil {
			output.PrintError("Failed to remove tag: %v", err)
			return err
		}

		fmt.Println()
		output.PrintSuccess("Removed tag '%s' from component #%d", tag, id)
		fmt.Println()
		return nil
	},
}

var tagFilter string

var tagListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all tagged components",
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

		query := `
			SELECT c.id, c.name, f.path, t.name, ct.source
			FROM component_tags ct
			JOIN components c ON ct.component_id = c.id
			JOIN files f ON c.file_id = f.id
			JOIN tags t ON ct.tag_id = t.id
		`
		qArgs := []interface{}{}

		if tagFilter != "" {
			query += " WHERE t.name = ?"
			qArgs = append(qArgs, tagFilter)
		}

		query += " ORDER BY t.name, c.name"

		rows, err := s.DB().QueryContext(ctx, query, qArgs...)
		if err != nil {
			output.PrintError("Query failed: %v", err)
			return err
		}
		defer rows.Close()

		table := &output.Table{
			Headers: []string{"ID", "Component", "Path", "Tag", "Source"},
		}
		for rows.Next() {
			var id int64
			var name, path, tag, source string
			rows.Scan(&id, &name, &path, &tag, &source)

			tagDisplay := output.Green(tag)
			if source == "manual" {
				tagDisplay = output.Yellow(tag)
			}

			table.Rows = append(table.Rows, []string{
				fmt.Sprintf("%d", id),
				name,
				path,
				tagDisplay,
				output.Dim(source),
			})
		}

		fmt.Println()
		if len(table.Rows) == 0 {
			output.PrintWarning("No tags found")
			output.PrintHint("repokit tag add <id> <tag> to add a tag")
		} else {
			table.Print()
			fmt.Println()
			output.PrintSuccess("%d tags found", len(table.Rows))
		}
		fmt.Println()
		return nil
	},
}

func init() {
	tagListCmd.Flags().StringVar(&tagFilter, "filter", "", "Filter by tag name")

	tagCmd.AddCommand(tagAddCmd)
	tagCmd.AddCommand(tagRemoveCmd)
	tagCmd.AddCommand(tagListCmd)
	rootCmd.AddCommand(tagCmd)
}
