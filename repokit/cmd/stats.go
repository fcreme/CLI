package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"
	"github.com/usuario/repokit/internal/config"
	"github.com/usuario/repokit/internal/output"
	"github.com/usuario/repokit/internal/store"
)

var statsCmd = &cobra.Command{
	Use:   "stats",
	Short: "Show codebase analytics",
	Long: `Display detailed analytics about the indexed codebase.

Shows:
  - Lines of code per folder
  - Biggest components
  - Most complex files (by import count)
  - Component kind breakdown
  - Import/export ratios

Examples:
  repokit stats`,
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

		// Basic counts
		var totalFiles, totalComponents, totalHooks, totalImports, totalExports, totalTypes int
		s.DB().QueryRowContext(ctx, "SELECT COUNT(*) FROM files").Scan(&totalFiles)
		s.DB().QueryRowContext(ctx, "SELECT COUNT(*) FROM components WHERE kind != 'hook'").Scan(&totalComponents)
		s.DB().QueryRowContext(ctx, "SELECT COUNT(*) FROM components WHERE kind = 'hook'").Scan(&totalHooks)
		s.DB().QueryRowContext(ctx, "SELECT COUNT(*) FROM imports").Scan(&totalImports)
		s.DB().QueryRowContext(ctx, "SELECT COUNT(*) FROM exports").Scan(&totalExports)
		s.DB().QueryRowContext(ctx, "SELECT COUNT(*) FROM type_definitions").Scan(&totalTypes)

		if totalFiles == 0 {
			fmt.Println()
			output.PrintWarning("No files indexed yet")
			output.PrintHint("repokit index")
			fmt.Println()
			return nil
		}

		fmt.Println()
		fmt.Printf("  %s\n", output.Bold("Codebase Analytics"))
		fmt.Println()

		// Overview
		fmt.Printf("  %s\n", output.Bold("Overview:"))
		fmt.Printf("    Files:            %s\n", output.Cyan(fmt.Sprintf("%d", totalFiles)))
		fmt.Printf("    Components:       %s\n", output.Cyan(fmt.Sprintf("%d", totalComponents)))
		fmt.Printf("    Hooks:            %s\n", output.Cyan(fmt.Sprintf("%d", totalHooks)))
		fmt.Printf("    Type definitions: %s\n", output.Cyan(fmt.Sprintf("%d", totalTypes)))
		fmt.Printf("    Imports:          %s\n", output.Cyan(fmt.Sprintf("%d", totalImports)))
		fmt.Printf("    Exports:          %s\n", output.Cyan(fmt.Sprintf("%d", totalExports)))

		// Files per folder
		fmt.Println()
		fmt.Printf("  %s\n", output.Bold("Files per Folder:"))
		fmt.Println()
		folderCounts := make(map[string]int)
		rows, err := s.DB().QueryContext(ctx, "SELECT path FROM files")
		if err == nil {
			defer rows.Close()
			for rows.Next() {
				var path string
				rows.Scan(&path)
				dir := filepath.Dir(path)
				if dir == "." {
					dir = "(root)"
				}
				folderCounts[dir]++
			}
		}

		type folderStat struct {
			folder string
			count  int
		}
		var folders []folderStat
		for f, c := range folderCounts {
			folders = append(folders, folderStat{f, c})
		}
		sort.Slice(folders, func(i, j int) bool {
			return folders[i].count > folders[j].count
		})
		maxFolders := 10
		if len(folders) < maxFolders {
			maxFolders = len(folders)
		}
		for _, f := range folders[:maxFolders] {
			bar := strings.Repeat("█", f.count)
			fmt.Printf("    %s %s %s\n",
				output.Cyan(fmt.Sprintf("%3d", f.count)),
				output.Green(bar),
				f.folder,
			)
		}

		// Biggest components (by line count)
		fmt.Println()
		fmt.Printf("  %s\n", output.Bold("Biggest Components:"))
		fmt.Println()
		bigRows, err := s.DB().QueryContext(ctx, `
			SELECT c.name, f.path, (c.line_end - c.line_start) as lines
			FROM components c JOIN files f ON c.file_id = f.id
			ORDER BY lines DESC
			LIMIT 8
		`)
		if err == nil {
			defer bigRows.Close()
			for bigRows.Next() {
				var name, path string
				var lines int
				bigRows.Scan(&name, &path, &lines)
				barLen := lines / 20
				if barLen > 30 {
					barLen = 30
				}
				if barLen < 1 {
					barLen = 1
				}
				bar := strings.Repeat("█", barLen)
				colorFn := output.Green
				if lines > 300 {
					colorFn = output.Red
				} else if lines > 150 {
					colorFn = output.Yellow
				}
				fmt.Printf("    %s %s %s %s\n",
					output.Cyan(fmt.Sprintf("%4d", lines)),
					colorFn(bar),
					name,
					output.Dim(path),
				)
			}
		}

		// Most complex files (by import count)
		fmt.Println()
		fmt.Printf("  %s\n", output.Bold("Most Complex Files (by imports):"))
		fmt.Println()
		complexRows, err := s.DB().QueryContext(ctx, `
			SELECT f.path, COUNT(i.id) as import_count
			FROM files f
			JOIN imports i ON i.file_id = f.id
			GROUP BY f.id
			ORDER BY import_count DESC
			LIMIT 8
		`)
		if err == nil {
			defer complexRows.Close()
			for complexRows.Next() {
				var path string
				var count int
				complexRows.Scan(&path, &count)
				bar := strings.Repeat("█", count)
				colorFn := output.Green
				if count > 15 {
					colorFn = output.Red
				} else if count > 10 {
					colorFn = output.Yellow
				}
				fmt.Printf("    %s %s %s\n",
					output.Cyan(fmt.Sprintf("%3d", count)),
					colorFn(bar),
					path,
				)
			}
		}

		// Component kind breakdown
		fmt.Println()
		fmt.Printf("  %s\n", output.Bold("Component Types:"))
		fmt.Println()
		kindRows, err := s.DB().QueryContext(ctx, `
			SELECT kind, COUNT(*) as cnt
			FROM components
			GROUP BY kind
			ORDER BY cnt DESC
		`)
		if err == nil {
			defer kindRows.Close()
			for kindRows.Next() {
				var kind string
				var count int
				kindRows.Scan(&kind, &count)
				bar := strings.Repeat("█", count)
				fmt.Printf("    %s %s %s\n",
					output.Cyan(fmt.Sprintf("%3d", count)),
					output.Green(bar),
					kind,
				)
			}
		}

		// External vs internal imports
		fmt.Println()
		fmt.Printf("  %s\n", output.Bold("Import Breakdown:"))
		var externalImports, internalImports int
		s.DB().QueryRowContext(ctx, "SELECT COUNT(*) FROM imports WHERE is_external = 1").Scan(&externalImports)
		s.DB().QueryRowContext(ctx, "SELECT COUNT(*) FROM imports WHERE is_external = 0").Scan(&internalImports)
		fmt.Printf("    External (npm):  %s\n", output.Cyan(fmt.Sprintf("%d", externalImports)))
		fmt.Printf("    Internal:        %s\n", output.Cyan(fmt.Sprintf("%d", internalImports)))
		if totalImports > 0 {
			ratio := float64(internalImports) / float64(totalImports) * 100
			fmt.Printf("    Internal ratio:  %s\n", output.Cyan(fmt.Sprintf("%.0f%%", ratio)))
		}

		fmt.Println()
		output.PrintHint("repokit health for project health score")
		output.PrintHint("repokit deps for dependency analysis")
		fmt.Println()

		return nil
	},
}

func init() {
	rootCmd.AddCommand(statsCmd)
}
