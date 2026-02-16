package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/usuario/repokit/internal/config"
	"github.com/usuario/repokit/internal/connect"
	"github.com/usuario/repokit/internal/output"
)

var connectCmd = &cobra.Command{
	Use:   "connect <path|git-url>",
	Short: "Connect to a repository",
	Long: `Connect repokit to a code repository.

Accepts a local directory path or a git URL. When given a git URL,
the repository is cloned to the current directory.

Creates a .repokit/ directory in the repository root to store
configuration and index data.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		target := args[0]

		repoPath, err := connect.Connect(target)
		if err != nil {
			output.PrintError("Failed to connect: %v", err)
			return err
		}

		// Save as global active project so commands work from any directory
		config.SetActiveProject(repoPath)

		output.PrintSuccess("Connected to repository: %s", repoPath)
		fmt.Println()
		output.PrintHint("repokit index")
		output.PrintHint("repokit stack")
		return nil
	},
}

func init() {
	rootCmd.AddCommand(connectCmd)
}
