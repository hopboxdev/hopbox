//go:build k8s

package main

import (
	"github.com/hopboxdev/hopbox/internal/config"
	"github.com/hopboxdev/hopbox/internal/core/ports"
	"github.com/hopboxdev/hopbox/internal/k8sclient"
	k8sstorage "github.com/hopboxdev/hopbox/providers/storage/kubernetes"
)

// newStorage returns no error today (see main.go), so a misconfigured kubeconfig
// or storage spec fails fast: hopboxd cannot run without storage.
func newStorage(cfg config.Config) ports.Storage {
	cli, err := k8sclient.New(cfg.Kubeconfig)
	if err != nil {
		panic("hopboxd: k8spvc storage: " + err.Error())
	}
	s, err := k8sstorage.New(cli, cfg.KubeNamespace, cfg.KubeStorageClass, cfg.KubeHomeSize)
	if err != nil {
		panic("hopboxd: k8spvc storage: " + err.Error())
	}
	return s
}
