package cmd

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/fcreme/CLI/repokit/internal/config"
	"github.com/fcreme/CLI/repokit/internal/output"
	"github.com/fcreme/CLI/repokit/internal/store"
)

var healthCmd = &cobra.Command{
	Use:     "health",
	Aliases: []string{"h"},
	Short:   "Calculate project health score",
	Long: `Analyze the indexed project and produce a health score from 0-100.

Factors:
  - Code quality issues (lint errors/warnings)
  - Circular dependencies
  - Orphan components (potential dead code)
  - Oversized components
  - Prop bloat
  - Import complexity

Grade scale: A (90-100), B (80-89), C (70-79), D (60-69), F (<60)

Examples:
  repokit health`,
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

		// Gather metrics
		var totalComponents, totalFiles, totalImports int
		s.DB().QueryRowContext(ctx, "SELECT COUNT(*) FROM components").Scan(&totalComponents)
		s.DB().QueryRowContext(ctx, "SELECT COUNT(*) FROM files").Scan(&totalFiles)
		s.DB().QueryRowContext(ctx, "SELECT COUNT(*) FROM imports").Scan(&totalImports)

		if totalComponents == 0 {
			fmt.Println()
			output.PrintWarning("No components indexed yet")
			output.PrintHint("repokit index --force")
			fmt.Println()
			return nil
		}

		score := 100
		var penalties []healthPenalty

		// 1. Oversized components (>300 lines)
		var oversizedCount int
		s.DB().QueryRowContext(ctx, `
			SELECT COUNT(*) FROM components c
			WHERE (c.line_end - c.line_start) > 300
		`).Scan(&oversizedCount)
		if oversizedCount > 0 {
			penalty := minInt(oversizedCount*5, 20)
			score -= penalty
			penalties = append(penalties, healthPenalty{
				category: "Oversized components",
				count:    oversizedCount,
				penalty:  penalty,
				detail:   fmt.Sprintf("%d components over 300 lines", oversizedCount),
			})
		}

		// 2. Prop bloat (>10 props)
		var propBloatCount int
		s.DB().QueryRowContext(ctx, `
			SELECT COUNT(*) FROM (
				SELECT td.id FROM type_definitions td
				JOIN props p ON p.type_def_id = td.id
				WHERE td.name LIKE '%Props%'
				GROUP BY td.id
				HAVING COUNT(p.id) > 10
			)
		`).Scan(&propBloatCount)
		if propBloatCount > 0 {
			penalty := minInt(propBloatCount*3, 15)
			score -= penalty
			penalties = append(penalties, healthPenalty{
				category: "Prop bloat",
				count:    propBloatCount,
				penalty:  penalty,
				detail:   fmt.Sprintf("%d interfaces with >10 props", propBloatCount),
			})
		}

		// 3. Multi-component files (>2 components per file)
		var multiCompFiles int
		s.DB().QueryRowContext(ctx, `
			SELECT COUNT(*) FROM (
				SELECT f.id FROM files f
				JOIN components c ON c.file_id = f.id
				WHERE c.kind != 'hook'
				GROUP BY f.id
				HAVING COUNT(c.id) > 2
			)
		`).Scan(&multiCompFiles)
		if multiCompFiles > 0 {
			penalty := minInt(multiCompFiles*2, 10)
			score -= penalty
			penalties = append(penalties, healthPenalty{
				category: "Multi-component files",
				count:    multiCompFiles,
				penalty:  penalty,
				detail:   fmt.Sprintf("%d files with >2 components", multiCompFiles),
			})
		}

		// 4. Unused exports
		var unusedExports int
		s.DB().QueryRowContext(ctx, `
			SELECT COUNT(*) FROM exports e
			JOIN files f ON e.file_id = f.id
			WHERE e.kind IN ('function', 'const', 'class')
			AND e.name != 'default'
			AND NOT EXISTS (
				SELECT 1 FROM imports i
				WHERE i.imported_names LIKE '%' || e.name || '%'
				AND i.file_id != e.file_id
			)
		`).Scan(&unusedExports)
		if unusedExports > 0 {
			penalty := minInt(unusedExports*2, 15)
			score -= penalty
			penalties = append(penalties, healthPenalty{
				category: "Unused exports",
				count:    unusedExports,
				penalty:  penalty,
				detail:   fmt.Sprintf("%d exports never imported", unusedExports),
			})
		}

		// 5. Excessive imports (>15 per file)
		var excessiveImportFiles int
		s.DB().QueryRowContext(ctx, `
			SELECT COUNT(*) FROM (
				SELECT f.id FROM files f
				JOIN imports i ON i.file_id = f.id
				GROUP BY f.id
				HAVING COUNT(i.id) > 15
			)
		`).Scan(&excessiveImportFiles)
		if excessiveImportFiles > 0 {
			penalty := minInt(excessiveImportFiles*3, 10)
			score -= penalty
			penalties = append(penalties, healthPenalty{
				category: "Excessive imports",
				count:    excessiveImportFiles,
				penalty:  penalty,
				detail:   fmt.Sprintf("%d files with >15 imports", excessiveImportFiles),
			})
		}

		// 6. Circular dependencies
		edges, err := s.GetImportGraph(ctx)
		if err == nil {
			graph := buildGraph(edges)
			cycles := detectCycles(graph)
			if len(cycles) > 0 {
				penalty := minInt(len(cycles)*8, 20)
				score -= penalty
				penalties = append(penalties, healthPenalty{
					category: "Circular dependencies",
					count:    len(cycles),
					penalty:  penalty,
					detail:   fmt.Sprintf("%d circular import chains", len(cycles)),
				})
			}
		}

		// 7. Missing props types
		var missingPropsType int
		s.DB().QueryRowContext(ctx, `
			SELECT COUNT(*) FROM components c
			WHERE c.kind = 'function_component'
			AND c.export_type IN ('default', 'named')
			AND NOT EXISTS (
				SELECT 1 FROM type_definitions td
				WHERE td.file_id = c.file_id
				AND (td.name = c.name || 'Props' OR td.name LIKE '%Props%')
			)
		`).Scan(&missingPropsType)
		if missingPropsType > 0 {
			penalty := minInt(missingPropsType, 10)
			score -= penalty
			penalties = append(penalties, healthPenalty{
				category: "Missing props types",
				count:    missingPropsType,
				penalty:  penalty,
				detail:   fmt.Sprintf("%d exported components without Props type", missingPropsType),
			})
		}

		// Clamp score
		if score < 0 {
			score = 0
		}

		// Display
		grade, gradeColor := getGrade(score)

		fmt.Println()
		fmt.Println(output.Banner("PROJECT HEALTH", "repokit v"+Version))
		fmt.Println()

		// Score + grade box
		scoreLines := []string{
			"",
			"  ┌───┐",
			"  │ " + gradeColor(grade) + " │   " + output.Bold(fmt.Sprintf("%d / 100", score)),
			"  └───┘",
		}
		if len(penalties) > 0 {
			scoreLines = append(scoreLines, "")
			for _, p := range penalties {
				barLen := p.penalty
				if barLen > 10 {
					barLen = 10
				}
				bar := strings.Repeat("█", barLen)
				scoreLines = append(scoreLines, fmt.Sprintf("  %s  %-22s  %-10s  %s",
					output.Red(fmt.Sprintf("-%-2d", p.penalty)),
					p.category,
					output.Red(bar),
					output.Dim(p.detail),
				))
			}
		}
		fmt.Println(output.Box("Score", scoreLines))

		// Codebase summary
		fmt.Println()
		codebaseLines := []string{
			fmt.Sprintf(" %-13s %s    %-13s %s    %-13s %s",
				output.Dim("Components:"), output.Cyan(fmt.Sprintf("%d", totalComponents)),
				output.Dim("Files:"), output.Cyan(fmt.Sprintf("%d", totalFiles)),
				output.Dim("Imports:"), output.Cyan(fmt.Sprintf("%d", totalImports))),
		}
		fmt.Println(output.Box("Codebase", codebaseLines))

		fmt.Println()
		switch {
		case score >= 90:
			output.PrintSuccess("Excellent project health!")
		case score >= 80:
			output.PrintSuccess("Good project health")
			output.PrintHint("repokit lint for detailed issues")
		case score >= 70:
			output.PrintWarning("Fair project health - some issues to address")
			output.PrintHint("repokit lint for detailed issues")
		case score >= 60:
			output.PrintWarning("Project health needs attention")
			output.PrintHint("repokit lint for detailed issues")
			output.PrintHint("repokit deps --circular to find circular deps")
		default:
			output.PrintError("Project health is critical")
			output.PrintHint("repokit lint for detailed issues")
			output.PrintHint("repokit deps --circular to find circular deps")
			output.PrintHint("repokit deps --orphans to find dead code")
		}
		fmt.Println()

		return nil
	},
}

type healthPenalty struct {
	category string
	count    int
	penalty  int
	detail   string
}

func getGrade(score int) (string, func(a ...interface{}) string) {
	switch {
	case score >= 90:
		return "A", output.Green
	case score >= 80:
		return "B", output.Green
	case score >= 70:
		return "C", output.Yellow
	case score >= 60:
		return "D", output.Yellow
	default:
		return "F", output.Red
	}
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func init() {
	rootCmd.AddCommand(healthCmd)
}
