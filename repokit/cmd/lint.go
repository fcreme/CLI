package cmd

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"sort"

	"github.com/spf13/cobra"
	"github.com/usuario/repokit/internal/config"
	"github.com/usuario/repokit/internal/output"
	"github.com/usuario/repokit/internal/store"
)

var (
	lintMaxLines    int
	lintMaxProps    int
	lintMaxImports  int
)

var lintCmd = &cobra.Command{
	Use:     "lint",
	Aliases: []string{"l"},
	Short:   "Detect code quality issues in components",
	Long: `Analyze indexed components for common code quality problems.

Checks for:
  - Oversized components (too many lines)
  - Too many props (prop bloat)
  - Files with too many components
  - Unused exports (never imported internally)
  - Components missing props type definitions
  - Files with excessive import count

Examples:
  repokit lint
  repokit lint --max-lines 200 --max-props 8`,
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
		var issues []lintIssue

		issues = append(issues, checkOversizedComponents(ctx, s, lintMaxLines)...)
		issues = append(issues, checkPropBloat(ctx, s, lintMaxProps)...)
		issues = append(issues, checkMultiComponentFiles(ctx, s)...)
		issues = append(issues, checkMissingPropsType(ctx, s)...)
		issues = append(issues, checkUnusedExports(ctx, s)...)
		issues = append(issues, checkExcessiveImports(ctx, s, lintMaxImports)...)

		if len(issues) == 0 {
			fmt.Println()
			output.PrintSuccess("No code quality issues detected")
			fmt.Println()
			return nil
		}

		// Group by severity
		sort.Slice(issues, func(i, j int) bool {
			if issues[i].severity != issues[j].severity {
				return issues[i].severity < issues[j].severity
			}
			return issues[i].file < issues[j].file
		})

		fmt.Println()
		table := &output.Table{
			Headers: []string{"Severity", "Issue", "File", "Detail"},
		}
		for _, issue := range issues {
			sev := output.Yellow("warn")
			if issue.severity == 0 {
				sev = output.Red("error")
			} else if issue.severity == 2 {
				sev = output.Dim("info")
			}
			table.Rows = append(table.Rows, []string{
				sev,
				issue.rule,
				issue.file,
				issue.detail,
			})
		}
		table.Print()

		// Summary
		errors, warns, infos := 0, 0, 0
		for _, i := range issues {
			switch i.severity {
			case 0:
				errors++
			case 1:
				warns++
			case 2:
				infos++
			}
		}
		fmt.Println()
		summary := fmt.Sprintf("%d issues", len(issues))
		if errors > 0 {
			summary += fmt.Sprintf(" (%s errors", output.Red(fmt.Sprintf("%d", errors)))
		} else {
			summary += " ("
		}
		if warns > 0 {
			if errors > 0 {
				summary += ", "
			}
			summary += fmt.Sprintf("%s warnings", output.Yellow(fmt.Sprintf("%d", warns)))
		}
		if infos > 0 {
			summary += fmt.Sprintf(", %d info", infos)
		}
		summary += ")"
		output.PrintWarning(summary)
		output.PrintHint("repokit show <id> to inspect a flagged component")
		fmt.Println()

		return nil
	},
}

type lintIssue struct {
	severity int // 0=error, 1=warn, 2=info
	rule     string
	file     string
	detail   string
}

func checkOversizedComponents(ctx context.Context, s *store.Store, maxLines int) []lintIssue {
	var issues []lintIssue
	rows, err := s.DB().QueryContext(ctx, `
		SELECT c.name, f.path, c.line_start, c.line_end
		FROM components c JOIN files f ON c.file_id = f.id
		WHERE (c.line_end - c.line_start) > ?
		ORDER BY (c.line_end - c.line_start) DESC
	`, maxLines)
	if err != nil {
		return nil
	}
	defer rows.Close()

	for rows.Next() {
		var name, path string
		var start, end int
		rows.Scan(&name, &path, &start, &end)
		lines := end - start
		sev := 1 // warn
		if lines > maxLines*2 {
			sev = 0 // error
		}
		issues = append(issues, lintIssue{
			severity: sev,
			rule:     "oversized-component",
			file:     path,
			detail:   fmt.Sprintf("%s is %d lines (max %d)", name, lines, maxLines),
		})
	}
	return issues
}

func checkPropBloat(ctx context.Context, s *store.Store, maxProps int) []lintIssue {
	var issues []lintIssue
	rows, err := s.DB().QueryContext(ctx, `
		SELECT td.name, f.path, COUNT(p.id) as prop_count
		FROM type_definitions td
		JOIN files f ON td.file_id = f.id
		JOIN props p ON p.type_def_id = td.id
		WHERE td.name LIKE '%Props%'
		GROUP BY td.id
		HAVING prop_count > ?
		ORDER BY prop_count DESC
	`, maxProps)
	if err != nil {
		return nil
	}
	defer rows.Close()

	for rows.Next() {
		var name, path string
		var count int
		rows.Scan(&name, &path, &count)
		issues = append(issues, lintIssue{
			severity: 1,
			rule:     "prop-bloat",
			file:     path,
			detail:   fmt.Sprintf("%s has %d props (max %d)", name, count, maxProps),
		})
	}
	return issues
}

func checkMultiComponentFiles(ctx context.Context, s *store.Store) []lintIssue {
	var issues []lintIssue
	rows, err := s.DB().QueryContext(ctx, `
		SELECT f.path, COUNT(c.id) as comp_count
		FROM files f
		JOIN components c ON c.file_id = f.id
		WHERE c.kind != 'hook'
		GROUP BY f.id
		HAVING comp_count > 2
		ORDER BY comp_count DESC
	`)
	if err != nil {
		return nil
	}
	defer rows.Close()

	for rows.Next() {
		var path string
		var count int
		rows.Scan(&path, &count)
		issues = append(issues, lintIssue{
			severity: 2,
			rule:     "multi-component-file",
			file:     path,
			detail:   fmt.Sprintf("%d components in one file (consider splitting)", count),
		})
	}
	return issues
}

func checkMissingPropsType(ctx context.Context, s *store.Store) []lintIssue {
	var issues []lintIssue
	rows, err := s.DB().QueryContext(ctx, `
		SELECT c.name, f.path
		FROM components c
		JOIN files f ON c.file_id = f.id
		WHERE c.kind = 'function_component'
		AND c.export_type IN ('default', 'named')
		AND NOT EXISTS (
			SELECT 1 FROM type_definitions td
			WHERE td.file_id = c.file_id
			AND (td.name = c.name || 'Props' OR td.name LIKE '%Props%')
		)
	`)
	if err != nil {
		return nil
	}
	defer rows.Close()

	for rows.Next() {
		var name, path string
		rows.Scan(&name, &path)
		issues = append(issues, lintIssue{
			severity: 2,
			rule:     "missing-props-type",
			file:     path,
			detail:   fmt.Sprintf("%s has no Props type definition", name),
		})
	}
	return issues
}

func checkUnusedExports(ctx context.Context, s *store.Store) []lintIssue {
	var issues []lintIssue
	rows, err := s.DB().QueryContext(ctx, `
		SELECT e.name, f.path
		FROM exports e
		JOIN files f ON e.file_id = f.id
		WHERE e.kind IN ('function', 'const', 'class')
		AND e.name != 'default'
		AND NOT EXISTS (
			SELECT 1 FROM imports i
			WHERE i.imported_names LIKE '%' || e.name || '%'
			AND i.file_id != e.file_id
		)
	`)
	if err != nil {
		return nil
	}
	defer rows.Close()

	for rows.Next() {
		var name, path string
		if err := rows.Scan(&name, &path); err != nil {
			continue
		}
		issues = append(issues, lintIssue{
			severity: 1,
			rule:     "unused-export",
			file:     path,
			detail:   fmt.Sprintf("'%s' is exported but never imported", name),
		})
	}
	return issues
}

func checkExcessiveImports(ctx context.Context, s *store.Store, maxImports int) []lintIssue {
	var issues []lintIssue
	rows, err := s.DB().QueryContext(ctx, `
		SELECT f.path, COUNT(i.id) as import_count
		FROM files f
		JOIN imports i ON i.file_id = f.id
		GROUP BY f.id
		HAVING import_count > ?
		ORDER BY import_count DESC
	`, maxImports)
	if err != nil {
		return nil
	}
	defer rows.Close()

	for rows.Next() {
		var path string
		var count int
		rows.Scan(&path, &count)
		issues = append(issues, lintIssue{
			severity: 2,
			rule:     "excessive-imports",
			file:     path,
			detail:   fmt.Sprintf("%d imports (max %d) - consider splitting", count, maxImports),
		})
	}
	return issues
}

// Suppress unused import warning
var _ = sql.ErrNoRows

func init() {
	lintCmd.Flags().IntVar(&lintMaxLines, "max-lines", 300, "Max lines per component")
	lintCmd.Flags().IntVar(&lintMaxProps, "max-props", 10, "Max props per interface")
	lintCmd.Flags().IntVar(&lintMaxImports, "max-imports", 15, "Max imports per file")

	rootCmd.AddCommand(lintCmd)
}
