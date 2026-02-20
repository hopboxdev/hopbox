package agent

import (
	"strings"
	"time"

	"github.com/hopboxdev/hopbox/internal/manifest"
	"github.com/hopboxdev/hopbox/internal/service"
)

// BuildServiceManager creates a service.Manager populated from the workspace
// manifest. Only docker-type services are registered; others are skipped.
// This is called both at agent startup and on workspace.sync.
func BuildServiceManager(ws *manifest.Workspace) *service.Manager {
	mgr := service.NewManager()
	for name, svc := range ws.Services {
		var dataPaths []string
		var volumes []string
		for _, d := range svc.Data {
			if d.Host != "" {
				dataPaths = append(dataPaths, d.Host)
			}
			if d.Host != "" && d.Container != "" {
				volumes = append(volumes, d.Host+":"+d.Container)
			}
		}

		var hc *service.HealthCheck
		if svc.Health != nil && svc.Health.HTTP != "" {
			hc = &service.HealthCheck{HTTP: svc.Health.HTTP}
			if svc.Health.Interval != "" {
				hc.Interval, _ = time.ParseDuration(svc.Health.Interval)
			}
			if svc.Health.Timeout != "" {
				hc.Timeout, _ = time.ParseDuration(svc.Health.Timeout)
			}
		}

		def := &service.Def{
			Name:      name,
			Type:      svc.Type,
			Image:     svc.Image,
			Command:   svc.Command,
			Ports:     svc.Ports,
			Env:       svc.Env,
			DependsOn: svc.DependsOn,
			Health:    hc,
			DataPaths: dataPaths,
		}

		var backend service.Backend
		if svc.Type == "docker" {
			ports := make([]string, 0, len(svc.Ports))
			for _, p := range svc.Ports {
				if strings.ContainsRune(p, ':') {
					ports = append(ports, p) // already "host:container"
				} else {
					ports = append(ports, p+":"+p) // bare port → N:N
				}
			}
			backend = &service.DockerBackend{
				Image:   svc.Image,
				Cmd:     strings.Fields(svc.Command),
				Env:     svc.Env,
				Ports:   ports,
				Volumes: volumes,
			}
		}
		if backend != nil {
			mgr.Register(def, backend)
		}
	}
	return mgr
}
