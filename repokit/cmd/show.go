package cmd

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"

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

		fmt.Println()
		fmt.Println(output.Banner("COMPONENT "+comp.Name, fmt.Sprintf("#%d", comp.ID)))
		fmt.Println()

		// Identity box
		idLines := []string{
			fmt.Sprintf(" %-13s %s", output.Dim("Kind:"), output.Cyan(string(comp.Kind))),
			fmt.Sprintf(" %-13s %s", output.Dim("File:"), comp.FilePath),
			fmt.Sprintf(" %-13s %d - %d", output.Dim("Lines:"), comp.LineStart, comp.LineEnd),
			fmt.Sprintf(" %-13s %s", output.Dim("Export:"), comp.ExportType),
			fmt.Sprintf(" %-13s %d", output.Dim("Usage count:"), comp.UsageCount),
		}
		if len(comp.Tags) > 0 {
			var tagStrs []string
			for _, t := range comp.Tags {
				tagStrs = append(tagStrs, output.Green(t.Name))
			}
			idLines = append(idLines, fmt.Sprintf(" %-13s %s", output.Dim("Tags:"), strings.Join(tagStrs, ", ")))
		}
		fmt.Println(output.Box("Identity", idLines))

		// Props box
		propsName := comp.Name + "Props"
		var tdID int64
		var tdBody string
		err = s.DB().QueryRowContext(ctx,
			"SELECT id, body FROM type_definitions WHERE name = ? AND file_id = ?",
			propsName, comp.FileID,
		).Scan(&tdID, &tdBody)
		if err == nil && tdBody != "" {
			var propLines []string
			propRows, err := s.DB().QueryContext(ctx,
				"SELECT name, type_annotation, is_optional FROM props WHERE type_def_id = ?", tdID)
			if err == nil {
				for propRows.Next() {
					var name, typeAnn string
					var isOptional bool
					if propRows.Scan(&name, &typeAnn, &isOptional) == nil {
						opt := " "
						if isOptional {
							opt = output.Yellow("?")
						}
						if len(typeAnn) > 38 {
							typeAnn = typeAnn[:35] + "..."
						}
						propLines = append(propLines, fmt.Sprintf(" %-22s%s %s", name, opt, output.Dim(typeAnn)))
					}
				}
				propRows.Close()
			}
			if len(propLines) > 0 {
				fmt.Println()
				fmt.Println(output.Box("Props ("+propsName+")", propLines))
			}
		}

		// Imports box
		importRows, err := s.DB().QueryContext(ctx,
			"SELECT source_path, imported_names, is_external FROM imports WHERE file_id = ? ORDER BY is_external, source_path",
			comp.FileID)
		if err == nil {
			var impLines []string
			for importRows.Next() {
				var source, names string
				var isExternal bool
				if importRows.Scan(&source, &names, &isExternal) == nil {
					tag := output.Dim("local")
					if isExternal {
						tag = output.Dim("pkg  ")
					}
					if len(source) > 32 {
						source = source[:29] + "..."
					}
					if len(names) > 28 {
						names = names[:25] + "..."
					}
					impLines = append(impLines, fmt.Sprintf(" %s  %-32s %s",
						tag, output.Cyan(source), output.Dim(names)))
				}
			}
			importRows.Close()
			if len(impLines) > 0 {
				fmt.Println()
				fmt.Println(output.Box("Imports", impLines))
			}
		}

		// Source preview (outside box — lines can be wider than 70)
		if comp.RawSource != "" {
			fmt.Println()
			fmt.Printf("  %s\n", output.Bold("Source preview:"))
			fmt.Println(output.Dim("  " + strings.Repeat("─", 70)))
			lines := strings.Split(comp.RawSource, "\n")
			maxLines := 25
			if len(lines) > maxLines {
				for i := 0; i < maxLines; i++ {
					fmt.Printf("  %s  %s\n", output.Dim(fmt.Sprintf("%4d", comp.LineStart+i)), lines[i])
				}
				fmt.Printf("  %s\n", output.Dim(fmt.Sprintf("       ... +%d more lines", len(lines)-maxLines)))
			} else {
				for i, line := range lines {
					fmt.Printf("  %s  %s\n", output.Dim(fmt.Sprintf("%4d", comp.LineStart+i)), line)
				}
			}
			fmt.Println(output.Dim("  " + strings.Repeat("─", 70)))
		}

		fmt.Println()
		return nil
	},
}

func init() {
	rootCmd.AddCommand(showCmd)
}
