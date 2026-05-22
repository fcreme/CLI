package cmd

import (
	"fmt"
	"os"

	"github.com/fatih/color"
	"github.com/spf13/cobra"
	"github.com/fcreme/CLI/repokit/internal/config"
	"github.com/fcreme/CLI/repokit/internal/output"
	"github.com/fcreme/CLI/repokit/internal/suggest"
)

var (
	suggestFile  string
	suggestApply bool
)

var suggestCmd = &cobra.Command{
	Use:   `suggest "<description>"`,
	Short: "Suggest a code modification as a unified diff",
	Long: `Generate a code modification based on a natural language description.

Produces a unified diff showing the proposed changes. By default runs
in dry-run mode (shows the diff without modifying files).

Supported modifications:
  - Add a prop to a component
  - Add a state variable (useState)
  - Add an import statement
  - Add a useEffect hook
  - Wrap JSX with a provider

Safety:
  - Dry-run by default (use --apply to write changes)
  - Automatic backup before modification
  - Idempotency checks (won't duplicate existing code)

Examples:
  repokit suggest "add onClick prop" --file src/components/Button.tsx
  repokit suggest "add loading state" --file src/components/UserCard.tsx
  repokit suggest "add import { motion } from 'framer-motion'" --file src/App.tsx
  repokit suggest "add useEffect" --file src/components/Dashboard.tsx --apply`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		description := args[0]

		if suggestFile == "" {
			output.PrintError("--file flag is required")
			return fmt.Errorf("missing --file flag")
		}

		cwd, _ := os.Getwd()
		repoRoot, err := config.FindRepoRoot(cwd)
		if err != nil {
			output.PrintError("%v", err)
			return err
		}

		req := suggest.SuggestRequest{
			Description: description,
			FilePath:    suggestFile,
			RepoRoot:    repoRoot,
			Apply:       suggestApply,
		}

		result, err := suggest.Suggest(req)
		if err != nil {
			output.PrintError("Suggest failed: %v", err)
			return err
		}

		// Print the diff
		fmt.Println()
		output.PrintInfo("Strategy: %s", result.Strategy)
		output.PrintInfo("File: %s", result.FilePath)
		fmt.Println()

		// Colorize the diff output
		printColorDiff(result.UnifiedDiff)

		if result.Applied {
			fmt.Println()
			output.PrintSuccess("Changes applied to %s", result.FilePath)
			output.PrintInfo("Backup saved to %s", result.BackupPath)
		} else {
			fmt.Println()
			output.PrintHint("add --apply to write changes to disk")
		}

		return nil
	},
}

func printColorDiff(diff string) {
	red := color.New(color.FgRed)
	green := color.New(color.FgGreen)
	cyan := color.New(color.FgCyan)
	bold := color.New(color.Bold)

	for _, line := range splitLines(diff) {
		switch {
		case len(line) > 0 && line[0] == '+' && !hasPrefix(line, "+++"):
			green.Println(line)
		case len(line) > 0 && line[0] == '-' && !hasPrefix(line, "---"):
			red.Println(line)
		case hasPrefix(line, "@@"):
			cyan.Println(line)
		case hasPrefix(line, "---") || hasPrefix(line, "+++"):
			bold.Println(line)
		default:
			fmt.Println(line)
		}
	}
}

func splitLines(s string) []string {
	var lines []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			lines = append(lines, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		lines = append(lines, s[start:])
	}
	return lines
}

func hasPrefix(s, prefix string) bool {
	return len(s) >= len(prefix) && s[:len(prefix)] == prefix
}

func init() {
	suggestCmd.Flags().StringVar(&suggestFile, "file", "", "Target file (repo-relative path, required)")
	suggestCmd.Flags().BoolVar(&suggestApply, "apply", false, "Apply the changes (default: dry-run)")

	rootCmd.AddCommand(suggestCmd)
}
