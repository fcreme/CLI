package suggest

import (
	"fmt"
	"regexp"
	"strings"
)

// Strategy represents a deterministic code modification strategy.
type Strategy interface {
	Name() string
	Matches(description string) bool
	Apply(source string, params StrategyParams) (string, error)
}

// StrategyParams holds parsed parameters for a strategy.
type StrategyParams struct {
	Description string
	PropName    string
	PropType    string
	ImportPath  string
	ImportName  string
	StateName   string
	StateType   string
	EffectDeps  string
}

// ParseParams extracts structured parameters from a description string.
func ParseParams(description string) StrategyParams {
	params := StrategyParams{Description: description}
	lower := strings.ToLower(description)

	// Extract prop name: "add X prop", "add prop X"
	propPatterns := []*regexp.Regexp{
		regexp.MustCompile(`(?i)add\s+(?:a\s+)?(\w+)\s+prop`),
		regexp.MustCompile(`(?i)add\s+prop\s+(\w+)`),
	}
	for _, re := range propPatterns {
		if m := re.FindStringSubmatch(description); m != nil {
			params.PropName = m[1]
			break
		}
	}

	// Extract prop type from "X: type" or "X (type)"
	typePatterns := []*regexp.Regexp{
		regexp.MustCompile(`(?i)(\w+)\s*:\s*(\w+)\s+prop`),
		regexp.MustCompile(`(?i)prop\s+(\w+)\s*:\s*(\w+)`),
		regexp.MustCompile(`(?i)(\w+)\s+prop\s*\((\w+)\)`),
	}
	for _, re := range typePatterns {
		if m := re.FindStringSubmatch(description); m != nil {
			params.PropName = m[1]
			params.PropType = m[2]
			break
		}
	}

	// Default prop type
	if params.PropName != "" && params.PropType == "" {
		if strings.Contains(lower, "bool") || strings.HasPrefix(params.PropName, "is") || strings.HasPrefix(params.PropName, "has") {
			params.PropType = "boolean"
		} else if strings.Contains(lower, "handler") || strings.HasPrefix(params.PropName, "on") {
			params.PropType = "() => void"
		} else if strings.Contains(lower, "number") || strings.Contains(lower, "count") || strings.Contains(lower, "size") {
			params.PropType = "number"
		} else {
			params.PropType = "string"
		}
	}

	// Extract import: "add import { X } from 'module'" or "add X from 'module'"
	importBraceRe := regexp.MustCompile(`(?i)import\s+\{\s*(\w+)\s*\}\s+from\s+['"]([^'"]+)['"]`)
	if m := importBraceRe.FindStringSubmatch(description); m != nil {
		params.ImportName = m[1]
		params.ImportPath = m[2]
	}
	if params.ImportName == "" {
		importRe := regexp.MustCompile(`(?i)(?:add|import)\s+(\w+)\s+from\s+['"]([^'"]+)['"]`)
		if m := importRe.FindStringSubmatch(description); m != nil {
			params.ImportName = m[1]
			params.ImportPath = m[2]
		}
	}

	// Extract state name
	stateRe := regexp.MustCompile(`(?i)add\s+(?:a\s+)?(\w+)\s+state`)
	if m := stateRe.FindStringSubmatch(description); m != nil {
		params.StateName = m[1]
	}
	if params.StateName == "" && strings.Contains(lower, "loading") {
		params.StateName = "loading"
		params.StateType = "boolean"
	}

	return params
}

// SelectStrategy picks the best strategy for a description.
func SelectStrategy(description string) Strategy {
	lower := strings.ToLower(description)

	if strings.Contains(lower, "import") && (strings.Contains(lower, "add") || strings.Contains(lower, "include")) {
		return &AddImportStrategy{}
	}
	if strings.Contains(lower, "prop") {
		return &AddPropStrategy{}
	}
	if strings.Contains(lower, "state") || strings.Contains(lower, "loading") {
		return &AddStateStrategy{}
	}
	if strings.Contains(lower, "effect") || strings.Contains(lower, "useeffect") {
		return &AddEffectStrategy{}
	}
	if strings.Contains(lower, "wrap") && strings.Contains(lower, "provider") {
		return &WrapWithProviderStrategy{}
	}

	// Default: try to infer from description
	if strings.Contains(lower, "prop") || strings.Contains(lower, "onclick") || strings.Contains(lower, "classname") {
		return &AddPropStrategy{}
	}

	return &AddPropStrategy{} // safe default
}

// --- AddImport Strategy ---

type AddImportStrategy struct{}

func (s *AddImportStrategy) Name() string { return "add_import" }

func (s *AddImportStrategy) Matches(desc string) bool {
	lower := strings.ToLower(desc)
	return strings.Contains(lower, "import")
}

func (s *AddImportStrategy) Apply(source string, params StrategyParams) (string, error) {
	if params.ImportName == "" || params.ImportPath == "" {
		return "", fmt.Errorf("could not determine import name and path from description")
	}

	importLine := fmt.Sprintf("import { %s } from '%s'", params.ImportName, params.ImportPath)

	// Check if already imported
	if strings.Contains(source, params.ImportName) && strings.Contains(source, params.ImportPath) {
		return "", fmt.Errorf("import '%s' from '%s' already exists", params.ImportName, params.ImportPath)
	}

	// Find last import line and insert after it
	lines := strings.Split(source, "\n")
	lastImportIdx := -1
	for i, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "import ") {
			lastImportIdx = i
		}
	}

	if lastImportIdx >= 0 {
		// Insert after last import
		newLines := make([]string, 0, len(lines)+1)
		newLines = append(newLines, lines[:lastImportIdx+1]...)
		newLines = append(newLines, importLine)
		newLines = append(newLines, lines[lastImportIdx+1:]...)
		return strings.Join(newLines, "\n"), nil
	}

	// No imports found, add at the top (after directives)
	insertIdx := 0
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "'use ") || strings.HasPrefix(trimmed, "\"use ") {
			insertIdx = i + 1
			break
		}
	}

	newLines := make([]string, 0, len(lines)+2)
	newLines = append(newLines, lines[:insertIdx]...)
	newLines = append(newLines, importLine, "")
	newLines = append(newLines, lines[insertIdx:]...)
	return strings.Join(newLines, "\n"), nil
}

// --- AddProp Strategy ---

type AddPropStrategy struct{}

func (s *AddPropStrategy) Name() string { return "add_prop" }

func (s *AddPropStrategy) Matches(desc string) bool {
	return strings.Contains(strings.ToLower(desc), "prop")
}

func (s *AddPropStrategy) Apply(source string, params StrategyParams) (string, error) {
	if params.PropName == "" {
		return "", fmt.Errorf("could not determine prop name from description: %q", params.Description)
	}

	propType := params.PropType
	if propType == "" {
		propType = "string"
	}

	// Check if prop already exists
	if strings.Contains(source, params.PropName+":") || strings.Contains(source, params.PropName+"?:") {
		return "", fmt.Errorf("prop '%s' already exists", params.PropName)
	}

	lines := strings.Split(source, "\n")
	modified := false

	// Find the Props interface/type and add the prop
	interfaceRe := regexp.MustCompile(`^(?:export\s+)?(?:interface|type)\s+\w*Props\w*\s*(?:=\s*)?\{`)
	for i, line := range lines {
		if interfaceRe.MatchString(strings.TrimSpace(line)) {
			// Find the closing brace
			for j := i + 1; j < len(lines); j++ {
				if strings.TrimSpace(lines[j]) == "}" {
					// Insert prop before closing brace
					indent := "  "
					propLine := fmt.Sprintf("%s%s?: %s", indent, params.PropName, propType)
					newLines := make([]string, 0, len(lines)+1)
					newLines = append(newLines, lines[:j]...)
					newLines = append(newLines, propLine)
					newLines = append(newLines, lines[j:]...)
					lines = newLines
					modified = true
					break
				}
			}
			break
		}
	}

	if !modified {
		return "", fmt.Errorf("could not find Props interface/type to add prop '%s'", params.PropName)
	}

	// Also add the prop to the destructuring in the function signature
	funcRe := regexp.MustCompile(`(\{\s*[^}]*)(}\s*:\s*\w*Props)`)
	result := strings.Join(lines, "\n")
	if m := funcRe.FindStringSubmatch(result); m != nil {
		existing := strings.TrimSpace(m[1])
		if existing == "{" {
			result = funcRe.ReplaceAllString(result, "{ "+params.PropName+"$2")
		} else {
			result = funcRe.ReplaceAllString(result, "$1, "+params.PropName+"$2")
		}
	}

	return result, nil
}

// --- AddState Strategy ---

type AddStateStrategy struct{}

func (s *AddStateStrategy) Name() string { return "add_state" }

func (s *AddStateStrategy) Matches(desc string) bool {
	lower := strings.ToLower(desc)
	return strings.Contains(lower, "state") || strings.Contains(lower, "loading")
}

func (s *AddStateStrategy) Apply(source string, params StrategyParams) (string, error) {
	stateName := params.StateName
	if stateName == "" {
		stateName = "value"
	}

	// Capitalize for setter
	setter := "set" + strings.ToUpper(stateName[:1]) + stateName[1:]

	// Default value based on type
	defaultVal := "null"
	if params.StateType == "boolean" {
		defaultVal = "false"
	} else if params.StateType == "string" {
		defaultVal = "''"
	} else if params.StateType == "number" {
		defaultVal = "0"
	} else if params.StateType == "array" {
		defaultVal = "[]"
	}

	stateGeneric := ""
	if params.StateType != "" {
		stateGeneric = "<" + params.StateType + ">"
	}

	stateLine := fmt.Sprintf("  const [%s, %s] = useState%s(%s)", stateName, setter, stateGeneric, defaultVal)

	// Check if already exists
	if strings.Contains(source, stateName+",") && strings.Contains(source, setter) {
		return "", fmt.Errorf("state '%s' already exists", stateName)
	}

	// Ensure useState is imported
	modified := ensureHookImport(source, "useState")

	// Insert state declaration at the top of the component body
	lines := strings.Split(modified, "\n")
	funcBodyRe := regexp.MustCompile(`(?:function\s+\w+\s*\([^)]*\)\s*\{|=>\s*\{|function\s*\([^)]*\)\s*\{)`)
	inserted := false
	for i, line := range lines {
		if funcBodyRe.MatchString(line) && !strings.HasPrefix(strings.TrimSpace(line), "//") {
			// Insert after the opening brace
			newLines := make([]string, 0, len(lines)+1)
			newLines = append(newLines, lines[:i+1]...)
			newLines = append(newLines, stateLine)
			newLines = append(newLines, lines[i+1:]...)
			lines = newLines
			inserted = true
			break
		}
	}

	if !inserted {
		return "", fmt.Errorf("could not find component function body to insert state")
	}

	return strings.Join(lines, "\n"), nil
}

// --- AddEffect Strategy ---

type AddEffectStrategy struct{}

func (s *AddEffectStrategy) Name() string { return "add_effect" }

func (s *AddEffectStrategy) Matches(desc string) bool {
	lower := strings.ToLower(desc)
	return strings.Contains(lower, "effect") || strings.Contains(lower, "useeffect")
}

func (s *AddEffectStrategy) Apply(source string, params StrategyParams) (string, error) {
	effectBlock := `  useEffect(() => {
    // TODO: implement effect
  }, [])`

	// Ensure useEffect is imported
	modified := ensureHookImport(source, "useEffect")

	// Insert before the return statement
	lines := strings.Split(modified, "\n")
	for i, line := range lines {
		if strings.TrimSpace(line) == "return (" || strings.TrimSpace(line) == "return(" {
			newLines := make([]string, 0, len(lines)+4)
			newLines = append(newLines, lines[:i]...)
			newLines = append(newLines, effectBlock)
			newLines = append(newLines, "")
			newLines = append(newLines, lines[i:]...)
			return strings.Join(newLines, "\n"), nil
		}
	}

	return "", fmt.Errorf("could not find return statement to insert effect before")
}

// --- WrapWithProvider Strategy ---

type WrapWithProviderStrategy struct{}

func (s *WrapWithProviderStrategy) Name() string { return "wrap_provider" }

func (s *WrapWithProviderStrategy) Matches(desc string) bool {
	lower := strings.ToLower(desc)
	return strings.Contains(lower, "wrap") && strings.Contains(lower, "provider")
}

func (s *WrapWithProviderStrategy) Apply(source string, params StrategyParams) (string, error) {
	providerName := params.ImportName
	if providerName == "" {
		// Try to extract from description
		re := regexp.MustCompile(`(?i)wrap\s+(?:with\s+)?(\w+)`)
		if m := re.FindStringSubmatch(params.Description); m != nil {
			providerName = m[1]
		}
	}
	if providerName == "" {
		return "", fmt.Errorf("could not determine provider name from description")
	}

	// Find the return statement and wrap the JSX
	lines := strings.Split(source, "\n")
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "return (" || trimmed == "return(" {
			// Find the matching close paren
			depth := 0
			for j := i; j < len(lines); j++ {
				for _, ch := range lines[j] {
					if ch == '(' {
						depth++
					} else if ch == ')' {
						depth--
						if depth == 0 {
							// Wrap: add provider after return ( and before closing )
							indent := getIndent(lines[i+1])
							lines[i+1] = indent + "<" + providerName + ">\n" + indent + "  " + strings.TrimSpace(lines[i+1])
							closeIndent := getIndent(lines[j])
							lines[j] = closeIndent + "  " + strings.TrimSpace(lines[j-1]) + "\n" + closeIndent + "</" + providerName + ">\n" + closeIndent + ")"
							return strings.Join(lines, "\n"), nil
						}
					}
				}
			}
		}
	}

	return "", fmt.Errorf("could not find JSX return to wrap with provider")
}

// --- Helpers ---

func ensureHookImport(source, hookName string) string {
	// Check if hook is already imported
	if strings.Contains(source, hookName) {
		importRe := regexp.MustCompile(`import\s+\{([^}]*)\}\s+from\s+['"]react['"]`)
		if m := importRe.FindStringSubmatch(source); m != nil {
			if strings.Contains(m[1], hookName) {
				return source // already imported
			}
			// Add to existing import
			newImport := strings.TrimSpace(m[1]) + ", " + hookName
			return importRe.ReplaceAllString(source, fmt.Sprintf("import { %s } from 'react'", newImport))
		}
	}

	// Check if react import exists
	importRe := regexp.MustCompile(`import\s+\{([^}]*)\}\s+from\s+['"]react['"]`)
	if m := importRe.FindStringSubmatch(source); m != nil {
		if !strings.Contains(m[1], hookName) {
			newImport := strings.TrimSpace(m[1]) + ", " + hookName
			return importRe.ReplaceAllString(source, fmt.Sprintf("import { %s } from 'react'", newImport))
		}
		return source
	}

	// No react import, add one
	lines := strings.Split(source, "\n")
	insertIdx := 0
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "'use ") || strings.HasPrefix(trimmed, "\"use ") {
			insertIdx = i + 1
			continue
		}
		if strings.HasPrefix(trimmed, "import ") {
			insertIdx = i
			break
		}
	}

	importLine := fmt.Sprintf("import { %s } from 'react'", hookName)
	newLines := make([]string, 0, len(lines)+1)
	newLines = append(newLines, lines[:insertIdx]...)
	newLines = append(newLines, importLine)
	newLines = append(newLines, lines[insertIdx:]...)
	return strings.Join(newLines, "\n")
}

func getIndent(line string) string {
	for i, ch := range line {
		if ch != ' ' && ch != '\t' {
			return line[:i]
		}
	}
	return line
}
