//go:build k8s

package main

import (
	"os"

	"github.com/mesadev/mesa/internal/core/ports"
	"github.com/mesadev/mesa/internal/k8sclient"
	k8scompute "github.com/mesadev/mesa/providers/compute/kubernetes"
	k8sstorage "github.com/mesadev/mesa/providers/storage/kubernetes"
)

// mesa-provider is the in-cluster remote server; it reads its k8s config from the
// environment (KUBECONFIG empty -> in-cluster). Pods reach mesad via Service DNS,
// so the advertise arg is unused here.
func kubeNamespace() string {
	if ns := os.Getenv("MESA_K8S_NAMESPACE"); ns != "" {
		return ns
	}
	return "mesa-workspaces"
}

func newCompute(string) (ports.Compute, error) {
	cli, err := k8sclient.New(os.Getenv("KUBECONFIG"))
	if err != nil {
		return nil, err
	}
	return k8scompute.New(cli, kubeNamespace()), nil
}

func newStorage() ports.Storage {
	cli, err := k8sclient.New(os.Getenv("KUBECONFIG"))
	if err != nil {
		panic("mesa-provider: k8s storage: " + err.Error())
	}
	s, err := k8sstorage.New(cli, kubeNamespace(), os.Getenv("MESA_K8S_STORAGECLASS"), os.Getenv("MESA_K8S_HOME_SIZE"))
	if err != nil {
		panic("mesa-provider: k8s storage: " + err.Error())
	}
	return s
}
