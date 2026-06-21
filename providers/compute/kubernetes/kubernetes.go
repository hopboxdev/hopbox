//go:build k8s

// Package kubernetes (compute) maps a neutral ProvisionRequest onto a bare Pod
// (a singleton pet the reconciler owns, not a Deployment). An initContainer
// seeds the hopbox-agent binary from AgentImage.ImageRef into a shared emptyDir;
// the workspace container mounts it read-only and runs AgentImage.TargetPath.
// No k8s type crosses ports.*.
package kubernetes

import (
	"context"
	"fmt"
	"path"
	"strconv"
	"strings"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/hopboxdev/hopbox/internal/core/ports"
)

const (
	labelWorkspace = "hopbox.workspace_id"
	agentVolume    = "hopbox-agent"
	defaultTarget  = "/hopbox/hopbox-agent"
)

type Provider struct {
	cli kubernetes.Interface
	ns  string
}

var _ ports.Compute = (*Provider)(nil)

func New(cli kubernetes.Interface, namespace string) *Provider {
	if namespace == "" {
		namespace = "hopbox-workspaces"
	}
	return &Provider{cli: cli, ns: namespace}
}

func podName(wsID string) string { return "hopbox-" + wsID }

// ref encoding is provider-opaque to the core: "<namespace>/<podname>".
func (p *Provider) ref(name string) string { return p.ns + "/" + name }
func splitRef(ref string) (ns, name string) {
	if i := strings.IndexByte(ref, '/'); i >= 0 {
		return ref[:i], ref[i+1:]
	}
	return "", ref
}

func (p *Provider) Provision(ctx context.Context, r ports.ProvisionRequest) (ports.Instance, error) {
	if r.Agent.ImageRef == "" || r.Agent.BinaryPath == "" {
		return ports.Instance{}, fmt.Errorf("kubernetes: Agent needs ImageRef+BinaryPath (HostBinaryPath is docker-only)")
	}
	target := r.Agent.TargetPath
	if target == "" {
		target = defaultTarget
	}
	agentDir := path.Dir(target) // e.g. "/hopbox"

	env := make([]corev1.EnvVar, 0, len(r.Env))
	for k, v := range r.Env {
		env = append(env, corev1.EnvVar{Name: k, Value: v})
	}

	// volumes: agent emptyDir + one PVC volume per requested mount (Source = claim name)
	volumes := []corev1.Volume{{
		Name:         agentVolume,
		VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}},
	}}
	wsMounts := []corev1.VolumeMount{{Name: agentVolume, MountPath: agentDir, ReadOnly: true}}
	for i, m := range r.Mounts {
		vName := "hopbox-mount-" + strconv.Itoa(i)
		volumes = append(volumes, corev1.Volume{
			Name: vName,
			VolumeSource: corev1.VolumeSource{
				PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: m.Source},
			},
		})
		wsMounts = append(wsMounts, corev1.VolumeMount{Name: vName, MountPath: m.Target, ReadOnly: m.ReadOnly})
	}

	ws := corev1.Container{
		Name:         "workspace",
		Image:        r.ImageRef,
		Command:      []string{target},
		Env:          env,
		VolumeMounts: wsMounts,
	}
	if r.MemMB > 0 {
		ws.Resources = corev1.ResourceRequirements{
			Limits: corev1.ResourceList{corev1.ResourceMemory: *resource.NewQuantity(r.MemMB*1024*1024, resource.BinarySI)},
		}
	}

	name := podName(r.WorkspaceID)
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:   name,
			Labels: map[string]string{labelWorkspace: r.WorkspaceID},
		},
		Spec: corev1.PodSpec{
			RestartPolicy: corev1.RestartPolicyNever,
			Volumes:       volumes,
			InitContainers: []corev1.Container{{
				Name:         "hopbox-agent-init",
				Image:        r.Agent.ImageRef,
				Command:      []string{"cp", r.Agent.BinaryPath, target},
				VolumeMounts: []corev1.VolumeMount{{Name: agentVolume, MountPath: agentDir}},
			}},
			Containers: []corev1.Container{ws},
		},
	}

	pods := p.cli.CoreV1().Pods(p.ns)
	// Idempotency mirrors the docker provider: a self-heal re-provision reuses the
	// stable name while a dead Pod may still hold it. Delete-stale-then-create.
	if err := pods.Delete(ctx, name, metav1.DeleteOptions{}); err != nil && !apierrors.IsNotFound(err) {
		return ports.Instance{}, fmt.Errorf("kubernetes: remove stale pod %q: %w", name, err)
	}
	if _, err := pods.Create(ctx, pod, metav1.CreateOptions{}); err != nil {
		return ports.Instance{}, fmt.Errorf("kubernetes: create pod %q: %w", name, err)
	}
	return ports.Instance{Ref: p.ref(name), Phase: ports.InstanceRunning}, nil
}

func (p *Provider) Status(ctx context.Context, ref string) (ports.Instance, error) {
	ns, name := splitRef(ref)
	if ns == "" {
		ns = p.ns
	}
	pod, err := p.cli.CoreV1().Pods(ns).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			return ports.Instance{Ref: ref, Phase: ports.InstanceGone}, nil
		}
		return ports.Instance{}, fmt.Errorf("kubernetes: get pod %q: %w", ref, err)
	}
	phase := ports.InstanceStopped
	switch pod.Status.Phase {
	case corev1.PodRunning:
		phase = ports.InstanceRunning
	case corev1.PodSucceeded:
		phase = ports.InstanceGone
	case corev1.PodFailed:
		phase = ports.InstanceFailed
	case corev1.PodPending, "": // still coming up (fake clientset leaves phase empty)
		phase = ports.InstanceStopped
	}
	return ports.Instance{Ref: ref, Phase: phase}, nil
}

// Stop deletes the Pod but keeps any PVC (idle suspend).
func (p *Provider) Stop(ctx context.Context, ref string) error { return p.delete(ctx, ref) }

// Destroy deletes the Pod (the PVC is removed only by Storage.Delete on purge).
func (p *Provider) Destroy(ctx context.Context, ref string) error { return p.delete(ctx, ref) }

func (p *Provider) delete(ctx context.Context, ref string) error {
	ns, name := splitRef(ref)
	if ns == "" {
		ns = p.ns
	}
	err := p.cli.CoreV1().Pods(ns).Delete(ctx, name, metav1.DeleteOptions{})
	if err != nil && !apierrors.IsNotFound(err) {
		return fmt.Errorf("kubernetes: delete pod %q: %w", ref, err)
	}
	return nil
}
