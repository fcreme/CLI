package scaffold

import (
	"path/filepath"
	"strings"

	"github.com/usuario/repokit/pkg/models"
)

// Conventions holds the detected patterns from an existing component.
type Conventions struct {
	// File placement
	Directory    string // directory where the component lives
	FilePattern  string // "flat" (Component.tsx) or "barrel" (Component/index.tsx)
	FileExt      string // ".tsx", ".ts", ".jsx", ".js"

	// Naming
	ExportStyle  string // "default" or "named"
	FunctionStyle string // "declaration" (function X) or "arrow" (const X = () =>)

	// Props
	PropsStyle   string // "interface" or "type"
	PropsInline  bool   // props defined inline vs separate type

	// Imports
	HasReactImport bool
	UsesClientDirective bool // 'use client'
	UsesServerDirective bool // 'use server'
	ImportGroups []ImportGroup

	// Styling
	StyleApproach string // "tailwind", "css-module", "styled-components", "inline", "none"

	// Hooks used
	HooksUsed []string
}

// ImportGroup represents a group of imports from the source component.
type ImportGroup struct {
	Source string
	Names  []string
	IsType bool
}

// ExtractConventions analyzes an existing component to determine coding conventions.
// fullFileContent is the complete file content (optional, for detecting file-level directives).
func ExtractConventions(comp *models.Component, fullFileContent string) *Conventions {
	conv := &Conventions{
		FileExt: filepath.Ext(comp.FilePath),
	}

	source := comp.RawSource
	if source == "" {
		// Minimal defaults
		conv.Directory = filepath.Dir(comp.FilePath)
		conv.ExportStyle = comp.ExportType
		conv.FunctionStyle = "declaration"
		conv.FileExt = ".tsx"
		return conv
	}

	// Directory
	conv.Directory = filepath.ToSlash(filepath.Dir(comp.FilePath))

	// Check for barrel pattern (index.tsx inside a folder named after the component)
	baseName := filepath.Base(comp.FilePath)
	if baseName == "index.tsx" || baseName == "index.ts" || baseName == "index.jsx" || baseName == "index.js" {
		conv.FilePattern = "barrel"
	} else {
		conv.FilePattern = "flat"
	}

	// Export style
	conv.ExportStyle = comp.ExportType
	if conv.ExportStyle == "" || conv.ExportStyle == "none" {
		conv.ExportStyle = "named"
	}

	// Function style
	if strings.Contains(source, "function "+comp.Name) {
		conv.FunctionStyle = "declaration"
	} else if strings.Contains(source, "const "+comp.Name) || strings.Contains(source, "let "+comp.Name) {
		conv.FunctionStyle = "arrow"
	} else {
		conv.FunctionStyle = "declaration"
	}

	// Directives - check full file content first, then source
	checkContent := source
	if fullFileContent != "" {
		checkContent = fullFileContent
	}
	conv.UsesClientDirective = strings.Contains(checkContent, "'use client'") || strings.Contains(checkContent, "\"use client\"")
	conv.UsesServerDirective = strings.Contains(checkContent, "'use server'") || strings.Contains(checkContent, "\"use server\"")

	// React import
	conv.HasReactImport = strings.Contains(source, "from 'react'") || strings.Contains(source, "from \"react\"")

	// Props style
	if strings.Contains(source, "interface "+comp.Name+"Props") {
		conv.PropsStyle = "interface"
	} else if strings.Contains(source, "type "+comp.Name+"Props") {
		conv.PropsStyle = "type"
	} else {
		conv.PropsStyle = "interface"
	}

	// Style approach
	conv.StyleApproach = detectStyleApproach(source)

	// Hooks used
	conv.HooksUsed = detectHooksUsed(source)

	return conv
}

func detectStyleApproach(source string) string {
	lower := strings.ToLower(source)
	if strings.Contains(lower, "classname=") && (strings.Contains(lower, "flex") || strings.Contains(lower, "bg-") || strings.Contains(lower, "text-")) {
		return "tailwind"
	}
	if strings.Contains(lower, "styles.") && strings.Contains(lower, "import") {
		return "css-module"
	}
	if strings.Contains(lower, "styled.") || strings.Contains(lower, "styled(") {
		return "styled-components"
	}
	if strings.Contains(lower, "style={{") || strings.Contains(lower, "style={") {
		return "inline"
	}
	return "none"
}

func detectHooksUsed(source string) []string {
	hooks := []string{
		"useState", "useEffect", "useRef", "useMemo", "useCallback",
		"useContext", "useReducer", "useLayoutEffect",
	}
	var used []string
	for _, h := range hooks {
		if strings.Contains(source, h) {
			used = append(used, h)
		}
	}
	return used
}

// DeriveTargetDir determines where a new component should be placed.
func DeriveTargetDir(conv *Conventions, newName string) string {
	if conv.FilePattern == "barrel" {
		return filepath.ToSlash(filepath.Join(conv.Directory, newName))
	}
	return conv.Directory
}

// DeriveFileName determines the filename for a new component.
func DeriveFileName(conv *Conventions, newName string) string {
	if conv.FilePattern == "barrel" {
		return "index" + conv.FileExt
	}
	return newName + conv.FileExt
}
