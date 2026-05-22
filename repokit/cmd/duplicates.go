package cmd

import (
	"context"
	"fmt"
	"math"
	"os"
	"sort"
	"strings"

	"github.com/spf13/cobra"
	"github.com/fcreme/CLI/repokit/internal/config"
	"github.com/fcreme/CLI/repokit/internal/output"
	"github.com/fcreme/CLI/repokit/internal/store"
)

var dupThreshold float64

var duplicatesCmd = &cobra.Command{
	Use:     "duplicates",
	Aliases: []string{"dup"},
	Short:   "Detect similar or duplicate components",
	Long: `Find components that are structurally similar, which may indicate
copy-paste code that should be consolidated into shared components.

Compares components by:
  - Name similarity (Levenshtein distance)
  - Size similarity (line count)
  - Import overlap (shared dependencies)
  - Prop overlap (shared prop names)
  - Same folder location

Examples:
  repokit duplicates
  repokit duplicates --threshold 0.5
  repokit dup`,
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

		// Load all components with their metadata
		components, err := loadComponentsForComparison(ctx, s)
		if err != nil {
			output.PrintError("Loading components: %v", err)
			return err
		}

		if len(components) < 2 {
			fmt.Println()
			output.PrintSuccess("Not enough components to compare")
			fmt.Println()
			return nil
		}

		// Compare all pairs
		var pairs []dupPair
		for i := 0; i < len(components); i++ {
			for j := i + 1; j < len(components); j++ {
				score := compareSimilarity(&components[i], &components[j])
				if score >= dupThreshold {
					pairs = append(pairs, dupPair{
						a:      &components[i],
						b:      &components[j],
						score:  score,
						reason: lastReason,
					})
				}
			}
		}

		sort.Slice(pairs, func(i, j int) bool {
			return pairs[i].score > pairs[j].score
		})

		fmt.Println()
		fmt.Println(output.Banner("DUPLICATE COMPONENTS", fmt.Sprintf("threshold %.0f%%", dupThreshold*100)))
		fmt.Println()

		if len(pairs) == 0 {
			fmt.Println(output.Box("Results", []string{
				" " + output.Green("✓") + " No similar components found",
			}))
			fmt.Println()
			return nil
		}

		maxShow := 15
		if len(pairs) < maxShow {
			maxShow = len(pairs)
		}

		var lines []string
		for _, p := range pairs[:maxShow] {
			pct := int(p.score * 100)
			scoreColor := output.Yellow
			if pct >= 80 {
				scoreColor = output.Red
			} else if pct < 50 {
				scoreColor = output.Dim
			}
			lines = append(lines, fmt.Sprintf(" %s  #%-4d %-18s ↔  #%-4d %-18s",
				scoreColor(fmt.Sprintf("%3d%%", pct)),
				p.a.id, p.a.name,
				p.b.id, p.b.name))
			if p.reason != "" {
				reason := p.reason
				if len(reason) > 60 {
					reason = reason[:57] + "..."
				}
				lines = append(lines, "        "+output.Dim(reason))
			}
		}
		if len(pairs) > maxShow {
			lines = append(lines, " "+output.Dim(fmt.Sprintf("... and %d more pairs", len(pairs)-maxShow)))
		}
		fmt.Println(output.Box(fmt.Sprintf("Similar Pairs (%d)", len(pairs)), lines))

		fmt.Println()
		output.PrintHint("repokit show <id> to inspect a component")
		output.PrintHint("use --threshold 0.8 for stricter matching")
		fmt.Println()

		return nil
	},
}

type compData struct {
	id      int64
	name    string
	path    string
	kind    string
	lines   int
	imports []string
	props   []string
}

type dupPair struct {
	a, b   *compData
	score  float64
	reason string
}

func loadComponentsForComparison(ctx context.Context, s *store.Store) ([]compData, error) {
	rows, err := s.DB().QueryContext(ctx, `
		SELECT c.id, c.name, f.path, c.kind, (c.line_end - c.line_start) as lines
		FROM components c
		JOIN files f ON c.file_id = f.id
		ORDER BY c.id
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var components []compData
	for rows.Next() {
		var c compData
		rows.Scan(&c.id, &c.name, &c.path, &c.kind, &c.lines)
		components = append(components, c)
	}

	// Load imports per file
	importsByFile := make(map[string][]string)
	irows, err := s.DB().QueryContext(ctx, `
		SELECT f.path, i.source_path
		FROM imports i
		JOIN files f ON i.file_id = f.id
		WHERE i.is_external = 1
	`)
	if err == nil {
		defer irows.Close()
		for irows.Next() {
			var path, source string
			irows.Scan(&path, &source)
			importsByFile[path] = append(importsByFile[path], source)
		}
	}

	// Load props per component
	propsByComp := make(map[int64][]string)
	prows, err := s.DB().QueryContext(ctx, `
		SELECT c.id, p.name
		FROM components c
		JOIN type_definitions td ON td.file_id = c.file_id
			AND (td.name = c.name || 'Props' OR td.name LIKE c.name || '%Props%')
		JOIN props p ON p.type_def_id = td.id
	`)
	if err == nil {
		defer prows.Close()
		for prows.Next() {
			var id int64
			var prop string
			prows.Scan(&id, &prop)
			propsByComp[id] = append(propsByComp[id], prop)
		}
	}

	// Attach to components
	for i := range components {
		components[i].imports = importsByFile[components[i].path]
		components[i].props = propsByComp[components[i].id]
	}

	return components, nil
}

func compareSimilarity(a, b *compData) float64 {
	var scores []float64
	var reasons []string

	// 1. Name similarity (Levenshtein-based)
	nameSim := 1.0 - float64(levenshtein(strings.ToLower(a.name), strings.ToLower(b.name)))/float64(max(len(a.name), len(b.name)))
	if nameSim > 0.5 {
		scores = append(scores, nameSim*0.25)
		reasons = append(reasons, fmt.Sprintf("names %.0f%% similar", nameSim*100))
	}

	// 2. Size similarity
	if a.lines > 0 && b.lines > 0 {
		sizeSim := 1.0 - math.Abs(float64(a.lines-b.lines))/float64(max(a.lines, b.lines))
		if sizeSim > 0.7 {
			scores = append(scores, sizeSim*0.15)
			reasons = append(reasons, fmt.Sprintf("size %d vs %d lines", a.lines, b.lines))
		}
	}

	// 3. Import overlap (Jaccard similarity)
	if len(a.imports) > 0 || len(b.imports) > 0 {
		importSim := jaccardSimilarity(a.imports, b.imports)
		if importSim > 0.3 {
			scores = append(scores, importSim*0.30)
			reasons = append(reasons, fmt.Sprintf("imports %.0f%% overlap", importSim*100))
		}
	}

	// 4. Prop overlap
	if len(a.props) > 0 && len(b.props) > 0 {
		propSim := jaccardSimilarity(a.props, b.props)
		if propSim > 0.3 {
			scores = append(scores, propSim*0.30)
			reasons = append(reasons, fmt.Sprintf("props %.0f%% overlap", propSim*100))
		}
	}

	// 5. Same folder bonus
	if dirOf(a.path) == dirOf(b.path) {
		scores = append(scores, 0.05)
	}

	// Same kind is expected, different kind makes them less likely duplicates
	if a.kind != b.kind {
		return 0
	}

	total := 0.0
	for _, s := range scores {
		total += s
	}

	// Normalize: if we have few signals, don't inflate
	if len(scores) <= 1 {
		total *= 0.5
	}

	// Cap at 1.0
	if total > 1.0 {
		total = 1.0
	}

	// Store reason as a global (hack via closure not possible, so we use a package var)
	lastReason = strings.Join(reasons, ", ")

	return total
}

var lastReason string

func jaccardSimilarity(a, b []string) float64 {
	if len(a) == 0 && len(b) == 0 {
		return 0
	}
	setA := make(map[string]bool)
	for _, s := range a {
		setA[s] = true
	}
	setB := make(map[string]bool)
	for _, s := range b {
		setB[s] = true
	}

	intersection := 0
	for k := range setA {
		if setB[k] {
			intersection++
		}
	}

	union := len(setA)
	for k := range setB {
		if !setA[k] {
			union++
		}
	}

	if union == 0 {
		return 0
	}
	return float64(intersection) / float64(union)
}

func dirOf(path string) string {
	for i := len(path) - 1; i >= 0; i-- {
		if path[i] == '/' || path[i] == '\\' {
			return path[:i]
		}
	}
	return ""
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func init() {
	duplicatesCmd.Flags().Float64Var(&dupThreshold, "threshold", 0.3, "Minimum similarity score (0.0-1.0)")
	rootCmd.AddCommand(duplicatesCmd)
}
