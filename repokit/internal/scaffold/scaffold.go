package scaffold

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/template"

	"github.com/fcreme/CLI/repokit/internal/store"
	"github.com/fcreme/CLI/repokit/pkg/models"
)

// ScaffoldRequest describes what to scaffold.
type ScaffoldRequest struct {
	NewName   string
	BasedOnID int64
	TargetDir string // override target directory (optional)
	Write     bool   // if true, write to disk; if false, dry-run
	RepoRoot  string
}

// ScaffoldResult holds the generated output.
type ScaffoldResult struct {
	FilePath string // proposed file path (repo-relative)
	AbsPath  string // absolute path on disk
	Content  string // generated file content
	Written  bool   // whether the file was actually created
}

// Scaffold generates a new component based on an existing one.
func Scaffold(ctx context.Context, s *store.Store, req ScaffoldRequest) (*ScaffoldResult, error) {
	// Load the base component
	base, err := s.GetComponentByID(ctx, req.BasedOnID)
	if err != nil {
		return nil, fmt.Errorf("loading base component (id=%d): %w", req.BasedOnID, err)
	}

	// Read the full file for directive detection
	fullFilePath := filepath.Join(req.RepoRoot, filepath.FromSlash(base.FilePath))
	fullContent, _ := os.ReadFile(fullFilePath)

	// Extract conventions
	conv := ExtractConventions(base, string(fullContent))

	// Determine target directory and filename
	targetDir := conv.Directory
	if req.TargetDir != "" {
		targetDir = req.TargetDir
	} else {
		targetDir = DeriveTargetDir(conv, req.NewName)
	}

	fileName := DeriveFileName(conv, req.NewName)
	relPath := filepath.ToSlash(filepath.Join(targetDir, fileName))
	absPath := filepath.Join(req.RepoRoot, filepath.FromSlash(relPath))

	// Generate content from template
	content, err := generateComponent(req.NewName, base, conv)
	if err != nil {
		return nil, fmt.Errorf("generating component: %w", err)
	}

	result := &ScaffoldResult{
		FilePath: relPath,
		AbsPath:  absPath,
		Content:  content,
	}

	// Write if requested
	if req.Write {
		// Check if file already exists
		if _, err := os.Stat(absPath); err == nil {
			return nil, fmt.Errorf("file already exists: %s", relPath)
		}

		// Create directory
		dir := filepath.Dir(absPath)
		if err := os.MkdirAll(dir, 0755); err != nil {
			return nil, fmt.Errorf("creating directory: %w", err)
		}

		// Write file
		if err := os.WriteFile(absPath, []byte(content), 0644); err != nil {
			return nil, fmt.Errorf("writing file: %w", err)
		}
		result.Written = true
	}

	return result, nil
}

// componentData holds template variables.
type componentData struct {
	Name            string
	BaseName        string
	PropsName       string
	ExportKeyword   string
	ExportDefault   string
	FuncKeyword     string
	ClientDirective string
	PropsStyle      string
	StyleImports    string
	HookImports     string
	ReactImport     string
	PropsBody       string
	HooksBody       string
	ReturnBody      string
}

const componentTemplate = `{{.ClientDirective}}{{.ReactImport}}{{.HookImports}}{{.StyleImports}}
{{.PropsStyle}}

{{.ExportKeyword}}{{.FuncKeyword}} {{.Name}}({{ if .PropsName }}{ }: {{.PropsName}}{{ end }}) {
{{.HooksBody}}  return (
    <div>
      <h2>{{.Name}}</h2>
      {/* TODO: implement {{.Name}} based on {{.BaseName}} */}
    </div>
  )
}
{{.ExportDefault}}`

func generateComponent(newName string, base *models.Component, conv *Conventions) (string, error) {
	data := componentData{
		Name:     newName,
		BaseName: base.Name,
	}

	// Props name
	propsName := newName + "Props"
	data.PropsName = propsName

	// Client directive
	if conv.UsesClientDirective {
		data.ClientDirective = "'use client'\n\n"
	}

	// React import
	if conv.HasReactImport || len(conv.HooksUsed) > 0 {
		if len(conv.HooksUsed) > 0 {
			data.ReactImport = fmt.Sprintf("import { %s } from 'react'\n", strings.Join(conv.HooksUsed, ", "))
		} else {
			data.ReactImport = "import React from 'react'\n"
		}
	}

	// Props definition
	if conv.PropsStyle == "type" {
		data.PropsStyle = fmt.Sprintf("type %s = {\n  // TODO: define props\n}", propsName)
	} else {
		data.PropsStyle = fmt.Sprintf("interface %s {\n  // TODO: define props\n}", propsName)
	}

	// Export style
	if conv.ExportStyle == "default" {
		if conv.FunctionStyle == "arrow" {
			data.ExportKeyword = "const "
			data.FuncKeyword = ""
			data.ExportDefault = fmt.Sprintf("\nexport default %s\n", newName)
		} else {
			data.ExportKeyword = "export default "
			data.FuncKeyword = "function"
			data.ExportDefault = ""
		}
	} else {
		data.ExportKeyword = "export "
		data.FuncKeyword = "function"
		data.ExportDefault = ""
	}

	// Arrow function style
	if conv.FunctionStyle == "arrow" && conv.ExportStyle != "default" {
		data.ExportKeyword = "export const "
		data.FuncKeyword = ""
	}

	// Hooks body
	if len(conv.HooksUsed) > 0 {
		var hookLines []string
		for _, h := range conv.HooksUsed {
			switch h {
			case "useState":
				hookLines = append(hookLines, "  const [state, setState] = useState(null)")
			case "useEffect":
				hookLines = append(hookLines, "  useEffect(() => {\n    // TODO: implement effect\n  }, [])")
			case "useRef":
				hookLines = append(hookLines, "  const ref = useRef(null)")
			case "useMemo":
				hookLines = append(hookLines, "  const memoized = useMemo(() => {\n    // TODO: implement\n    return null\n  }, [])")
			case "useCallback":
				hookLines = append(hookLines, "  const callback = useCallback(() => {\n    // TODO: implement\n  }, [])")
			}
		}
		if len(hookLines) > 0 {
			data.HooksBody = strings.Join(hookLines, "\n") + "\n\n"
		}
	}

	// Handle arrow function template differently
	tmplStr := componentTemplate
	if conv.FunctionStyle == "arrow" {
		tmplStr = buildArrowTemplate(conv)
	}

	tmpl, err := template.New("component").Parse(tmplStr)
	if err != nil {
		return "", fmt.Errorf("parsing template: %w", err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("executing template: %w", err)
	}

	// Clean up excessive blank lines
	result := cleanupBlankLines(buf.String())
	return result, nil
}

func buildArrowTemplate(conv *Conventions) string {
	return `{{.ClientDirective}}{{.ReactImport}}{{.HookImports}}{{.StyleImports}}
{{.PropsStyle}}

{{.ExportKeyword}}{{.Name}} = ({{ if .PropsName }}{ }: {{.PropsName}}{{ end }}) => {
{{.HooksBody}}  return (
    <div>
      <h2>{{.Name}}</h2>
      {/* TODO: implement {{.Name}} based on {{.BaseName}} */}
    </div>
  )
}
{{.ExportDefault}}`
}

func cleanupBlankLines(s string) string {
	lines := strings.Split(s, "\n")
	var result []string
	prevBlank := false
	for _, line := range lines {
		isBlank := strings.TrimSpace(line) == ""
		if isBlank && prevBlank {
			continue
		}
		result = append(result, line)
		prevBlank = isBlank
	}
	// Trim leading/trailing blank lines
	for len(result) > 0 && strings.TrimSpace(result[0]) == "" {
		result = result[1:]
	}
	for len(result) > 0 && strings.TrimSpace(result[len(result)-1]) == "" {
		result = result[:len(result)-1]
	}
	return strings.Join(result, "\n") + "\n"
}
