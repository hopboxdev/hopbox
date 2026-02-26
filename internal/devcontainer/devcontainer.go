package devcontainer

import (
	"encoding/json"
	"os"
	"regexp"
	"strings"

	"github.com/hopboxdev/hopbox/internal/manifest"
	"gopkg.in/yaml.v3"
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

// composeFile is the subset of docker-compose.yml we parse.
type composeFile struct {
	Services map[string]composeService `yaml:"services"`
}

type composeService struct {
	Image       string           `yaml:"image"`
	Ports       []string         `yaml:"ports"`
	Environment composeEnv       `yaml:"environment"`
	Volumes     []string         `yaml:"volumes"`
	DependsOn   composeDependsOn `yaml:"depends_on"`
}

// composeEnv handles both map and list formats for environment.
type composeEnv map[string]string

func (e *composeEnv) UnmarshalYAML(value *yaml.Node) error {
	m := make(map[string]string)
	if err := value.Decode(&m); err == nil {
		*e = m
		return nil
	}
	var list []string
	if err := value.Decode(&list); err != nil {
		return err
	}
	*e = make(map[string]string, len(list))
	for _, item := range list {
		k, v, _ := strings.Cut(item, "=")
		(*e)[k] = v
	}
	return nil
}

// composeDependsOn handles both list and map formats.
type composeDependsOn []string

func (d *composeDependsOn) UnmarshalYAML(value *yaml.Node) error {
	var list []string
	if err := value.Decode(&list); err == nil {
		*d = list
		return nil
	}
	var m map[string]any
	if err := value.Decode(&m); err != nil {
		return err
	}
	for k := range m {
		*d = append(*d, k)
	}
	return nil
}

// ParseComposeFile reads a docker-compose YAML and maps services to hopbox service definitions.
func ParseComposeFile(path string) (map[string]manifest.Service, []string) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, []string{"compose file not found: " + path}
	}

	var cf composeFile
	if err := yaml.Unmarshal(data, &cf); err != nil {
		return nil, []string{"parse compose file: " + err.Error()}
	}

	services := make(map[string]manifest.Service, len(cf.Services))
	var warnings []string

	for name, svc := range cf.Services {
		s := manifest.Service{
			Type:      "docker",
			Image:     svc.Image,
			Ports:     svc.Ports,
			Env:       map[string]string(svc.Environment),
			DependsOn: []string(svc.DependsOn),
		}

		for _, vol := range svc.Volumes {
			parts := strings.SplitN(vol, ":", 2)
			if len(parts) == 2 {
				s.Data = append(s.Data, manifest.DataMount{
					Host:      parts[0],
					Container: parts[1],
				})
			} else {
				warnings = append(warnings, "service "+name+": cannot map volume "+vol)
			}
		}

		services[name] = s
	}

	return services, warnings
}
