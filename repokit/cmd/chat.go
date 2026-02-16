package cmd

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"

	"github.com/fatih/color"
	"github.com/spf13/cobra"
	"github.com/usuario/repokit/internal/config"
	"github.com/usuario/repokit/internal/output"
	"github.com/usuario/repokit/internal/scaffold"
	"github.com/usuario/repokit/internal/store"
	"github.com/usuario/repokit/internal/suggest"
)

var chatCmd = &cobra.Command{
	Use:   "chat",
	Short: "Interactive repo-aware command interface",
	Long: `Start an interactive session that maps natural language to repokit commands.

This is a command interface, not an AI chatbot. It uses pattern matching
to interpret your intent and execute the appropriate repokit operations.

Examples of what you can say:
  "find all modals"           -> searches components tagged as modal
  "show me the navbar"        -> finds and displays navbar component
  "create a form like LoginForm" -> scaffolds based on LoginForm
  "add onClick to Button"     -> suggests a prop addition
  "what stack is this?"       -> shows detected technologies
  "help"                      -> shows available commands`,
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

		bold := color.New(color.Bold)
		dim := color.New(color.Faint)
		cyan := color.New(color.FgCyan)

		fmt.Println()
		bold.Println("  repokit interactive mode")
		dim.Println("  Type 'help' for commands, 'exit' to quit")
		fmt.Println()

		scanner := bufio.NewScanner(os.Stdin)
		var lastResults []int64 // track component IDs from last search

		for {
			cyan.Print("  repokit> ")
			if !scanner.Scan() {
				break
			}

			input := strings.TrimSpace(scanner.Text())
			if input == "" {
				continue
			}

			lower := strings.ToLower(input)

			switch {
			case lower == "exit" || lower == "quit" || lower == "q":
				fmt.Println("  Goodbye!")
				return nil

			case lower == "help" || lower == "h" || lower == "?":
				printChatHelp()

			case lower == "stack" || lower == "what stack" || strings.Contains(lower, "stack"):
				handleChatStack(ctx, s)

			case lower == "status":
				handleChatStatus(ctx, s, repoRoot)

			case strings.HasPrefix(lower, "find ") || strings.HasPrefix(lower, "search ") ||
				strings.HasPrefix(lower, "show me ") || strings.HasPrefix(lower, "list "):
				lastResults = handleChatFind(ctx, s, input)

			case strings.HasPrefix(lower, "show ") || strings.HasPrefix(lower, "detail ") ||
				strings.HasPrefix(lower, "inspect "):
				handleChatShow(ctx, s, input, lastResults)

			case strings.HasPrefix(lower, "create ") || strings.HasPrefix(lower, "scaffold ") ||
				strings.HasPrefix(lower, "generate ") || strings.HasPrefix(lower, "new "):
				handleChatScaffold(ctx, s, input, repoRoot, lastResults)

			case strings.HasPrefix(lower, "add ") || strings.HasPrefix(lower, "suggest "):
				handleChatSuggest(input, repoRoot)

			case lower == "components" || lower == "all":
				lastResults = handleChatFind(ctx, s, "find all")

			case lower == "hooks":
				lastResults = handleChatFind(ctx, s, "find hooks")

			default:
				// Try to interpret as a component search
				if len(input) > 1 {
					lastResults = handleChatFind(ctx, s, "find "+input)
				} else {
					dim.Println("  I didn't understand that. Type 'help' for commands.")
				}
			}

			fmt.Println()
		}

		return nil
	},
}

func printChatHelp() {
	dim := color.New(color.Faint)
	bold := color.New(color.Bold)

	fmt.Println()
	bold.Println("  Available commands:")
	fmt.Println()
	fmt.Printf("  %-35s %s\n", "find <query>", "Search for components")
	fmt.Printf("  %-35s %s\n", "find --tag modal|form|table|...", "Search by tag")
	fmt.Printf("  %-35s %s\n", "hooks", "List all hooks")
	fmt.Printf("  %-35s %s\n", "show <id>", "Show component details")
	fmt.Printf("  %-35s %s\n", "create <Name> like <id|name>", "Scaffold a component")
	fmt.Printf("  %-35s %s\n", "add <prop> to <file>", "Suggest a code change")
	fmt.Printf("  %-35s %s\n", "stack", "Show detected technologies")
	fmt.Printf("  %-35s %s\n", "status", "Show index summary")
	fmt.Printf("  %-35s %s\n", "help", "Show this help")
	fmt.Printf("  %-35s %s\n", "exit", "Quit chat mode")
	fmt.Println()
	dim.Println("  You can also just type a component name to search for it.")
}

func handleChatStack(ctx context.Context, s *store.Store) {
	items, _ := s.GetStack(ctx)
	if len(items) == 0 {
		output.PrintWarning("No stack detected. Run 'repokit stack' first.")
		return
	}
	table := &output.Table{
		Headers: []string{"Category", "Technology", "Version"},
	}
	for _, item := range items {
		v := item.Version
		if v == "" {
			v = "-"
		}
		table.Rows = append(table.Rows, []string{item.Category, item.Name, v})
	}
	fmt.Println()
	table.Print()
}

func handleChatStatus(ctx context.Context, s *store.Store, repoRoot string) {
	var fc, cc, hc int
	s.DB().QueryRowContext(ctx, "SELECT COUNT(*) FROM files").Scan(&fc)
	s.DB().QueryRowContext(ctx, "SELECT COUNT(*) FROM components WHERE kind != 'hook'").Scan(&cc)
	s.DB().QueryRowContext(ctx, "SELECT COUNT(*) FROM components WHERE kind = 'hook'").Scan(&hc)

	fmt.Printf("  %s files, %s components, %s hooks indexed\n",
		output.Cyan(fmt.Sprintf("%d", fc)),
		output.Cyan(fmt.Sprintf("%d", cc)),
		output.Cyan(fmt.Sprintf("%d", hc)),
	)
}

func handleChatFind(ctx context.Context, s *store.Store, input string) []int64 {
	lower := strings.ToLower(input)
	query := ""
	kind := ""
	var tags []string

	// Parse input
	if strings.Contains(lower, "hook") {
		kind = "hook"
	}

	// Tag extraction
	tagKeywords := []string{"modal", "form", "table", "card", "layout", "page", "button", "list", "provider", "chart", "util"}
	for _, tag := range tagKeywords {
		if strings.Contains(lower, tag) {
			tags = append(tags, tag)
		}
	}

	// Extract search term (remove command words)
	cleanRe := regexp.MustCompile(`(?i)^(?:find|search|show me|list|all)\s+(?:all\s+)?(?:the\s+)?`)
	query = cleanRe.ReplaceAllString(input, "")
	query = strings.TrimSpace(query)

	// Don't search for tag words
	for _, tag := range tags {
		query = strings.ReplaceAll(strings.ToLower(query), tag+"s", "")
		query = strings.ReplaceAll(strings.ToLower(query), tag, "")
	}
	if kind != "" {
		query = strings.ReplaceAll(strings.ToLower(query), "hooks", "")
		query = strings.ReplaceAll(strings.ToLower(query), "hook", "")
	}
	query = strings.TrimSpace(query)

	components, err := s.SearchComponents(ctx, query, kind, tags, 20)
	if err != nil {
		output.PrintError("Search failed: %v", err)
		return nil
	}

	if len(components) == 0 {
		fmt.Println("  No components found.")
		return nil
	}

	var ids []int64
	table := &output.Table{
		Headers: []string{"#", "ID", "Name", "Kind", "Path"},
	}
	for i, c := range components {
		ids = append(ids, c.ID)
		tagStr := ""
		if len(c.Tags) > 0 {
			var tn []string
			for _, t := range c.Tags {
				tn = append(tn, t.Name)
			}
			tagStr = " [" + strings.Join(tn, ",") + "]"
		}
		table.Rows = append(table.Rows, []string{
			fmt.Sprintf("%d", i+1),
			fmt.Sprintf("%d", c.ID),
			c.Name + tagStr,
			string(c.Kind),
			c.FilePath,
		})
	}
	fmt.Println()
	table.Print()

	return ids
}

func handleChatShow(ctx context.Context, s *store.Store, input string, lastResults []int64) {
	// Extract ID
	re := regexp.MustCompile(`\d+`)
	match := re.FindString(input)
	if match == "" {
		fmt.Println("  Please specify a component ID: show <id>")
		return
	}

	id, _ := strconv.ParseInt(match, 10, 64)

	// Check if it's a result index (#1, #2) vs actual ID
	if id <= int64(len(lastResults)) && id > 0 && len(lastResults) > 0 {
		// Could be a result index; ask or assume
		// For small numbers when we have results, use as index
		idx := id - 1
		if idx < int64(len(lastResults)) {
			id = lastResults[idx]
		}
	}

	comp, err := s.GetComponentByID(ctx, id)
	if err != nil {
		output.PrintError("Component %d not found", id)
		return
	}

	fmt.Println()
	fmt.Printf("  %s %s (%s)\n", output.Bold(comp.Name), output.Dim(string(comp.Kind)), output.Cyan(comp.FilePath))
	fmt.Printf("  Lines %d-%d, export: %s, usage: %d\n", comp.LineStart, comp.LineEnd, comp.ExportType, comp.UsageCount)

	if len(comp.Tags) > 0 {
		var tn []string
		for _, t := range comp.Tags {
			tn = append(tn, t.Name)
		}
		fmt.Printf("  Tags: %s\n", output.Green(strings.Join(tn, ", ")))
	}

	if comp.RawSource != "" {
		lines := strings.Split(comp.RawSource, "\n")
		max := 15
		if len(lines) < max {
			max = len(lines)
		}
		fmt.Println()
		for i := 0; i < max; i++ {
			fmt.Printf("  %s %s\n", output.Dim(fmt.Sprintf("%3d", comp.LineStart+i)), lines[i])
		}
		if len(lines) > max {
			fmt.Printf("  %s\n", output.Dim(fmt.Sprintf("  ... +%d more lines", len(lines)-max)))
		}
	}
}

func handleChatScaffold(ctx context.Context, s *store.Store, input string, repoRoot string, lastResults []int64) {
	// Parse: "create CustomerModal like 5" or "create CustomerModal like Navbar"
	re := regexp.MustCompile(`(?i)(?:create|scaffold|generate|new)\s+(\w+)\s+(?:like|from|based on|based)\s+(\w+)`)
	m := re.FindStringSubmatch(input)
	if m == nil {
		fmt.Println("  Usage: create <NewName> like <component-id or name>")
		fmt.Println("  Example: create CustomerModal like 5")
		return
	}

	newName := m[1]
	baseRef := m[2]

	// Try as ID first
	var baseID int64
	if id, err := strconv.ParseInt(baseRef, 10, 64); err == nil {
		baseID = id
	} else {
		// Search by name
		comps, err := s.SearchComponents(ctx, baseRef, "", nil, 1)
		if err != nil || len(comps) == 0 {
			output.PrintError("Component '%s' not found", baseRef)
			return
		}
		baseID = comps[0].ID
	}

	req := scaffold.ScaffoldRequest{
		NewName:   newName,
		BasedOnID: baseID,
		Write:     false,
		RepoRoot:  repoRoot,
	}

	result, err := scaffold.Scaffold(ctx, s, req)
	if err != nil {
		output.PrintError("Scaffold failed: %v", err)
		return
	}

	fmt.Println()
	fmt.Printf("  %s %s\n", output.Bold("Target:"), result.FilePath)
	fmt.Println()
	fmt.Println(output.Dim("  --- generated code ---"))
	for _, line := range strings.Split(result.Content, "\n") {
		fmt.Printf("  %s\n", line)
	}
	fmt.Println(output.Dim("  --- end ---"))
	fmt.Println()
	fmt.Printf("  Run %s to write the file.\n", output.Cyan(fmt.Sprintf("repokit scaffold component %s --based %d --write", newName, baseID)))
}

func handleChatSuggest(input string, repoRoot string) {
	// Parse: "add onClick prop to components/Button.tsx"
	re := regexp.MustCompile(`(?i)(?:add|suggest)\s+(.+?)\s+(?:to|in|for)\s+(\S+)`)
	m := re.FindStringSubmatch(input)
	if m == nil {
		fmt.Println("  Usage: add <description> to <file-path>")
		fmt.Println("  Example: add onClick prop to components/Button.tsx")
		return
	}

	description := m[1]
	filePath := m[2]

	req := suggest.SuggestRequest{
		Description: description,
		FilePath:    filePath,
		RepoRoot:    repoRoot,
		Apply:       false,
	}

	result, err := suggest.Suggest(req)
	if err != nil {
		output.PrintError("Suggest failed: %v", err)
		return
	}

	fmt.Println()
	fmt.Printf("  %s %s on %s\n", output.Bold("Strategy:"), result.Strategy, result.FilePath)
	fmt.Println()
	printColorDiff(result.UnifiedDiff)
	fmt.Println()
	fmt.Printf("  Use %s to apply.\n",
		output.Cyan(fmt.Sprintf(`repokit suggest "%s" --file %s --apply`, description, filePath)))
}

func init() {
	rootCmd.AddCommand(chatCmd)
}
