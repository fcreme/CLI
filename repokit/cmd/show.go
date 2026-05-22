package cmd

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/fatih/color"
	"github.com/spf13/cobra"
	"github.com/fcreme/CLI/repokit/internal/config"
	"github.com/fcreme/CLI/repokit/internal/output"
	"github.com/fcreme/CLI/repokit/internal/store"
)

var showCmd = &cobra.Command{
	Use:     "show <component-id>",
	Aliases: []string{"s"},
	Short:   "Show detailed information about a component",
	Long: `Display full details for a component including source code,
props, tags, imports, and usage information.

Example:
  repokit show 3
  repokit s 3`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		id, err := strconv.ParseInt(args[0], 10, 64)
		if err != nil {
			output.PrintError("Invalid component ID: %s", args[0])
			output.PrintHint("repokit find component \"\" to list all components with their IDs")
			return err
		}

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

		comp, err := s.GetComponentByID(ctx, id)
		if err != nil {
			fmt.Println()
			output.PrintError("Component #%d not found", id)
			output.PrintHint("repokit find component \"\" to list all components with their IDs")
			fmt.Println()
			return fmt.Errorf("component not found")
		}

		dim := color.New(color.Faint).SprintFunc()
		bold := color.New(color.Bold).SprintFunc()
		cyan := color.New(color.FgCyan).SprintFunc()
		green := color.New(color.FgGreen).SprintFunc()
		yellow := color.New(color.FgYellow).SprintFunc()

		fmt.Println()
		fmt.Printf("  %s %s\n", bold("Component:"), cyan(comp.Name))
		fmt.Printf("  %s %s\n", bold("Kind:"), string(comp.Kind))
		fmt.Printf("  %s %s\n", bold("File:"), comp.FilePath)
		fmt.Printf("  %s %d-%d\n", bold("Lines:"), comp.LineStart, comp.LineEnd)
		fmt.Printf("  %s %s\n", bold("Export:"), comp.ExportType)
		fmt.Printf("  %s %d\n", bold("Usage count:"), comp.UsageCount)

		// Tags
		if len(comp.Tags) > 0 {
			var tagStrs []string
			for _, t := range comp.Tags {
				tagStrs = append(tagStrs, green(t.Name))
			}
			fmt.Printf("  %s %s\n", bold("Tags:"), strings.Join(tagStrs, ", "))
		}

		// Props - query from the type_definitions + props tables
		propsName := comp.Name + "Props"
		var tdID int64
		var tdBody string
		err = s.DB().QueryRowContext(ctx,
			"SELECT id, body FROM type_definitions WHERE name = ? AND file_id = ?",
			propsName, comp.FileID,
		).Scan(&tdID, &tdBody)
		if err == nil && tdBody != "" {
			fmt.Println()
			fmt.Printf("  %s\n", bold("Props ("+propsName+"):"))

			// Query individual props
			propRows, err := s.DB().QueryContext(ctx,
				"SELECT name, type_annotation, is_optional FROM props WHERE type_def_id = ?", tdID)
			if err == nil {
				defer propRows.Close()
				for propRows.Next() {
					var name, typeAnn string
					var isOptional bool
					if err := propRows.Scan(&name, &typeAnn, &isOptional); err == nil {
						opt := ""
						if isOptional {
							opt = yellow("?")
						}
						fmt.Printf("    %s%s: %s\n", name, opt, dim(typeAnn))
					}
				}
			}
		}

		// Imports for this file
		importRows, err := s.DB().QueryContext(ctx,
			"SELECT source_path, imported_names, is_external FROM imports WHERE file_id = ? ORDER BY is_external, source_path",
			comp.FileID)
		if err == nil {
			defer importRows.Close()
			fmt.Println()
			fmt.Printf("  %s\n", bold("Imports:"))
			for importRows.Next() {
				var source, names string
				var isExternal bool
				if err := importRows.Scan(&source, &names, &isExternal); err == nil {
					prefix := dim("  local")
					if isExternal {
						prefix = dim("  pkg  ")
					}
					fmt.Printf("    %s  %s  %s\n", prefix, cyan(source), dim(names))
				}
			}
		}

		// Source code preview
		if comp.RawSource != "" {
			fmt.Println()
			fmt.Printf("  %s\n", bold("Source preview:"))
			fmt.Println(dim("  " + strings.Repeat("-", 60)))
			lines := strings.Split(comp.RawSource, "\n")
			maxLines := 25
			if len(lines) > maxLines {
				for i := 0; i < maxLines; i++ {
					fmt.Printf("  %s %s\n", dim(fmt.Sprintf("%3d", comp.LineStart+i)), lines[i])
				}
				fmt.Printf("  %s\n", dim(fmt.Sprintf("  ... +%d more lines", len(lines)-maxLines)))
			} else {
				for i, line := range lines {
					fmt.Printf("  %s %s\n", dim(fmt.Sprintf("%3d", comp.LineStart+i)), line)
				}
			}
			fmt.Println(dim("  " + strings.Repeat("-", 60)))
		}

		fmt.Println()
		return nil
	},
}

func init() {
	rootCmd.AddCommand(showCmd)
}
