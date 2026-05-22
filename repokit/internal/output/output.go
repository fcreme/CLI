package output

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/fatih/color"
)

// ansiRe matches ANSI escape sequences for stripping.
var ansiRe = regexp.MustCompile(`\x1b\[[0-9;]*m`)

// visibleLen returns the visible width of a string in terminal cells,
// excluding ANSI escape codes. Counts runes, treating each rune as width 1.
// (Does not handle wide-East-Asian or emoji correctly, but works for
// box-drawing chars, arrows, and most symbols we use.)
func visibleLen(s string) int {
	return utf8.RuneCountInString(ansiRe.ReplaceAllString(s, ""))
}

// padRight pads a string with spaces to the given visible width.
// If the string is already at or beyond the target width, returns it as-is.
func padRight(s string, visWidth int) string {
	vl := visibleLen(s)
	if vl >= visWidth {
		return s
	}
	return s + strings.Repeat(" ", visWidth-vl)
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

// BoxWidth is the total visual width of Box and Banner output (including borders).
const BoxWidth = 74

const (
	bcTL = "┌"
	bcTR = "┐"
	bcBL = "└"
	bcBR = "┘"
	bcH  = "─"
	bcV  = "│"

	bnTL = "╔"
	bnTR = "╗"
	bnBL = "╚"
	bnBR = "╝"
	bnH  = "═"
	bnV  = "║"
)

// Banner returns a double-line title block with the given title and optional subtitle.
// Subtitle is right-aligned. Returns empty string in JSON mode.
func Banner(title, subtitle string) string {
	if JSONMode {
		return ""
	}
	inner := BoxWidth - 2
	top := bnTL + strings.Repeat(bnH, inner) + bnTR
	bot := bnBL + strings.Repeat(bnH, inner) + bnBR

	titlePart := "  " + Bold(title)
	subPart := ""
	if subtitle != "" {
		subPart = Dim(subtitle) + "  "
	}
	pad := inner - visibleLen(titlePart) - visibleLen(subPart)
	if pad < 1 {
		pad = 1
	}
	middle := bnV + titlePart + strings.Repeat(" ", pad) + subPart + bnV
	return top + "\n" + middle + "\n" + bot
}

// Box returns a labeled single-line bordered section.
// Each line in `lines` should be at most (BoxWidth - 4) visible characters;
// longer lines will overflow the right border.
// Returns empty string in JSON mode.
func Box(title string, lines []string) string {
	if JSONMode {
		return ""
	}
	inner := BoxWidth - 2
	label := " " + Bold(title) + " "
	fill := inner - 1 - visibleLen(label)
	if fill < 0 {
		fill = 0
	}
	top := bcTL + bcH + label + strings.Repeat(bcH, fill) + bcTR
	bot := bcBL + strings.Repeat(bcH, inner) + bcBR

	contentW := inner - 2
	var sb strings.Builder
	sb.WriteString(top)
	sb.WriteString("\n")
	for _, ln := range lines {
		sb.WriteString(bcV)
		sb.WriteString(" ")
		sb.WriteString(padRight(ln, contentW))
		sb.WriteString(" ")
		sb.WriteString(bcV)
		sb.WriteString("\n")
	}
	sb.WriteString(bot)
	return sb.String()
}

// Divider returns a horizontal dim line of standard box width.
func Divider() string {
	if JSONMode {
		return ""
	}
	return Dim(strings.Repeat(bcH, BoxWidth))
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
