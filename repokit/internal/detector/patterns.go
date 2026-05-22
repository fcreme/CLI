package detector

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/fcreme/CLI/repokit/internal/store"
	"github.com/fcreme/CLI/repokit/pkg/models"
)

// DetectPatterns analyzes the indexed files to detect project conventions.
func DetectPatterns(ctx context.Context, repoRoot string, s *store.Store) ([]models.Pattern, error) {
	var patterns []models.Pattern

	// Get all indexed files
	rows, err := s.DB().QueryContext(ctx, "SELECT path, language FROM files ORDER BY path")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var files []fileInfo
	dirMap := make(map[string][]string) // dir -> filenames
	extCount := make(map[string]int)

	for rows.Next() {
		var f fileInfo
		if err := rows.Scan(&f.path, &f.language); err != nil {
			continue
		}
		files = append(files, f)
		dir := filepath.ToSlash(filepath.Dir(f.path))
		dirMap[dir] = append(dirMap[dir], filepath.Base(f.path))
		ext := filepath.Ext(f.path)
		extCount[ext]++
	}

	// Detect folder structure patterns
	patterns = append(patterns, detectFolderPatterns(dirMap)...)

	// Detect naming conventions
	patterns = append(patterns, detectNamingPatterns(files)...)

	// Detect barrel export pattern
	patterns = append(patterns, detectBarrelPattern(dirMap)...)

	// Detect test co-location
	patterns = append(patterns, detectTestPatterns(files, dirMap)...)

	// Detect import style from components
	patterns = append(patterns, detectImportPatterns(ctx, s)...)

	// Store patterns
	for _, p := range patterns {
		if err := s.InsertPattern(ctx, &p); err != nil {
			continue
		}
	}

	return patterns, nil
}

type fileInfo struct {
	path     string
	language string
}

func detectFolderPatterns(dirMap map[string][]string) []models.Pattern {
	var patterns []models.Pattern

	// Check for common React folder structures
	hasComponents := false
	hasPages := false
	hasApp := false
	hasHooks := false
	hasUtils := false
	hasContexts := false
	hasServices := false
	hasLib := false

	for dir := range dirMap {
		parts := strings.Split(dir, "/")
		for _, p := range parts {
			switch strings.ToLower(p) {
			case "components":
				hasComponents = true
			case "pages":
				hasPages = true
			case "app":
				hasApp = true
			case "hooks":
				hasHooks = true
			case "utils", "util", "helpers":
				hasUtils = true
			case "contexts", "context":
				hasContexts = true
			case "services", "api":
				hasServices = true
			case "lib":
				hasLib = true
			}
		}
	}

	if hasApp && hasComponents {
		patterns = append(patterns, models.Pattern{
			Kind:        "folder_structure",
			Description: "Next.js App Router structure with components/ directory",
			Confidence:  0.95,
		})
	} else if hasPages && hasComponents {
		patterns = append(patterns, models.Pattern{
			Kind:        "folder_structure",
			Description: "Pages-based routing with components/ directory",
			Confidence:  0.9,
		})
	}

	if hasHooks {
		patterns = append(patterns, models.Pattern{
			Kind:        "folder_structure",
			Description: "Custom hooks separated into hooks/ directory",
			Confidence:  0.9,
		})
	}

	if hasContexts {
		patterns = append(patterns, models.Pattern{
			Kind:        "folder_structure",
			Description: "Context providers separated into contexts/ directory",
			Confidence:  0.9,
		})
	}

	if hasUtils {
		patterns = append(patterns, models.Pattern{
			Kind:        "folder_structure",
			Description: "Utilities separated into utils/ or helpers/ directory",
			Confidence:  0.9,
		})
	}

	if hasServices || hasLib {
		patterns = append(patterns, models.Pattern{
			Kind:        "folder_structure",
			Description: "Service/API layer in services/ or lib/ directory",
			Confidence:  0.85,
		})
	}

	// Detect component sub-organization
	componentDirs := make(map[string]bool)
	for dir := range dirMap {
		if strings.Contains(dir, "components/") {
			parts := strings.Split(dir, "components/")
			if len(parts) > 1 {
				subDir := strings.Split(parts[1], "/")[0]
				if subDir != "" {
					componentDirs[subDir] = true
				}
			}
		}
	}

	if len(componentDirs) > 2 {
		dirs := make([]string, 0, len(componentDirs))
		for d := range componentDirs {
			dirs = append(dirs, d)
		}
		patterns = append(patterns, models.Pattern{
			Kind:        "folder_structure",
			Description: fmt.Sprintf("Components organized by category: %s", strings.Join(dirs, ", ")),
			Evidence:    dirs,
			Confidence:  0.9,
		})
	}

	return patterns
}

func detectNamingPatterns(files []fileInfo) []models.Pattern {
	var patterns []models.Pattern

	pascalCount := 0
	kebabCount := 0
	camelCount := 0
	totalComponents := 0

	for _, f := range files {
		ext := filepath.Ext(f.path)
		if ext != ".tsx" && ext != ".jsx" {
			continue
		}
		name := strings.TrimSuffix(filepath.Base(f.path), ext)
		if name == "index" || name == "page" || name == "layout" {
			continue
		}
		totalComponents++

		if isUpperFirst(name) && !strings.Contains(name, "-") && !strings.Contains(name, "_") {
			pascalCount++
		} else if strings.Contains(name, "-") {
			kebabCount++
		} else if isLowerFirst(name) {
			camelCount++
		}
	}

	if totalComponents > 0 {
		if float64(pascalCount)/float64(totalComponents) > 0.7 {
			patterns = append(patterns, models.Pattern{
				Kind:        "naming_convention",
				Description: "Component files use PascalCase naming (e.g., UserProfile.tsx)",
				Confidence:  float64(pascalCount) / float64(totalComponents),
			})
		} else if float64(kebabCount)/float64(totalComponents) > 0.7 {
			patterns = append(patterns, models.Pattern{
				Kind:        "naming_convention",
				Description: "Component files use kebab-case naming (e.g., user-profile.tsx)",
				Confidence:  float64(kebabCount) / float64(totalComponents),
			})
		}
	}

	return patterns
}

func detectBarrelPattern(dirMap map[string][]string) []models.Pattern {
	var patterns []models.Pattern
	barrelCount := 0
	totalDirs := 0

	for _, files := range dirMap {
		hasIndex := false
		hasOther := false
		for _, f := range files {
			name := strings.TrimSuffix(f, filepath.Ext(f))
			if name == "index" {
				hasIndex = true
			} else {
				hasOther = true
			}
		}
		if hasOther {
			totalDirs++
			if hasIndex {
				barrelCount++
			}
		}
	}

	if totalDirs > 2 && float64(barrelCount)/float64(totalDirs) > 0.5 {
		patterns = append(patterns, models.Pattern{
			Kind:        "import_style",
			Description: "Uses barrel exports (index.ts/tsx) for clean imports",
			Confidence:  float64(barrelCount) / float64(totalDirs),
		})
	}

	return patterns
}

func detectTestPatterns(files []fileInfo, dirMap map[string][]string) []models.Pattern {
	var patterns []models.Pattern

	testInSrc := 0
	testInTestDir := 0
	totalTests := 0

	for _, f := range files {
		name := filepath.Base(f.path)
		isTest := strings.Contains(name, ".test.") || strings.Contains(name, ".spec.") || strings.HasPrefix(name, "test_")
		if !isTest {
			continue
		}
		totalTests++

		dir := filepath.ToSlash(filepath.Dir(f.path))
		if strings.Contains(dir, "__tests__") || strings.Contains(dir, "tests/") || strings.Contains(dir, "test/") {
			testInTestDir++
		} else {
			testInSrc++
		}
	}

	if totalTests > 0 {
		if testInSrc > testInTestDir {
			patterns = append(patterns, models.Pattern{
				Kind:        "testing",
				Description: "Tests co-located with source files",
				Confidence:  float64(testInSrc) / float64(totalTests),
			})
		} else if testInTestDir > 0 {
			patterns = append(patterns, models.Pattern{
				Kind:        "testing",
				Description: "Tests separated into dedicated test directories",
				Confidence:  float64(testInTestDir) / float64(totalTests),
			})
		}
	}

	return patterns
}

func detectImportPatterns(ctx context.Context, s *store.Store) []models.Pattern {
	var patterns []models.Pattern

	// Check for path alias usage (@/)
	var aliasCount int
	s.DB().QueryRowContext(ctx,
		"SELECT COUNT(*) FROM imports WHERE source_path LIKE '@/%'",
	).Scan(&aliasCount)

	var relativeCount int
	s.DB().QueryRowContext(ctx,
		"SELECT COUNT(*) FROM imports WHERE source_path LIKE './%' OR source_path LIKE '../%'",
	).Scan(&relativeCount)

	if aliasCount > 0 && aliasCount > relativeCount/2 {
		patterns = append(patterns, models.Pattern{
			Kind:        "import_style",
			Description: "Uses path aliases (@/) for imports instead of relative paths",
			Confidence:  0.9,
		})
	}

	// Check for 'use client' directive usage
	var clientCount int
	s.DB().QueryRowContext(ctx,
		"SELECT COUNT(DISTINCT f.id) FROM files f JOIN components c ON c.file_id = f.id WHERE f.language IN ('tsx','jsx')",
	).Scan(&clientCount)

	if clientCount > 0 {
		patterns = append(patterns, models.Pattern{
			Kind:        "component_structure",
			Description: fmt.Sprintf("React Server Components architecture (%d client components)", clientCount),
			Confidence:  0.85,
		})
	}

	return patterns
}

func isUpperFirst(s string) bool {
	if len(s) == 0 {
		return false
	}
	return s[0] >= 'A' && s[0] <= 'Z'
}

func isLowerFirst(s string) bool {
	if len(s) == 0 {
		return false
	}
	return s[0] >= 'a' && s[0] <= 'z'
}
