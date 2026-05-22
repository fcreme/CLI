package cmd

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"

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

	type fileCount struct {
		file  string
		count int
	}
	var mostImported, mostImporting []fileCount
	for file, importers := range g.reverse {
		mostImported = append(mostImported, fileCount{file, len(importers)})
	}
	for file, imports := range g.forward {
		mostImporting = append(mostImporting, fileCount{file, len(imports)})
	}
	sort.Slice(mostImported, func(i, j int) bool { return mostImported[i].count > mostImported[j].count })
	sort.Slice(mostImporting, func(i, j int) bool { return mostImporting[i].count > mostImporting[j].count })

	orphanCount := 0
	for file := range g.allFiles {
		if len(g.reverse[file]) == 0 && len(g.forward[file]) > 0 {
			orphanCount++
		}
	}

	fmt.Println()
	fmt.Println(output.Banner("DEPENDENCY GRAPH", fmt.Sprintf("%d files", len(g.allFiles))))
	fmt.Println()

	fmt.Println(output.Box("Summary", []string{
		fmt.Sprintf(" %-20s %s", output.Dim("Files in graph:"), output.Cyan(fmt.Sprintf("%d", len(g.allFiles)))),
		fmt.Sprintf(" %-20s %s", output.Dim("Internal imports:"), output.Cyan(fmt.Sprintf("%d", totalEdges))),
		fmt.Sprintf(" %-20s %s", output.Dim("Entry points:"), output.Cyan(fmt.Sprintf("%d", orphanCount))),
	}))

	if len(mostImported) > 0 {
		max := 8
		if len(mostImported) < max {
			max = len(mostImported)
		}
		maxC := mostImported[0].count
		var lines []string
		for _, fc := range mostImported[:max] {
			file := fc.file
			if len(file) > 32 {
				file = "..." + file[len(file)-29:]
			}
			lines = append(lines, fmt.Sprintf(" %s  %-24s  %s",
				output.Cyan(fmt.Sprintf("%3d", fc.count)),
				output.Green(scaleBar(fc.count, maxC, 24)),
				file))
		}
		fmt.Println()
		fmt.Println(output.Box("Most Imported (hub files)", lines))
	}

	if len(mostImporting) > 0 {
		max := 8
		if len(mostImporting) < max {
			max = len(mostImporting)
		}
		maxC := mostImporting[0].count
		var lines []string
		for _, fc := range mostImporting[:max] {
			file := fc.file
			if len(file) > 32 {
				file = "..." + file[len(file)-29:]
			}
			lines = append(lines, fmt.Sprintf(" %s  %-24s  %s",
				output.Cyan(fmt.Sprintf("%3d", fc.count)),
				output.Yellow(scaleBar(fc.count, maxC, 24)),
				file))
		}
		fmt.Println()
		fmt.Println(output.Box("Most Dependencies (complex files)", lines))
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
	fmt.Println(output.Banner("ORPHAN COMPONENTS", fmt.Sprintf("%d found", len(orphans))))
	fmt.Println()

	if len(orphans) == 0 {
		fmt.Println(output.Box("Results", []string{
			" " + output.Green("✓") + " All components are imported somewhere",
		}))
		fmt.Println()
		return nil
	}

	var lines []string
	maxShow := 20
	if len(orphans) < maxShow {
		maxShow = len(orphans)
	}
	for _, o := range orphans[:maxShow] {
		entry := o
		if len(entry) > 66 {
			entry = entry[:63] + "..."
		}
		lines = append(lines, fmt.Sprintf(" %s %s", output.Yellow("●"), entry))
	}
	if len(orphans) > maxShow {
		lines = append(lines, " "+output.Dim(fmt.Sprintf("... and %d more", len(orphans)-maxShow)))
	}
	fmt.Println(output.Box("Components Never Imported Internally", lines))
	fmt.Println()
	output.PrintInfo("These may be entry points, pages, or dead code")
	fmt.Println()
	return nil
}

func showCircular(g *depGraph) error {
	cycles := detectCycles(g)

	fmt.Println()
	fmt.Println(output.Banner("CIRCULAR DEPENDENCIES", fmt.Sprintf("%d cycle(s)", len(cycles))))
	fmt.Println()

	if len(cycles) == 0 {
		fmt.Println(output.Box("Results", []string{
			" " + output.Green("✓") + " No circular dependencies detected",
		}))
		fmt.Println()
		return nil
	}

	var lines []string
	for i, cycle := range cycles {
		lines = append(lines, fmt.Sprintf(" %s Cycle %d", output.Red("●"), i+1))
		for j, file := range cycle {
			arrow := "→"
			if j == len(cycle)-1 {
				arrow = "↩"
			}
			f := file
			if len(f) > 60 {
				f = "..." + f[len(f)-57:]
			}
			lines = append(lines, fmt.Sprintf("     %s %s", output.Red(arrow), f))
		}
		if i < len(cycles)-1 {
			lines = append(lines, "")
		}
	}
	fmt.Println(output.Box("Cycles", lines))
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
