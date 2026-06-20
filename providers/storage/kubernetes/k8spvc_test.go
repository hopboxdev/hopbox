//go:build k8s

package kubernetes

import (
	"context"
	"testing"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/mesadev/mesa/internal/core/ports"
)

func TestEnsureHomeCreatesPVCThenIsIdempotent(t *testing.T) {
	cli := fake.NewSimpleClientset()
	p, err := New(cli, "mesa-workspaces", "fast", "2Gi")
	if err != nil {
		t.Fatal(err)
	}
	m1, err := p.EnsureHome(context.Background(), ports.HomeRequest{WorkspaceID: "w1", TenantID: "default"})
	if err != nil {
		t.Fatal(err)
	}
	if m1.Source != "mesa-home-w1" || m1.Target != "/home/dev" {
		t.Fatalf("bad mount: %+v", m1)
	}
	// the PVC actually exists with the requested class + size
	pvc, err := cli.CoreV1().PersistentVolumeClaims("mesa-workspaces").Get(context.Background(), "mesa-home-w1", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("pvc not created: %v", err)
	}
	if pvc.Spec.StorageClassName == nil || *pvc.Spec.StorageClassName != "fast" {
		t.Fatalf("storageclass not set: %+v", pvc.Spec.StorageClassName)
	}
	// idempotent: second call returns the same source and does not error
	m2, err := p.EnsureHome(context.Background(), ports.HomeRequest{WorkspaceID: "w1"})
	if err != nil || m2.Source != m1.Source {
		t.Fatalf("idempotent ensure: m2=%+v err=%v", m2, err)
	}
}

func TestDeleteRemovesPVCAndIsIdempotent(t *testing.T) {
	cli := fake.NewSimpleClientset()
	p, _ := New(cli, "mesa-workspaces", "", "1Gi")
	m, _ := p.EnsureHome(context.Background(), ports.HomeRequest{WorkspaceID: "w1"})
	if err := p.Delete(context.Background(), m.Source); err != nil {
		t.Fatal(err)
	}
	if _, err := cli.CoreV1().PersistentVolumeClaims("mesa-workspaces").Get(context.Background(), m.Source, metav1.GetOptions{}); !apierrors.IsNotFound(err) {
		t.Fatalf("pvc still present after delete: %v", err)
	}
	if err := p.Delete(context.Background(), m.Source); err != nil {
		t.Fatalf("second delete must be idempotent: %v", err)
	}
}
