package output

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"

	"github.com/fatih/color"
)

// ansiRe matches ANSI escape sequences for stripping.
var ansiRe = regexp.MustCompile(`\x1b\[[0-9;]*m`)

// visibleLen returns the length of a string excluding ANSI escape codes.
func visibleLen(s string) int {
	return len(ansiRe.ReplaceAllString(s, ""))
}

// padRight pads a string to the given visible width, accounting for ANSI codes.
func padRight(s string, visWidth int) string {
	extra := len(s) - visibleLen(s)
	return fmt.Sprintf("%-*s", visWidth+extra, s)
}

var (
	Bold    = color.New(color.Bold).SprintFunc()
	Green   = color.New(color.FgGreen).SprintFunc()
	Yellow  = color.New(color.FgYellow).SprintFunc()
	Red     = color.New(color.FgRed).SprintFunc()
	Cyan    = color.New(color.FgCyan).SprintFunc()
	Dim     = color.New(color.Faint).SprintFunc()
	Success = color.New(color.FgGreen, color.Bold).SprintFunc()
	Error   = color.New(color.FgRed, color.Bold).SprintFunc()
)

// JSONMode controls whether output is formatted as JSON.
var JSONMode bool

// Writer is the default output writer (os.Stdout).
var Writer io.Writer = os.Stdout

// PrintSuccess prints a success message.
func PrintSuccess(format string, args ...interface{}) {
	msg := fmt.Sprintf(format, args...)
	if JSONMode {
		printJSON(map[string]string{"status": "success", "message": msg})
		return
	}
	fmt.Fprintf(Writer, "%s %s\n", Success("[OK]"), msg)
}

// PrintError prints an error message.
func PrintError(format string, args ...interface{}) {
	msg := fmt.Sprintf(format, args...)
	if JSONMode {
		printJSON(map[string]string{"status": "error", "message": msg})
		return
	}
	fmt.Fprintf(os.Stderr, "%s %s\n", Error("[ERROR]"), msg)
}

// PrintInfo prints an informational message.
func PrintInfo(format string, args ...interface{}) {
	msg := fmt.Sprintf(format, args...)
	if JSONMode {
		return // suppress info in JSON mode
	}
	fmt.Fprintf(Writer, "%s %s\n", Cyan("[INFO]"), msg)
}

// PrintWarning prints a warning message.
func PrintWarning(format string, args ...interface{}) {
	msg := fmt.Sprintf(format, args...)
	if JSONMode {
		printJSON(map[string]string{"status": "warning", "message": msg})
		return
	}
	fmt.Fprintf(Writer, "%s %s\n", Yellow("[WARN]"), msg)
}

// PrintHint prints a next-step suggestion in dimmed text.
func PrintHint(format string, args ...interface{}) {
	if JSONMode {
		return
	}
	msg := fmt.Sprintf(format, args...)
	fmt.Fprintf(Writer, "  %s %s\n", Dim("->"), Dim("try: "+msg))
}

// PrintFooter prints a fixed reminder about how to see all commands.
func PrintFooter() {
	if JSONMode {
		return
	}
	fmt.Fprintf(Writer, "  %s\n", Dim("Run 'repokit' to see all commands or 'repokit help <command>' for details."))
}

// PrintJSON outputs data as formatted JSON.
func PrintJSON(data interface{}) {
	printJSON(data)
}

func printJSON(data interface{}) {
	enc := json.NewEncoder(Writer)
	enc.SetIndent("", "  ")
	enc.Encode(data)
}

// Table prints data in a simple aligned table format.
type Table struct {
	Headers []string
	Rows    [][]string
}

// Print renders the table to the writer.
func (t *Table) Print() {
	if JSONMode {
		t.printAsJSON()
		return
	}

	if len(t.Rows) == 0 {
		fmt.Fprintln(Writer, Dim("  No results found."))
		return
	}

	// Calculate column widths based on visible text (strips ANSI codes)
	widths := make([]int, len(t.Headers))
	for i, h := range t.Headers {
		widths[i] = len(h)
	}
	for _, row := range t.Rows {
		for i, cell := range row {
			if i < len(widths) && visibleLen(cell) > widths[i] {
				widths[i] = visibleLen(cell)
			}
		}
	}

	// Print header
	header := ""
	separator := ""
	for i, h := range t.Headers {
		header += "  " + padRight(Bold(h), widths[i]+2)
		separator += "  " + strings.Repeat("-", widths[i]+2)
	}
	fmt.Fprintln(Writer, header)
	fmt.Fprintln(Writer, Dim(separator))

	// Print rows
	for _, row := range t.Rows {
		line := ""
		for i, cell := range row {
			if i < len(widths) {
				line += "  " + padRight(cell, widths[i]+2)
			}
		}
		fmt.Fprintln(Writer, line)
	}
}

func (t *Table) printAsJSON() {
	var records []map[string]string
	for _, row := range t.Rows {
		record := make(map[string]string)
		for i, h := range t.Headers {
			if i < len(row) {
				record[h] = row[i]
			}
		}
		records = append(records, record)
	}
	printJSON(records)
}
