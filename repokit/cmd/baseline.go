package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/fcreme/CLI/repokit/internal/config"
	"github.com/fcreme/CLI/repokit/internal/output"
	"github.com/fcreme/CLI/repokit/internal/store"
)

// baselineSnapshot is the persisted shape of a saved baseline.
type baselineSnapshot struct {
	Label        string            `json:"label"`
	CreatedAt    string            `json:"created_at"`
	Version      string            `json:"version"`
	HealthScore  int               `json:"health_score"`
	HealthGrade  string            `json:"health_grade"`
	Penalties    []baselinePenalty `json:"penalties"`
	TotalFiles   int               `json:"total_files"`
	TotalComps   int               `json:"total_components"`
	TotalHooks   int               `json:"total_hooks"`
	TotalImports int               `json:"total_imports"`
	CycleCount   int               `json:"cycle_count"`
	DupCount     int               `json:"duplicate_count"`
	Issues       []baselineIssue   `json:"issues"`
}

type baselinePenalty struct {
	Category string `json:"category"`
	Count    int    `json:"count"`
	Penalty  int    `json:"penalty"`
}

// baselineIssue is keyed by "file::name" so we can compute new vs resolved.
type baselineIssue struct {
	Kind string `json:"kind"`
	Key  string `json:"key"`
}

var baselineCmd = &cobra.Command{
	Use:   "baseline",
	Short: "Save and compare project health snapshots",
	Long: `Capture the current state so you can measure whether changes
improve or degrade project health over time.

Subcommands:
  save [label]    Save current state as a baseline (default label: timestamp)
  list            List saved baselines
  diff [label]    Compare current state against a baseline (default: latest)
  rm <label>      Delete a baseline

Examples:
  repokit baseline save before-refactor
  repokit baseline diff before-refactor
  repokit baseline diff               # diff against most recent baseline
  repokit baseline list
  repokit baseline rm before-refactor`,
}

var baselineSaveCmd = &cobra.Command{
	Use:   "save [label]",
	Short: "Save current project state as a baseline",
	Args:  cobra.MaximumNArgs(1),
	RunE:  runBaselineSave,
}

var baselineListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all saved baselines",
	RunE:  runBaselineList,
}

var baselineDiffCmd = &cobra.Command{
	Use:   "diff [label]",
	Short: "Compare current project state against a baseline",
	Args:  cobra.MaximumNArgs(1),
	RunE:  runBaselineDiff,
}

var baselineRmCmd = &cobra.Command{
	Use:   "rm <label>",
	Short: "Delete a saved baseline",
	Args:  cobra.ExactArgs(1),
	RunE:  runBaselineRm,
}

func init() {
	baselineCmd.AddCommand(baselineSaveCmd)
	baselineCmd.AddCommand(baselineListCmd)
	baselineCmd.AddCommand(baselineDiffCmd)
	baselineCmd.AddCommand(baselineRmCmd)
	rootCmd.AddCommand(baselineCmd)
}

// ---------- helpers ----------

var labelSafeRe = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

func sanitizeLabel(label string) string {
	s := labelSafeRe.ReplaceAllString(label, "-")
	s = strings.Trim(s, "-")
	if s == "" {
		s = "untitled"
	}
	return s
}

func baselineDir(repoRoot string) string {
	return filepath.Join(repoRoot, ".repokit", "baselines")
}

func baselinePath(repoRoot, label string) string {
	return filepath.Join(baselineDir(repoRoot), label+".json")
}

func loadBaseline(path string) (*baselineSnapshot, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var bs baselineSnapshot
	if err := json.Unmarshal(data, &bs); err != nil {
		return nil, fmt.Errorf("parsing baseline: %w", err)
	}
	return &bs, nil
}

func listBaselineFiles(repoRoot string) ([]string, error) {
	dir := baselineDir(repoRoot)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var paths []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".json") {
			paths = append(paths, filepath.Join(dir, e.Name()))
		}
	}
	return paths, nil
}

func mostRecentBaseline(repoRoot string) (*baselineSnapshot, string, error) {
	paths, err := listBaselineFiles(repoRoot)
	if err != nil || len(paths) == 0 {
		return nil, "", fmt.Errorf("no baselines found")
	}
	type entry struct {
		path string
		mod  time.Time
	}
	var entries []entry
	for _, p := range paths {
		st, err := os.Stat(p)
		if err != nil {
			continue
		}
		entries = append(entries, entry{p, st.ModTime()})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].mod.After(entries[j].mod) })
	if len(entries) == 0 {
		return nil, "", fmt.Errorf("no baselines found")
	}
	bs, err := loadBaseline(entries[0].path)
	return bs, entries[0].path, err
}

// gatherBaseline captures current project metrics into a snapshot.
func gatherBaseline(ctx context.Context, s *store.Store, label string) *baselineSnapshot {
	score, penalties := analyzeHealthScore(ctx, s)
	grade, _ := getGrade(score)

	bs := &baselineSnapshot{
		Label:       label,
		CreatedAt:   time.Now().UTC().Format(time.RFC3339),
		Version:     Version,
		HealthScore: score,
		HealthGrade: grade,
	}
	for _, p := range penalties {
		bs.Penalties = append(bs.Penalties, baselinePenalty{p.category, p.count, p.penalty})
	}

	s.DB().QueryRowContext(ctx, "SELECT COUNT(*) FROM files").Scan(&bs.TotalFiles)
	s.DB().QueryRowContext(ctx, "SELECT COUNT(*) FROM components WHERE kind != 'hook'").Scan(&bs.TotalComps)
	s.DB().QueryRowContext(ctx, "SELECT COUNT(*) FROM components WHERE kind = 'hook'").Scan(&bs.TotalHooks)
	s.DB().QueryRowContext(ctx, "SELECT COUNT(*) FROM imports").Scan(&bs.TotalImports)

	if edges, err := s.GetImportGraph(ctx); err == nil {
		g := buildGraph(edges)
		bs.CycleCount = len(detectCycles(g))
	}

	if comps, err := loadComponentsForComparison(ctx, s); err == nil && len(comps) >= 2 {
		for i := 0; i < len(comps); i++ {
			for j := i + 1; j < len(comps); j++ {
				if compareSimilarity(&comps[i], &comps[j]) >= 0.7 {
					bs.DupCount++
				}
			}
		}
	}

	bs.Issues = gatherIssueKeys(ctx, s)
	return bs
}

func gatherIssueKeys(ctx context.Context, s *store.Store) []baselineIssue {
	var out []baselineIssue
	rows, err := s.DB().QueryContext(ctx, `
		SELECT f.path, c.name
		FROM components c JOIN files f ON c.file_id = f.id
		WHERE (c.line_end - c.line_start) > 300`)
	if err == nil {
		for rows.Next() {
			var path, name string
			if rows.Scan(&path, &name) == nil {
				out = append(out, baselineIssue{Kind: "oversized", Key: path + "::" + name})
			}
		}
		rows.Close()
	}
	rows, err = s.DB().QueryContext(ctx, `
		SELECT f.path, td.name
		FROM type_definitions td JOIN files f ON td.file_id = f.id
		JOIN props p ON p.type_def_id = td.id
		WHERE td.name LIKE '%Props%'
		GROUP BY td.id HAVING COUNT(p.id) > 10`)
	if err == nil {
		for rows.Next() {
			var path, name string
			if rows.Scan(&path, &name) == nil {
				out = append(out, baselineIssue{Kind: "prop bloat", Key: path + "::" + name})
			}
		}
		rows.Close()
	}
	return out
}

// ---------- subcommand runners ----------

func runBaselineSave(cmd *cobra.Command, args []string) error {
	cwd, _ := os.Getwd()
	repoRoot, err := config.FindRepoRoot(cwd)
	if err != nil {
		output.PrintError("%v", err)
		return err
	}

	label := time.Now().UTC().Format("20060102-150405")
	if len(args) > 0 {
		label = sanitizeLabel(args[0])
	}

	s, err := store.Open(config.DBPath(repoRoot))
	if err != nil {
		output.PrintError("Opening index: %v", err)
		return err
	}
	defer s.Close()

	ctx := context.Background()
	bs := gatherBaseline(ctx, s, label)

	if err := os.MkdirAll(baselineDir(repoRoot), 0755); err != nil {
		output.PrintError("Creating baseline dir: %v", err)
		return err
	}
	path := baselinePath(repoRoot, label)
	data, err := json.MarshalIndent(bs, "", "  ")
	if err != nil {
		output.PrintError("Marshaling baseline: %v", err)
		return err
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		output.PrintError("Writing baseline: %v", err)
		return err
	}

	fmt.Println()
	fmt.Println(output.Banner("BASELINE SAVED", label))
	fmt.Println()
	_, gradeColor := getGrade(bs.HealthScore)
	fmt.Println(output.Box("Snapshot", []string{
		fmt.Sprintf(" %-13s %s", output.Dim("Label:"), label),
		fmt.Sprintf(" %-13s %s   %s", output.Dim("Health:"),
			gradeColor(bs.HealthGrade),
			output.Bold(fmt.Sprintf("%d / 100", bs.HealthScore))),
		fmt.Sprintf(" %-13s %s", output.Dim("Components:"), output.Cyan(fmt.Sprintf("%d", bs.TotalComps))),
		fmt.Sprintf(" %-13s %s", output.Dim("Files:"), output.Cyan(fmt.Sprintf("%d", bs.TotalFiles))),
		fmt.Sprintf(" %-13s %s", output.Dim("Cycles:"), output.Cyan(fmt.Sprintf("%d", bs.CycleCount))),
		fmt.Sprintf(" %-13s %s", output.Dim("Duplicates:"), output.Cyan(fmt.Sprintf("%d", bs.DupCount))),
		fmt.Sprintf(" %-13s %s", output.Dim("Issues:"), output.Cyan(fmt.Sprintf("%d", len(bs.Issues)))),
	}))
	fmt.Println()
	output.PrintHint("repokit baseline diff %s   to compare later", label)
	fmt.Println()
	return nil
}

func runBaselineList(cmd *cobra.Command, args []string) error {
	cwd, _ := os.Getwd()
	repoRoot, err := config.FindRepoRoot(cwd)
	if err != nil {
		output.PrintError("%v", err)
		return err
	}

	paths, err := listBaselineFiles(repoRoot)
	if err != nil {
		output.PrintError("Listing baselines: %v", err)
		return err
	}

	fmt.Println()
	fmt.Println(output.Banner("BASELINES", fmt.Sprintf("%d saved", len(paths))))
	fmt.Println()

	if len(paths) == 0 {
		fmt.Println(output.Box("Empty", []string{
			" " + output.Dim("No baselines saved yet."),
			" " + output.Dim("Run 'repokit baseline save [label]' to capture one."),
		}))
		fmt.Println()
		return nil
	}

	type entry struct {
		bs   *baselineSnapshot
		mod  time.Time
	}
	var entries []entry
	for _, p := range paths {
		bs, err := loadBaseline(p)
		if err != nil {
			continue
		}
		st, _ := os.Stat(p)
		entries = append(entries, entry{bs, st.ModTime()})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].mod.After(entries[j].mod) })

	var lines []string
	for _, e := range entries {
		_, gradeColor := getGrade(e.bs.HealthScore)
		when := e.mod.Local().Format("2006-01-02 15:04")
		lines = append(lines, fmt.Sprintf(" %s  %-24s %s %-3d/100   %s",
			output.Dim(when),
			output.Cyan(e.bs.Label),
			gradeColor(e.bs.HealthGrade),
			e.bs.HealthScore,
			output.Dim(fmt.Sprintf("(%d issues)", len(e.bs.Issues)))))
	}
	fmt.Println(output.Box("Saved Baselines", lines))
	fmt.Println()
	return nil
}

func runBaselineRm(cmd *cobra.Command, args []string) error {
	cwd, _ := os.Getwd()
	repoRoot, err := config.FindRepoRoot(cwd)
	if err != nil {
		output.PrintError("%v", err)
		return err
	}
	label := sanitizeLabel(args[0])
	path := baselinePath(repoRoot, label)
	if _, err := os.Stat(path); err != nil {
		output.PrintError("No baseline named '%s'", label)
		return err
	}
	if err := os.Remove(path); err != nil {
		output.PrintError("Removing baseline: %v", err)
		return err
	}
	output.PrintSuccess("Removed baseline '%s'", label)
	return nil
}

func runBaselineDiff(cmd *cobra.Command, args []string) error {
	cwd, _ := os.Getwd()
	repoRoot, err := config.FindRepoRoot(cwd)
	if err != nil {
		output.PrintError("%v", err)
		return err
	}

	var oldBS *baselineSnapshot
	var oldPath string
	if len(args) > 0 {
		label := sanitizeLabel(args[0])
		oldPath = baselinePath(repoRoot, label)
		oldBS, err = loadBaseline(oldPath)
		if err != nil {
			output.PrintError("Loading baseline '%s': %v", label, err)
			output.PrintHint("repokit baseline list")
			return err
		}
	} else {
		oldBS, oldPath, err = mostRecentBaseline(repoRoot)
		if err != nil {
			output.PrintError("%v", err)
			output.PrintHint("repokit baseline save [label]   to create one")
			return err
		}
	}

	s, err := store.Open(config.DBPath(repoRoot))
	if err != nil {
		output.PrintError("Opening index: %v", err)
		return err
	}
	defer s.Close()

	ctx := context.Background()
	curBS := gatherBaseline(ctx, s, "current")

	renderBaselineDiff(oldBS, curBS, oldPath)
	return nil
}

// ---------- diff rendering ----------

func renderBaselineDiff(old, cur *baselineSnapshot, oldPath string) {
	fmt.Println()
	subtitle := fmt.Sprintf("current ← %s", old.Label)
	fmt.Println(output.Banner("BASELINE DIFF", subtitle))
	fmt.Println()

	// Score delta box
	_, oldColor := getGrade(old.HealthScore)
	_, curColor := getGrade(cur.HealthScore)
	scoreDelta := cur.HealthScore - old.HealthScore
	deltaStr := formatDelta(scoreDelta, true)

	scoreLine := fmt.Sprintf(" %s %s %d   →   %s %s %d   %s",
		oldColor(old.HealthGrade),
		output.Dim("·"),
		old.HealthScore,
		curColor(cur.HealthGrade),
		output.Dim("·"),
		cur.HealthScore,
		deltaStr)

	metaLine := fmt.Sprintf(" %s baseline saved %s",
		output.Dim("·"),
		output.Dim(old.CreatedAt))

	fmt.Println(output.Box("Health", []string{scoreLine, metaLine}))

	// Metric deltas
	fmt.Println()
	fmt.Println(output.Box("Metric Changes", []string{
		formatMetricLine("Components:", old.TotalComps, cur.TotalComps, false),
		formatMetricLine("Files:     ", old.TotalFiles, cur.TotalFiles, false),
		formatMetricLine("Imports:   ", old.TotalImports, cur.TotalImports, false),
		formatMetricLine("Cycles:    ", old.CycleCount, cur.CycleCount, true),
		formatMetricLine("Duplicates:", old.DupCount, cur.DupCount, true),
		formatMetricLine("Issues:    ", len(old.Issues), len(cur.Issues), true),
	}))

	// Per-category penalty changes
	oldPenMap := make(map[string]baselinePenalty)
	for _, p := range old.Penalties {
		oldPenMap[p.Category] = p
	}
	curPenMap := make(map[string]baselinePenalty)
	for _, p := range cur.Penalties {
		curPenMap[p.Category] = p
	}
	allCats := make(map[string]bool)
	for k := range oldPenMap {
		allCats[k] = true
	}
	for k := range curPenMap {
		allCats[k] = true
	}
	if len(allCats) > 0 {
		var cats []string
		for k := range allCats {
			cats = append(cats, k)
		}
		sort.Strings(cats)
		var penLines []string
		for _, cat := range cats {
			o := oldPenMap[cat].Count
			c := curPenMap[cat].Count
			if o == c {
				continue
			}
			penLines = append(penLines, fmt.Sprintf(" %-24s %3d → %3d   %s",
				cat, o, c, formatDelta(c-o, true)))
		}
		if len(penLines) > 0 {
			fmt.Println()
			fmt.Println(output.Box("Penalty Categories (changes only)", penLines))
		}
	}

	// New vs resolved issues
	oldIssueMap := make(map[string]baselineIssue)
	for _, i := range old.Issues {
		oldIssueMap[i.Key] = i
	}
	curIssueMap := make(map[string]baselineIssue)
	for _, i := range cur.Issues {
		curIssueMap[i.Key] = i
	}

	var newIssues []baselineIssue
	for k, i := range curIssueMap {
		if _, ok := oldIssueMap[k]; !ok {
			newIssues = append(newIssues, i)
		}
	}
	var resolved []baselineIssue
	for k, i := range oldIssueMap {
		if _, ok := curIssueMap[k]; !ok {
			resolved = append(resolved, i)
		}
	}

	if len(resolved) > 0 {
		var lines []string
		maxShow := 8
		if len(resolved) < maxShow {
			maxShow = len(resolved)
		}
		for _, i := range resolved[:maxShow] {
			lines = append(lines, fmt.Sprintf(" %s  %-12s %s",
				output.Green("✓"), i.Kind, output.Dim(i.Key)))
		}
		if len(resolved) > maxShow {
			lines = append(lines, " "+output.Dim(fmt.Sprintf("... and %d more", len(resolved)-maxShow)))
		}
		fmt.Println()
		fmt.Println(output.Box(fmt.Sprintf("Resolved (%d)", len(resolved)), lines))
	}

	if len(newIssues) > 0 {
		var lines []string
		maxShow := 8
		if len(newIssues) < maxShow {
			maxShow = len(newIssues)
		}
		for _, i := range newIssues[:maxShow] {
			lines = append(lines, fmt.Sprintf(" %s  %-12s %s",
				output.Red("!"), i.Kind, output.Dim(i.Key)))
		}
		if len(newIssues) > maxShow {
			lines = append(lines, " "+output.Dim(fmt.Sprintf("... and %d more", len(newIssues)-maxShow)))
		}
		fmt.Println()
		fmt.Println(output.Box(fmt.Sprintf("New Issues (%d)", len(newIssues)), lines))
	}

	if len(resolved) == 0 && len(newIssues) == 0 && scoreDelta == 0 {
		fmt.Println()
		fmt.Println(output.Box("Summary", []string{
			" " + output.Dim("·") + " No changes between baseline and current state",
		}))
	}

	fmt.Println()
}

// formatDelta returns a colored "+N" / "-N" / "·" string.
// If lowerIsBetter, a negative delta is green; otherwise positive is green.
func formatDelta(delta int, lowerIsBetter bool) string {
	if delta == 0 {
		return output.Dim("·")
	}
	if delta > 0 {
		s := fmt.Sprintf("+%d", delta)
		if lowerIsBetter {
			return output.Red(s)
		}
		return output.Green(s)
	}
	s := fmt.Sprintf("%d", delta)
	if lowerIsBetter {
		return output.Green(s)
	}
	return output.Red(s)
}

func formatMetricLine(label string, oldV, curV int, lowerIsBetter bool) string {
	return fmt.Sprintf(" %s %s %s %s   %s",
		output.Dim(label),
		output.Cyan(fmt.Sprintf("%4d", oldV)),
		output.Dim("→"),
		output.Cyan(fmt.Sprintf("%4d", curV)),
		formatDelta(curV-oldV, lowerIsBetter))
}
