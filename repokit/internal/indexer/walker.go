package indexer

import (
	"bufio"
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// WalkedFile represents a file discovered during directory walking.
type WalkedFile struct {
	Path     string // relative to repo root
	AbsPath  string
	Hash     string // SHA-256
	Language string
	Size     int64
}

// WalkRepo walks the repository respecting .gitignore and returns all supported files.
func WalkRepo(repoRoot string) ([]WalkedFile, error) {
	// Try to use git ls-files first (respects .gitignore)
	files, err := walkWithGit(repoRoot)
	if err != nil {
		// Fallback to manual walk
		return walkManual(repoRoot)
	}
	return files, nil
}

func walkWithGit(repoRoot string) ([]WalkedFile, error) {
	cmd := exec.Command("git", "-C", repoRoot, "ls-files", "--cached", "--others", "--exclude-standard")
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	var files []WalkedFile
	scanner := bufio.NewScanner(strings.NewReader(string(out)))
	for scanner.Scan() {
		relPath := filepath.ToSlash(scanner.Text())
		if !isSupportedFile(relPath) {
			continue
		}

		absPath := filepath.Join(repoRoot, filepath.FromSlash(relPath))
		info, err := os.Stat(absPath)
		if err != nil {
			continue
		}

		hash, err := hashFile(absPath)
		if err != nil {
			continue
		}

		files = append(files, WalkedFile{
			Path:     relPath,
			AbsPath:  absPath,
			Hash:     hash,
			Language: detectLanguage(relPath),
			Size:     info.Size(),
		})
	}

	return files, scanner.Err()
}

func walkManual(repoRoot string) ([]WalkedFile, error) {
	var files []WalkedFile

	err := filepath.Walk(repoRoot, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // skip inaccessible files
		}

		// Skip hidden directories and common non-source directories
		if info.IsDir() {
			name := info.Name()
			if strings.HasPrefix(name, ".") || isSkippedDir(name) {
				return filepath.SkipDir
			}
			return nil
		}

		relPath, err := filepath.Rel(repoRoot, path)
		if err != nil {
			return nil
		}
		relPath = filepath.ToSlash(relPath)

		if !isSupportedFile(relPath) {
			return nil
		}

		hash, err := hashFile(path)
		if err != nil {
			return nil
		}

		files = append(files, WalkedFile{
			Path:     relPath,
			AbsPath:  path,
			Hash:     hash,
			Language: detectLanguage(relPath),
			Size:     info.Size(),
		})
		return nil
	})

	return files, err
}

func hashFile(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	hash := sha256.Sum256(data)
	return fmt.Sprintf("%x", hash), nil
}

func detectLanguage(path string) string {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".tsx":
		return "tsx"
	case ".ts":
		return "ts"
	case ".jsx":
		return "jsx"
	case ".js", ".mjs", ".cjs":
		return "js"
	case ".css":
		return "css"
	case ".scss":
		return "scss"
	case ".less":
		return "less"
	case ".json":
		return "json"
	case ".html":
		return "html"
	case ".md":
		return "markdown"
	case ".go":
		return "go"
	case ".py":
		return "python"
	default:
		return ""
	}
}

var supportedExtensions = map[string]bool{
	".ts": true, ".tsx": true, ".js": true, ".jsx": true,
	".mjs": true, ".cjs": true,
	".css": true, ".scss": true, ".less": true,
	".json": true, ".html": true,
	".go": true, ".py": true,
}

func isSupportedFile(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	return supportedExtensions[ext]
}

var skippedDirs = map[string]bool{
	"node_modules": true, "dist": true, "build": true, ".git": true,
	".repokit": true, "coverage": true, ".next": true, ".nuxt": true,
	"__pycache__": true, "vendor": true, ".cache": true, ".turbo": true,
	".output": true, ".vercel": true,
}

func isSkippedDir(name string) bool {
	return skippedDirs[name]
}
