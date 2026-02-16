package parser

import (
	"regexp"
	"strings"
)

// ParsedFile holds all extracted information from a single source file.
type ParsedFile struct {
	Components      []ParsedComponent
	Hooks           []ParsedHook
	Imports         []ParsedImport
	Exports         []ParsedExport
	TypeDefinitions []ParsedTypeDef
	HasJSX          bool
}

// ParsedComponent represents an extracted React component.
type ParsedComponent struct {
	Name       string
	Kind       string // "function_component", "arrow_component", "class_component"
	ExportType string // "default", "named", "none"
	LineStart  int
	LineEnd    int
	PropsType  string // the type name used for props, e.g. "ButtonProps"
	Source     string
}

// ParsedHook represents an extracted custom hook.
type ParsedHook struct {
	Name       string
	ExportType string
	LineStart  int
	LineEnd    int
	Source     string
}

// ParsedImport represents an extracted import statement.
type ParsedImport struct {
	Source        string   // the module specifier
	DefaultName   string   // default import name
	NamedImports  []string // named imports
	NamespaceImport string // namespace import name
	Line          int
}

// ParsedExport represents an extracted export.
type ParsedExport struct {
	Name string
	Kind string // "function", "const", "class", "type", "interface", "default"
	Line int
}

// ParsedTypeDef represents an extracted interface or type alias.
type ParsedTypeDef struct {
	Name      string
	Kind      string // "interface", "type_alias"
	Body      string
	Props     []ParsedProp
	LineStart int
	LineEnd   int
}

// ParsedProp represents a single property from an interface or type.
type ParsedProp struct {
	Name           string
	TypeAnnotation string
	IsOptional     bool
}

// Parse analyzes a source file and extracts all structured information.
func Parse(content string, language string) *ParsedFile {
	pf := &ParsedFile{}

	lines := strings.Split(content, "\n")

	pf.Imports = parseImports(content, lines)
	pf.Exports = parseExports(content, lines)
	pf.TypeDefinitions = parseTypeDefs(content, lines)
	pf.HasJSX = detectJSX(content)

	switch language {
	case "tsx", "jsx":
		pf.Components = parseComponents(content, lines)
		pf.Hooks = parseHooks(content, lines)
	case "ts", "js":
		// TS/JS files can still export hooks
		pf.Hooks = parseHooks(content, lines)
		// Check if there's JSX (some .ts files may have components)
		if pf.HasJSX {
			pf.Components = parseComponents(content, lines)
		}
	}

	return pf
}

// --- Import parsing ---

var (
	// import { x, y } from "module"
	reNamedImport = regexp.MustCompile(`(?m)^import\s+\{([^}]+)\}\s+from\s+['"]([^'"]+)['"]`)
	// import Name from "module"
	reDefaultImport = regexp.MustCompile(`(?m)^import\s+(\w+)\s+from\s+['"]([^'"]+)['"]`)
	// import Name, { x, y } from "module"
	reMixedImport = regexp.MustCompile(`(?m)^import\s+(\w+)\s*,\s*\{([^}]+)\}\s+from\s+['"]([^'"]+)['"]`)
	// import * as Name from "module"
	reNamespaceImport = regexp.MustCompile(`(?m)^import\s+\*\s+as\s+(\w+)\s+from\s+['"]([^'"]+)['"]`)
	// import "module" (side-effect)
	reSideEffectImport = regexp.MustCompile(`(?m)^import\s+['"]([^'"]+)['"]`)
)

func parseImports(content string, lines []string) []ParsedImport {
	var imports []ParsedImport

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "import") {
			continue
		}

		lineNum := i + 1

		// Mixed: import Default, { named } from "module"
		if m := reMixedImport.FindStringSubmatch(trimmed); m != nil {
			imports = append(imports, ParsedImport{
				DefaultName:  m[1],
				NamedImports: splitNames(m[2]),
				Source:       m[3],
				Line:         lineNum,
			})
			continue
		}

		// Namespace: import * as Name from "module"
		if m := reNamespaceImport.FindStringSubmatch(trimmed); m != nil {
			imports = append(imports, ParsedImport{
				NamespaceImport: m[1],
				Source:          m[2],
				Line:            lineNum,
			})
			continue
		}

		// Named: import { x, y } from "module"
		if m := reNamedImport.FindStringSubmatch(trimmed); m != nil {
			imports = append(imports, ParsedImport{
				NamedImports: splitNames(m[1]),
				Source:       m[2],
				Line:         lineNum,
			})
			continue
		}

		// Default: import Name from "module"
		if m := reDefaultImport.FindStringSubmatch(trimmed); m != nil {
			imports = append(imports, ParsedImport{
				DefaultName: m[1],
				Source:      m[2],
				Line:        lineNum,
			})
			continue
		}

		// Side-effect: import "module"
		if m := reSideEffectImport.FindStringSubmatch(trimmed); m != nil {
			imports = append(imports, ParsedImport{
				Source: m[1],
				Line:   lineNum,
			})
			continue
		}
	}

	return imports
}

func splitNames(s string) []string {
	var names []string
	for _, part := range strings.Split(s, ",") {
		name := strings.TrimSpace(part)
		// Handle "Name as Alias"
		if idx := strings.Index(name, " as "); idx >= 0 {
			name = strings.TrimSpace(name[idx+4:])
		}
		if name != "" {
			names = append(names, name)
		}
	}
	return names
}

// --- Export parsing ---

var (
	reExportDefault  = regexp.MustCompile(`(?m)^export\s+default\s+(?:function|class)\s+(\w+)`)
	reExportDefaultExpr = regexp.MustCompile(`(?m)^export\s+default\s+(\w+)`)
	reExportNamed    = regexp.MustCompile(`(?m)^export\s+(?:const|let|var|function|class|async\s+function)\s+(\w+)`)
	reExportType     = regexp.MustCompile(`(?m)^export\s+(?:type|interface)\s+(\w+)`)
	reExportBrace    = regexp.MustCompile(`(?m)^export\s+\{([^}]+)\}`)
)

func parseExports(content string, lines []string) []ParsedExport {
	var exports []ParsedExport

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "export") {
			continue
		}

		lineNum := i + 1

		// export default function/class Name
		if m := reExportDefault.FindStringSubmatch(trimmed); m != nil {
			exports = append(exports, ParsedExport{Name: m[1], Kind: "default", Line: lineNum})
			continue
		}

		// export default Name
		if m := reExportDefaultExpr.FindStringSubmatch(trimmed); m != nil {
			exports = append(exports, ParsedExport{Name: m[1], Kind: "default", Line: lineNum})
			continue
		}

		// export type/interface Name
		if m := reExportType.FindStringSubmatch(trimmed); m != nil {
			exports = append(exports, ParsedExport{Name: m[1], Kind: "type", Line: lineNum})
			continue
		}

		// export const/function/class Name
		if m := reExportNamed.FindStringSubmatch(trimmed); m != nil {
			kind := "const"
			if strings.Contains(trimmed, "function") {
				kind = "function"
			} else if strings.Contains(trimmed, "class") {
				kind = "class"
			}
			exports = append(exports, ParsedExport{Name: m[1], Kind: kind, Line: lineNum})
			continue
		}

		// export { Name1, Name2 }
		if m := reExportBrace.FindStringSubmatch(trimmed); m != nil {
			for _, name := range splitNames(m[1]) {
				exports = append(exports, ParsedExport{Name: name, Kind: "named", Line: lineNum})
			}
			continue
		}
	}

	return exports
}

// --- Component parsing ---

var (
	// export default function ComponentName(props: Props) {
	reFuncComponent = regexp.MustCompile(`(?m)^(?:export\s+(?:default\s+)?)?function\s+([A-Z]\w*)\s*\(([^)]*)\)`)
	// const ComponentName: React.FC<Props> = (...) => {
	// const ComponentName = (props: Props) => {
	// export const ComponentName = (...) => {
	reArrowComponent = regexp.MustCompile(`(?m)^(?:export\s+(?:default\s+)?)?(?:const|let|var)\s+([A-Z]\w*)\s*(?::\s*(?:React\.)?FC\s*<\s*(\w+)\s*>)?\s*=\s*(?:\([^)]*\)|(\w+))\s*(?::\s*\w+)?\s*=>`)
	// class ComponentName extends React.Component
	reClassComponent = regexp.MustCompile(`(?m)^(?:export\s+(?:default\s+)?)?class\s+([A-Z]\w*)\s+extends\s+(?:React\.)?(?:Component|PureComponent)`)
)

func parseComponents(content string, lines []string) []ParsedComponent {
	var components []ParsedComponent
	exportMap := buildExportMap(content)

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		lineNum := i + 1

		// Function component
		if m := reFuncComponent.FindStringSubmatch(trimmed); m != nil {
			name := m[1]
			propsType := extractPropsType(m[2])
			endLine := findBlockEnd(lines, i)
			// Verify it contains JSX
			block := strings.Join(lines[i:min(endLine, len(lines))], "\n")
			if !hasJSXReturn(block) {
				continue
			}
			components = append(components, ParsedComponent{
				Name:       name,
				Kind:       "function_component",
				ExportType: getExportType(name, trimmed, exportMap),
				LineStart:  lineNum,
				LineEnd:    endLine,
				PropsType:  propsType,
				Source:      block,
			})
			continue
		}

		// Arrow component
		if m := reArrowComponent.FindStringSubmatch(trimmed); m != nil {
			name := m[1]
			propsType := m[2] // from React.FC<Props>
			endLine := findBlockEnd(lines, i)
			block := strings.Join(lines[i:min(endLine, len(lines))], "\n")
			if !hasJSXReturn(block) {
				continue
			}
			if propsType == "" {
				propsType = name + "Props"
			}
			components = append(components, ParsedComponent{
				Name:       name,
				Kind:       "function_component",
				ExportType: getExportType(name, trimmed, exportMap),
				LineStart:  lineNum,
				LineEnd:    endLine,
				PropsType:  propsType,
				Source:      block,
			})
			continue
		}

		// Class component
		if m := reClassComponent.FindStringSubmatch(trimmed); m != nil {
			name := m[1]
			endLine := findBlockEnd(lines, i)
			block := strings.Join(lines[i:min(endLine, len(lines))], "\n")
			components = append(components, ParsedComponent{
				Name:       name,
				Kind:       "class_component",
				ExportType: getExportType(name, trimmed, exportMap),
				LineStart:  lineNum,
				LineEnd:    endLine,
				Source:      block,
			})
			continue
		}
	}

	return components
}

// --- Hook parsing ---

var reHookFunc = regexp.MustCompile(`(?m)^(?:export\s+(?:default\s+)?)?(?:function|const|let|var)\s+(use[A-Z]\w*)\s*[=(]`)

func parseHooks(content string, lines []string) []ParsedHook {
	var hooks []ParsedHook
	exportMap := buildExportMap(content)

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		lineNum := i + 1

		if m := reHookFunc.FindStringSubmatch(trimmed); m != nil {
			name := m[1]
			endLine := findBlockEnd(lines, i)
			block := strings.Join(lines[i:min(endLine, len(lines))], "\n")
			hooks = append(hooks, ParsedHook{
				Name:       name,
				ExportType: getExportType(name, trimmed, exportMap),
				LineStart:  lineNum,
				LineEnd:    endLine,
				Source:      block,
			})
		}
	}

	return hooks
}

// --- Type definition parsing ---

var (
	reInterface = regexp.MustCompile(`(?m)^(?:export\s+)?interface\s+(\w+)(?:\s+extends\s+[^{]+)?\s*\{`)
	reTypeAlias = regexp.MustCompile(`(?m)^(?:export\s+)?type\s+(\w+)\s*(?:<[^>]+>)?\s*=\s*\{`)
)

func parseTypeDefs(content string, lines []string) []ParsedTypeDef {
	var defs []ParsedTypeDef

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		lineNum := i + 1

		var name, kind string

		if m := reInterface.FindStringSubmatch(trimmed); m != nil {
			name = m[1]
			kind = "interface"
		} else if m := reTypeAlias.FindStringSubmatch(trimmed); m != nil {
			name = m[1]
			kind = "type_alias"
		}

		if name == "" {
			continue
		}

		endLine := findBlockEnd(lines, i)
		body := strings.Join(lines[i:min(endLine, len(lines))], "\n")
		props := parsePropsFromBody(body)

		defs = append(defs, ParsedTypeDef{
			Name:      name,
			Kind:      kind,
			Body:      body,
			Props:     props,
			LineStart: lineNum,
			LineEnd:   endLine,
		})
	}

	return defs
}

// parsePropsFromBody extracts individual properties from an interface/type body.
var rePropLine = regexp.MustCompile(`^\s+(\w+)(\?)?:\s*(.+?);?\s*$`)

func parsePropsFromBody(body string) []ParsedProp {
	var props []ParsedProp
	lines := strings.Split(body, "\n")

	for _, line := range lines {
		if m := rePropLine.FindStringSubmatch(line); m != nil {
			props = append(props, ParsedProp{
				Name:           m[1],
				IsOptional:     m[2] == "?",
				TypeAnnotation: strings.TrimRight(m[3], ";"),
			})
		}
	}

	return props
}

// --- JSX detection ---

var reJSXTag = regexp.MustCompile(`<[A-Za-z][A-Za-z0-9.]*(?:\s|>|/)`)

func detectJSX(content string) bool {
	return reJSXTag.MatchString(content)
}

func hasJSXReturn(block string) bool {
	// Check for return ( <JSX or return <JSX or arrow => ( <JSX or => <JSX
	if reJSXTag.MatchString(block) {
		return true
	}
	return false
}

// --- Helpers ---

func extractPropsType(params string) string {
	params = strings.TrimSpace(params)
	if params == "" {
		return ""
	}
	// props: ButtonProps or { name, onClick }: ButtonProps
	if idx := strings.LastIndex(params, ":"); idx >= 0 {
		typePart := strings.TrimSpace(params[idx+1:])
		// Remove generics
		if brk := strings.Index(typePart, "<"); brk >= 0 {
			typePart = typePart[:brk]
		}
		return typePart
	}
	return ""
}

func buildExportMap(content string) map[string]string {
	m := make(map[string]string)
	// Find "export default Name" at end of file
	re := regexp.MustCompile(`(?m)^export\s+default\s+(\w+)\s*;?\s*$`)
	for _, match := range re.FindAllStringSubmatch(content, -1) {
		m[match[1]] = "default"
	}
	return m
}

func getExportType(name, line string, exportMap map[string]string) string {
	if strings.Contains(line, "export default") {
		return "default"
	}
	if strings.HasPrefix(strings.TrimSpace(line), "export") {
		return "named"
	}
	if t, ok := exportMap[name]; ok {
		return t
	}
	return "none"
}

func findBlockEnd(lines []string, startIdx int) int {
	depth := 0
	started := false
	for i := startIdx; i < len(lines); i++ {
		for _, ch := range lines[i] {
			if ch == '{' || ch == '(' {
				depth++
				started = true
			} else if ch == '}' || ch == ')' {
				depth--
			}
		}
		if started && depth <= 0 {
			return i + 1
		}
	}
	return len(lines)
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
