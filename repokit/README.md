# Repokit

A repo-aware developer assistant CLI that analyzes codebases, detects patterns, tracks dependencies, and generates code following your project's conventions.

Built for **React/TypeScript** projects (Next.js, Tailwind, etc.), with generic support for any JavaScript/TypeScript codebase.

## Quick Start

```bash
# Connect to a repository
repokit connect ./my-project

# Index the project
repokit index

# Explore
repokit status          # Dashboard overview
repokit find component  # List all components
repokit stack           # Detect tech stack
repokit health          # Project health score
```

## Installation

### Build from Source

Requires **Go 1.24.0+**. No C compiler needed.

```bash
git clone https://github.com/fcreme/CLI.git
cd CLI/repokit
go build -o repokit.exe
```

### Verify

```bash
repokit version
# repokit v0.2.0
```

## Commands

### Getting Started

| Command | Alias | Description |
|---------|-------|-------------|
| `connect <path\|url>` | | Connect to a local repo or clone from a git URL |
| `index [--force]` | | Parse files and build the component index (incremental by default) |
| `stack` | | Detect technology stack (frameworks, styling, state management) |
| `status` | `st` | Dashboard with connection status, index summary, component counts |

### Explore

| Command | Alias | Description |
|---------|-------|-------------|
| `find component [query]` | `f` | Search components by name, kind, or tags |
| `show <id>` | `s` | Display full component details (props, imports, source preview) |
| `deps [id]` | `d` | Dependency graph analysis |
| `lint` | `l` | Code quality checks |
| `patterns` | | Show detected project conventions |

### Analyze

| Command | Alias | Description |
|---------|-------|-------------|
| `health` | `h` | Project health score (0-100, grade A-F) |
| `stats` | | Codebase analytics (LOC, biggest components, complexity) |
| `duplicates` | `dup` | Find similar or duplicate components |
| `check` | | CI/CD quality gate |

### Generate

| Command | Description |
|---------|-------------|
| `scaffold component <Name> --based <id>` | Generate a component from a template (dry-run by default) |
| `suggest "<description>" --file <path>` | Propose code modifications as unified diff (dry-run by default) |

Add `--write` or `--apply` to actually create/modify files.

### Manage

| Command | Description |
|---------|-------------|
| `tag add\|remove\|list` | Manage component tags (e.g., "needs-refactor", "deprecated") |
| `open <id>` | Open a component in your editor |
| `export [--html\|--md]` | Generate project reports |
| `projects` / `switch <n>` | Manage and switch between multiple projects |
| `chat` | Interactive natural language REPL |

All commands support `--json` for machine-readable output.

## Features

### Smart Indexing

- Incremental indexing (skips unchanged files)
- Detects React components, hooks, HOCs, context providers
- Tracks imports, exports, and type definitions
- Automatic tagging based on folder structure

### Dependency Analysis

```bash
repokit deps --circular   # Find circular imports
repokit deps --orphans    # Find dead code candidates
repokit deps --tree       # Tree visualization
repokit deps --mermaid    # Mermaid diagram output
repokit deps --reverse    # Reverse dependency lookup
```

### Code Quality

```bash
repokit lint
repokit lint --max-lines 200 --max-props 8 --max-imports 15
```

Checks for: oversized components, prop bloat, multi-component files, missing types, unused exports, excessive imports.

### Code Generation

```bash
# Preview what would be created
repokit scaffold component NewButton --based 5

# Create the file
repokit scaffold component NewButton --based 5 --write

# Suggest a modification (preview)
repokit suggest "add onClick handler" --file src/Button.tsx

# Apply the change (creates automatic backup)
repokit suggest "add onClick handler" --file src/Button.tsx --apply
```

### Interactive Mode

```bash
repokit chat
> find all modal components
> show me the navbar
> what's the health score
> create form like LoginForm
> exit
```

### Reporting

```bash
repokit health          # Score + grade
repokit stats           # Detailed analytics
repokit export --html   # Full HTML report (auto-opens in browser)
repokit export --md     # Markdown report
```

## Architecture

```
repokit/
├── main.go           # Entry point
├── cmd/              # CLI commands (Cobra)
├── internal/         # Business logic
│   ├── config/       # Configuration management
│   ├── store/        # SQLite database operations
│   ├── indexer/      # File parsing & component detection
│   ├── parser/       # Regex-based code parsing
│   ├── detector/     # Tech stack & pattern detection
│   ├── scaffold/     # Component generation
│   ├── suggest/      # Code modification suggestions
│   ├── search/       # Component search
│   ├── connect/      # Repository connection
│   └── output/       # Terminal formatting
└── pkg/models/       # Domain types
```

Data is stored in a SQLite database at `.repokit/index.db` in your repository root.

## Dependencies

| Package | Purpose |
|---------|---------|
| [cobra](https://github.com/spf13/cobra) | CLI framework |
| [color](https://github.com/fatih/color) | Terminal colors |
| [sqlite](https://pkg.go.dev/modernc.org/sqlite) | Pure Go SQLite (no CGo) |

## Design Principles

- **Safe by default** - scaffold and suggest run in dry-run mode; add `--write`/`--apply` to execute
- **Automatic backups** - file modifications create backups before changes
- **Incremental** - indexing only processes changed files
- **No CGo** - pure Go build, no C compiler required
- **Offline** - everything runs locally, no external services

## License

MIT
