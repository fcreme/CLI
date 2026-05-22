package detector

import (
	"strings"

	"github.com/fcreme/CLI/repokit/pkg/models"
)

// TagResult holds a tag assignment with confidence.
type TagResult struct {
	Tag        string
	Confidence float64
}

// AutoTag determines tags for a component based on heuristics.
func AutoTag(comp *models.Component) []TagResult {
	var tags []TagResult
	name := strings.ToLower(comp.Name)
	source := strings.ToLower(comp.RawSource)
	path := strings.ToLower(comp.FilePath)

	// Modal / Dialog
	if containsAny(name, "modal", "dialog", "popup", "overlay") ||
		strings.Contains(source, "createportal") ||
		containsAny(source, "isopen", "onclose", "isvisible") {
		tags = append(tags, TagResult{"modal", 0.9})
	}

	// Form
	if containsAny(name, "form", "input", "field", "checkbox", "select", "radio", "textarea") ||
		strings.Contains(source, "<form") ||
		containsAny(source, "onsubmit", "handlesubmit", "useform", "register(") {
		tags = append(tags, TagResult{"form", 0.9})
	}

	// Table
	if containsAny(name, "table", "datagrid", "datatable", "grid") ||
		strings.Contains(source, "<table") ||
		containsAny(source, "<thead", "<tbody", "columns=", "usecolumn") {
		tags = append(tags, TagResult{"table", 0.9})
	}

	// Card
	if containsAny(name, "card", "tile", "panel") {
		tags = append(tags, TagResult{"card", 0.85})
	}

	// Layout
	if containsAny(name, "layout", "sidebar", "header", "footer", "navbar", "nav", "topbar", "appbar") ||
		containsAny(path, "layout", "layouts") {
		tags = append(tags, TagResult{"layout", 0.85})
	}

	// Page / Route
	if containsAny(name, "page", "screen", "view") ||
		containsAny(path, "pages/", "app/", "screens/", "views/") {
		tags = append(tags, TagResult{"page", 0.8})
	}

	// Hook
	if comp.Kind == models.KindHook || strings.HasPrefix(comp.Name, "use") {
		tags = append(tags, TagResult{"hook", 1.0})
	}

	// Utility / Helper
	if containsAny(path, "utils/", "util/", "helpers/", "helper/", "lib/") {
		tags = append(tags, TagResult{"util", 0.8})
	}

	// Button
	if containsAny(name, "button", "btn") {
		tags = append(tags, TagResult{"button", 0.9})
	}

	// List
	if containsAny(name, "list", "feed", "timeline") {
		tags = append(tags, TagResult{"list", 0.85})
	}

	// Provider / Context
	if containsAny(name, "provider", "context") ||
		strings.Contains(source, "createcontext") {
		tags = append(tags, TagResult{"provider", 0.9})
	}

	// Chart / Visualization
	if containsAny(name, "chart", "graph", "visualization", "plot") {
		tags = append(tags, TagResult{"chart", 0.9})
	}

	return tags
}

func containsAny(s string, substrs ...string) bool {
	for _, sub := range substrs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}
