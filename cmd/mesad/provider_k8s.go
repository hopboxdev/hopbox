//go:build k8s

package main

import (
	"github.com/mesadev/mesa/internal/config"
	"github.com/mesadev/mesa/internal/core/ports"
	k8scompute "github.com/mesadev/mesa/providers/compute/kubernetes"
	"github.com/mesadev/mesa/internal/k8sclient"
)

func newCompute(cfg config.Config) (ports.Compute, error) {
	cli, err := k8sclient.New(cfg.Kubeconfig)
	if err != nil {
		return nil, err
	}
	return k8scompute.New(cli, cfg.KubeNamespace), nil
}
