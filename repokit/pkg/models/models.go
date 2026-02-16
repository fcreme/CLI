package models

import "time"

// EntityKind represents the type of a code entity.
type EntityKind string

const (
	KindFunctionComponent EntityKind = "function_component"
	KindClassComponent    EntityKind = "class_component"
	KindHook              EntityKind = "hook"
	KindHOC               EntityKind = "hoc"
	KindContextProvider   EntityKind = "context_provider"
)

// Component represents a detected code component (React component, hook, etc.).
type Component struct {
	ID         int64
	FileID     int64
	FilePath   string // denormalized for convenience
	Name       string
	Kind       EntityKind
	ExportType string // "default", "named", "none"
	LineStart  int
	LineEnd    int
	RawSource  string
	Props      []Prop
	Tags       []Tag
	UsageCount int
	CreatedAt  time.Time
}

// Prop represents a single property in a component's props interface.
type Prop struct {
	ID             int64
	TypeDefID      int64
	ComponentID    int64
	Name           string
	TypeAnnotation string
	IsOptional     bool
	DefaultValue   string
}

// Tag represents a classification label for a component.
type Tag struct {
	ID         int64
	Name       string
	Confidence float64
	Source     string // "auto" or "manual"
}

// Import represents an import statement in a source file.
type Import struct {
	ID            int64
	FileID        int64
	SourcePath    string // the import specifier: "./Button" or "react"
	ResolvedPath  string // resolved to a repo-relative file path
	IsExternal    bool
	ImportedNames []string
}

// Export represents an export from a source file.
type Export struct {
	ID        int64
	FileID    int64
	Name      string // "default" or the named export
	Kind      string // "function", "class", "const", "type", "interface"
	LineStart int
}

// TypeDefinition represents an interface or type alias declaration.
type TypeDefinition struct {
	ID        int64
	FileID    int64
	Name      string
	Kind      string // "interface", "type_alias", "enum"
	Body      string
	LineStart int
	LineEnd   int
}

// StackItem represents a detected technology in the project.
type StackItem struct {
	ID       int64
	Category string // "framework", "language", "styling", "state", "testing", "bundler"
	Name     string
	Version  string
	Evidence string // JSON describing how it was detected
}

// Pattern represents a detected convention in the project.
type Pattern struct {
	ID          int64
	Kind        string // "folder_structure", "naming_convention", "import_style", "component_structure"
	Description string
	Evidence    []string
	Confidence  float64
}

// FileRecord represents an indexed file.
type FileRecord struct {
	ID            int64
	Path          string // relative to repo root
	Hash          string // SHA-256
	Language      string // "tsx", "ts", "css", "json", etc.
	SizeBytes     int64
	LastIndexedAt time.Time
}
