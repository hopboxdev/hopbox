//go:build k8s

package main

import (
	"github.com/hopboxdev/hopbox/internal/config"
	"github.com/hopboxdev/hopbox/internal/core/ports"
	"github.com/hopboxdev/hopbox/internal/k8sclient"
	k8scompute "github.com/hopboxdev/hopbox/providers/compute/kubernetes"
)

func newCompute(cfg config.Config) (ports.Compute, error) {
	cli, err := k8sclient.New(cfg.Kubeconfig)
	if err != nil {
		return nil, err
	}
	return k8scompute.New(cli, cfg.KubeNamespace), nil
}
