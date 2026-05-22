package cmd

import (
	"context"
	"fmt"
	"html/template"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/fcreme/CLI/repokit/internal/config"
	"github.com/fcreme/CLI/repokit/internal/output"
	"github.com/fcreme/CLI/repokit/internal/store"
)

var (
	exportFormat string
	exportOpen   bool
)

var exportCmd = &cobra.Command{
	Use:   "export",
	Short: "Export project report",
	Long: `Generate a project report in HTML or Markdown format.

The HTML report is a standalone file you can open in a browser or share.

Examples:
  repokit export --html
  repokit export --html --open
  repokit export --md`,
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
		data, err := gatherReportData(ctx, s, repoRoot)
		if err != nil {
			output.PrintError("Gathering data: %v", err)
			return err
		}

		// Handle shorthand flags
		if html, _ := cmd.Flags().GetBool("html"); html {
			exportFormat = "html"
		}
		if md, _ := cmd.Flags().GetBool("md"); md {
			exportFormat = "md"
		}

		var outPath string
		switch exportFormat {
		case "html":
			outPath, err = exportHTML(data, repoRoot)
		case "md":
			outPath, err = exportMarkdown(data, repoRoot)
		default:
			output.PrintError("Unknown format: %s (use --html or --md)", exportFormat)
			return fmt.Errorf("unknown format")
		}

		if err != nil {
			output.PrintError("Export failed: %v", err)
			return err
		}

		fmt.Println()
		output.PrintSuccess("Report exported to: %s", outPath)

		if exportOpen {
			openInBrowser(outPath)
		} else {
			output.PrintHint("add --open to open in browser")
		}
		fmt.Println()

		return nil
	},
}

type reportData struct {
	ProjectName   string
	ProjectPath   string
	GeneratedAt   string
	Version       string
	HealthScore   int
	HealthGrade   string
	TotalFiles    int
	TotalComps    int
	TotalHooks    int
	TotalImports  int
	TotalExports  int
	TotalTypes    int
	ExtImports    int
	IntImports    int
	Stack         []stackEntry
	Components    []reportComponent
	BiggestComps  []reportComponent
	FolderCounts  []folderCount
	LintIssues    int
}

type stackEntry struct {
	Category string
	Name     string
	Version  string
}

type reportComponent struct {
	ID       int64
	Name     string
	Kind     string
	Path     string
	Lines    int
	Export   string
}

type folderCount struct {
	Folder string
	Count  int
}

func gatherReportData(ctx context.Context, s *store.Store, repoRoot string) (*reportData, error) {
	data := &reportData{
		ProjectName: filepath.Base(repoRoot),
		ProjectPath: repoRoot,
		GeneratedAt: time.Now().Format("2006-01-02 15:04:05"),
		Version:     Version,
	}

	s.DB().QueryRowContext(ctx, "SELECT COUNT(*) FROM files").Scan(&data.TotalFiles)
	s.DB().QueryRowContext(ctx, "SELECT COUNT(*) FROM components WHERE kind != 'hook'").Scan(&data.TotalComps)
	s.DB().QueryRowContext(ctx, "SELECT COUNT(*) FROM components WHERE kind = 'hook'").Scan(&data.TotalHooks)
	s.DB().QueryRowContext(ctx, "SELECT COUNT(*) FROM imports").Scan(&data.TotalImports)
	s.DB().QueryRowContext(ctx, "SELECT COUNT(*) FROM exports").Scan(&data.TotalExports)
	s.DB().QueryRowContext(ctx, "SELECT COUNT(*) FROM type_definitions").Scan(&data.TotalTypes)
	s.DB().QueryRowContext(ctx, "SELECT COUNT(*) FROM imports WHERE is_external = 1").Scan(&data.ExtImports)
	s.DB().QueryRowContext(ctx, "SELECT COUNT(*) FROM imports WHERE is_external = 0").Scan(&data.IntImports)

	// Health score (simplified version of health command logic)
	data.HealthScore = 100
	var oversized, propBloat, multiComp, unused, missingProps int
	s.DB().QueryRowContext(ctx, "SELECT COUNT(*) FROM components WHERE (line_end - line_start) > 300").Scan(&oversized)
	s.DB().QueryRowContext(ctx, `SELECT COUNT(*) FROM (SELECT td.id FROM type_definitions td JOIN props p ON p.type_def_id = td.id WHERE td.name LIKE '%Props%' GROUP BY td.id HAVING COUNT(p.id) > 10)`).Scan(&propBloat)
	s.DB().QueryRowContext(ctx, `SELECT COUNT(*) FROM (SELECT f.id FROM files f JOIN components c ON c.file_id = f.id WHERE c.kind != 'hook' GROUP BY f.id HAVING COUNT(c.id) > 2)`).Scan(&multiComp)
	s.DB().QueryRowContext(ctx, `SELECT COUNT(*) FROM exports e JOIN files f ON e.file_id = f.id WHERE e.kind IN ('function','const','class') AND e.name != 'default' AND NOT EXISTS (SELECT 1 FROM imports i WHERE i.imported_names LIKE '%' || e.name || '%' AND i.file_id != e.file_id)`).Scan(&unused)
	s.DB().QueryRowContext(ctx, `SELECT COUNT(*) FROM components c WHERE c.kind = 'function_component' AND c.export_type IN ('default','named') AND NOT EXISTS (SELECT 1 FROM type_definitions td WHERE td.file_id = c.file_id AND (td.name = c.name || 'Props' OR td.name LIKE '%Props%'))`).Scan(&missingProps)

	data.HealthScore -= minInt(oversized*5, 20)
	data.HealthScore -= minInt(propBloat*3, 15)
	data.HealthScore -= minInt(multiComp*2, 10)
	data.HealthScore -= minInt(unused*2, 15)
	data.HealthScore -= minInt(missingProps, 10)
	data.LintIssues = oversized + propBloat + multiComp + unused + missingProps
	if data.HealthScore < 0 {
		data.HealthScore = 0
	}

	switch {
	case data.HealthScore >= 90:
		data.HealthGrade = "A"
	case data.HealthScore >= 80:
		data.HealthGrade = "B"
	case data.HealthScore >= 70:
		data.HealthGrade = "C"
	case data.HealthScore >= 60:
		data.HealthGrade = "D"
	default:
		data.HealthGrade = "F"
	}

	// Stack
	stackRows, err := s.DB().QueryContext(ctx, "SELECT category, name, version FROM stack ORDER BY category, name")
	if err == nil {
		defer stackRows.Close()
		for stackRows.Next() {
			var se stackEntry
			stackRows.Scan(&se.Category, &se.Name, &se.Version)
			data.Stack = append(data.Stack, se)
		}
	}

	// All components
	compRows, err := s.DB().QueryContext(ctx, `
		SELECT c.id, c.name, c.kind, f.path, (c.line_end - c.line_start) as lines, c.export_type
		FROM components c JOIN files f ON c.file_id = f.id
		ORDER BY c.name
	`)
	if err == nil {
		defer compRows.Close()
		for compRows.Next() {
			var rc reportComponent
			compRows.Scan(&rc.ID, &rc.Name, &rc.Kind, &rc.Path, &rc.Lines, &rc.Export)
			data.Components = append(data.Components, rc)
		}
	}

	// Biggest components
	bigRows, err := s.DB().QueryContext(ctx, `
		SELECT c.id, c.name, c.kind, f.path, (c.line_end - c.line_start) as lines, c.export_type
		FROM components c JOIN files f ON c.file_id = f.id
		ORDER BY lines DESC LIMIT 10
	`)
	if err == nil {
		defer bigRows.Close()
		for bigRows.Next() {
			var rc reportComponent
			bigRows.Scan(&rc.ID, &rc.Name, &rc.Kind, &rc.Path, &rc.Lines, &rc.Export)
			data.BiggestComps = append(data.BiggestComps, rc)
		}
	}

	// Folder counts
	folders := make(map[string]int)
	fRows, err := s.DB().QueryContext(ctx, "SELECT path FROM files")
	if err == nil {
		defer fRows.Close()
		for fRows.Next() {
			var p string
			fRows.Scan(&p)
			dir := filepath.Dir(p)
			if dir == "." {
				dir = "(root)"
			}
			folders[dir]++
		}
	}
	for f, c := range folders {
		data.FolderCounts = append(data.FolderCounts, folderCount{f, c})
	}
	sort.Slice(data.FolderCounts, func(i, j int) bool {
		return data.FolderCounts[i].Count > data.FolderCounts[j].Count
	})

	return data, nil
}

func exportHTML(data *reportData, repoRoot string) (string, error) {
	funcMap := template.FuncMap{
		"lower": strings.ToLower,
		"ge": func(a, b int) bool { return a >= b },
		"healthRingOffset": func(score int) int {
			// Circle circumference = 2 * pi * 46 ≈ 289
			// offset = circumference * (1 - score/100)
			return 289 - (289 * score / 100)
		},
		"importPercent": func(ext, int_ int) int {
			total := ext + int_
			if total == 0 {
				return 50
			}
			return ext * 100 / total
		},
		"internalPercent": func(ext, int_ int) int {
			total := ext + int_
			if total == 0 {
				return 50
			}
			return int_ * 100 / total
		},
		"barHTML": func(lines int) template.HTML {
			width := lines / 3
			if width > 200 {
				width = 200
			}
			if width < 4 {
				width = 4
			}
			cls := "bar-green"
			if lines > 300 {
				cls = "bar-red"
			} else if lines > 150 {
				cls = "bar-yellow"
			}
			return template.HTML(fmt.Sprintf(`<span class="bar %s" style="width:%dpx"></span>`, cls, width))
		},
		"folderBarHTML": func(count int) template.HTML {
			width := count * 24
			if width > 200 {
				width = 200
			}
			return template.HTML(fmt.Sprintf(`<span class="bar bar-accent" style="width:%dpx"></span>`, width))
		},
	}
	tmpl, err := template.New("report").Funcs(funcMap).Parse(htmlTemplate)
	if err != nil {
		return "", fmt.Errorf("parsing template: %w", err)
	}

	outPath := filepath.Join(repoRoot, ".repokit", "report.html")
	f, err := os.Create(outPath)
	if err != nil {
		return "", fmt.Errorf("creating file: %w", err)
	}
	defer f.Close()

	if err := tmpl.Execute(f, data); err != nil {
		return "", fmt.Errorf("executing template: %w", err)
	}

	return outPath, nil
}

func exportMarkdown(data *reportData, repoRoot string) (string, error) {
	outPath := filepath.Join(repoRoot, ".repokit", "report.md")
	f, err := os.Create(outPath)
	if err != nil {
		return "", fmt.Errorf("creating file: %w", err)
	}
	defer f.Close()

	fmt.Fprintf(f, "# %s - Project Report\n\n", data.ProjectName)
	fmt.Fprintf(f, "*Generated: %s*\n\n", data.GeneratedAt)
	fmt.Fprintf(f, "## Health: %s (%d/100)\n\n", data.HealthGrade, data.HealthScore)

	fmt.Fprintf(f, "## Overview\n\n")
	fmt.Fprintf(f, "| Metric | Count |\n|--------|-------|\n")
	fmt.Fprintf(f, "| Files | %d |\n", data.TotalFiles)
	fmt.Fprintf(f, "| Components | %d |\n", data.TotalComps)
	fmt.Fprintf(f, "| Hooks | %d |\n", data.TotalHooks)
	fmt.Fprintf(f, "| Types | %d |\n", data.TotalTypes)
	fmt.Fprintf(f, "| Imports | %d |\n", data.TotalImports)
	fmt.Fprintf(f, "| Exports | %d |\n", data.TotalExports)

	if len(data.Stack) > 0 {
		fmt.Fprintf(f, "\n## Tech Stack\n\n")
		fmt.Fprintf(f, "| Category | Technology | Version |\n|----------|-----------|--------|\n")
		for _, s := range data.Stack {
			v := s.Version
			if v == "" {
				v = "-"
			}
			fmt.Fprintf(f, "| %s | %s | %s |\n", s.Category, s.Name, v)
		}
	}

	if len(data.Components) > 0 {
		fmt.Fprintf(f, "\n## Components\n\n")
		fmt.Fprintf(f, "| Name | Kind | Path | Lines |\n|------|------|------|-------|\n")
		for _, c := range data.Components {
			fmt.Fprintf(f, "| %s | %s | %s | %d |\n", c.Name, c.Kind, c.Path, c.Lines)
		}
	}

	if len(data.BiggestComps) > 0 {
		fmt.Fprintf(f, "\n## Biggest Components\n\n")
		fmt.Fprintf(f, "| Name | Lines | Path |\n|------|-------|------|\n")
		for _, c := range data.BiggestComps {
			fmt.Fprintf(f, "| %s | %d | %s |\n", c.Name, c.Lines, c.Path)
		}
	}

	fmt.Fprintf(f, "\n---\n*Generated by repokit v%s*\n", Version)

	return outPath, nil
}

func openInBrowser(path string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("cmd", "/c", "start", "", path)
	case "darwin":
		cmd = exec.Command("open", path)
	default:
		cmd = exec.Command("xdg-open", path)
	}
	cmd.Start()
}

var htmlTemplate = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>{{.ProjectName}} - Repokit Report</title>
<style>
  :root {
    --bg-primary: #0d1117;
    --bg-secondary: #161b22;
    --bg-tertiary: #1c2129;
    --border: #21262d;
    --border-hover: #30363d;
    --text-primary: #e6edf3;
    --text-secondary: #8b949e;
    --text-muted: #656d76;
    --accent: #58a6ff;
    --accent-muted: #1f6feb;
    --green: #3fb950;
    --green-muted: #238636;
    --yellow: #d29922;
    --red: #f85149;
    --purple: #bc8cff;
  }

  * { margin: 0; padding: 0; box-sizing: border-box; }

  body {
    font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, Oxygen, Ubuntu, sans-serif;
    background: var(--bg-primary);
    color: var(--text-primary);
    line-height: 1.6;
  }

  /* Header */
  .header {
    background: linear-gradient(135deg, #161b22 0%, #0d1117 100%);
    border-bottom: 1px solid var(--border);
    padding: 32px 0;
    position: sticky;
    top: 0;
    z-index: 100;
    backdrop-filter: blur(12px);
  }
  .header-inner {
    max-width: 1100px;
    margin: 0 auto;
    padding: 0 24px;
    display: flex;
    align-items: center;
    justify-content: space-between;
  }
  .header-left {
    display: flex;
    align-items: center;
    gap: 16px;
  }
  .logo {
    width: 36px;
    height: 36px;
    background: linear-gradient(135deg, var(--accent) 0%, var(--purple) 100%);
    border-radius: 10px;
    display: flex;
    align-items: center;
    justify-content: center;
    font-weight: 800;
    font-size: 18px;
    color: #fff;
  }
  .header h1 {
    font-size: 22px;
    font-weight: 700;
    color: var(--text-primary);
  }
  .header h1 span { color: var(--text-secondary); font-weight: 400; font-size: 15px; margin-left: 8px; }
  .header-meta { color: var(--text-muted); font-size: 13px; text-align: right; }

  /* Nav */
  .nav {
    max-width: 1100px;
    margin: 0 auto;
    padding: 12px 24px;
    display: flex;
    gap: 6px;
    flex-wrap: wrap;
    border-bottom: 1px solid var(--border);
  }
  .nav a {
    color: var(--text-secondary);
    text-decoration: none;
    font-size: 13px;
    padding: 6px 14px;
    border-radius: 20px;
    transition: all 0.2s;
  }
  .nav a:hover {
    color: var(--text-primary);
    background: var(--bg-secondary);
  }

  /* Main */
  .container { max-width: 1100px; margin: 0 auto; padding: 32px 24px 60px; }

  /* Section */
  .section { margin-bottom: 40px; }
  .section-title {
    font-size: 15px;
    font-weight: 600;
    color: var(--text-secondary);
    text-transform: uppercase;
    letter-spacing: 1.5px;
    margin-bottom: 16px;
    display: flex;
    align-items: center;
    gap: 10px;
  }
  .section-title::after {
    content: '';
    flex: 1;
    height: 1px;
    background: var(--border);
  }

  /* Health Ring */
  .health-panel {
    display: flex;
    align-items: center;
    gap: 32px;
    background: var(--bg-secondary);
    border: 1px solid var(--border);
    border-radius: 16px;
    padding: 28px 36px;
    margin-bottom: 28px;
  }
  .health-ring {
    position: relative;
    width: 110px;
    height: 110px;
    flex-shrink: 0;
  }
  .health-ring svg { transform: rotate(-90deg); }
  .health-ring .ring-bg { fill: none; stroke: var(--border); stroke-width: 8; }
  .health-ring .ring-fg { fill: none; stroke-width: 8; stroke-linecap: round; transition: stroke-dashoffset 1s ease; }
  .health-ring .ring-a, .health-ring .ring-b { stroke: var(--green); }
  .health-ring .ring-c { stroke: var(--yellow); }
  .health-ring .ring-d, .health-ring .ring-f { stroke: var(--red); }
  .health-ring-label {
    position: absolute;
    top: 50%;
    left: 50%;
    transform: translate(-50%, -50%);
    text-align: center;
  }
  .health-ring-label .grade-letter {
    font-size: 36px;
    font-weight: 800;
    line-height: 1;
  }
  .grade-letter-a, .grade-letter-b { color: var(--green); }
  .grade-letter-c { color: var(--yellow); }
  .grade-letter-d, .grade-letter-f { color: var(--red); }
  .health-ring-label .grade-score { font-size: 12px; color: var(--text-secondary); }
  .health-details { flex: 1; }
  .health-details h3 { font-size: 20px; margin-bottom: 4px; color: var(--text-primary); }
  .health-details p { color: var(--text-secondary); font-size: 14px; }
  .health-stat-row {
    display: flex;
    gap: 24px;
    margin-top: 14px;
  }
  .health-stat {
    display: flex;
    align-items: center;
    gap: 6px;
    font-size: 13px;
    color: var(--text-secondary);
  }
  .health-stat .dot {
    width: 8px;
    height: 8px;
    border-radius: 50%;
  }

  /* Stat Cards */
  .stats-grid {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(155px, 1fr));
    gap: 12px;
    margin-bottom: 28px;
  }
  .stat-card {
    background: var(--bg-secondary);
    border: 1px solid var(--border);
    border-radius: 12px;
    padding: 20px;
    text-align: center;
    transition: border-color 0.2s, transform 0.2s;
  }
  .stat-card:hover {
    border-color: var(--border-hover);
    transform: translateY(-2px);
  }
  .stat-card .value {
    font-size: 30px;
    font-weight: 700;
    background: linear-gradient(135deg, var(--accent) 0%, var(--purple) 100%);
    -webkit-background-clip: text;
    -webkit-text-fill-color: transparent;
    background-clip: text;
  }
  .stat-card .stat-label {
    font-size: 11px;
    color: var(--text-muted);
    text-transform: uppercase;
    letter-spacing: 1.2px;
    margin-top: 4px;
  }

  /* Tables */
  .table-wrap {
    background: var(--bg-secondary);
    border: 1px solid var(--border);
    border-radius: 12px;
    overflow: hidden;
    margin-bottom: 6px;
  }
  table { width: 100%; border-collapse: collapse; }
  thead th {
    background: var(--bg-tertiary);
    color: var(--text-muted);
    text-align: left;
    padding: 12px 16px;
    font-size: 11px;
    font-weight: 600;
    text-transform: uppercase;
    letter-spacing: 1px;
    border-bottom: 1px solid var(--border);
  }
  td {
    padding: 10px 16px;
    border-bottom: 1px solid var(--border);
    font-size: 14px;
    color: var(--text-primary);
  }
  tr:last-child td { border-bottom: none; }
  tbody tr { transition: background 0.15s; }
  tbody tr:hover { background: rgba(88,166,255,0.04); }
  td.muted { color: var(--text-secondary); font-size: 13px; }
  td strong { font-weight: 600; }

  /* Tags / Badges */
  .badge {
    display: inline-block;
    padding: 3px 10px;
    border-radius: 20px;
    font-size: 11px;
    font-weight: 600;
    letter-spacing: 0.5px;
  }
  .badge-category {
    background: rgba(88,166,255,0.12);
    color: var(--accent);
    border: 1px solid rgba(88,166,255,0.2);
  }
  .badge-kind {
    background: rgba(188,140,255,0.12);
    color: var(--purple);
    border: 1px solid rgba(188,140,255,0.2);
  }
  .badge-hook {
    background: rgba(63,185,80,0.12);
    color: var(--green);
    border: 1px solid rgba(63,185,80,0.2);
  }

  /* Bars */
  .bar-container { display: flex; align-items: center; gap: 8px; }
  .bar {
    display: inline-block;
    height: 10px;
    border-radius: 5px;
    min-width: 4px;
    transition: width 0.3s ease;
  }
  .bar-green { background: linear-gradient(90deg, var(--green-muted), var(--green)); }
  .bar-yellow { background: linear-gradient(90deg, #b87d14, var(--yellow)); }
  .bar-red { background: linear-gradient(90deg, #b62324, var(--red)); }
  .bar-accent { background: linear-gradient(90deg, var(--accent-muted), var(--accent)); }
  .bar-label { font-size: 12px; color: var(--text-muted); min-width: 30px; }

  /* Import breakdown */
  .import-split {
    display: grid;
    grid-template-columns: 1fr 1fr;
    gap: 12px;
  }
  .import-card {
    background: var(--bg-secondary);
    border: 1px solid var(--border);
    border-radius: 12px;
    padding: 24px;
    text-align: center;
  }
  .import-card .value {
    font-size: 28px;
    font-weight: 700;
  }
  .import-card.external .value { color: var(--accent); }
  .import-card.internal .value { color: var(--green); }
  .import-card .stat-label {
    font-size: 11px;
    color: var(--text-muted);
    text-transform: uppercase;
    letter-spacing: 1.2px;
    margin-top: 4px;
  }
  .import-bar {
    height: 6px;
    border-radius: 3px;
    margin-top: 10px;
    overflow: hidden;
    background: var(--border);
    display: flex;
  }
  .import-bar-ext {
    background: var(--accent);
    transition: width 0.5s ease;
  }
  .import-bar-int {
    background: var(--green);
    transition: width 0.5s ease;
  }

  /* Search */
  .search-box {
    display: flex;
    align-items: center;
    gap: 8px;
    background: var(--bg-tertiary);
    border: 1px solid var(--border);
    border-radius: 8px;
    padding: 8px 14px;
    margin-bottom: 12px;
    max-width: 320px;
  }
  .search-box svg { color: var(--text-muted); flex-shrink: 0; }
  .search-box input {
    background: none;
    border: none;
    color: var(--text-primary);
    font-size: 14px;
    outline: none;
    width: 100%;
  }
  .search-box input::placeholder { color: var(--text-muted); }

  /* Footer */
  footer {
    margin-top: 48px;
    padding: 24px 0;
    border-top: 1px solid var(--border);
    color: var(--text-muted);
    font-size: 12px;
    text-align: center;
    display: flex;
    justify-content: center;
    align-items: center;
    gap: 8px;
  }
  footer .footer-logo {
    width: 16px;
    height: 16px;
    background: linear-gradient(135deg, var(--accent), var(--purple));
    border-radius: 4px;
    display: inline-block;
  }

  /* Responsive */
  @media (max-width: 700px) {
    .header-inner { flex-direction: column; align-items: flex-start; gap: 8px; }
    .header-meta { text-align: left; }
    .health-panel { flex-direction: column; text-align: center; padding: 20px; }
    .health-stat-row { justify-content: center; }
    .stats-grid { grid-template-columns: repeat(3, 1fr); }
    .import-split { grid-template-columns: 1fr; }
    .nav { padding: 8px 16px; }
  }
</style>
</head>
<body>

<div class="header">
  <div class="header-inner">
    <div class="header-left">
      <div class="logo">R</div>
      <h1>{{.ProjectName}} <span>report</span></h1>
    </div>
    <div class="header-meta">
      Generated {{.GeneratedAt}}<br>
      repokit v{{.Version}}
    </div>
  </div>
</div>

<div class="nav">
  <a href="#health">Health</a>
  <a href="#overview">Overview</a>
  {{if .Stack}}<a href="#stack">Stack</a>{{end}}
  {{if .BiggestComps}}<a href="#biggest">Biggest</a>{{end}}
  {{if .FolderCounts}}<a href="#folders">Folders</a>{{end}}
  {{if .Components}}<a href="#components">Components</a>{{end}}
  <a href="#imports">Imports</a>
</div>

<div class="container">

  <!-- Health -->
  <div class="section" id="health">
    <div class="health-panel">
      <div class="health-ring">
        <svg width="110" height="110" viewBox="0 0 110 110">
          <circle class="ring-bg" cx="55" cy="55" r="46"/>
          <circle class="ring-fg ring-{{.HealthGrade | lower}}"
            cx="55" cy="55" r="46"
            stroke-dasharray="289"
            stroke-dashoffset="{{healthRingOffset .HealthScore}}"/>
        </svg>
        <div class="health-ring-label">
          <div class="grade-letter grade-letter-{{.HealthGrade | lower}}">{{.HealthGrade}}</div>
          <div class="grade-score">{{.HealthScore}}/100</div>
        </div>
      </div>
      <div class="health-details">
        <h3>Project Health</h3>
        <p>{{if ge .HealthScore 90}}Excellent! Your codebase is in great shape.{{else if ge .HealthScore 80}}Good health. Minor improvements possible.{{else if ge .HealthScore 70}}Decent. Some areas need attention.{{else if ge .HealthScore 60}}Below average. Several issues to address.{{else}}Needs work. Multiple quality issues found.{{end}}</p>
        <div class="health-stat-row">
          <div class="health-stat">
            <span class="dot" style="background:var(--accent)"></span>
            {{.TotalComps}} components
          </div>
          <div class="health-stat">
            <span class="dot" style="background:var(--green)"></span>
            {{.TotalFiles}} files
          </div>
          {{if .LintIssues}}<div class="health-stat">
            <span class="dot" style="background:var(--yellow)"></span>
            {{.LintIssues}} issues
          </div>{{end}}
        </div>
      </div>
    </div>
  </div>

  <!-- Overview Stats -->
  <div class="section" id="overview">
    <div class="section-title">Overview</div>
    <div class="stats-grid">
      <div class="stat-card"><div class="value">{{.TotalFiles}}</div><div class="stat-label">Files</div></div>
      <div class="stat-card"><div class="value">{{.TotalComps}}</div><div class="stat-label">Components</div></div>
      <div class="stat-card"><div class="value">{{.TotalHooks}}</div><div class="stat-label">Hooks</div></div>
      <div class="stat-card"><div class="value">{{.TotalTypes}}</div><div class="stat-label">Types</div></div>
      <div class="stat-card"><div class="value">{{.TotalImports}}</div><div class="stat-label">Imports</div></div>
      <div class="stat-card"><div class="value">{{.TotalExports}}</div><div class="stat-label">Exports</div></div>
    </div>
  </div>

  {{if .Stack}}
  <!-- Tech Stack -->
  <div class="section" id="stack">
    <div class="section-title">Tech Stack</div>
    <div class="table-wrap">
    <table>
      <thead><tr><th>Category</th><th>Technology</th><th>Version</th></tr></thead>
      <tbody>
      {{range .Stack}}
      <tr>
        <td><span class="badge badge-category">{{.Category}}</span></td>
        <td><strong>{{.Name}}</strong></td>
        <td>{{if .Version}}<code style="color:var(--green);background:rgba(63,185,80,0.1);padding:2px 8px;border-radius:4px;font-size:13px">{{.Version}}</code>{{else}}<span style="color:var(--text-muted)">&mdash;</span>{{end}}</td>
      </tr>
      {{end}}
      </tbody>
    </table>
    </div>
  </div>
  {{end}}

  {{if .BiggestComps}}
  <!-- Biggest Components -->
  <div class="section" id="biggest">
    <div class="section-title">Biggest Components</div>
    <div class="table-wrap">
    <table>
      <thead><tr><th>Component</th><th>Lines</th><th style="width:40%">Size</th><th>Path</th></tr></thead>
      <tbody>
      {{range .BiggestComps}}
      <tr>
        <td><strong>{{.Name}}</strong></td>
        <td>{{.Lines}}</td>
        <td><div class="bar-container">{{barHTML .Lines}}<span class="bar-label">{{.Lines}}</span></div></td>
        <td class="muted">{{.Path}}</td>
      </tr>
      {{end}}
      </tbody>
    </table>
    </div>
  </div>
  {{end}}

  {{if .FolderCounts}}
  <!-- Folder Distribution -->
  <div class="section" id="folders">
    <div class="section-title">File Distribution</div>
    <div class="table-wrap">
    <table>
      <thead><tr><th>Folder</th><th>Files</th><th style="width:45%">Distribution</th></tr></thead>
      <tbody>
      {{range .FolderCounts}}
      <tr>
        <td><code style="color:var(--accent);font-size:13px">{{.Folder}}</code></td>
        <td>{{.Count}}</td>
        <td><div class="bar-container">{{folderBarHTML .Count}}<span class="bar-label">{{.Count}}</span></div></td>
      </tr>
      {{end}}
      </tbody>
    </table>
    </div>
  </div>
  {{end}}

  {{if .Components}}
  <!-- All Components -->
  <div class="section" id="components">
    <div class="section-title">All Components ({{len .Components}})</div>
    <div class="search-box">
      <svg width="16" height="16" viewBox="0 0 16 16" fill="currentColor"><path d="M11.5 7a4.5 4.5 0 1 1-9 0 4.5 4.5 0 0 1 9 0Zm-.82 4.74a6 6 0 1 1 1.06-1.06l3.04 3.04a.75.75 0 1 1-1.06 1.06l-3.04-3.04Z"/></svg>
      <input type="text" id="compSearch" placeholder="Filter components..." oninput="filterTable()">
    </div>
    <div class="table-wrap">
    <table id="compTable">
      <thead><tr><th>ID</th><th>Name</th><th>Kind</th><th>Path</th><th>Lines</th></tr></thead>
      <tbody>
      {{range .Components}}
      <tr>
        <td class="muted">{{.ID}}</td>
        <td><strong>{{.Name}}</strong></td>
        <td><span class="badge {{if eq .Kind "hook"}}badge-hook{{else}}badge-kind{{end}}">{{.Kind}}</span></td>
        <td class="muted">{{.Path}}</td>
        <td>{{.Lines}}</td>
      </tr>
      {{end}}
      </tbody>
    </table>
    </div>
  </div>
  {{end}}

  <!-- Import Breakdown -->
  <div class="section" id="imports">
    <div class="section-title">Import Breakdown</div>
    <div class="import-split">
      <div class="import-card external">
        <div class="value">{{.ExtImports}}</div>
        <div class="stat-label">External (npm)</div>
      </div>
      <div class="import-card internal">
        <div class="value">{{.IntImports}}</div>
        <div class="stat-label">Internal</div>
      </div>
    </div>
    {{if or .ExtImports .IntImports}}
    <div style="margin-top:12px;padding:0 4px">
      <div class="import-bar">
        <div class="import-bar-ext" style="width:{{importPercent .ExtImports .IntImports}}%"></div>
        <div class="import-bar-int" style="width:{{internalPercent .ExtImports .IntImports}}%"></div>
      </div>
      <div style="display:flex;justify-content:space-between;margin-top:6px;font-size:12px;color:var(--text-muted)">
        <span>External {{importPercent .ExtImports .IntImports}}%</span>
        <span>Internal {{internalPercent .ExtImports .IntImports}}%</span>
      </div>
    </div>
    {{end}}
  </div>

  <footer>
    <span class="footer-logo"></span>
    Generated by repokit v{{.Version}}
  </footer>
</div>

<script>
function filterTable() {
  const q = document.getElementById('compSearch').value.toLowerCase();
  const rows = document.querySelectorAll('#compTable tbody tr');
  rows.forEach(row => {
    const text = row.textContent.toLowerCase();
    row.style.display = text.includes(q) ? '' : 'none';
  });
}
</script>
</body>
</html>`

func init() {
	exportCmd.Flags().StringVar(&exportFormat, "format", "html", "Export format (html, md)")
	exportCmd.Flags().BoolVar(&exportOpen, "open", false, "Open report in browser after export")
	// Convenience flags
	exportCmd.Flags().Bool("html", false, "Export as HTML (shorthand for --format html)")
	exportCmd.Flags().Bool("md", false, "Export as Markdown (shorthand for --format md)")

	rootCmd.AddCommand(exportCmd)
}
