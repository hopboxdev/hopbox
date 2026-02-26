package devcontainer

import (
	"encoding/json"
	"regexp"
	"strings"

	"github.com/hopboxdev/hopbox/internal/manifest"
)

// StripJSONC removes // comments, /* */ comments, and trailing commas from JSONC.
// Preserves strings containing comment-like sequences.
func StripJSONC(data []byte) ([]byte, error) {
	s := string(data)
	var result strings.Builder
	i := 0
	for i < len(s) {
		// String literal — copy verbatim.
		if s[i] == '"' {
			result.WriteByte(s[i])
			i++
			for i < len(s) {
				result.WriteByte(s[i])
				if s[i] == '\\' && i+1 < len(s) {
					i++
					result.WriteByte(s[i])
				} else if s[i] == '"' {
					i++
					break
				}
				i++
			}
			continue
		}
		// Line comment.
		if i+1 < len(s) && s[i] == '/' && s[i+1] == '/' {
			for i < len(s) && s[i] != '\n' {
				i++
			}
			continue
		}
		// Block comment.
		if i+1 < len(s) && s[i] == '/' && s[i+1] == '*' {
			i += 2
			for i+1 < len(s) && (s[i] != '*' || s[i+1] != '/') {
				i++
			}
			if i+1 < len(s) {
				i += 2
			}
			continue
		}
		result.WriteByte(s[i])
		i++
	}

	// Remove trailing commas before } or ].
	re := regexp.MustCompile(`,\s*([}\]])`)
	cleaned := re.ReplaceAllString(result.String(), "$1")
	return []byte(cleaned), nil
}

// featureMap maps the short feature name (extracted from the ghcr.io URI) to a
// hopbox package definition.
var featureMap = map[string]manifest.Package{
	"node":       {Name: "nodejs", Backend: "nix"},
	"python":     {Name: "python3", Backend: "apt"},
	"go":         {Name: "go", Backend: "apt"},
	"rust":       {Name: "rustup", Backend: "apt"},
	"java":       {Name: "openjdk", Backend: "apt"},
	"git":        {Name: "git", Backend: "apt"},
	"github-cli": {Name: "gh", Backend: "apt"},
}

// skipFeatures are features that don't map to hopbox packages.
var skipFeatures = map[string]bool{
	"common-utils":             true,
	"docker-in-docker":         true,
	"docker-outside-of-docker": true,
}

// FeatureToPackages converts devcontainer features to hopbox packages.
// Returns packages and warnings for unmapped features.
func FeatureToPackages(features map[string]json.RawMessage) ([]manifest.Package, []string) {
	var pkgs []manifest.Package
	var warnings []string

	for uri := range features {
		name := featureName(uri)
		if skipFeatures[name] {
			continue
		}
		pkg, ok := featureMap[name]
		if !ok {
			warnings = append(warnings, "unknown feature "+uri)
			continue
		}
		// Extract version from feature options if present.
		var opts map[string]any
		if err := json.Unmarshal(features[uri], &opts); err == nil {
			if v, ok := opts["version"].(string); ok && v != "" && v != "latest" {
				pkg.Version = v
			}
		}
		pkgs = append(pkgs, pkg)
	}
	return pkgs, warnings
}

// featureName extracts the short name from a feature URI.
// "ghcr.io/devcontainers/features/node:1" → "node"
func featureName(uri string) string {
	// Strip version tag.
	if idx := strings.LastIndex(uri, ":"); idx > 0 {
		slash := strings.LastIndex(uri, "/")
		if idx > slash {
			uri = uri[:idx]
		}
	}
	// Take last path segment.
	if idx := strings.LastIndex(uri, "/"); idx >= 0 {
		return uri[idx+1:]
	}
	return uri
}

// imageMap maps devcontainer image path segments to hopbox packages.
var imageMap = map[string]string{
	"go":              "go",
	"python":          "python3",
	"typescript-node": "nodejs",
	"javascript-node": "nodejs",
	"node":            "nodejs",
	"rust":            "rustup",
	"java":            "openjdk",
	"dotnet":          "dotnet-sdk",
	"ruby":            "ruby",
	"php":             "php",
}

// ImageToPackages infers hopbox packages from a devcontainer image name.
func ImageToPackages(image string) ([]manifest.Package, string) {
	parts := strings.Split(image, "/")
	if len(parts) < 2 {
		return nil, "unknown image " + image
	}
	last := parts[len(parts)-1]
	name := last
	version := ""
	if idx := strings.Index(last, ":"); idx > 0 {
		name = last[:idx]
		version = last[idx+1:]
	}

	pkgName, ok := imageMap[name]
	if !ok {
		return nil, "unknown image " + image
	}

	pkg := manifest.Package{Name: pkgName, Backend: "apt"}
	// Only set version for images where the tag is a meaningful runtime version.
	if version != "" && !isOSTag(version) {
		pkg.Version = version
	}
	return []manifest.Package{pkg}, ""
}

func isOSTag(tag string) bool {
	osTags := []string{"ubuntu", "bookworm", "bullseye", "focal", "jammy", "noble", "latest", "lts"}
	for _, t := range osTags {
		if strings.HasPrefix(tag, t) {
			return true
		}
	}
	return false
}
