package store

const schemaSQL = `
CREATE TABLE IF NOT EXISTS meta (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS files (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    path            TEXT NOT NULL UNIQUE,
    hash            TEXT NOT NULL,
    language        TEXT,
    size_bytes      INTEGER,
    last_indexed_at TEXT NOT NULL DEFAULT (datetime('now'))
);
CREATE INDEX IF NOT EXISTS idx_files_path ON files(path);
CREATE INDEX IF NOT EXISTS idx_files_language ON files(language);

CREATE TABLE IF NOT EXISTS components (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    file_id     INTEGER NOT NULL REFERENCES files(id) ON DELETE CASCADE,
    name        TEXT NOT NULL,
    kind        TEXT NOT NULL,
    export_type TEXT,
    line_start  INTEGER,
    line_end    INTEGER,
    raw_source  TEXT,
    created_at  TEXT NOT NULL DEFAULT (datetime('now'))
);
CREATE INDEX IF NOT EXISTS idx_components_name ON components(name);
CREATE INDEX IF NOT EXISTS idx_components_kind ON components(kind);
CREATE INDEX IF NOT EXISTS idx_components_file ON components(file_id);

CREATE TABLE IF NOT EXISTS type_definitions (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    file_id    INTEGER NOT NULL REFERENCES files(id) ON DELETE CASCADE,
    name       TEXT NOT NULL,
    kind       TEXT NOT NULL,
    body       TEXT,
    line_start INTEGER,
    line_end   INTEGER
);
CREATE INDEX IF NOT EXISTS idx_typedefs_name ON type_definitions(name);

CREATE TABLE IF NOT EXISTS props (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    type_def_id     INTEGER NOT NULL REFERENCES type_definitions(id) ON DELETE CASCADE,
    component_id    INTEGER REFERENCES components(id) ON DELETE SET NULL,
    name            TEXT NOT NULL,
    type_annotation TEXT,
    is_optional     BOOLEAN DEFAULT 0,
    default_value   TEXT
);
CREATE INDEX IF NOT EXISTS idx_props_component ON props(component_id);

CREATE TABLE IF NOT EXISTS imports (
    id             INTEGER PRIMARY KEY AUTOINCREMENT,
    file_id        INTEGER NOT NULL REFERENCES files(id) ON DELETE CASCADE,
    source_path    TEXT NOT NULL,
    resolved_path  TEXT,
    is_external    BOOLEAN DEFAULT 0,
    imported_names TEXT
);
CREATE INDEX IF NOT EXISTS idx_imports_file ON imports(file_id);
CREATE INDEX IF NOT EXISTS idx_imports_resolved ON imports(resolved_path);

CREATE TABLE IF NOT EXISTS exports (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    file_id    INTEGER NOT NULL REFERENCES files(id) ON DELETE CASCADE,
    name       TEXT NOT NULL,
    kind       TEXT,
    line_start INTEGER
);
CREATE INDEX IF NOT EXISTS idx_exports_file ON exports(file_id);
CREATE INDEX IF NOT EXISTS idx_exports_name ON exports(name);

CREATE TABLE IF NOT EXISTS tags (
    id   INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL UNIQUE
);

CREATE TABLE IF NOT EXISTS component_tags (
    component_id INTEGER NOT NULL REFERENCES components(id) ON DELETE CASCADE,
    tag_id       INTEGER NOT NULL REFERENCES tags(id) ON DELETE CASCADE,
    confidence   REAL DEFAULT 1.0,
    source       TEXT DEFAULT 'auto',
    PRIMARY KEY (component_id, tag_id)
);

CREATE TABLE IF NOT EXISTS stack (
    id       INTEGER PRIMARY KEY AUTOINCREMENT,
    category TEXT NOT NULL,
    name     TEXT NOT NULL,
    version  TEXT,
    evidence TEXT,
    UNIQUE(category, name)
);

CREATE TABLE IF NOT EXISTS patterns (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    kind        TEXT NOT NULL,
    description TEXT NOT NULL,
    evidence    TEXT,
    confidence  REAL DEFAULT 1.0
);
`

const schemaVersion = "1"
