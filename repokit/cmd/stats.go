package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"
	"github.com/fcreme/CLI/repokit/internal/config"
	"github.com/fcreme/CLI/repokit/internal/output"
	"github.com/fcreme/CLI/repokit/internal/store"
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
		fmt.Println(output.Banner("CODEBASE ANALYTICS", "repokit v"+Version))
		fmt.Println()

		// Overview box
		fmt.Println(output.Box("Overview", []string{
			fmt.Sprintf(" %-18s %s    %-18s %s",
				output.Dim("Files:"), output.Cyan(fmt.Sprintf("%d", totalFiles)),
				output.Dim("Components:"), output.Cyan(fmt.Sprintf("%d", totalComponents))),
			fmt.Sprintf(" %-18s %s    %-18s %s",
				output.Dim("Hooks:"), output.Cyan(fmt.Sprintf("%d", totalHooks)),
				output.Dim("Type definitions:"), output.Cyan(fmt.Sprintf("%d", totalTypes))),
			fmt.Sprintf(" %-18s %s    %-18s %s",
				output.Dim("Imports:"), output.Cyan(fmt.Sprintf("%d", totalImports)),
				output.Dim("Exports:"), output.Cyan(fmt.Sprintf("%d", totalExports))),
		}))

		// Files per folder
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
		maxFolderCount := 0
		for f, c := range folderCounts {
			folders = append(folders, folderStat{f, c})
			if c > maxFolderCount {
				maxFolderCount = c
			}
		}
		sort.Slice(folders, func(i, j int) bool { return folders[i].count > folders[j].count })
		maxFolders := 10
		if len(folders) < maxFolders {
			maxFolders = len(folders)
		}
		var folderLines []string
		for _, f := range folders[:maxFolders] {
			bar := scaleBar(f.count, maxFolderCount, 24)
			folderLines = append(folderLines, fmt.Sprintf(" %s  %-24s  %s",
				output.Cyan(fmt.Sprintf("%3d", f.count)),
				output.Green(bar),
				f.folder))
		}
		if len(folderLines) > 0 {
			fmt.Println()
			fmt.Println(output.Box("Files per Folder", folderLines))
		}

		// Biggest components
		var bigLines []string
		bigRows, err := s.DB().QueryContext(ctx, `
			SELECT c.name, f.path, (c.line_end - c.line_start) as lines
			FROM components c JOIN files f ON c.file_id = f.id
			ORDER BY lines DESC LIMIT 8`)
		maxLines := 1
		var bigData []struct {
			name, path string
			lines      int
		}
		if err == nil {
			defer bigRows.Close()
			for bigRows.Next() {
				var name, path string
				var lines int
				bigRows.Scan(&name, &path, &lines)
				bigData = append(bigData, struct {
					name, path string
					lines      int
				}{name, path, lines})
				if lines > maxLines {
					maxLines = lines
				}
			}
		}
		for _, d := range bigData {
			bar := scaleBar(d.lines, maxLines, 24)
			colorFn := output.Green
			if d.lines > 300 {
				colorFn = output.Red
			} else if d.lines > 150 {
				colorFn = output.Yellow
			}
			path := d.path
			if len(path) > 24 {
				path = "..." + path[len(path)-21:]
			}
			bigLines = append(bigLines, fmt.Sprintf(" %s  %-24s  %-16s %s",
				output.Cyan(fmt.Sprintf("%4d", d.lines)),
				colorFn(bar),
				d.name,
				output.Dim(path)))
		}
		if len(bigLines) > 0 {
			fmt.Println()
			fmt.Println(output.Box("Biggest Components", bigLines))
		}

		// Most complex files (by import count)
		var complexLines []string
		complexRows, err := s.DB().QueryContext(ctx, `
			SELECT f.path, COUNT(i.id) as import_count
			FROM files f JOIN imports i ON i.file_id = f.id
			GROUP BY f.id ORDER BY import_count DESC LIMIT 8`)
		var complexData []struct {
			path  string
			count int
		}
		maxComplex := 1
		if err == nil {
			defer complexRows.Close()
			for complexRows.Next() {
				var path string
				var count int
				complexRows.Scan(&path, &count)
				complexData = append(complexData, struct {
					path  string
					count int
				}{path, count})
				if count > maxComplex {
					maxComplex = count
				}
			}
		}
		for _, d := range complexData {
			bar := scaleBar(d.count, maxComplex, 24)
			colorFn := output.Green
			if d.count > 15 {
				colorFn = output.Red
			} else if d.count > 10 {
				colorFn = output.Yellow
			}
			path := d.path
			if len(path) > 36 {
				path = "..." + path[len(path)-33:]
			}
			complexLines = append(complexLines, fmt.Sprintf(" %s  %-24s  %s",
				output.Cyan(fmt.Sprintf("%3d", d.count)),
				colorFn(bar),
				path))
		}
		if len(complexLines) > 0 {
			fmt.Println()
			fmt.Println(output.Box("Most Complex Files (by imports)", complexLines))
		}

		// Component kind breakdown
		var kindLines []string
		kindRows, err := s.DB().QueryContext(ctx, `
			SELECT kind, COUNT(*) as cnt FROM components GROUP BY kind ORDER BY cnt DESC`)
		var kindData []struct {
			kind  string
			count int
		}
		maxKind := 1
		if err == nil {
			defer kindRows.Close()
			for kindRows.Next() {
				var kind string
				var count int
				kindRows.Scan(&kind, &count)
				kindData = append(kindData, struct {
					kind  string
					count int
				}{kind, count})
				if count > maxKind {
					maxKind = count
				}
			}
		}
		for _, d := range kindData {
			bar := scaleBar(d.count, maxKind, 24)
			kindLines = append(kindLines, fmt.Sprintf(" %s  %-24s  %s",
				output.Cyan(fmt.Sprintf("%3d", d.count)),
				output.Green(bar),
				d.kind))
		}
		if len(kindLines) > 0 {
			fmt.Println()
			fmt.Println(output.Box("Component Types", kindLines))
		}

		// Import breakdown
		var externalImports, internalImports int
		s.DB().QueryRowContext(ctx, "SELECT COUNT(*) FROM imports WHERE is_external = 1").Scan(&externalImports)
		s.DB().QueryRowContext(ctx, "SELECT COUNT(*) FROM imports WHERE is_external = 0").Scan(&internalImports)
		importLines := []string{
			fmt.Sprintf(" %-18s %s", output.Dim("External (npm):"), output.Cyan(fmt.Sprintf("%d", externalImports))),
			fmt.Sprintf(" %-18s %s", output.Dim("Internal:"), output.Cyan(fmt.Sprintf("%d", internalImports))),
		}
		if totalImports > 0 {
			ratio := float64(internalImports) / float64(totalImports) * 100
			importLines = append(importLines, fmt.Sprintf(" %-18s %s",
				output.Dim("Internal ratio:"), output.Cyan(fmt.Sprintf("%.0f%%", ratio))))
		}
		fmt.Println()
		fmt.Println(output.Box("Import Breakdown", importLines))

		fmt.Println()
		output.PrintHint("repokit health for project health score")
		output.PrintHint("repokit deps for dependency analysis")
		fmt.Println()

		return nil
	},
}

// scaleBar returns a bar string scaled so that `value` maps proportionally
// to a bar of at most `maxWidth` filled characters, given the dataset max.
func scaleBar(value, dataMax, maxWidth int) string {
	if dataMax <= 0 {
		return ""
	}
	w := int(float64(value) / float64(dataMax) * float64(maxWidth))
	if w < 1 && value > 0 {
		w = 1
	}
	if w > maxWidth {
		w = maxWidth
	}
	return strings.Repeat("█", w)
}

func init() {
	rootCmd.AddCommand(statsCmd)
}
