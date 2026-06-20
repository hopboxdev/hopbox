//go:build k8s

package main

import (
	"github.com/mesadev/mesa/internal/config"
	"github.com/mesadev/mesa/internal/core/ports"
	k8sstorage "github.com/mesadev/mesa/providers/storage/kubernetes"
	"github.com/mesadev/mesa/internal/k8sclient"
)

// newStorage returns no error today (see main.go), so a misconfigured kubeconfig
// or storage spec fails fast: mesad cannot run without storage.
func newStorage(cfg config.Config) ports.Storage {
	cli, err := k8sclient.New(cfg.Kubeconfig)
	if err != nil {
		panic("mesad: k8spvc storage: " + err.Error())
	}
	s, err := k8sstorage.New(cli, cfg.KubeNamespace, cfg.KubeStorageClass, cfg.KubeHomeSize)
	if err != nil {
		panic("mesad: k8spvc storage: " + err.Error())
	}
	return s
}
