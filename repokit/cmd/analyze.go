package cmd

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/fcreme/CLI/repokit/internal/config"
	"github.com/fcreme/CLI/repokit/internal/connect"
	"github.com/fcreme/CLI/repokit/internal/detector"
	"github.com/fcreme/CLI/repokit/internal/indexer"
	"github.com/fcreme/CLI/repokit/internal/output"
	"github.com/fcreme/CLI/repokit/internal/store"
	"github.com/fcreme/CLI/repokit/pkg/models"
)

var (
	analyzeHTML  bool
	analyzeQuick bool
)

var analyzeCmd = &cobra.Command{
	Use:     "analyze",
	Aliases: []string{"a"},
	Short:   "One-shot: connect, index, and run every analysis",
	Long: `Run a full project analysis with a single command.

Pipeline:
  1. Auto-connect to the current directory if not already connected
  2. Incrementally index (skips unchanged files)
  3. Detect tech stack and project patterns
  4. Compute health score, lint findings, dependency cycles, duplicates

Exit code reflects health: 0 = grade A/B, 1 = C/D, 2 = F. Useful for CI.

Flags:
  --html       Also generate the full HTML report
  --quick      Skip cycle detection and duplicate analysis (faster)

Examples:
  cd my-project && repokit analyze
  repokit analyze --html
  repokit analyze --quick`,
	RunE: runAnalyze,
}

func init() {
	analyzeCmd.Flags().BoolVar(&analyzeHTML, "html", false, "Also generate an HTML report")
	analyzeCmd.Flags().BoolVar(&analyzeQuick, "quick", false, "Skip slow analyses (cycles, duplicates)")
	rootCmd.AddCommand(analyzeCmd)
}

func runAnalyze(cmd *cobra.Command, args []string) error {
	cwd, _ := os.Getwd()

	// Step 1: resolve or auto-connect
	repoRoot, err := config.FindRepoRoot(cwd)
	if err != nil {
		output.PrintInfo("No repokit project found, connecting %s ...", cwd)
		repoRoot, err = connect.Connect(cwd)
		if err != nil {
			output.PrintError("Connect failed: %v", err)
			return err
		}
		_ = config.SetActiveProject(repoRoot)
		output.PrintSuccess("Connected: %s", repoRoot)
	}

	// Step 2: open store
	s, err := store.Open(config.DBPath(repoRoot))
	if err != nil {
		output.PrintError("Opening index: %v", err)
		return err
	}
	defer s.Close()

	ctx := context.Background()
	start := time.Now()

	// Step 3: incremental index
	fmt.Println()
	fmt.Printf("  %s\n", output.Bold("Indexing"))
	idxRes, err := indexer.Index(ctx, repoRoot, s, indexer.IndexOptions{
		OnProgress: func(current, total int, file string) {
			if current%25 == 0 || current == total {
				fmt.Printf("\r    [%d/%d] %s", current, total, truncatePath(file, 50))
			}
		},
	})
	if err != nil {
		output.PrintError("Indexing failed: %v", err)
		return err
	}
	fmt.Println()
	_ = s.SetMeta(ctx, "last_indexed_at", time.Now().UTC().Format(time.RFC3339))

	// Step 4: detect stack + patterns
	stack, _ := detector.DetectStack(ctx, repoRoot, s)
	_, _ = detector.DetectPatterns(ctx, repoRoot, s)

	// Step 5: render sections
	fmt.Println()
	fmt.Printf("  %s\n", output.Bold("Repokit Analysis"))
	fmt.Printf("  %s %s\n", output.Dim("Repo:"), repoRoot)
	fmt.Printf("  %s %d files indexed, %d skipped (%d components, %d hooks)\n",
		output.Dim("Index:"),
		idxRes.FilesIndexed, idxRes.FilesSkipped, idxRes.Components, idxRes.Hooks)

	renderAnalyzeStack(stack)

	score, penalties := analyzeHealthScore(ctx, s)
	renderAnalyzeHealth(score, penalties)

	renderAnalyzeTopIssues(ctx, s)
	renderAnalyzePatterns(ctx, s)

	if !analyzeQuick {
		renderAnalyzeDeps(ctx, s)
		renderAnalyzeDuplicates(ctx, s)
	} else {
		fmt.Println()
		fmt.Printf("  %s\n", output.Dim("--quick: skipped dependency cycles and duplicate analysis"))
	}

	elapsed := time.Since(start).Round(time.Millisecond)
	fmt.Println()
	fmt.Printf("  %s %s\n\n", output.Dim("Analysis complete in"), elapsed)

	// Step 6: optional HTML report
	if analyzeHTML {
		data, err := gatherReportData(ctx, s, repoRoot)
		if err != nil {
			output.PrintWarning("HTML export skipped: %v", err)
		} else if path, err := exportHTML(data, repoRoot); err != nil {
			output.PrintWarning("HTML export failed: %v", err)
		} else {
			output.PrintSuccess("HTML report: %s", path)
		}
	}

	// Step 7: exit code reflects health (release store first)
	_ = s.Close()
	switch {
	case score < 60:
		os.Exit(2)
	case score < 80:
		os.Exit(1)
	}
	return nil
}

// ---------- section renderers ----------

func renderAnalyzeStack(items []models.StackItem) {
	fmt.Println()
	fmt.Printf("  %s\n", output.Bold("Tech Stack"))
	if len(items) == 0 {
		fmt.Printf("    %s\n", output.Dim("none detected"))
		return
	}
	max := 8
	if len(items) < max {
		max = len(items)
	}
	for _, it := range items[:max] {
		version := it.Version
		if version == "" {
			version = "-"
		}
		fmt.Printf("    %-12s %s %s\n",
			output.Dim(it.Category+":"),
			output.Cyan(it.Name),
			output.Dim(version))
	}
	if len(items) > max {
		fmt.Printf("    %s\n", output.Dim(fmt.Sprintf("... and %d more", len(items)-max)))
	}
}

func renderAnalyzeHealth(score int, penalties []healthPenalty) {
	grade, gradeColor := getGrade(score)
	fmt.Println()
	fmt.Printf("  %s %s  %s\n",
		output.Bold("Health:"),
		gradeColor(fmt.Sprintf("  %s  ", grade)),
		output.Bold(fmt.Sprintf("%d/100", score)))
	if len(penalties) == 0 {
		return
	}
	for _, p := range penalties {
		bar := strings.Repeat("*", p.penalty)
		fmt.Printf("    %s %-24s %s %s\n",
			output.Red(fmt.Sprintf("-%-2d", p.penalty)),
			p.category,
			output.Red(bar),
			output.Dim(p.detail))
	}
}

func renderAnalyzeTopIssues(ctx context.Context, s *store.Store) {
	type issue struct {
		kind string
		file string
		hint string
	}
	var issues []issue

	rows, err := s.DB().QueryContext(ctx, `
		SELECT f.path, c.name, (c.line_end - c.line_start) AS sz
		FROM components c JOIN files f ON c.file_id = f.id
		WHERE (c.line_end - c.line_start) > 300
		ORDER BY sz DESC LIMIT 5`)
	if err == nil {
		for rows.Next() {
			var path, name string
			var sz int
			if rows.Scan(&path, &name, &sz) == nil {
				issues = append(issues, issue{
					"oversized",
					path,
					fmt.Sprintf("%s (%d lines)", name, sz),
				})
			}
		}
		rows.Close()
	}

	rows, err = s.DB().QueryContext(ctx, `
		SELECT f.path, td.name, COUNT(p.id) AS n
		FROM type_definitions td
		JOIN files f ON td.file_id = f.id
		JOIN props p ON p.type_def_id = td.id
		WHERE td.name LIKE '%Props%'
		GROUP BY td.id
		HAVING n > 10
		ORDER BY n DESC LIMIT 3`)
	if err == nil {
		for rows.Next() {
			var path, name string
			var n int
			if rows.Scan(&path, &name, &n) == nil {
				issues = append(issues, issue{
					"prop bloat",
					path,
					fmt.Sprintf("%s (%d props)", name, n),
				})
			}
		}
		rows.Close()
	}

	if len(issues) == 0 {
		return
	}
	fmt.Println()
	fmt.Printf("  %s\n", output.Bold("Top Issues"))
	for _, i := range issues {
		fmt.Printf("    %s %s %s\n",
			output.Yellow(fmt.Sprintf("%-12s", i.kind)),
			output.Dim(i.file),
			i.hint)
	}
}

func renderAnalyzePatterns(ctx context.Context, s *store.Store) {
	rows, err := s.DB().QueryContext(ctx,
		`SELECT kind, description, confidence FROM patterns
		 ORDER BY confidence DESC LIMIT 6`)
	if err != nil {
		return
	}
	defer rows.Close()

	var lines []string
	for rows.Next() {
		var kind, desc string
		var conf float64
		if rows.Scan(&kind, &desc, &conf) == nil {
			lines = append(lines, fmt.Sprintf("    %-12s %s %s",
				output.Dim(kind+":"),
				desc,
				output.Dim(fmt.Sprintf("(%.0f%%)", conf*100))))
		}
	}
	if len(lines) == 0 {
		return
	}
	fmt.Println()
	fmt.Printf("  %s\n", output.Bold("Patterns"))
	for _, l := range lines {
		fmt.Println(l)
	}
}

func renderAnalyzeDeps(ctx context.Context, s *store.Store) {
	edges, err := s.GetImportGraph(ctx)
	if err != nil {
		return
	}
	g := buildGraph(edges)
	cycles := detectCycles(g)

	var orphans []string
	for file := range g.allFiles {
		if len(g.reverse[file]) == 0 && len(g.forward[file]) > 0 {
			orphans = append(orphans, file)
		}
	}
	sort.Strings(orphans)

	fmt.Println()
	fmt.Printf("  %s\n", output.Bold("Dependencies"))
	if len(cycles) == 0 {
		fmt.Printf("    %s no circular cycles\n", output.Green("OK"))
	} else {
		fmt.Printf("    %s %d circular cycle(s)\n", output.Red("!!"), len(cycles))
		for i, c := range cycles {
			if i >= 3 {
				fmt.Printf("    %s\n", output.Dim(fmt.Sprintf("... %d more cycles", len(cycles)-3)))
				break
			}
			fmt.Printf("      %s %s\n", output.Red("->"), strings.Join(c, " -> "))
		}
	}
	fmt.Printf("    %s %d entry-point files (no internal importers)\n",
		output.Dim("--"), len(orphans))
}

func renderAnalyzeDuplicates(ctx context.Context, s *store.Store) {
	components, err := loadComponentsForComparison(ctx, s)
	if err != nil || len(components) < 2 {
		return
	}

	type pair struct {
		a, b  string
		score float64
	}
	var pairs []pair
	const threshold = 0.7
	for i := 0; i < len(components); i++ {
		for j := i + 1; j < len(components); j++ {
			sc := compareSimilarity(&components[i], &components[j])
			if sc >= threshold {
				pairs = append(pairs, pair{components[i].name, components[j].name, sc})
			}
		}
	}
	sort.Slice(pairs, func(i, j int) bool { return pairs[i].score > pairs[j].score })

	fmt.Println()
	fmt.Printf("  %s\n", output.Bold("Duplicates"))
	if len(pairs) == 0 {
		fmt.Printf("    %s no high-similarity pairs (threshold 70%%)\n", output.Green("OK"))
		return
	}
	fmt.Printf("    %s %d similar pair(s) >=70%%\n", output.Yellow("--"), len(pairs))
	max := 3
	if len(pairs) < max {
		max = len(pairs)
	}
	for _, p := range pairs[:max] {
		fmt.Printf("      %s <-> %s %s\n",
			output.Cyan(p.a),
			output.Cyan(p.b),
			output.Dim(fmt.Sprintf("(%.0f%%)", p.score*100)))
	}
}

// ---------- health-score computation (same logic as health.go) ----------

func analyzeHealthScore(ctx context.Context, s *store.Store) (int, []healthPenalty) {
	score := 100
	var penalties []healthPenalty

	var totalComponents int
	s.DB().QueryRowContext(ctx, "SELECT COUNT(*) FROM components").Scan(&totalComponents)
	if totalComponents == 0 {
		return 0, nil
	}

	scanCount := func(query string) int {
		var n int
		_ = s.DB().QueryRowContext(ctx, query).Scan(&n)
		return n
	}

	if n := scanCount(`SELECT COUNT(*) FROM components c WHERE (c.line_end - c.line_start) > 300`); n > 0 {
		p := minInt(n*5, 20)
		score -= p
		penalties = append(penalties, healthPenalty{"Oversized components", n, p, fmt.Sprintf("%d components over 300 lines", n)})
	}
	if n := scanCount(`SELECT COUNT(*) FROM (
		SELECT td.id FROM type_definitions td
		JOIN props p ON p.type_def_id = td.id
		WHERE td.name LIKE '%Props%'
		GROUP BY td.id HAVING COUNT(p.id) > 10)`); n > 0 {
		p := minInt(n*3, 15)
		score -= p
		penalties = append(penalties, healthPenalty{"Prop bloat", n, p, fmt.Sprintf("%d interfaces with >10 props", n)})
	}
	if n := scanCount(`SELECT COUNT(*) FROM (
		SELECT f.id FROM files f
		JOIN components c ON c.file_id = f.id
		WHERE c.kind != 'hook'
		GROUP BY f.id HAVING COUNT(c.id) > 2)`); n > 0 {
		p := minInt(n*2, 10)
		score -= p
		penalties = append(penalties, healthPenalty{"Multi-component files", n, p, fmt.Sprintf("%d files with >2 components", n)})
	}
	if n := scanCount(`SELECT COUNT(*) FROM exports e
		JOIN files f ON e.file_id = f.id
		WHERE e.kind IN ('function','const','class') AND e.name != 'default'
		AND NOT EXISTS (
			SELECT 1 FROM imports i
			WHERE i.imported_names LIKE '%' || e.name || '%'
			AND i.file_id != e.file_id)`); n > 0 {
		p := minInt(n*2, 15)
		score -= p
		penalties = append(penalties, healthPenalty{"Unused exports", n, p, fmt.Sprintf("%d exports never imported", n)})
	}
	if n := scanCount(`SELECT COUNT(*) FROM (
		SELECT f.id FROM files f
		JOIN imports i ON i.file_id = f.id
		GROUP BY f.id HAVING COUNT(i.id) > 15)`); n > 0 {
		p := minInt(n*3, 10)
		score -= p
		penalties = append(penalties, healthPenalty{"Excessive imports", n, p, fmt.Sprintf("%d files with >15 imports", n)})
	}

	if edges, err := s.GetImportGraph(ctx); err == nil {
		g := buildGraph(edges)
		if cycles := detectCycles(g); len(cycles) > 0 {
			p := minInt(len(cycles)*8, 20)
			score -= p
			penalties = append(penalties, healthPenalty{"Circular dependencies", len(cycles), p, fmt.Sprintf("%d circular import chains", len(cycles))})
		}
	}

	if n := scanCount(`SELECT COUNT(*) FROM components c
		WHERE c.kind = 'function_component'
		AND c.export_type IN ('default','named')
		AND NOT EXISTS (
			SELECT 1 FROM type_definitions td
			WHERE td.file_id = c.file_id
			AND (td.name = c.name || 'Props' OR td.name LIKE '%Props%'))`); n > 0 {
		p := minInt(n, 10)
		score -= p
		penalties = append(penalties, healthPenalty{"Missing props types", n, p, fmt.Sprintf("%d exported components without Props type", n)})
	}

	if score < 0 {
		score = 0
	}
	return score, penalties
}

