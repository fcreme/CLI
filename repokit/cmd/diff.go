package cmd

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"
	"github.com/fcreme/CLI/repokit/internal/config"
	"github.com/fcreme/CLI/repokit/internal/output"
	"github.com/fcreme/CLI/repokit/internal/store"
)

var diffBase string

var diffCmd = &cobra.Command{
	Use:   "diff [base-branch]",
	Short: "Show component-level changes between a base branch and HEAD",
	Long: `Compare your current branch against a base branch (default: main).

Reports:
  - Source files added, modified, deleted
  - Components introduced or touched by the changes
  - File-level stats (per-extension, per-folder)

Examples:
  repokit diff              # compare HEAD against main
  repokit diff develop      # compare HEAD against develop
  repokit diff --base feat  # explicit flag form`,
	Args: cobra.MaximumNArgs(1),
	RunE: runDiff,
}

func init() {
	diffCmd.Flags().StringVar(&diffBase, "base", "main", "Base branch to compare against")
	rootCmd.AddCommand(diffCmd)
}

func runDiff(cmd *cobra.Command, args []string) error {
	base := diffBase
	if len(args) > 0 {
		base = args[0]
	}

	cwd, _ := os.Getwd()
	repoRoot, err := config.FindRepoRoot(cwd)
	if err != nil {
		output.PrintError("%v", err)
		return err
	}

	// Verify base ref exists
	if err := exec.Command("git", "-C", repoRoot, "rev-parse", "--verify", base).Run(); err != nil {
		output.PrintError("Base ref '%s' not found in this repository", base)
		output.PrintHint("git branch -a   to list branches")
		return err
	}

	// Get current branch name for the banner
	headBytes, _ := exec.Command("git", "-C", repoRoot, "rev-parse", "--abbrev-ref", "HEAD").Output()
	head := strings.TrimSpace(string(headBytes))
	if head == "" {
		head = "HEAD"
	}

	// Run git diff --name-status
	out, err := exec.Command("git", "-C", repoRoot, "diff", "--name-status", base+"...HEAD").Output()
	if err != nil {
		output.PrintError("git diff failed: %v", err)
		return err
	}

	var added, modified, deleted, renamed []string
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) < 2 {
			continue
		}
		status := parts[0]
		path := parts[len(parts)-1]
		switch {
		case strings.HasPrefix(status, "A"):
			added = append(added, path)
		case strings.HasPrefix(status, "M"):
			modified = append(modified, path)
		case strings.HasPrefix(status, "D"):
			deleted = append(deleted, path)
		case strings.HasPrefix(status, "R"):
			renamed = append(renamed, path)
			modified = append(modified, path) // treat renamed-to as modified
		}
	}

	// Render banner
	fmt.Println()
	fmt.Println(output.Banner("BRANCH DIFF", fmt.Sprintf("%s ← %s", head, base)))
	fmt.Println()

	totalChanged := len(added) + len(modified) + len(deleted)
	if totalChanged == 0 {
		fmt.Println(output.Box("Summary", []string{
			" " + output.Green("✓") + " No changes between " + head + " and " + base,
		}))
		fmt.Println()
		return nil
	}

	// Source-only filter for component analysis
	sourceExts := map[string]bool{
		".ts": true, ".tsx": true, ".js": true, ".jsx": true, ".mjs": true, ".cjs": true,
	}
	isSource := func(path string) bool {
		return sourceExts[strings.ToLower(filepath.Ext(path))]
	}

	addedSrc := filterPaths(added, isSource)
	modifiedSrc := filterPaths(modified, isSource)
	deletedSrc := filterPaths(deleted, isSource)

	// Summary box
	summaryLines := []string{
		fmt.Sprintf(" %s   %s files added       %s",
			output.Green("+"),
			output.Cyan(fmt.Sprintf("%3d", len(added))),
			output.Dim(fmt.Sprintf("(%d source)", len(addedSrc)))),
		fmt.Sprintf(" %s   %s files modified    %s",
			output.Yellow("~"),
			output.Cyan(fmt.Sprintf("%3d", len(modified))),
			output.Dim(fmt.Sprintf("(%d source)", len(modifiedSrc)))),
		fmt.Sprintf(" %s   %s files deleted     %s",
			output.Red("-"),
			output.Cyan(fmt.Sprintf("%3d", len(deleted))),
			output.Dim(fmt.Sprintf("(%d source)", len(deletedSrc)))),
	}
	fmt.Println(output.Box("Summary", summaryLines))

	// Per-extension breakdown
	extCounts := make(map[string]int)
	for _, p := range append(append(added, modified...), deleted...) {
		ext := strings.ToLower(filepath.Ext(p))
		if ext == "" {
			ext = "(no ext)"
		}
		extCounts[ext]++
	}
	if len(extCounts) > 0 {
		type extStat struct {
			ext   string
			count int
		}
		var stats []extStat
		maxC := 1
		for e, c := range extCounts {
			stats = append(stats, extStat{e, c})
			if c > maxC {
				maxC = c
			}
		}
		sort.Slice(stats, func(i, j int) bool { return stats[i].count > stats[j].count })
		maxShow := 8
		if len(stats) < maxShow {
			maxShow = len(stats)
		}
		var extLines []string
		for _, st := range stats[:maxShow] {
			bar := scaleBar(st.count, maxC, 24)
			extLines = append(extLines, fmt.Sprintf(" %s  %-24s  %s",
				output.Cyan(fmt.Sprintf("%3d", st.count)),
				output.Green(bar),
				st.ext))
		}
		fmt.Println()
		fmt.Println(output.Box("Files by Extension", extLines))
	}

	// Component impact: query index for components in modified/added source files
	s, err := store.Open(config.DBPath(repoRoot))
	if err == nil {
		defer s.Close()
		ctx := context.Background()

		affectedFiles := append(append([]string{}, addedSrc...), modifiedSrc...)
		var compLines []string
		seen := map[string]bool{}
		for _, file := range affectedFiles {
			rows, err := s.DB().QueryContext(ctx, `
				SELECT c.name, c.kind FROM components c
				JOIN files f ON c.file_id = f.id
				WHERE f.path = ?`, file)
			if err != nil {
				continue
			}
			for rows.Next() {
				var name, kind string
				if rows.Scan(&name, &kind) != nil {
					continue
				}
				key := file + "::" + name
				if seen[key] {
					continue
				}
				seen[key] = true
				marker := output.Yellow("~")
				for _, a := range addedSrc {
					if a == file {
						marker = output.Green("+")
						break
					}
				}
				path := file
				if len(path) > 30 {
					path = "..." + path[len(path)-27:]
				}
				compLines = append(compLines, fmt.Sprintf(" %s  %-18s %-12s %s",
					marker, name, output.Dim(kind), output.Dim(path)))
			}
			rows.Close()
		}
		if len(compLines) > 0 {
			fmt.Println()
			maxShow := 15
			if len(compLines) > maxShow {
				extra := len(compLines) - maxShow
				compLines = compLines[:maxShow]
				compLines = append(compLines, " "+output.Dim(fmt.Sprintf("... and %d more components", extra)))
			}
			fmt.Println(output.Box("Affected Components", compLines))
		}
	}

	// Renamed list (informational only)
	if len(renamed) > 0 {
		var renLines []string
		maxShow := 5
		if len(renamed) < maxShow {
			maxShow = len(renamed)
		}
		for _, r := range renamed[:maxShow] {
			renLines = append(renLines, " "+output.Dim("→")+" "+r)
		}
		if len(renamed) > maxShow {
			renLines = append(renLines, " "+output.Dim(fmt.Sprintf("... and %d more", len(renamed)-maxShow)))
		}
		fmt.Println()
		fmt.Println(output.Box(fmt.Sprintf("Renamed (%d)", len(renamed)), renLines))
	}

	fmt.Println()
	output.PrintHint("repokit analyze to see full project state after the diff")
	fmt.Println()
	return nil
}

func filterPaths(paths []string, keep func(string) bool) []string {
	var out []string
	for _, p := range paths {
		if keep(p) {
			out = append(out, p)
		}
	}
	return out
}
