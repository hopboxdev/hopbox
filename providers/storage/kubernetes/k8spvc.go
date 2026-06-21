//go:build k8s

// Package kubernetes (storage) persists a workspace home as a PersistentVolumeClaim
// and hands its claim name to a Compute provider as Mount.Source. This is the seam
// working: localfs returns a host path in Mount.Source, k8spvc returns a PVC claim —
// both opaque strings the matching compute provider knows how to mount.
package kubernetes

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/hopboxdev/hopbox/internal/core/ports"
)

const (
	homeTarget     = "/home/dev"
	labelWorkspace = "hopbox.workspace_id"
	labelTenant    = "hopbox.tenant"
)

type Provider struct {
	cli          kubernetes.Interface
	ns           string
	storageClass string
	size         resource.Quantity
}

var _ ports.Storage = (*Provider)(nil)

// New builds a k8spvc storage provider. size is a quantity string like "1Gi";
// storageClass "" means the cluster default.
func New(cli kubernetes.Interface, namespace, storageClass, size string) (*Provider, error) {
	if namespace == "" {
		namespace = "hopbox-workspaces"
	}
	if size == "" {
		size = "1Gi"
	}
	q, err := resource.ParseQuantity(size)
	if err != nil {
		return nil, fmt.Errorf("k8spvc: bad home size %q: %w", size, err)
	}
	return &Provider{cli: cli, ns: namespace, storageClass: storageClass, size: q}, nil
}

func pvcName(wsID string) string { return "hopbox-home-" + wsID }

func (p *Provider) EnsureHome(ctx context.Context, r ports.HomeRequest) (ports.Mount, error) {
	if r.WorkspaceID == "" {
		return ports.Mount{}, fmt.Errorf("k8spvc: empty workspace id")
	}
	name := pvcName(r.WorkspaceID)
	pvcs := p.cli.CoreV1().PersistentVolumeClaims(p.ns)

	// idempotent get-or-create
	if _, err := pvcs.Get(ctx, name, metav1.GetOptions{}); err == nil {
		return ports.Mount{Source: name, Target: homeTarget}, nil
	} else if !apierrors.IsNotFound(err) {
		return ports.Mount{}, fmt.Errorf("k8spvc: get pvc %q: %w", name, err)
	}

	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:   name,
			Labels: map[string]string{labelWorkspace: r.WorkspaceID, labelTenant: r.TenantID},
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{corev1.ResourceStorage: p.size},
			},
		},
	}
	if p.storageClass != "" {
		sc := p.storageClass
		pvc.Spec.StorageClassName = &sc
	}
	if _, err := pvcs.Create(ctx, pvc, metav1.CreateOptions{}); err != nil {
		if apierrors.IsAlreadyExists(err) { // lost a create race; the PVC is there
			return ports.Mount{Source: name, Target: homeTarget}, nil
		}
		return ports.Mount{}, fmt.Errorf("k8spvc: create pvc %q: %w", name, err)
	}
	return ports.Mount{Source: name, Target: homeTarget}, nil
}

// Delete removes a workspace home. homeRef is the PVC name returned by EnsureHome.
func (p *Provider) Delete(ctx context.Context, homeRef string) error {
	err := p.cli.CoreV1().PersistentVolumeClaims(p.ns).Delete(ctx, homeRef, metav1.DeleteOptions{})
	if err != nil && !apierrors.IsNotFound(err) {
		return fmt.Errorf("k8spvc: delete pvc %q: %w", homeRef, err)
	}
	return nil
}
