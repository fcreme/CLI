package detector

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/usuario/repokit/internal/store"
	"github.com/usuario/repokit/pkg/models"
)

// DetectStack analyzes the repository to detect its technology stack.
func DetectStack(ctx context.Context, repoRoot string, s *store.Store) ([]models.StackItem, error) {
	if err := s.ClearStack(ctx); err != nil {
		return nil, fmt.Errorf("clearing stack: %w", err)
	}

	var items []models.StackItem

	// Detect from package.json (also returns allDeps for version lookups)
	pkgItems, allDeps, err := detectFromPackageJSON(repoRoot)
	if err == nil {
		items = append(items, pkgItems...)
	}
	if allDeps == nil {
		allDeps = make(map[string]string)
	}

	// Detect from tsconfig.json
	if fileExists(filepath.Join(repoRoot, "tsconfig.json")) {
		tsVersion := ""
		if v, ok := allDeps["typescript"]; ok {
			tsVersion = cleanVersion(v)
		}
		items = append(items, models.StackItem{
			Category: "language",
			Name:     "TypeScript",
			Version:  tsVersion,
			Evidence: `{"file":"tsconfig.json"}`,
		})
	}

	// Detect bundlers/frameworks from config files
	items = append(items, detectConfigFiles(repoRoot, allDeps)...)

	// Detect from go.mod
	if fileExists(filepath.Join(repoRoot, "go.mod")) {
		items = append(items, models.StackItem{
			Category: "language",
			Name:     "Go",
			Evidence: `{"file":"go.mod"}`,
		})
	}

	// Detect from requirements.txt / pyproject.toml
	if fileExists(filepath.Join(repoRoot, "requirements.txt")) || fileExists(filepath.Join(repoRoot, "pyproject.toml")) {
		evidence := "requirements.txt"
		if fileExists(filepath.Join(repoRoot, "pyproject.toml")) {
			evidence = "pyproject.toml"
		}
		items = append(items, models.StackItem{
			Category: "language",
			Name:     "Python",
			Evidence: fmt.Sprintf(`{"file":"%s"}`, evidence),
		})
	}

	// Store all items
	for _, item := range items {
		if err := s.UpsertStackItem(ctx, &item); err != nil {
			return nil, fmt.Errorf("storing stack item %s: %w", item.Name, err)
		}
	}

	return s.GetStack(ctx)
}

func detectFromPackageJSON(repoRoot string) ([]models.StackItem, map[string]string, error) {
	data, err := os.ReadFile(filepath.Join(repoRoot, "package.json"))
	if err != nil {
		return nil, nil, err
	}

	var pkg struct {
		Dependencies    map[string]string `json:"dependencies"`
		DevDependencies map[string]string `json:"devDependencies"`
	}
	if err := json.Unmarshal(data, &pkg); err != nil {
		return nil, nil, err
	}

	var items []models.StackItem
	allDeps := mergeDepMaps(pkg.Dependencies, pkg.DevDependencies)

	// Frameworks
	frameworkMap := map[string]string{
		"react":      "React",
		"next":       "Next.js",
		"vue":        "Vue",
		"@angular/core": "Angular",
		"svelte":     "Svelte",
		"solid-js":   "Solid",
		"express":    "Express",
		"fastify":    "Fastify",
		"koa":        "Koa",
		"hono":       "Hono",
	}

	// Styling
	stylingMap := map[string]string{
		"tailwindcss":         "Tailwind CSS",
		"styled-components":   "styled-components",
		"@emotion/react":      "Emotion",
		"sass":                "Sass",
		"less":                "Less",
		"@chakra-ui/react":    "Chakra UI",
		"@mui/material":       "Material UI",
		"antd":                "Ant Design",
	}

	// State management
	stateMap := map[string]string{
		"redux":              "Redux",
		"@reduxjs/toolkit":   "Redux Toolkit",
		"zustand":            "Zustand",
		"jotai":              "Jotai",
		"recoil":             "Recoil",
		"mobx":               "MobX",
		"@tanstack/react-query": "TanStack Query",
	}

	// Testing
	testingMap := map[string]string{
		"jest":              "Jest",
		"vitest":            "Vitest",
		"@testing-library/react": "React Testing Library",
		"cypress":           "Cypress",
		"playwright":         "Playwright",
		"@playwright/test":   "Playwright",
	}

	// Bundlers
	bundlerMap := map[string]string{
		"vite":     "Vite",
		"webpack":  "Webpack",
		"esbuild":  "esbuild",
		"turbo":    "Turbo",
		"rollup":   "Rollup",
		"parcel":   "Parcel",
	}

	categories := []struct {
		category string
		mapping  map[string]string
	}{
		{"framework", frameworkMap},
		{"styling", stylingMap},
		{"state", stateMap},
		{"testing", testingMap},
		{"bundler", bundlerMap},
	}

	for _, cat := range categories {
		for pkg, name := range cat.mapping {
			if version, ok := allDeps[pkg]; ok {
				depType := "dependencies"
				if _, isDev := allDeps[pkg]; isDev {
					if _, isProd := allDeps[pkg]; !isProd {
						depType = "devDependencies"
					}
				}
				items = append(items, models.StackItem{
					Category: cat.category,
					Name:     name,
					Version:  cleanVersion(version),
					Evidence: fmt.Sprintf(`{"file":"package.json","field":"%s.%s"}`, depType, pkg),
				})
			}
		}
	}

	// JavaScript/Node is always present with package.json
	nodeVersion := ""
	if v, ok := allDeps["node"]; ok {
		nodeVersion = cleanVersion(v)
	}
	items = append(items, models.StackItem{
		Category: "language",
		Name:     "JavaScript",
		Version:  nodeVersion,
		Evidence: `{"file":"package.json"}`,
	})

	return items, allDeps, nil
}

func detectConfigFiles(repoRoot string, allDeps map[string]string) []models.StackItem {
	var items []models.StackItem

	// Maps config file -> category, display name, package.json key for version lookup
	configMap := map[string]struct {
		category string
		name     string
		pkg      string // package name in package.json for version lookup
	}{
		"vite.config.ts":       {"bundler", "Vite", "vite"},
		"vite.config.js":       {"bundler", "Vite", "vite"},
		"next.config.js":       {"framework", "Next.js", "next"},
		"next.config.mjs":      {"framework", "Next.js", "next"},
		"next.config.ts":       {"framework", "Next.js", "next"},
		"tailwind.config.js":   {"styling", "Tailwind CSS", "tailwindcss"},
		"tailwind.config.ts":   {"styling", "Tailwind CSS", "tailwindcss"},
		"postcss.config.js":    {"tooling", "PostCSS", "postcss"},
		"postcss.config.mjs":   {"tooling", "PostCSS", "postcss"},
		".eslintrc.json":       {"tooling", "ESLint", "eslint"},
		".eslintrc.js":         {"tooling", "ESLint", "eslint"},
		"eslint.config.js":     {"tooling", "ESLint", "eslint"},
		"eslint.config.mjs":    {"tooling", "ESLint", "eslint"},
		".prettierrc":          {"tooling", "Prettier", "prettier"},
		"prettier.config.js":   {"tooling", "Prettier", "prettier"},
		"jest.config.js":       {"testing", "Jest", "jest"},
		"jest.config.ts":       {"testing", "Jest", "jest"},
		"vitest.config.ts":     {"testing", "Vitest", "vitest"},
		"vitest.config.js":     {"testing", "Vitest", "vitest"},
		"cypress.config.js":    {"testing", "Cypress", "cypress"},
		"cypress.config.ts":    {"testing", "Cypress", "cypress"},
		"playwright.config.ts": {"testing", "Playwright", "playwright"},
		"webpack.config.js":    {"bundler", "Webpack", "webpack"},
		"rollup.config.js":     {"bundler", "Rollup", "rollup"},
		"docker-compose.yml":   {"infra", "Docker Compose", ""},
		"Dockerfile":           {"infra", "Docker", ""},
	}

	seen := make(map[string]bool) // avoid duplicate entries
	for file, info := range configMap {
		if fileExists(filepath.Join(repoRoot, file)) {
			if seen[info.name] {
				continue
			}
			seen[info.name] = true

			version := ""
			if info.pkg != "" {
				if v, ok := allDeps[info.pkg]; ok {
					version = cleanVersion(v)
				}
			}

			items = append(items, models.StackItem{
				Category: info.category,
				Name:     info.name,
				Version:  version,
				Evidence: fmt.Sprintf(`{"file":"%s"}`, file),
			})

			// Special: add Docker base image as separate runtime entry
			if info.name == "Docker" {
				if base := dockerBaseImage(repoRoot); base != "" {
					items = append(items, models.StackItem{
						Category: "runtime",
						Name:     "Docker Base Image",
						Version:  base,
						Evidence: `{"file":"Dockerfile","field":"FROM"}`,
					})
				}
			}
		}
	}

	return items
}

func mergeDepMaps(maps ...map[string]string) map[string]string {
	result := make(map[string]string)
	for _, m := range maps {
		for k, v := range m {
			result[k] = v
		}
	}
	return result
}

// dockerBaseImage reads the first FROM line in a Dockerfile and returns the image:tag.
func dockerBaseImage(repoRoot string) string {
	f, err := os.Open(filepath.Join(repoRoot, "Dockerfile"))
	if err != nil {
		return ""
	}
	defer f.Close()

	re := regexp.MustCompile(`(?i)^FROM\s+(\S+)`)
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		if m := re.FindStringSubmatch(scanner.Text()); m != nil {
			return m[1] // e.g. "node:18-alpine"
		}
	}
	return ""
}

// cleanVersion strips semver prefixes like ^, ~, >=, etc.
func cleanVersion(v string) string {
	v = strings.TrimSpace(v)
	v = strings.TrimLeft(v, "^~>=<! ")
	return v
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
