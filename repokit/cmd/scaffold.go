package cmd

import (
	"context"
	"fmt"
	"os"

	"github.com/fatih/color"
	"github.com/spf13/cobra"
	"github.com/usuario/repokit/internal/config"
	"github.com/usuario/repokit/internal/output"
	"github.com/usuario/repokit/internal/scaffold"
	"github.com/usuario/repokit/internal/store"
)

var (
	scaffoldBasedOn int64
	scaffoldDir     string
	scaffoldWrite   bool
)

var scaffoldCmd = &cobra.Command{
	Use:   "scaffold",
	Short: "Generate code based on existing patterns",
}

var scaffoldComponentCmd = &cobra.Command{
	Use:   "component <Name>",
	Short: "Scaffold a new component based on an existing one",
	Long: `Generate a new component following the conventions of an existing component.

The generated code will match the base component's:
  - export style (default/named)
  - function style (declaration/arrow)
  - props pattern (interface/type)
  - hook usage patterns
  - styling approach
  - directory structure

Examples:
  repokit scaffold component CustomerModal --based 5
  repokit scaffold component UserProfile --based 3 --write
  repokit scaffold component DataGrid --based 8 --dir src/components/tables`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		newName := args[0]

		if scaffoldBasedOn == 0 {
			output.PrintError("--based flag is required (component ID to use as template)")
			output.PrintInfo("Use 'repokit find component' to find component IDs")
			return fmt.Errorf("missing --based flag")
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

		req := scaffold.ScaffoldRequest{
			NewName:   newName,
			BasedOnID: scaffoldBasedOn,
			TargetDir: scaffoldDir,
			Write:     scaffoldWrite,
			RepoRoot:  repoRoot,
		}

		result, err := scaffold.Scaffold(ctx, s, req)
		if err != nil {
			output.PrintError("Scaffold failed: %v", err)
			return err
		}

		if result.Written {
			output.PrintSuccess("Created %s", result.FilePath)
		} else {
			// Dry-run: show the generated content
			fmt.Println()
			output.PrintInfo("Generated component (dry-run):")
			output.PrintInfo("Target: %s", result.FilePath)
			fmt.Println()
			fmt.Println(color.New(color.FgHiBlack).Sprint("--- begin generated code ---"))
			fmt.Println(result.Content)
			fmt.Println(color.New(color.FgHiBlack).Sprint("--- end generated code ---"))
			fmt.Println()
			output.PrintHint("add --write to create the file on disk")
		}

		return nil
	},
}

func init() {
	scaffoldComponentCmd.Flags().Int64Var(&scaffoldBasedOn, "based", 0, "Component ID to use as template (required)")
	scaffoldComponentCmd.Flags().StringVar(&scaffoldDir, "dir", "", "Override target directory")
	scaffoldComponentCmd.Flags().BoolVar(&scaffoldWrite, "write", false, "Write the file to disk (default: dry-run)")

	scaffoldCmd.AddCommand(scaffoldComponentCmd)
	rootCmd.AddCommand(scaffoldCmd)
}
