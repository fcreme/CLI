package cmd

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"

	"github.com/spf13/cobra"
	"github.com/fcreme/CLI/repokit/internal/config"
	"github.com/fcreme/CLI/repokit/internal/output"
	"github.com/fcreme/CLI/repokit/internal/store"
)

var openEditor string

var openCmd = &cobra.Command{
	Use:   "open <component-id>",
	Short: "Open a component in your editor",
	Long: `Open a component's source file in your editor at the correct line.

Defaults to VS Code (code). Use --editor to specify a different editor.

Examples:
  repokit open 22
  repokit open 22 --editor cursor
  repokit open 22 --editor vim`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		id, err := strconv.ParseInt(args[0], 10, 64)
		if err != nil {
			output.PrintError("Invalid component ID: %s", args[0])
			output.PrintHint("repokit find component \"\" to list all components")
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
			output.PrintHint("repokit find component \"\" to list all components")
			fmt.Println()
			return err
		}

		absPath := filepath.Join(repoRoot, comp.FilePath)

		// Determine editor
		editor := openEditor
		if editor == "" {
			// Try common editors
			editors := []string{"code", "cursor", "vim", "nano", "notepad"}
			for _, e := range editors {
				if _, err := exec.LookPath(e); err == nil {
					editor = e
					break
				}
			}
		}

		if editor == "" {
			output.PrintError("No editor found. Use --editor to specify one")
			return fmt.Errorf("no editor")
		}

		// Build command based on editor
		var editorCmd *exec.Cmd
		switch editor {
		case "code", "cursor":
			// VS Code / Cursor: code --goto file:line
			editorCmd = exec.Command(editor, "--goto", fmt.Sprintf("%s:%d", absPath, comp.LineStart))
		case "vim", "nvim":
			editorCmd = exec.Command(editor, fmt.Sprintf("+%d", comp.LineStart), absPath)
		case "nano":
			editorCmd = exec.Command(editor, fmt.Sprintf("+%d", comp.LineStart), absPath)
		default:
			editorCmd = exec.Command(editor, absPath)
		}

		editorCmd.Stdin = os.Stdin
		editorCmd.Stdout = os.Stdout
		editorCmd.Stderr = os.Stderr

		fmt.Println()
		output.PrintInfo("Opening %s at line %d in %s...", comp.Name, comp.LineStart, editor)

		if err := editorCmd.Start(); err != nil {
			output.PrintError("Failed to open editor: %v", err)
			output.PrintHint("repokit open %d --editor <editor-name>", id)
			return err
		}

		output.PrintSuccess("Opened %s (%s:%d)", comp.Name, comp.FilePath, comp.LineStart)
		fmt.Println()
		return nil
	},
}

func init() {
	openCmd.Flags().StringVar(&openEditor, "editor", "", "Editor to use (default: auto-detect)")
	rootCmd.AddCommand(openCmd)
}
