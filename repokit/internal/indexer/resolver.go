package indexer

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// ImportResolver resolves import specifiers to repo-relative file paths.
type ImportResolver struct {
	repoRoot   string
	pathAliases map[string]string // "@/*" -> "src/*"
	fileIndex  map[string]bool   // set of known repo-relative paths
}

// NewImportResolver creates a resolver by parsing tsconfig.json and building a file index.
func NewImportResolver(repoRoot string, files []WalkedFile) *ImportResolver {
	r := &ImportResolver{
		repoRoot:    repoRoot,
		pathAliases: make(map[string]string),
		fileIndex:   make(map[string]bool),
	}

	// Build file index
	for _, f := range files {
		r.fileIndex[f.Path] = true
	}

	// Parse tsconfig.json
	r.parseTsConfig()

	return r
}

func (r *ImportResolver) parseTsConfig() {
	data, err := os.ReadFile(filepath.Join(r.repoRoot, "tsconfig.json"))
	if err != nil {
		return
	}

	var tsconfig struct {
		CompilerOptions struct {
			BaseURL string              `json:"baseUrl"`
			Paths   map[string][]string `json:"paths"`
		} `json:"compilerOptions"`
	}
	if err := json.Unmarshal(data, &tsconfig); err != nil {
		return
	}

	baseURL := tsconfig.CompilerOptions.BaseURL
	if baseURL == "" {
		baseURL = "."
	}

	for alias, targets := range tsconfig.CompilerOptions.Paths {
		if len(targets) == 0 {
			continue
		}
		// "@/*" -> ["./src/*"] => alias="@/", target="src/"
		aliasPrefix := strings.TrimSuffix(alias, "*")
		targetPrefix := strings.TrimSuffix(targets[0], "*")
		targetPrefix = strings.TrimPrefix(targetPrefix, "./")

		// Combine with baseURL
		if baseURL != "." && baseURL != "" {
			targetPrefix = filepath.ToSlash(filepath.Join(baseURL, targetPrefix))
		}

		r.pathAliases[aliasPrefix] = targetPrefix
	}
}

// Resolve resolves an import specifier relative to the importing file.
// Returns the repo-relative path of the imported file, or empty string if unresolved.
func (r *ImportResolver) Resolve(importSource string, fromFile string) string {
	// External packages
	if isExternalImport(importSource) {
		return ""
	}

	// Try path alias resolution
	for alias, target := range r.pathAliases {
		if strings.HasPrefix(importSource, alias) {
			rest := strings.TrimPrefix(importSource, alias)
			resolved := filepath.ToSlash(filepath.Join(target, rest))
			if found := r.findFile(resolved); found != "" {
				return found
			}
		}
	}

	// Relative import resolution
	if strings.HasPrefix(importSource, ".") {
		dir := filepath.Dir(fromFile)
		resolved := filepath.ToSlash(filepath.Join(dir, importSource))
		resolved = filepath.Clean(resolved)
		resolved = filepath.ToSlash(resolved)
		if found := r.findFile(resolved); found != "" {
			return found
		}
	}

	return ""
}

// findFile tries to resolve a path to an actual file, checking extensions and index files.
func (r *ImportResolver) findFile(path string) string {
	// Direct match
	if r.fileIndex[path] {
		return path
	}

	// Try with extensions
	extensions := []string{".tsx", ".ts", ".jsx", ".js", ".mjs"}
	for _, ext := range extensions {
		candidate := path + ext
		if r.fileIndex[candidate] {
			return candidate
		}
	}

	// Try as directory with index file
	indexFiles := []string{
		filepath.ToSlash(filepath.Join(path, "index.tsx")),
		filepath.ToSlash(filepath.Join(path, "index.ts")),
		filepath.ToSlash(filepath.Join(path, "index.jsx")),
		filepath.ToSlash(filepath.Join(path, "index.js")),
	}
	for _, idx := range indexFiles {
		if r.fileIndex[idx] {
			return idx
		}
	}

	return ""
}

func isExternalImport(source string) bool {
	return !strings.HasPrefix(source, ".") && !strings.HasPrefix(source, "/") && !strings.HasPrefix(source, "@/")
}
