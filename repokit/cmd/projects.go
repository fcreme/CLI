package cmd

import (
	"fmt"
	"strconv"

	"github.com/spf13/cobra"
	"github.com/fcreme/CLI/repokit/internal/config"
	"github.com/fcreme/CLI/repokit/internal/output"
)

var projectsCmd = &cobra.Command{
	Use:     "projects",
	Aliases: []string{"p"},
	Short:   "List all connected projects",
	Long: `Show all projects that have been connected with 'repokit connect'.

The active project is marked with an arrow.

Examples:
  repokit projects
  repokit p`,
	Run: func(cmd *cobra.Command, args []string) {
		reg := config.LoadProjects()
		active := config.GetActiveProject()

		fmt.Println()
		fmt.Println(output.Banner("CONNECTED PROJECTS", fmt.Sprintf("%d total", len(reg.Projects))))
		fmt.Println()

		if len(reg.Projects) == 0 {
			fmt.Println(output.Box("Empty", []string{
				" " + output.Dim("No projects connected yet."),
				" " + output.Dim("Run 'repokit connect <path>' to connect one."),
			}))
			fmt.Println()
			return
		}

		var lines []string
		for i, p := range reg.Projects {
			marker := "  "
			name := p.Name
			if p.Path == active {
				marker = output.Green("→ ")
				name = output.Green(p.Name)
			}
			status := output.Green("ok")
			if !config.Exists(p.Path) {
				status = output.Red("missing")
			}
			path := p.Path
			if len(path) > 36 {
				path = "..." + path[len(path)-33:]
			}
			lines = append(lines, fmt.Sprintf(" %s%s  %-18s %-36s %s",
				marker,
				output.Dim(fmt.Sprintf("[%d]", i+1)),
				name,
				output.Dim(path),
				status))
		}
		fmt.Println(output.Box("Projects", lines))
		fmt.Println()
		output.PrintHint("repokit switch <number> to change active project")
		fmt.Println()
	},
}

var switchCmd = &cobra.Command{
	Use:   "switch <number|name>",
	Short: "Switch active project",
	Long: `Switch the active project to a different connected project.

Use the project number from 'repokit projects' or the project name.

Examples:
  repokit switch 2
  repokit switch cryptoapp`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		target := args[0]
		reg := config.LoadProjects()

		if len(reg.Projects) == 0 {
			output.PrintWarning("No projects connected yet")
			output.PrintHint("repokit connect <path> to connect a project")
			return nil
		}

		// Try as number first
		if num, err := strconv.Atoi(target); err == nil {
			if num >= 1 && num <= len(reg.Projects) {
				project := reg.Projects[num-1]
				if !config.Exists(project.Path) {
					output.PrintError("Project at %s no longer exists", project.Path)
					return fmt.Errorf("project missing")
				}
				config.SetActiveProject(project.Path)
				fmt.Println()
				output.PrintSuccess("Switched to: %s (%s)", project.Name, project.Path)
				output.PrintHint("repokit status")
				fmt.Println()
				return nil
			}
			output.PrintError("Invalid project number: %d (have %d projects)", num, len(reg.Projects))
			output.PrintHint("repokit projects to see the list")
			return fmt.Errorf("invalid project number")
		}

		// Try as name
		for _, p := range reg.Projects {
			if p.Name == target {
				if !config.Exists(p.Path) {
					output.PrintError("Project at %s no longer exists", p.Path)
					return fmt.Errorf("project missing")
				}
				config.SetActiveProject(p.Path)
				fmt.Println()
				output.PrintSuccess("Switched to: %s (%s)", p.Name, p.Path)
				output.PrintHint("repokit status")
				fmt.Println()
				return nil
			}
		}

		fmt.Println()
		output.PrintError("Project '%s' not found", target)
		output.PrintHint("repokit projects to see all connected projects")
		fmt.Println()
		return fmt.Errorf("project not found")
	},
}

func init() {
	rootCmd.AddCommand(projectsCmd)
	rootCmd.AddCommand(switchCmd)
}
