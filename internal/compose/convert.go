package compose

import (
	"fmt"
	"strings"

	"github.com/hopboxdev/hopbox/internal/manifest"
	"gopkg.in/yaml.v3"
)

// composeFile is the subset of docker-compose.yml we parse.
type composeFile struct {
	Services map[string]composeService `yaml:"services"`
}

type composeService struct {
	Image       string            `yaml:"image"`
	Ports       []string          `yaml:"ports"`
	Environment composeEnv        `yaml:"environment"`
	Volumes     []string          `yaml:"volumes"`
	DependsOn   composeDependsOn  `yaml:"depends_on"`
	Healthcheck *composeHealthRaw `yaml:"healthcheck"`
}

// composeHealthRaw represents a Docker Compose healthcheck.
type composeHealthRaw struct {
	Test     composeHealthTest `yaml:"test"`
	Interval string            `yaml:"interval"`
	Timeout  string            `yaml:"timeout"`
}

// composeHealthTest handles both string and list formats.
type composeHealthTest string

func (t *composeHealthTest) UnmarshalYAML(value *yaml.Node) error {
	var s string
	if err := value.Decode(&s); err == nil {
		*t = composeHealthTest(s)
		return nil
	}
	var list []string
	if err := value.Decode(&list); err != nil {
		return err
	}
	// ["CMD-SHELL", "curl ..."] or ["CMD", "curl", "..."]
	if len(list) >= 2 && (list[0] == "CMD-SHELL" || list[0] == "CMD") {
		*t = composeHealthTest(strings.Join(list[1:], " "))
	} else {
		*t = composeHealthTest(strings.Join(list, " "))
	}
	return nil
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

// Convert parses raw docker-compose YAML bytes and returns a hopbox Workspace
// along with warnings for unmapped fields.
func Convert(data []byte) (*manifest.Workspace, []string, error) {
	var cf composeFile
	if err := yaml.Unmarshal(data, &cf); err != nil {
		return nil, nil, fmt.Errorf("parse compose file: %w", err)
	}

	if len(cf.Services) == 0 {
		return nil, nil, fmt.Errorf("no services found in compose file")
	}

	ws := &manifest.Workspace{
		Name:     "myapp",
		Services: make(map[string]manifest.Service, len(cf.Services)),
	}
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
				// Named volumes (no path separator) can't be directly mapped.
				if !strings.Contains(parts[0], "/") && !strings.HasPrefix(parts[0], ".") {
					warnings = append(warnings, fmt.Sprintf("service %s: named volume %q not directly supported — use a host path", name, parts[0]))
					continue
				}
				s.Data = append(s.Data, manifest.DataMount{
					Host:      parts[0],
					Container: parts[1],
				})
			} else {
				warnings = append(warnings, fmt.Sprintf("service %s: cannot map volume %q", name, vol))
			}
		}

		if svc.Healthcheck != nil && string(svc.Healthcheck.Test) != "" {
			s.Health = &manifest.HealthCheck{
				Exec:     string(svc.Healthcheck.Test),
				Interval: svc.Healthcheck.Interval,
				Timeout:  svc.Healthcheck.Timeout,
			}
		}

		ws.Services[name] = s
	}

	return ws, warnings, nil
}
