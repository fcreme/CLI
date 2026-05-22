package cmd

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/fatih/color"
	"github.com/spf13/cobra"
	"github.com/fcreme/CLI/repokit/internal/config"
	"github.com/fcreme/CLI/repokit/internal/output"
	"github.com/fcreme/CLI/repokit/internal/store"
)

var (
	depsOrphans  bool
	depsCircular bool
	depsTree     bool
	depsMermaid  bool
	depsReverse  bool
)

var depsCmd = &cobra.Command{
	Use:     "deps [component-id]",
	Aliases: []string{"d"},
	Short:   "Analyze dependency graph",
	Long: `Analyze the import dependency graph of the repository.

Without arguments, shows a summary of the dependency graph.
With a component ID, shows what it imports and what imports it.

Flags:
  --orphans     List components with zero imports (dead code candidates)
  --circular    Detect circular import chains
  --tree        Show full dependency tree
  --mermaid     Output as Mermaid diagram
  --reverse     Show reverse dependencies (who imports this)

Examples:
  repokit deps                  Show graph summary
  repokit deps 3                Show deps for component #3
  repokit deps --orphans        Find dead code
  repokit deps --circular       Detect circular imports
  repokit deps --tree           Full dependency tree
  repokit deps --mermaid        Mermaid diagram output`,
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

		edges, err := s.GetImportGraph(ctx)
		if err != nil {
			output.PrintError("Loading dependency graph: %v", err)
			return err
		}

		// Build adjacency lists
		graph := buildGraph(edges)

		if depsOrphans {
			return showOrphans(ctx, s, graph)
		}
		if depsCircular {
			return showCircular(graph)
		}
		if depsMermaid {
			return showMermaid(graph)
		}

		// Single component deps
		if len(args) > 0 {
			id, err := strconv.ParseInt(args[0], 10, 64)
			if err != nil {
				output.PrintError("Invalid component ID: %s", args[0])
				return err
			}
			return showComponentDeps(ctx, s, graph, id, depsReverse)
		}

		// Default: graph summary
		if depsTree {
			return showTree(graph)
		}
		if err := showGraphSummary(graph); err != nil {
			return err
		}
		output.PrintHint("repokit deps --orphans    find dead code")
		output.PrintHint("repokit deps --circular   detect circular imports")
		output.PrintHint("repokit deps --tree       full dependency tree")
		output.PrintHint("repokit deps --mermaid    export as diagram")
		fmt.Println()
		return nil
	},
}

type depGraph struct {
	forward  map[string][]depEdge // file -> imports
	reverse  map[string][]depEdge // file -> imported by
	allFiles map[string]bool
}

type depEdge struct {
	target string
	names  []string
}

func buildGraph(edges []store.ImportEdge) *depGraph {
	g := &depGraph{
		forward:  make(map[string][]depEdge),
		reverse:  make(map[string][]depEdge),
		allFiles: make(map[string]bool),
	}
	for _, e := range edges {
		g.forward[e.FromFile] = append(g.forward[e.FromFile], depEdge{target: e.ToFile, names: e.ImportedNames})
		g.reverse[e.ToFile] = append(g.reverse[e.ToFile], depEdge{target: e.FromFile, names: e.ImportedNames})
		g.allFiles[e.FromFile] = true
		g.allFiles[e.ToFile] = true
	}
	return g
}

func showGraphSummary(g *depGraph) error {
	totalEdges := 0
	for _, edges := range g.forward {
		totalEdges += len(edges)
	}

	// Find most imported files
	type fileCount struct {
		file  string
		count int
	}
	var mostImported []fileCount
	for file, importers := range g.reverse {
		mostImported = append(mostImported, fileCount{file, len(importers)})
	}
	sort.Slice(mostImported, func(i, j int) bool {
		return mostImported[i].count > mostImported[j].count
	})

	// Find most importing files
	var mostImporting []fileCount
	for file, imports := range g.forward {
		mostImporting = append(mostImporting, fileCount{file, len(imports)})
	}
	sort.Slice(mostImporting, func(i, j int) bool {
		return mostImporting[i].count > mostImporting[j].count
	})

	// Files with no internal importers (leaf/entry files)
	orphanCount := 0
	for file := range g.allFiles {
		if len(g.reverse[file]) == 0 && len(g.forward[file]) > 0 {
			orphanCount++
		}
	}

	fmt.Println()
	fmt.Printf("  %s\n", output.Bold("Dependency Graph Summary"))
	fmt.Printf("    Files in graph:    %s\n", output.Cyan(fmt.Sprintf("%d", len(g.allFiles))))
	fmt.Printf("    Internal imports:  %s\n", output.Cyan(fmt.Sprintf("%d", totalEdges)))
	fmt.Printf("    Entry points:      %s\n", output.Cyan(fmt.Sprintf("%d", orphanCount)))

	if len(mostImported) > 0 {
		fmt.Println()
		fmt.Printf("  %s\n", output.Bold("Most Imported (hub files):"))
		max := 8
		if len(mostImported) < max {
			max = len(mostImported)
		}
		for _, fc := range mostImported[:max] {
			bar := strings.Repeat("█", fc.count)
			fmt.Printf("    %s %s %s\n", output.Cyan(fmt.Sprintf("%2d", fc.count)), color.New(color.FgGreen).Sprint(bar), fc.file)
		}
	}

	if len(mostImporting) > 0 {
		fmt.Println()
		fmt.Printf("  %s\n", output.Bold("Most Dependencies (complex files):"))
		max := 8
		if len(mostImporting) < max {
			max = len(mostImporting)
		}
		for _, fc := range mostImporting[:max] {
			bar := strings.Repeat("█", fc.count)
			fmt.Printf("    %s %s %s\n", output.Cyan(fmt.Sprintf("%2d", fc.count)), color.New(color.FgYellow).Sprint(bar), fc.file)
		}
	}

	fmt.Println()
	return nil
}

func showComponentDeps(ctx context.Context, s *store.Store, g *depGraph, compID int64, reverse bool) error {
	comp, err := s.GetComponentByID(ctx, compID)
	if err != nil {
		output.PrintError("Component %d not found", compID)
		return err
	}

	fmt.Println()
	fmt.Printf("  %s %s (%s)\n", output.Bold("Component:"), output.Cyan(comp.Name), comp.FilePath)

	// Forward deps (what this file imports)
	if imports := g.forward[comp.FilePath]; len(imports) > 0 {
		fmt.Println()
		fmt.Printf("  %s (%d)\n", output.Bold("Imports:"), len(imports))
		for _, dep := range imports {
			names := ""
			if len(dep.names) > 0 {
				names = output.Dim(" {" + strings.Join(dep.names, ", ") + "}")
			}
			fmt.Printf("    → %s%s\n", dep.target, names)
		}
	}

	// Reverse deps (what imports this file)
	if importers := g.reverse[comp.FilePath]; len(importers) > 0 {
		fmt.Println()
		fmt.Printf("  %s (%d)\n", output.Bold("Imported by:"), len(importers))
		for _, dep := range importers {
			names := ""
			if len(dep.names) > 0 {
				names = output.Dim(" {" + strings.Join(dep.names, ", ") + "}")
			}
			fmt.Printf("    ← %s%s\n", dep.target, names)
		}
	}

	if len(g.forward[comp.FilePath]) == 0 && len(g.reverse[comp.FilePath]) == 0 {
		output.PrintWarning("No internal dependencies found for this component")
	}

	fmt.Println()
	return nil
}

func showOrphans(ctx context.Context, s *store.Store, g *depGraph) error {
	// Find files that are never imported by anything
	compFiles, err := s.GetComponentFiles(ctx)
	if err != nil {
		return err
	}

	var orphans []string
	for name, file := range compFiles {
		if len(g.reverse[file]) == 0 {
			orphans = append(orphans, fmt.Sprintf("%s (%s)", name, file))
		}
	}

	sort.Strings(orphans)

	fmt.Println()
	if len(orphans) == 0 {
		output.PrintSuccess("No orphan components found - all components are imported somewhere")
		return nil
	}

	fmt.Printf("  %s\n", output.Bold("Orphan Components (never imported internally):"))
	fmt.Println()
	for _, o := range orphans {
		fmt.Printf("    %s %s\n", output.Yellow("●"), o)
	}
	fmt.Println()
	output.PrintInfo("%d components with no internal importers", len(orphans))
	output.PrintInfo("These may be entry points, pages, or dead code")
	fmt.Println()
	return nil
}

func showCircular(g *depGraph) error {
	cycles := detectCycles(g)

	fmt.Println()
	if len(cycles) == 0 {
		output.PrintSuccess("No circular dependencies detected")
		fmt.Println()
		return nil
	}

	fmt.Printf("  %s\n", output.Bold("Circular Dependencies:"))
	fmt.Println()
	for i, cycle := range cycles {
		fmt.Printf("  %s Cycle %d:\n", output.Red("●"), i+1)
		for j, file := range cycle {
			arrow := "→"
			if j == len(cycle)-1 {
				arrow = "↩"
			}
			fmt.Printf("      %s %s\n", arrow, file)
		}
		fmt.Println()
	}
	output.PrintWarning("%d circular dependency chains detected", len(cycles))
	fmt.Println()
	return nil
}

func detectCycles(g *depGraph) [][]string {
	var cycles [][]string
	visited := make(map[string]int) // 0=unvisited, 1=in-stack, 2=done
	stack := make([]string, 0)

	var dfs func(node string)
	dfs = func(node string) {
		if visited[node] == 2 {
			return
		}
		if visited[node] == 1 {
			// Found cycle - extract it
			cycleStart := -1
			for i, s := range stack {
				if s == node {
					cycleStart = i
					break
				}
			}
			if cycleStart >= 0 {
				cycle := make([]string, len(stack)-cycleStart)
				copy(cycle, stack[cycleStart:])
				cycle = append(cycle, node)
				cycles = append(cycles, cycle)
			}
			return
		}

		visited[node] = 1
		stack = append(stack, node)

		for _, edge := range g.forward[node] {
			dfs(edge.target)
		}

		stack = stack[:len(stack)-1]
		visited[node] = 2
	}

	for file := range g.allFiles {
		if visited[file] == 0 {
			dfs(file)
		}
	}

	return cycles
}

func showTree(g *depGraph) error {
	// Find root files (files that nothing imports)
	var roots []string
	for file := range g.allFiles {
		if len(g.reverse[file]) == 0 {
			roots = append(roots, file)
		}
	}
	sort.Strings(roots)

	fmt.Println()
	fmt.Printf("  %s\n", output.Bold("Dependency Tree"))
	fmt.Println()

	visited := make(map[string]bool)
	for _, root := range roots {
		printTree(g, root, "", true, visited)
	}
	fmt.Println()
	return nil
}

func printTree(g *depGraph, file string, prefix string, isLast bool, visited map[string]bool) {
	connector := "├── "
	if isLast {
		connector = "└── "
	}
	fmt.Printf("  %s%s%s\n", prefix, connector, file)

	if visited[file] {
		childPrefix := prefix + "│   "
		if isLast {
			childPrefix = prefix + "    "
		}
		fmt.Printf("  %s%s\n", childPrefix, output.Dim("(circular ref)"))
		return
	}
	visited[file] = true

	children := g.forward[file]
	for i, child := range children {
		childPrefix := prefix + "│   "
		if isLast {
			childPrefix = prefix + "    "
		}
		printTree(g, child.target, childPrefix, i == len(children)-1, visited)
	}

	delete(visited, file) // allow revisiting in other branches
}

func showMermaid(g *depGraph) error {
	fmt.Println("graph TD")

	// Create short IDs for files
	ids := make(map[string]string)
	counter := 0
	getID := func(file string) string {
		if id, ok := ids[file]; ok {
			return id
		}
		counter++
		id := fmt.Sprintf("F%d", counter)
		ids[file] = id
		// Use filename as label
		parts := strings.Split(file, "/")
		label := parts[len(parts)-1]
		fmt.Printf("    %s[\"%s\"]\n", id, label)
		return id
	}

	for from, edges := range g.forward {
		fromID := getID(from)
		for _, edge := range edges {
			toID := getID(edge.target)
			fmt.Printf("    %s --> %s\n", fromID, toID)
		}
	}

	return nil
}

func init() {
	depsCmd.Flags().BoolVar(&depsOrphans, "orphans", false, "List components with no internal importers")
	depsCmd.Flags().BoolVar(&depsCircular, "circular", false, "Detect circular import chains")
	depsCmd.Flags().BoolVar(&depsTree, "tree", false, "Show full dependency tree")
	depsCmd.Flags().BoolVar(&depsMermaid, "mermaid", false, "Output as Mermaid diagram")
	depsCmd.Flags().BoolVar(&depsReverse, "reverse", false, "Show reverse dependencies")

	rootCmd.AddCommand(depsCmd)
}
