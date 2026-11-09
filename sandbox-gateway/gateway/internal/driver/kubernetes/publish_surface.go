package kubernetes

import (
	"context"
	"fmt"
	"strconv"

	"sandbox-gateway/internal/driver"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func (d *Driver) convergedServicePorts(existing []corev1.ServicePort, configured []int, public bool) []corev1.ServicePort {
	ports := append([]int(nil), configured...)
	for _, p := range existing {
		ports = driver.AppendPort(ports, int(p.Port))
	}
	if public {
		internal := make(map[int]bool, len(d.opts.InternalPorts))
		for _, p := range d.opts.InternalPorts {
			internal[p] = true
		}
		filtered := ports[:0]
		for _, p := range ports {
			if !internal[p] {
				filtered = append(filtered, p)
			}
		}
		ports = filtered
	}
	return servicePorts(ports)
}

// The platform preview port is added by ApplyK8sPreviewDirect at create, not
// the global app catalogue. Recover it from this sandbox's owned environment
// even if an older gateway already converged its Service back to defaults.
func (d *Driver) retainedPreviewPort(ctx context.Context, id string) (int, error) {
	secret, err := d.cs.CoreV1().Secrets(d.opts.Namespace).Get(ctx, d.secretName(id), metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("read retained preview configuration: %w", err)
	}
	if !d.releaseLabels(id, secret.Labels) {
		return 0, fmt.Errorf("retained preview configuration belongs to another sandbox")
	}
	if string(secret.Data[driver.EnvPreviewDirect]) != "1" {
		return 0, nil
	}
	p := driver.DefaultK8sPreviewPort
	if raw := string(secret.Data[driver.EnvPreviewPort]); raw != "" {
		p, err = strconv.Atoi(raw)
		if err != nil || p < 1 || p > 65535 {
			return 0, fmt.Errorf("retained preview port is invalid")
		}
	}
	for _, private := range d.opts.InternalPorts {
		if p == private {
			return 0, fmt.Errorf("retained preview port is reserved for an internal endpoint")
		}
	}
	return p, nil
}
