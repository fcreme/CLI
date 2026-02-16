package suggest

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// BackupAndApply creates a backup of the original file and writes the modified content.
func BackupAndApply(repoRoot, filePath, modifiedContent string) (backupPath string, err error) {
	absPath := filepath.Join(repoRoot, filepath.FromSlash(filePath))

	// Read original for backup
	original, err := os.ReadFile(absPath)
	if err != nil {
		return "", fmt.Errorf("reading original file: %w", err)
	}

	// Create backup
	timestamp := time.Now().Format("20060102-150405")
	backupDir := filepath.Join(repoRoot, ".repokit", "backups", timestamp)
	backupFile := filepath.Join(backupDir, filepath.FromSlash(filePath))

	if err := os.MkdirAll(filepath.Dir(backupFile), 0755); err != nil {
		return "", fmt.Errorf("creating backup directory: %w", err)
	}

	if err := os.WriteFile(backupFile, original, 0644); err != nil {
		return "", fmt.Errorf("writing backup: %w", err)
	}

	// Write modified content
	if err := os.WriteFile(absPath, []byte(modifiedContent), 0644); err != nil {
		// Try to restore from backup
		os.WriteFile(absPath, original, 0644)
		return "", fmt.Errorf("writing modified file: %w", err)
	}

	relBackup, _ := filepath.Rel(repoRoot, backupFile)
	return filepath.ToSlash(relBackup), nil
}
