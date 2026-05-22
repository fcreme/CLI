package cmd

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/fsnotify/fsnotify"
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
	analyzeHTML       bool
	analyzeQuick      bool
	analyzeWatch      bool
	analyzeDebounceMs int
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
  --watch      Re-run the analysis on every source file change (Ctrl+C to exit)

Examples:
  cd my-project && repokit analyze
  repokit analyze --html
  repokit analyze --quick
  repokit analyze --watch`,
	RunE: runAnalyze,
}

func init() {
	analyzeCmd.Flags().BoolVar(&analyzeHTML, "html", false, "Also generate an HTML report")
	analyzeCmd.Flags().BoolVar(&analyzeQuick, "quick", false, "Skip slow analyses (cycles, duplicates)")
	analyzeCmd.Flags().BoolVar(&analyzeWatch, "watch", false, "Re-run analysis on file changes")
	analyzeCmd.Flags().IntVar(&analyzeDebounceMs, "debounce", 500, "Debounce window in ms for watch mode")
	rootCmd.AddCommand(analyzeCmd)
}

func runAnalyze(cmd *cobra.Command, args []string) error {
	cwd, _ := os.Getwd()

	// Resolve or auto-connect
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

	s, err := store.Open(config.DBPath(repoRoot))
	if err != nil {
		output.PrintError("Opening index: %v", err)
		return err
	}
	defer s.Close()

	ctx := context.Background()

	// First pass
	score, err := runAnalysisPass(ctx, s, repoRoot, 1)
	if err != nil {
		return err
	}

	// Watch mode takes over and never returns until Ctrl+C
	if analyzeWatch {
		return runAnalyzeWatchLoop(ctx, s, repoRoot)
	}

	// Optional HTML
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

	// Exit code reflects health (release store first)
	_ = s.Close()
	switch {
	case score < 60:
		os.Exit(2)
	case score < 80:
		os.Exit(1)
	}
	return nil
}

// runAnalysisPass runs one full index + render cycle and returns the health score.
// passNum is shown in the banner subtitle so users can see live iteration count.
func runAnalysisPass(ctx context.Context, s *store.Store, repoRoot string, passNum int) (int, error) {
	start := time.Now()

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
		return 0, err
	}
	fmt.Println()
	_ = s.SetMeta(ctx, "last_indexed_at", time.Now().UTC().Format(time.RFC3339))

	stack, _ := detector.DetectStack(ctx, repoRoot, s)
	_, _ = detector.DetectPatterns(ctx, repoRoot, s)

	subtitle := "v" + Version
	if analyzeWatch {
		subtitle = fmt.Sprintf("pass #%d · %s", passNum, time.Now().Format("15:04:05"))
	}
	fmt.Println()
	fmt.Println(output.Banner("REPOKIT ANALYSIS", subtitle))
	fmt.Println()
	fmt.Printf("  %s %s\n", output.Dim("Repo: "), repoRoot)
	fmt.Printf("  %s %d files indexed, %d skipped  %s %d components, %d hooks\n",
		output.Dim("Index:"),
		idxRes.FilesIndexed, idxRes.FilesSkipped,
		output.Dim("·"),
		idxRes.Components, idxRes.Hooks)

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
	return score, nil
}

// runAnalyzeWatchLoop watches source files and re-runs analysis on change.
func runAnalyzeWatchLoop(ctx context.Context, s *store.Store, repoRoot string) error {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		output.PrintError("Creating watcher: %v", err)
		return err
	}
	defer watcher.Close()

	// Recursively add directories, skipping noisy paths.
	ignored := map[string]bool{
		"node_modules": true,
		".git":         true,
		".repokit":     true,
		"dist":         true,
		"build":        true,
		".next":        true,
		".turbo":       true,
		"coverage":     true,
		".cache":       true,
		"out":          true,
	}
	walkErr := filepath.WalkDir(repoRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil || !d.IsDir() {
			return nil
		}
		if ignored[d.Name()] {
			return filepath.SkipDir
		}
		_ = watcher.Add(path)
		return nil
	})
	if walkErr != nil {
		output.PrintWarning("Some directories couldn't be watched: %v", walkErr)
	}

	footer := output.Dim("─── Watching for changes (Ctrl+C to exit) ───")
	fmt.Println(footer)

	debounce := time.Duration(analyzeDebounceMs) * time.Millisecond
	timer := time.NewTimer(time.Hour)
	timer.Stop()

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, os.Interrupt)
	defer signal.Stop(sigs)

	passNum := 2
	for {
		select {
		case <-sigs:
			fmt.Println()
			output.PrintInfo("Watch stopped")
			fmt.Println()
			return nil
		case ev, ok := <-watcher.Events:
			if !ok {
				return nil
			}
			if !isAnalyzableSource(ev.Name) {
				continue
			}
			// Coalesce bursts of events into a single rerun
			timer.Reset(debounce)
		case err, ok := <-watcher.Errors:
			if !ok {
				return nil
			}
			output.PrintWarning("Watch error: %v", err)
		case <-timer.C:
			clearAnalyzeScreen()
			if _, err := runAnalysisPass(ctx, s, repoRoot, passNum); err != nil {
				output.PrintError("Pass failed: %v", err)
			}
			passNum++
			fmt.Println(footer)
		}
	}
}

func clearAnalyzeScreen() {
	// ANSI: clear screen + cursor home
	fmt.Print("\x1b[2J\x1b[H")
}

func isAnalyzableSource(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".ts", ".tsx", ".js", ".jsx", ".mjs", ".cjs":
		return true
	}
	return false
}

// ---------- section renderers ----------

func renderAnalyzeStack(items []models.StackItem) {
	if len(items) == 0 {
		fmt.Println()
		fmt.Println(output.Box("Tech Stack", []string{output.Dim("none detected")}))
		return
	}
	maxShow := 8
	if len(items) < maxShow {
		maxShow = len(items)
	}
	lines := make([]string, 0, maxShow+1)
	for _, it := range items[:maxShow] {
		version := it.Version
		if version == "" {
			version = "-"
		}
		lines = append(lines, fmt.Sprintf(" %-12s %-20s %s",
			output.Dim(it.Category),
			output.Cyan(it.Name),
			output.Dim(version)))
	}
	if len(items) > maxShow {
		lines = append(lines, " "+output.Dim(fmt.Sprintf("... and %d more", len(items)-maxShow)))
	}
	fmt.Println()
	fmt.Println(output.Box("Tech Stack", lines))
}

func renderAnalyzeHealth(score int, penalties []healthPenalty) {
	grade, gradeColor := getGrade(score)
	lines := []string{
		"",
		"  ┌───┐",
		"  │ " + gradeColor(grade) + " │   " + output.Bold(fmt.Sprintf("%d / 100", score)),
		"  └───┘",
	}
	if len(penalties) > 0 {
		lines = append(lines, "")
		for _, p := range penalties {
			barLen := p.penalty
			if barLen > 10 {
				barLen = 10
			}
			bar := strings.Repeat("█", barLen)
			lines = append(lines, fmt.Sprintf("  %s  %-22s  %-10s  %s",
				output.Red(fmt.Sprintf("-%-2d", p.penalty)),
				p.category,
				output.Red(bar),
				output.Dim(p.detail)))
		}
	}
	fmt.Println()
	fmt.Println(output.Box("Health", lines))
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
	lines := make([]string, 0, len(issues))
	for _, i := range issues {
		// trim file path to keep line within box
		file := i.file
		if len(file) > 30 {
			file = "..." + file[len(file)-27:]
		}
		lines = append(lines, fmt.Sprintf(" %s  %-30s  %s",
			output.Yellow(fmt.Sprintf("%-11s", i.kind)),
			output.Dim(file),
			i.hint))
	}
	fmt.Println()
	fmt.Println(output.Box("Top Issues", lines))
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
			if len(desc) > 40 {
				desc = desc[:37] + "..."
			}
			lines = append(lines, fmt.Sprintf(" %-14s %-40s %s",
				output.Dim(kind),
				desc,
				output.Dim(fmt.Sprintf("(%.0f%%)", conf*100))))
		}
	}
	if len(lines) == 0 {
		return
	}
	fmt.Println()
	fmt.Println(output.Box("Patterns", lines))
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

	var lines []string
	if len(cycles) == 0 {
		lines = append(lines, " "+output.Green("✓")+" no circular cycles")
	} else {
		lines = append(lines, " "+output.Red("✗")+fmt.Sprintf(" %d circular cycle(s)", len(cycles)))
		for i, c := range cycles {
			if i >= 3 {
				lines = append(lines, " "+output.Dim(fmt.Sprintf("   ... %d more cycles", len(cycles)-3)))
				break
			}
			chain := strings.Join(c, " → ")
			if len(chain) > 60 {
				chain = chain[:57] + "..."
			}
			lines = append(lines, "   "+output.Red("↳")+" "+chain)
		}
	}
	lines = append(lines, " "+output.Dim("·")+fmt.Sprintf(" %d entry-point files (no internal importers)", len(orphans)))
	fmt.Println()
	fmt.Println(output.Box("Dependencies", lines))
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

	var lines []string
	if len(pairs) == 0 {
		lines = append(lines, " "+output.Green("✓")+" no high-similarity pairs (threshold 70%)")
	} else {
		lines = append(lines, " "+output.Yellow("⚠")+fmt.Sprintf(" %d similar pair(s) ≥70%%", len(pairs)))
		maxShow := 3
		if len(pairs) < maxShow {
			maxShow = len(pairs)
		}
		for _, p := range pairs[:maxShow] {
			lines = append(lines, fmt.Sprintf("   %s ↔ %s %s",
				output.Cyan(p.a),
				output.Cyan(p.b),
				output.Dim(fmt.Sprintf("(%.0f%%)", p.score*100))))
		}
	}
	fmt.Println()
	fmt.Println(output.Box("Duplicates", lines))
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

