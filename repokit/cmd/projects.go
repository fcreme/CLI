package cmd

import (
	"fmt"
	"strconv"

	"github.com/spf13/cobra"
	"github.com/usuario/repokit/internal/config"
	"github.com/usuario/repokit/internal/output"
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

		if len(reg.Projects) == 0 {
			fmt.Println()
			output.PrintWarning("No projects connected yet")
			output.PrintHint("repokit connect <path> to connect a project")
			fmt.Println()
			return
		}

		fmt.Println()
		fmt.Printf("  %s\n", output.Bold("Connected Projects:"))
		fmt.Println()
		for i, p := range reg.Projects {
			marker := "  "
			name := p.Name
			if p.Path == active {
				marker = output.Green("->")
				name = output.Green(p.Name)
			}
			valid := output.Green("ok")
			if !config.Exists(p.Path) {
				valid = output.Red("missing")
			}
			fmt.Printf("  %s %s  %-20s  %s  %s\n",
				marker,
				output.Dim(fmt.Sprintf("[%d]", i+1)),
				name,
				output.Dim(p.Path),
				valid,
			)
		}
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
