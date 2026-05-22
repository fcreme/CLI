package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/fcreme/CLI/repokit/pkg/models"
)

// UpsertFile inserts or updates a file record. Returns the file ID.
func (s *Store) UpsertFile(ctx context.Context, f *models.FileRecord) (int64, error) {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO files (path, hash, language, size_bytes, last_indexed_at)
		VALUES (?, ?, ?, ?, datetime('now'))
		ON CONFLICT(path) DO UPDATE SET
			hash = excluded.hash,
			language = excluded.language,
			size_bytes = excluded.size_bytes,
			last_indexed_at = datetime('now')
	`, f.Path, f.Hash, f.Language, f.SizeBytes)
	if err != nil {
		return 0, fmt.Errorf("upserting file %s: %w", f.Path, err)
	}

	// Always query by path - LastInsertId is unreliable with ON CONFLICT DO UPDATE
	return s.getFileID(ctx, f.Path)
}

func (s *Store) getFileID(ctx context.Context, path string) (int64, error) {
	var id int64
	err := s.db.QueryRowContext(ctx, "SELECT id FROM files WHERE path = ?", path).Scan(&id)
	return id, err
}

// FileNeedsReindex returns true if the file doesn't exist in the index or its hash has changed.
func (s *Store) FileNeedsReindex(ctx context.Context, path, hash string) (bool, error) {
	var existingHash string
	err := s.db.QueryRowContext(ctx,
		"SELECT hash FROM files WHERE path = ?", path,
	).Scan(&existingHash)
	if err == sql.ErrNoRows {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	return existingHash != hash, nil
}

// DeleteFileAndDependents removes a file and all its dependent records (cascade).
func (s *Store) DeleteFileAndDependents(ctx context.Context, fileID int64) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM files WHERE id = ?", fileID)
	return err
}

// InsertComponent inserts a component record and returns its ID.
func (s *Store) InsertComponent(ctx context.Context, c *models.Component) (int64, error) {
	result, err := s.db.ExecContext(ctx, `
		INSERT INTO components (file_id, name, kind, export_type, line_start, line_end, raw_source)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`, c.FileID, c.Name, string(c.Kind), c.ExportType, c.LineStart, c.LineEnd, c.RawSource)
	if err != nil {
		return 0, fmt.Errorf("inserting component %s: %w", c.Name, err)
	}
	return result.LastInsertId()
}

// InsertTypeDef inserts a type definition and returns its ID.
func (s *Store) InsertTypeDef(ctx context.Context, td *models.TypeDefinition) (int64, error) {
	result, err := s.db.ExecContext(ctx, `
		INSERT INTO type_definitions (file_id, name, kind, body, line_start, line_end)
		VALUES (?, ?, ?, ?, ?, ?)
	`, td.FileID, td.Name, td.Kind, td.Body, td.LineStart, td.LineEnd)
	if err != nil {
		return 0, fmt.Errorf("inserting type definition %s: %w", td.Name, err)
	}
	return result.LastInsertId()
}

// InsertProp inserts a prop record.
func (s *Store) InsertProp(ctx context.Context, p *models.Prop) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO props (type_def_id, component_id, name, type_annotation, is_optional, default_value)
		VALUES (?, ?, ?, ?, ?, ?)
	`, p.TypeDefID, p.ComponentID, p.Name, p.TypeAnnotation, p.IsOptional, p.DefaultValue)
	return err
}

// InsertImport inserts an import record.
func (s *Store) InsertImport(ctx context.Context, imp *models.Import) error {
	namesJSON, _ := json.Marshal(imp.ImportedNames)
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO imports (file_id, source_path, resolved_path, is_external, imported_names)
		VALUES (?, ?, ?, ?, ?)
	`, imp.FileID, imp.SourcePath, imp.ResolvedPath, imp.IsExternal, string(namesJSON))
	return err
}

// InsertExport inserts an export record.
func (s *Store) InsertExport(ctx context.Context, exp *models.Export) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO exports (file_id, name, kind, line_start)
		VALUES (?, ?, ?, ?)
	`, exp.FileID, exp.Name, exp.Kind, exp.LineStart)
	return err
}

// UpsertStackItem inserts or updates a stack detection result.
func (s *Store) UpsertStackItem(ctx context.Context, item *models.StackItem) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO stack (category, name, version, evidence)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(category, name) DO UPDATE SET
			version = excluded.version,
			evidence = excluded.evidence
	`, item.Category, item.Name, item.Version, item.Evidence)
	return err
}

// GetStack returns all detected stack items.
func (s *Store) GetStack(ctx context.Context) ([]models.StackItem, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT id, category, name, version, evidence FROM stack ORDER BY category, name")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []models.StackItem
	for rows.Next() {
		var item models.StackItem
		var evidence sql.NullString
		if err := rows.Scan(&item.ID, &item.Category, &item.Name, &item.Version, &evidence); err != nil {
			return nil, err
		}
		item.Evidence = evidence.String
		items = append(items, item)
	}
	return items, rows.Err()
}

// ClearStack removes all stack detection results.
func (s *Store) ClearStack(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM stack")
	return err
}

// TagComponent adds a tag to a component.
func (s *Store) TagComponent(ctx context.Context, componentID int64, tagName string, confidence float64, source string) error {
	// Ensure tag exists
	_, err := s.db.ExecContext(ctx, "INSERT OR IGNORE INTO tags (name) VALUES (?)", tagName)
	if err != nil {
		return err
	}

	var tagID int64
	if err := s.db.QueryRowContext(ctx, "SELECT id FROM tags WHERE name = ?", tagName).Scan(&tagID); err != nil {
		return err
	}

	_, err = s.db.ExecContext(ctx, `
		INSERT OR REPLACE INTO component_tags (component_id, tag_id, confidence, source)
		VALUES (?, ?, ?, ?)
	`, componentID, tagID, confidence, source)
	return err
}

// SearchComponents finds components matching the given criteria.
func (s *Store) SearchComponents(ctx context.Context, query string, kind string, tags []string, limit int) ([]models.Component, error) {
	var conditions []string
	var args []interface{}

	baseQuery := `
		SELECT c.id, c.file_id, f.path, c.name, c.kind, c.export_type,
		       c.line_start, c.line_end, c.raw_source, c.created_at,
		       (SELECT COUNT(*) FROM imports i WHERE i.resolved_path = f.path) as usage_count
		FROM components c
		JOIN files f ON c.file_id = f.id
	`

	if query != "" {
		conditions = append(conditions, "LOWER(c.name) LIKE LOWER(?)")
		args = append(args, "%"+query+"%")
	}
	if kind != "" {
		conditions = append(conditions, "c.kind = ?")
		args = append(args, kind)
	}
	if len(tags) > 0 {
		for _, tag := range tags {
			conditions = append(conditions, `c.id IN (
				SELECT ct.component_id FROM component_tags ct
				JOIN tags t ON ct.tag_id = t.id WHERE t.name = ?
			)`)
			args = append(args, tag)
		}
	}

	if len(conditions) > 0 {
		baseQuery += " WHERE " + strings.Join(conditions, " AND ")
	}

	baseQuery += " ORDER BY c.name"
	if limit > 0 {
		baseQuery += fmt.Sprintf(" LIMIT %d", limit)
	}

	rows, err := s.db.QueryContext(ctx, baseQuery, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var components []models.Component
	for rows.Next() {
		var c models.Component
		var exportType, rawSource sql.NullString
		var createdAt string
		if err := rows.Scan(
			&c.ID, &c.FileID, &c.FilePath, &c.Name, &c.Kind, &exportType,
			&c.LineStart, &c.LineEnd, &rawSource, &createdAt, &c.UsageCount,
		); err != nil {
			return nil, err
		}
		c.ExportType = exportType.String
		c.RawSource = rawSource.String
		c.CreatedAt, _ = time.Parse("2006-01-02 15:04:05", createdAt)
		components = append(components, c)
	}
	return components, rows.Err()
}

// GetComponentByID returns a single component by ID with its tags.
func (s *Store) GetComponentByID(ctx context.Context, id int64) (*models.Component, error) {
	var c models.Component
	var exportType, rawSource sql.NullString
	var createdAt string

	err := s.db.QueryRowContext(ctx, `
		SELECT c.id, c.file_id, f.path, c.name, c.kind, c.export_type,
		       c.line_start, c.line_end, c.raw_source, c.created_at,
		       (SELECT COUNT(*) FROM imports i WHERE i.resolved_path = f.path) as usage_count
		FROM components c
		JOIN files f ON c.file_id = f.id
		WHERE c.id = ?
	`, id).Scan(
		&c.ID, &c.FileID, &c.FilePath, &c.Name, &c.Kind, &exportType,
		&c.LineStart, &c.LineEnd, &rawSource, &createdAt, &c.UsageCount,
	)
	if err != nil {
		return nil, err
	}
	c.ExportType = exportType.String
	c.RawSource = rawSource.String
	c.CreatedAt, _ = time.Parse("2006-01-02 15:04:05", createdAt)

	// Load tags
	tagRows, err := s.db.QueryContext(ctx, `
		SELECT t.id, t.name, ct.confidence, ct.source
		FROM component_tags ct
		JOIN tags t ON ct.tag_id = t.id
		WHERE ct.component_id = ?
	`, id)
	if err != nil {
		return &c, nil // return component without tags
	}
	defer tagRows.Close()

	for tagRows.Next() {
		var tag models.Tag
		if err := tagRows.Scan(&tag.ID, &tag.Name, &tag.Confidence, &tag.Source); err != nil {
			continue
		}
		c.Tags = append(c.Tags, tag)
	}

	return &c, nil
}

// SetMeta sets a metadata key-value pair.
func (s *Store) SetMeta(ctx context.Context, key, value string) error {
	_, err := s.db.ExecContext(ctx,
		"INSERT OR REPLACE INTO meta (key, value) VALUES (?, ?)",
		key, value,
	)
	return err
}

// GetMeta gets a metadata value by key.
func (s *Store) GetMeta(ctx context.Context, key string) (string, error) {
	var value string
	err := s.db.QueryRowContext(ctx, "SELECT value FROM meta WHERE key = ?", key).Scan(&value)
	return value, err
}

// ClearFileData removes all components, imports, exports for a file (before re-indexing).
func (s *Store) ClearFileData(ctx context.Context, fileID int64) error {
	for _, table := range []string{"components", "imports", "exports", "type_definitions"} {
		if _, err := s.db.ExecContext(ctx, fmt.Sprintf("DELETE FROM %s WHERE file_id = ?", table), fileID); err != nil {
			return err
		}
	}
	return nil
}

// InsertPattern inserts a detected pattern.
func (s *Store) InsertPattern(ctx context.Context, p *models.Pattern) error {
	evidenceJSON, _ := json.Marshal(p.Evidence)
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO patterns (kind, description, evidence, confidence)
		VALUES (?, ?, ?, ?)
	`, p.Kind, p.Description, string(evidenceJSON), p.Confidence)
	return err
}

// --- Transaction-aware batch methods for indexing ---

// TxUpsertFile inserts or updates a file within a transaction.
func (ts *TxStore) UpsertFile(ctx context.Context, f *models.FileRecord) (int64, error) {
	_, err := ts.tx.ExecContext(ctx, `
		INSERT INTO files (path, hash, language, size_bytes, last_indexed_at)
		VALUES (?, ?, ?, ?, datetime('now'))
		ON CONFLICT(path) DO UPDATE SET
			hash = excluded.hash,
			language = excluded.language,
			size_bytes = excluded.size_bytes,
			last_indexed_at = datetime('now')
	`, f.Path, f.Hash, f.Language, f.SizeBytes)
	if err != nil {
		return 0, fmt.Errorf("upserting file %s: %w", f.Path, err)
	}
	var id int64
	err = ts.tx.QueryRowContext(ctx, "SELECT id FROM files WHERE path = ?", f.Path).Scan(&id)
	return id, err
}

// TxClearFileData removes all indexed data for a file within a transaction.
func (ts *TxStore) ClearFileData(ctx context.Context, fileID int64) error {
	for _, table := range []string{"components", "imports", "exports", "type_definitions"} {
		if _, err := ts.tx.ExecContext(ctx, fmt.Sprintf("DELETE FROM %s WHERE file_id = ?", table), fileID); err != nil {
			return err
		}
	}
	return nil
}

// TxInsertComponent inserts a component within a transaction.
func (ts *TxStore) InsertComponent(ctx context.Context, c *models.Component) (int64, error) {
	result, err := ts.tx.ExecContext(ctx, `
		INSERT INTO components (file_id, name, kind, export_type, line_start, line_end, raw_source)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`, c.FileID, c.Name, string(c.Kind), c.ExportType, c.LineStart, c.LineEnd, c.RawSource)
	if err != nil {
		return 0, err
	}
	return result.LastInsertId()
}

// TxInsertImport inserts an import within a transaction.
func (ts *TxStore) InsertImport(ctx context.Context, imp *models.Import) error {
	namesJSON, _ := json.Marshal(imp.ImportedNames)
	_, err := ts.tx.ExecContext(ctx, `
		INSERT INTO imports (file_id, source_path, resolved_path, is_external, imported_names)
		VALUES (?, ?, ?, ?, ?)
	`, imp.FileID, imp.SourcePath, imp.ResolvedPath, imp.IsExternal, string(namesJSON))
	return err
}

// TxInsertExport inserts an export within a transaction.
func (ts *TxStore) InsertExport(ctx context.Context, exp *models.Export) error {
	_, err := ts.tx.ExecContext(ctx, `
		INSERT INTO exports (file_id, name, kind, line_start)
		VALUES (?, ?, ?, ?)
	`, exp.FileID, exp.Name, exp.Kind, exp.LineStart)
	return err
}

// TxInsertTypeDef inserts a type definition within a transaction.
func (ts *TxStore) InsertTypeDef(ctx context.Context, td *models.TypeDefinition) (int64, error) {
	result, err := ts.tx.ExecContext(ctx, `
		INSERT INTO type_definitions (file_id, name, kind, body, line_start, line_end)
		VALUES (?, ?, ?, ?, ?, ?)
	`, td.FileID, td.Name, td.Kind, td.Body, td.LineStart, td.LineEnd)
	if err != nil {
		return 0, err
	}
	return result.LastInsertId()
}

// TxTagComponent tags a component within a transaction.
func (ts *TxStore) TagComponent(ctx context.Context, componentID int64, tagName string, confidence float64, source string) error {
	_, err := ts.tx.ExecContext(ctx, "INSERT OR IGNORE INTO tags (name) VALUES (?)", tagName)
	if err != nil {
		return err
	}
	var tagID int64
	if err := ts.tx.QueryRowContext(ctx, "SELECT id FROM tags WHERE name = ?", tagName).Scan(&tagID); err != nil {
		return err
	}
	_, err = ts.tx.ExecContext(ctx, `
		INSERT OR REPLACE INTO component_tags (component_id, tag_id, confidence, source)
		VALUES (?, ?, ?, ?)
	`, componentID, tagID, confidence, source)
	return err
}

// FileNeedsReindex checks if a file needs re-indexing (works outside transaction).
func (s *Store) GetAllFileHashes(ctx context.Context) (map[string]string, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT path, hash FROM files")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	hashes := make(map[string]string)
	for rows.Next() {
		var path, hash string
		if err := rows.Scan(&path, &hash); err != nil {
			continue
		}
		hashes[path] = hash
	}
	return hashes, rows.Err()
}

// GetImportGraph returns all resolved internal imports for dependency analysis.
func (s *Store) GetImportGraph(ctx context.Context) ([]ImportEdge, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT f1.path, i.resolved_path, i.imported_names
		FROM imports i
		JOIN files f1 ON i.file_id = f1.id
		WHERE i.is_external = 0 AND i.resolved_path IS NOT NULL AND i.resolved_path != ''
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var edges []ImportEdge
	for rows.Next() {
		var e ImportEdge
		var names string
		if err := rows.Scan(&e.FromFile, &e.ToFile, &names); err != nil {
			continue
		}
		json.Unmarshal([]byte(names), &e.ImportedNames)
		edges = append(edges, e)
	}
	return edges, rows.Err()
}

// ImportEdge represents a dependency between two files.
type ImportEdge struct {
	FromFile      string
	ToFile        string
	ImportedNames []string
}

// GetComponentFiles returns a map of component name -> file path.
func (s *Store) GetComponentFiles(ctx context.Context) (map[string]string, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT c.name, f.path FROM components c JOIN files f ON c.file_id = f.id
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	m := make(map[string]string)
	for rows.Next() {
		var name, path string
		rows.Scan(&name, &path)
		m[name] = path
	}
	return m, rows.Err()
}
