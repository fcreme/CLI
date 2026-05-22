package cmd

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/spf13/cobra"
	"github.com/fcreme/CLI/repokit/internal/output"
)

var (
	Version = "0.2.0"
	jsonOut bool
)

var rootCmd = &cobra.Command{
	Use:   "repokit",
	Short: "Repo-aware CLI developer assistant",
	Long:  "repokit - repo-aware developer assistant",
	PersistentPreRun: func(cmd *cobra.Command, args []string) {
		output.JSONMode = jsonOut
	},
	PersistentPostRun: func(cmd *cobra.Command, args []string) {
		// Don't show footer on the root command (it already shows the full help)
		if cmd.Name() != "repokit" {
			output.PrintFooter()
		}
	},
	Run: func(cmd *cobra.Command, args []string) {
		printWelcome()
	},
}

func printWelcome() {
	fmt.Println()
	fmt.Printf("  %s %s\n", output.Bold("repokit"), output.Dim("v"+Version))
	fmt.Printf("  %s\n", output.Dim("repo-aware developer assistant"))
	fmt.Println()

	fmt.Printf("  %s\n", output.Bold("Quick Start:"))
	fmt.Printf("    %-25s %s\n", output.Cyan("analyze"), output.Bold("Full one-shot analysis of cwd")+output.Dim(" (alias: a)"))
	fmt.Println()

	fmt.Printf("  %s\n", output.Bold("Getting Started:"))
	fmt.Printf("    %-25s %s\n", output.Cyan("connect <path>"), "Connect to a repository")
	fmt.Printf("    %-25s %s\n", output.Cyan("index"), "Index the repository")
	fmt.Printf("    %-25s %s\n", output.Cyan("stack"), "Detect tech stack")
	fmt.Printf("    %-25s %s\n", output.Cyan("status"), "Dashboard overview "+output.Dim("(alias: st)"))
	fmt.Println()

	fmt.Printf("  %s\n", output.Bold("Explore:"))
	fmt.Printf("    %-25s %s\n", output.Cyan("find component <q>"), "Search components "+output.Dim("(alias: f)"))
	fmt.Printf("    %-25s %s\n", output.Cyan("show <id>"), "Component details "+output.Dim("(alias: s)"))
	fmt.Printf("    %-25s %s\n", output.Cyan("deps"), "Dependency graph "+output.Dim("(alias: d)"))
	fmt.Printf("    %-25s %s\n", output.Cyan("lint"), "Code quality checks "+output.Dim("(alias: l)"))
	fmt.Printf("    %-25s %s\n", output.Cyan("patterns"), "Project conventions")
	fmt.Println()

	fmt.Printf("  %s\n", output.Bold("Analyze:"))
	fmt.Printf("    %-25s %s\n", output.Cyan("health"), "Project health score "+output.Dim("(alias: h)"))
	fmt.Printf("    %-25s %s\n", output.Cyan("stats"), "Codebase analytics")
	fmt.Printf("    %-25s %s\n", output.Cyan("duplicates"), "Find similar components "+output.Dim("(alias: dup)"))
	fmt.Printf("    %-25s %s\n", output.Cyan("diff [base]"), "Compare branch against base")
	fmt.Printf("    %-25s %s\n", output.Cyan("check"), "CI/CD quality gate")
	fmt.Println()

	fmt.Printf("  %s\n", output.Bold("Generate:"))
	fmt.Printf("    %-25s %s\n", output.Cyan("scaffold component"), "Generate from conventions")
	fmt.Printf("    %-25s %s\n", output.Cyan("suggest"), "Suggest code modifications")
	fmt.Println()

	fmt.Printf("  %s\n", output.Bold("Manage:"))
	fmt.Printf("    %-25s %s\n", output.Cyan("tag add/remove/list"), "Manage component tags")
	fmt.Printf("    %-25s %s\n", output.Cyan("open <id>"), "Open component in editor")
	fmt.Printf("    %-25s %s\n", output.Cyan("export --html"), "Export project report")
	fmt.Println()

	fmt.Printf("  %s\n", output.Bold("Projects:"))
	fmt.Printf("    %-25s %s\n", output.Cyan("projects"), "List connected projects "+output.Dim("(alias: p)"))
	fmt.Printf("    %-25s %s\n", output.Cyan("switch <n>"), "Switch active project")
	fmt.Println()

	fmt.Printf("  %s\n", output.Bold("Interactive:"))
	fmt.Printf("    %-25s %s\n", output.Cyan("chat"), "Natural language REPL")
	fmt.Println()

	fmt.Printf("  Run %s for details on any command.\n", output.Dim("repokit help <command>"))
	fmt.Println()
}

// allCommandNames returns all registered command names + aliases.
func allCommandNames() []string {
	var names []string
	for _, cmd := range rootCmd.Commands() {
		names = append(names, cmd.Name())
		names = append(names, cmd.Aliases...)
	}
	return names
}

// levenshtein computes the edit distance between two strings.
func levenshtein(a, b string) int {
	la, lb := len(a), len(b)
	if la == 0 {
		return lb
	}
	if lb == 0 {
		return la
	}

	matrix := make([][]int, la+1)
	for i := range matrix {
		matrix[i] = make([]int, lb+1)
		matrix[i][0] = i
	}
	for j := 0; j <= lb; j++ {
		matrix[0][j] = j
	}

	for i := 1; i <= la; i++ {
		for j := 1; j <= lb; j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			matrix[i][j] = min(
				matrix[i-1][j]+1,
				matrix[i][j-1]+1,
				matrix[i-1][j-1]+cost,
			)
		}
	}
	return matrix[la][lb]
}

func min(vals ...int) int {
	m := vals[0]
	for _, v := range vals[1:] {
		if v < m {
			m = v
		}
	}
	return m
}

// suggestCommand finds the closest matching command for a typo.
func suggestCommand(unknown string) string {
	names := allCommandNames()
	type candidate struct {
		name string
		dist int
	}
	var candidates []candidate
	for _, name := range names {
		d := levenshtein(strings.ToLower(unknown), strings.ToLower(name))
		if d <= 3 { // max 3 edits
			candidates = append(candidates, candidate{name, d})
		}
	}
	if len(candidates) == 0 {
		return ""
	}
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].dist < candidates[j].dist
	})
	return candidates[0].name
}

func Execute() {
	rootCmd.SilenceErrors = true
	rootCmd.SilenceUsage = true

	if err := rootCmd.Execute(); err != nil {
		errMsg := err.Error()
		// Check for unknown command and suggest
		if strings.HasPrefix(errMsg, "unknown command") {
			parts := strings.Fields(errMsg)
			// Extract the unknown command name from the error
			for _, p := range parts {
				p = strings.Trim(p, `"`)
				if suggestion := suggestCommand(p); suggestion != "" {
					fmt.Println()
					output.PrintError("Unknown command: %s", p)
					output.PrintHint("did you mean '%s'?", suggestion)
					fmt.Println()
					os.Exit(1)
				}
			}
			fmt.Println()
			output.PrintError("%s", errMsg)
			output.PrintHint("run 'repokit' to see all available commands")
			fmt.Println()
		}
		// Other errors are already printed by the commands themselves
		os.Exit(1)
	}
}

func init() {
	rootCmd.PersistentFlags().BoolVar(&jsonOut, "json", false, "Output in JSON format")
	rootCmd.AddCommand(versionCmd)
}

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print the version number",
	Run: func(cmd *cobra.Command, args []string) {
		if jsonOut {
			output.PrintJSON(map[string]string{"version": Version})
		} else {
			fmt.Printf("repokit v%s\n", Version)
		}
	},
}
