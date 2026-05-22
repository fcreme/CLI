package cmd

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/fcreme/CLI/repokit/internal/config"
	"github.com/fcreme/CLI/repokit/internal/output"
	"github.com/fcreme/CLI/repokit/internal/store"
)

var (
	checkMinHealth   int
	checkMaxLint     int
	checkNoCircular  bool
)

var checkCmd = &cobra.Command{
	Use:   "check",
	Short: "CI/CD quality gate",
	Long: `Run quality checks and exit with code 1 if thresholds are not met.

Designed for CI/CD pipelines. Returns exit code 0 on pass, 1 on fail.

Thresholds:
  --min-health 70    Fail if health score < 70 (default: 60)
  --max-lint 10      Fail if lint issues > 10 (default: unlimited)
  --no-circular      Fail if circular dependencies exist

Examples:
  repokit check
  repokit check --min-health 80
  repokit check --min-health 70 --no-circular
  repokit check --max-lint 5`,
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
		passed := true
		var results []checkResult

		// Health score check
		healthScore := calculateHealthScore(ctx, s)
		healthPassed := healthScore >= checkMinHealth
		if !healthPassed {
			passed = false
		}
		results = append(results, checkResult{
			name:   "Health score",
			value:  fmt.Sprintf("%d/100", healthScore),
			thresh: fmt.Sprintf(">= %d", checkMinHealth),
			passed: healthPassed,
		})

		// Lint issues count
		lintCount := countLintIssues(ctx, s)
		if checkMaxLint > 0 {
			lintPassed := lintCount <= checkMaxLint
			if !lintPassed {
				passed = false
			}
			results = append(results, checkResult{
				name:   "Lint issues",
				value:  fmt.Sprintf("%d", lintCount),
				thresh: fmt.Sprintf("<= %d", checkMaxLint),
				passed: lintPassed,
			})
		} else {
			results = append(results, checkResult{
				name:   "Lint issues",
				value:  fmt.Sprintf("%d", lintCount),
				thresh: "no limit",
				passed: true,
			})
		}

		// Circular deps check
		if checkNoCircular {
			circularCount := countCircularDeps(ctx, s)
			circPassed := circularCount == 0
			if !circPassed {
				passed = false
			}
			results = append(results, checkResult{
				name:   "Circular deps",
				value:  fmt.Sprintf("%d", circularCount),
				thresh: "0",
				passed: circPassed,
			})
		}

		// Display results
		fmt.Println()
		fmt.Printf("  %s\n", output.Bold("Quality Gate Results:"))
		fmt.Println()

		for _, r := range results {
			icon := output.Green("PASS")
			if !r.passed {
				icon = output.Red("FAIL")
			}
			fmt.Printf("  %s  %-20s  %s  %s\n",
				icon,
				r.name,
				output.Cyan(r.value),
				output.Dim("(threshold: "+r.thresh+")"),
			)
		}

		fmt.Println()
		if passed {
			output.PrintSuccess("All quality checks passed")
		} else {
			output.PrintError("Quality checks failed")
			output.PrintHint("repokit health for detailed breakdown")
			output.PrintHint("repokit lint for specific issues")
		}
		fmt.Println()

		if !passed {
			return fmt.Errorf("quality checks failed")
		}
		return nil
	},
}

type checkResult struct {
	name   string
	value  string
	thresh string
	passed bool
}

func calculateHealthScore(ctx context.Context, s *store.Store) int {
	score := 100
	var oversized, propBloat, multiComp, unused, missingProps, excessive int

	s.DB().QueryRowContext(ctx, "SELECT COUNT(*) FROM components WHERE (line_end - line_start) > 300").Scan(&oversized)
	s.DB().QueryRowContext(ctx, `SELECT COUNT(*) FROM (SELECT td.id FROM type_definitions td JOIN props p ON p.type_def_id = td.id WHERE td.name LIKE '%Props%' GROUP BY td.id HAVING COUNT(p.id) > 10)`).Scan(&propBloat)
	s.DB().QueryRowContext(ctx, `SELECT COUNT(*) FROM (SELECT f.id FROM files f JOIN components c ON c.file_id = f.id WHERE c.kind != 'hook' GROUP BY f.id HAVING COUNT(c.id) > 2)`).Scan(&multiComp)
	s.DB().QueryRowContext(ctx, `SELECT COUNT(*) FROM exports e JOIN files f ON e.file_id = f.id WHERE e.kind IN ('function','const','class') AND e.name != 'default' AND NOT EXISTS (SELECT 1 FROM imports i WHERE i.imported_names LIKE '%' || e.name || '%' AND i.file_id != e.file_id)`).Scan(&unused)
	s.DB().QueryRowContext(ctx, `SELECT COUNT(*) FROM components c WHERE c.kind = 'function_component' AND c.export_type IN ('default','named') AND NOT EXISTS (SELECT 1 FROM type_definitions td WHERE td.file_id = c.file_id AND (td.name = c.name || 'Props' OR td.name LIKE '%Props%'))`).Scan(&missingProps)
	s.DB().QueryRowContext(ctx, `SELECT COUNT(*) FROM (SELECT f.id FROM files f JOIN imports i ON i.file_id = f.id GROUP BY f.id HAVING COUNT(i.id) > 15)`).Scan(&excessive)

	score -= minInt(oversized*5, 20)
	score -= minInt(propBloat*3, 15)
	score -= minInt(multiComp*2, 10)
	score -= minInt(unused*2, 15)
	score -= minInt(missingProps, 10)
	score -= minInt(excessive*3, 10)

	if score < 0 {
		score = 0
	}
	return score
}

func countLintIssues(ctx context.Context, s *store.Store) int {
	total := 0
	var count int

	s.DB().QueryRowContext(ctx, "SELECT COUNT(*) FROM components WHERE (line_end - line_start) > 300").Scan(&count)
	total += count
	s.DB().QueryRowContext(ctx, `SELECT COUNT(*) FROM (SELECT td.id FROM type_definitions td JOIN props p ON p.type_def_id = td.id WHERE td.name LIKE '%Props%' GROUP BY td.id HAVING COUNT(p.id) > 10)`).Scan(&count)
	total += count
	s.DB().QueryRowContext(ctx, `SELECT COUNT(*) FROM (SELECT f.id FROM files f JOIN components c ON c.file_id = f.id WHERE c.kind != 'hook' GROUP BY f.id HAVING COUNT(c.id) > 2)`).Scan(&count)
	total += count
	s.DB().QueryRowContext(ctx, `SELECT COUNT(*) FROM exports e JOIN files f ON e.file_id = f.id WHERE e.kind IN ('function','const','class') AND e.name != 'default' AND NOT EXISTS (SELECT 1 FROM imports i WHERE i.imported_names LIKE '%' || e.name || '%' AND i.file_id != e.file_id)`).Scan(&count)
	total += count

	return total
}

func countCircularDeps(ctx context.Context, s *store.Store) int {
	edges, err := s.GetImportGraph(ctx)
	if err != nil {
		return 0
	}
	graph := buildGraph(edges)
	cycles := detectCycles(graph)
	return len(cycles)
}

func init() {
	checkCmd.Flags().IntVar(&checkMinHealth, "min-health", 60, "Minimum health score (0-100)")
	checkCmd.Flags().IntVar(&checkMaxLint, "max-lint", 0, "Maximum lint issues (0 = no limit)")
	checkCmd.Flags().BoolVar(&checkNoCircular, "no-circular", false, "Fail if circular dependencies exist")

	rootCmd.AddCommand(checkCmd)
}
