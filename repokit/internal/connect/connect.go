package connect

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/usuario/repokit/internal/config"
)

// Connect validates the given path or clones a git URL, then initializes .repokit.
func Connect(target string) (string, error) {
	// Determine if target is a git URL or local path
	if isGitURL(target) {
		return connectGitURL(target)
	}
	return connectLocalPath(target)
}

func connectLocalPath(path string) (string, error) {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolving path: %w", err)
	}

	info, err := os.Stat(absPath)
	if err != nil {
		return "", fmt.Errorf("path does not exist: %s", absPath)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("path is not a directory: %s", absPath)
	}

	// Detect git remote URL if it's a git repo
	remoteURL := detectRemoteURL(absPath)

	cfg := &config.Config{
		RepoPath:  absPath,
		RemoteURL: remoteURL,
	}

	if err := config.Save(absPath, cfg); err != nil {
		return "", err
	}

	return absPath, nil
}

func connectGitURL(url string) (string, error) {
	// Extract repo name from URL for the local directory name
	repoName := extractRepoName(url)
	if repoName == "" {
		return "", fmt.Errorf("could not determine repository name from URL: %s", url)
	}

	cwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("getting working directory: %w", err)
	}
	targetDir := filepath.Join(cwd, repoName)

	// Clone the repository
	cmd := exec.Command("git", "clone", url, targetDir)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("cloning repository: %w", err)
	}

	cfg := &config.Config{
		RepoPath:  targetDir,
		RemoteURL: url,
	}

	if err := config.Save(targetDir, cfg); err != nil {
		return "", err
	}

	return targetDir, nil
}

func isGitURL(s string) bool {
	return strings.HasPrefix(s, "http://") ||
		strings.HasPrefix(s, "https://") ||
		strings.HasPrefix(s, "git@") ||
		strings.HasPrefix(s, "git://") ||
		strings.HasSuffix(s, ".git")
}

func extractRepoName(url string) string {
	// Handle git@github.com:user/repo.git
	if strings.Contains(url, ":") && strings.HasPrefix(url, "git@") {
		parts := strings.Split(url, "/")
		name := parts[len(parts)-1]
		return strings.TrimSuffix(name, ".git")
	}

	// Handle https://github.com/user/repo.git
	parts := strings.Split(strings.TrimRight(url, "/"), "/")
	if len(parts) == 0 {
		return ""
	}
	name := parts[len(parts)-1]
	return strings.TrimSuffix(name, ".git")
}

func detectRemoteURL(repoPath string) string {
	cmd := exec.Command("git", "-C", repoPath, "remote", "get-url", "origin")
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
