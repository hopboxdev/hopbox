//go:build k8s

// Package k8sclient builds a kubernetes.Interface for the k8s providers. It is
// the only place client construction lives, so the providers stay injectable
// (tests pass a fake.Clientset; production passes the real client from here).
package k8sclient

import (
	"fmt"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

// New returns a clientset. If kubeconfig is empty it uses in-cluster config
// (the mesad-in-cluster / mesa-provider-in-cluster deploy); otherwise it loads
// the given kubeconfig file (local dev against a remote cluster).
func New(kubeconfig string) (kubernetes.Interface, error) {
	var (
		cfg *rest.Config
		err error
	)
	if kubeconfig == "" {
		cfg, err = rest.InClusterConfig()
		if err != nil {
			return nil, fmt.Errorf("k8sclient: in-cluster config: %w", err)
		}
	} else {
		cfg, err = clientcmd.BuildConfigFromFlags("", kubeconfig)
		if err != nil {
			return nil, fmt.Errorf("k8sclient: load kubeconfig %q: %w", kubeconfig, err)
		}
	}
	cli, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("k8sclient: new clientset: %w", err)
	}
	return cli, nil
}
