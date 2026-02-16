package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

const (
	RepokitDir  = ".repokit"
	ConfigFile  = "config.json"
	IndexDBFile = "index.db"
)

// Config holds the repokit configuration for a connected repository.
type Config struct {
	RepoPath  string `json:"repo_path"`
	RemoteURL string `json:"remote_url,omitempty"`
}

// RepokitPath returns the .repokit directory path for the given repo root.
func RepokitPath(repoRoot string) string {
	return filepath.Join(repoRoot, RepokitDir)
}

// ConfigPath returns the config.json path for the given repo root.
func ConfigPath(repoRoot string) string {
	return filepath.Join(repoRoot, RepokitDir, ConfigFile)
}

// DBPath returns the index.db path for the given repo root.
func DBPath(repoRoot string) string {
	return filepath.Join(repoRoot, RepokitDir, IndexDBFile)
}

// Load reads the config from the .repokit directory.
func Load(repoRoot string) (*Config, error) {
	data, err := os.ReadFile(ConfigPath(repoRoot))
	if err != nil {
		return nil, fmt.Errorf("reading config: %w", err)
	}
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}
	return &cfg, nil
}

// Save writes the config to the .repokit directory.
func Save(repoRoot string, cfg *Config) error {
	dir := RepokitPath(repoRoot)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("creating .repokit directory: %w", err)
	}

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling config: %w", err)
	}

	if err := os.WriteFile(ConfigPath(repoRoot), data, 0644); err != nil {
		return fmt.Errorf("writing config: %w", err)
	}
	return nil
}

// Exists checks if a repokit config exists at the given path.
func Exists(repoRoot string) bool {
	_, err := os.Stat(ConfigPath(repoRoot))
	return err == nil
}

// GlobalConfigDir returns the global repokit config directory (~/.repokit/).
func GlobalConfigDir() string {
	if runtime.GOOS == "windows" {
		return filepath.Join(os.Getenv("USERPROFILE"), ".repokit")
	}
	return filepath.Join(os.Getenv("HOME"), ".repokit")
}

// GlobalConfigPath returns the path to the global active project file.
func GlobalConfigPath() string {
	return filepath.Join(GlobalConfigDir(), "active_project.json")
}

// ActiveProject holds the globally remembered active project path.
type ActiveProject struct {
	RepoRoot string `json:"repo_root"`
}

// ProjectEntry represents a connected project in the global registry.
type ProjectEntry struct {
	Path string `json:"path"`
	Name string `json:"name"`
}

// ProjectsRegistry holds all connected projects.
type ProjectsRegistry struct {
	Projects []ProjectEntry `json:"projects"`
}

// ProjectsRegistryPath returns the path to the projects registry file.
func ProjectsRegistryPath() string {
	return filepath.Join(GlobalConfigDir(), "projects.json")
}

// SetActiveProject saves the active project globally and adds it to the registry.
func SetActiveProject(repoRoot string) error {
	dir := GlobalConfigDir()
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	data, _ := json.MarshalIndent(ActiveProject{RepoRoot: repoRoot}, "", "  ")
	if err := os.WriteFile(GlobalConfigPath(), data, 0644); err != nil {
		return err
	}
	// Also register in projects list
	return AddProject(repoRoot)
}

// AddProject adds a project to the global registry if not already there.
func AddProject(repoRoot string) error {
	reg := LoadProjects()
	for _, p := range reg.Projects {
		if p.Path == repoRoot {
			return nil // already registered
		}
	}
	name := filepath.Base(repoRoot)
	reg.Projects = append(reg.Projects, ProjectEntry{Path: repoRoot, Name: name})
	return SaveProjects(reg)
}

// RemoveProject removes a project from the global registry.
func RemoveProject(repoRoot string) error {
	reg := LoadProjects()
	var filtered []ProjectEntry
	for _, p := range reg.Projects {
		if p.Path != repoRoot {
			filtered = append(filtered, p)
		}
	}
	reg.Projects = filtered
	return SaveProjects(reg)
}

// LoadProjects loads the projects registry.
func LoadProjects() *ProjectsRegistry {
	data, err := os.ReadFile(ProjectsRegistryPath())
	if err != nil {
		return &ProjectsRegistry{}
	}
	var reg ProjectsRegistry
	if json.Unmarshal(data, &reg) != nil {
		return &ProjectsRegistry{}
	}
	return &reg
}

// SaveProjects saves the projects registry.
func SaveProjects(reg *ProjectsRegistry) error {
	dir := GlobalConfigDir()
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	data, _ := json.MarshalIndent(reg, "", "  ")
	return os.WriteFile(ProjectsRegistryPath(), data, 0644)
}

// GetActiveProject returns the globally saved active project, or empty string.
func GetActiveProject() string {
	data, err := os.ReadFile(GlobalConfigPath())
	if err != nil {
		return ""
	}
	var ap ActiveProject
	if json.Unmarshal(data, &ap) != nil {
		return ""
	}
	return ap.RepoRoot
}

// FindRepoRoot walks up from the given directory to find a .repokit config.
// Falls back to the global active project if no local .repokit is found.
func FindRepoRoot(startDir string) (string, error) {
	dir, err := filepath.Abs(startDir)
	if err != nil {
		return "", err
	}

	// Walk up looking for .repokit/
	for {
		if Exists(dir) {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}

	// Fall back to global active project
	if active := GetActiveProject(); active != "" {
		if Exists(active) {
			return active, nil
		}
	}

	return "", fmt.Errorf("no repokit project found (use 'repokit connect' first)")
}
