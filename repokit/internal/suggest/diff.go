package suggest

import (
	"fmt"
	"strings"
)

// ComputeUnifiedDiff generates a unified diff between original and modified content.
func ComputeUnifiedDiff(filePath, original, modified string) string {
	origLines := strings.Split(original, "\n")
	modLines := strings.Split(modified, "\n")

	var diff strings.Builder
	diff.WriteString(fmt.Sprintf("--- a/%s\n", filePath))
	diff.WriteString(fmt.Sprintf("+++ b/%s\n", filePath))

	// Simple line-by-line diff with context
	chunks := computeChunks(origLines, modLines)
	for _, chunk := range chunks {
		diff.WriteString(chunk)
	}

	return diff.String()
}

type change struct {
	kind byte // ' ' context, '-' removed, '+' added
	line string
}

func computeChunks(origLines, modLines []string) []string {
	// Compute LCS-based diff
	changes := diffLines(origLines, modLines)

	// Group changes into hunks with context
	var chunks []string
	contextLines := 3
	i := 0
	for i < len(changes) {
		// Find next change
		start := i
		for start < len(changes) && changes[start].kind == ' ' {
			start++
		}
		if start >= len(changes) {
			break
		}

		// Include context before
		contextStart := start - contextLines
		if contextStart < 0 {
			contextStart = 0
		}

		// Find end of change block (including context after)
		end := start
		for end < len(changes) {
			if changes[end].kind != ' ' {
				// Find end of this change group
				for end < len(changes) && changes[end].kind != ' ' {
					end++
				}
				// Add trailing context
				trailingEnd := end + contextLines
				if trailingEnd > len(changes) {
					trailingEnd = len(changes)
				}
				// Check if next change is within context range
				nextChange := end
				for nextChange < len(changes) && changes[nextChange].kind == ' ' {
					nextChange++
				}
				if nextChange < trailingEnd+contextLines && nextChange < len(changes) {
					end = nextChange
					continue
				}
				end = trailingEnd
				break
			}
			end++
		}

		// Calculate line numbers for the hunk header
		origStart := 1
		origCount := 0
		modStart := 1
		modCount := 0

		// Count original/modified lines up to contextStart
		origLine := 0
		modLine := 0
		for j := 0; j < contextStart; j++ {
			switch changes[j].kind {
			case ' ':
				origLine++
				modLine++
			case '-':
				origLine++
			case '+':
				modLine++
			}
		}
		origStart = origLine + 1
		modStart = modLine + 1

		// Count lines in this hunk
		for j := contextStart; j < end; j++ {
			switch changes[j].kind {
			case ' ':
				origCount++
				modCount++
			case '-':
				origCount++
			case '+':
				modCount++
			}
		}

		// Build hunk
		var hunk strings.Builder
		hunk.WriteString(fmt.Sprintf("@@ -%d,%d +%d,%d @@\n", origStart, origCount, modStart, modCount))
		for j := contextStart; j < end; j++ {
			switch changes[j].kind {
			case ' ':
				hunk.WriteString(" " + changes[j].line + "\n")
			case '-':
				hunk.WriteString("-" + changes[j].line + "\n")
			case '+':
				hunk.WriteString("+" + changes[j].line + "\n")
			}
		}

		chunks = append(chunks, hunk.String())
		i = end
	}

	return chunks
}

// diffLines computes a line-level diff using a simple LCS algorithm.
func diffLines(a, b []string) []change {
	// Build LCS table
	m, n := len(a), len(b)
	dp := make([][]int, m+1)
	for i := range dp {
		dp[i] = make([]int, n+1)
	}
	for i := 1; i <= m; i++ {
		for j := 1; j <= n; j++ {
			if a[i-1] == b[j-1] {
				dp[i][j] = dp[i-1][j-1] + 1
			} else if dp[i-1][j] >= dp[i][j-1] {
				dp[i][j] = dp[i-1][j]
			} else {
				dp[i][j] = dp[i][j-1]
			}
		}
	}

	// Backtrack to build diff
	var changes []change
	i, j := m, n
	for i > 0 || j > 0 {
		if i > 0 && j > 0 && a[i-1] == b[j-1] {
			changes = append([]change{{' ', a[i-1]}}, changes...)
			i--
			j--
		} else if j > 0 && (i == 0 || dp[i][j-1] >= dp[i-1][j]) {
			changes = append([]change{{'+', b[j-1]}}, changes...)
			j--
		} else {
			changes = append([]change{{'-', a[i-1]}}, changes...)
			i--
		}
	}

	return changes
}
