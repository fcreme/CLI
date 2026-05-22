package indexer

import (
	"context"
	"path/filepath"

	"github.com/fcreme/CLI/repokit/internal/parser"
	"github.com/fcreme/CLI/repokit/pkg/models"
)

// ReactAnalyzer extracts React components, hooks, and TypeScript types from source files.
type ReactAnalyzer struct {
	resolver *ImportResolver
}

func NewReactAnalyzer() *ReactAnalyzer {
	return &ReactAnalyzer{}
}

// SetResolver sets the import resolver for path alias resolution.
func (a *ReactAnalyzer) SetResolver(r *ImportResolver) {
	a.resolver = r
}

func (a *ReactAnalyzer) SupportedExtensions() []string {
	return []string{".tsx", ".ts", ".jsx", ".js", ".mjs"}
}

func (a *ReactAnalyzer) Analyze(ctx context.Context, filePath string, content []byte, language string) (*AnalysisResult, error) {
	parsed := parser.Parse(string(content), language)
	result := &AnalysisResult{}

	// Build a map of type definitions for prop linking
	typeDefMap := make(map[string]*parser.ParsedTypeDef)
	for i := range parsed.TypeDefinitions {
		td := &parsed.TypeDefinitions[i]
		typeDefMap[td.Name] = td
	}

	// Convert components
	for _, pc := range parsed.Components {
		comp := models.Component{
			FilePath:   filePath,
			Name:       pc.Name,
			Kind:       models.EntityKind(pc.Kind),
			ExportType: pc.ExportType,
			LineStart:  pc.LineStart,
			LineEnd:    pc.LineEnd,
			RawSource:  pc.Source,
		}
		result.Components = append(result.Components, comp)
	}

	// Convert hooks as components with kind=hook
	for _, ph := range parsed.Hooks {
		hook := models.Component{
			FilePath:   filePath,
			Name:       ph.Name,
			Kind:       models.KindHook,
			ExportType: ph.ExportType,
			LineStart:  ph.LineStart,
			LineEnd:    ph.LineEnd,
			RawSource:  ph.Source,
		}
		result.Hooks = append(result.Hooks, hook)
	}

	// Convert type definitions
	for _, ptd := range parsed.TypeDefinitions {
		td := models.TypeDefinition{
			Name:      ptd.Name,
			Kind:      ptd.Kind,
			Body:      ptd.Body,
			LineStart: ptd.LineStart,
			LineEnd:   ptd.LineEnd,
		}
		result.TypeDefinitions = append(result.TypeDefinitions, td)
	}

	// Convert imports
	for _, pi := range parsed.Imports {
		imp := models.Import{
			SourcePath: pi.Source,
			IsExternal: isExternalImport(pi.Source),
		}

		var names []string
		if pi.DefaultName != "" {
			names = append(names, pi.DefaultName)
		}
		names = append(names, pi.NamedImports...)
		if pi.NamespaceImport != "" {
			names = append(names, "*:"+pi.NamespaceImport)
		}
		imp.ImportedNames = names

		// Resolve imports using resolver (handles path aliases, extensions, index files)
		if a.resolver != nil {
			imp.IsExternal = isExternalImport(pi.Source)
			if !imp.IsExternal {
				imp.ResolvedPath = a.resolver.Resolve(pi.Source, filePath)
			}
		} else if !imp.IsExternal {
			imp.ResolvedPath = resolveImportPath(filePath, pi.Source)
		}

		result.Imports = append(result.Imports, imp)
	}

	// Convert exports
	for _, pe := range parsed.Exports {
		exp := models.Export{
			Name:      pe.Name,
			Kind:      pe.Kind,
			LineStart: pe.Line,
		}
		result.Exports = append(result.Exports, exp)
	}

	return result, nil
}

// Note: isExternalImport is defined in resolver.go

// resolveImportPath resolves a relative import to a repo-relative path.
func resolveImportPath(currentFile, importSource string) string {
	dir := filepath.Dir(currentFile)
	resolved := filepath.Join(dir, importSource)
	resolved = filepath.ToSlash(resolved)

	// Clean the path
	resolved = filepath.Clean(resolved)
	resolved = filepath.ToSlash(resolved)

	return resolved
}
