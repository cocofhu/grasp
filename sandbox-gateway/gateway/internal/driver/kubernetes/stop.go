package kubernetes

import (
	"context"
	"fmt"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
)

// observedStopped requires controller convergence and actual process exit.
// Spec.replicas=0 is only intent: a terminating Pod can still run user code.
func (d *Driver) observedStopped(ctx context.Context, id string, dep *appsv1.Deployment) (bool, error) {
	if dep.Spec.Replicas == nil || *dep.Spec.Replicas != 0 || dep.Status.ObservedGeneration < dep.Generation || dep.Status.Replicas != 0 || dep.Status.ReadyReplicas != 0 {
		return false, nil
	}
	pods, err := d.cs.CoreV1().Pods(d.opts.Namespace).List(ctx, metav1.ListOptions{LabelSelector: labels.Set(d.selector(id)).String()})
	if err != nil {
		return false, err
	}
	for _, pod := range pods.Items {
		if pod.Status.Phase != corev1.PodSucceeded && pod.Status.Phase != corev1.PodFailed {
			return false, nil
		}
	}
	return true, nil
}

func (d *Driver) waitStopped(ctx context.Context, id string) error {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for {
		dep, err := d.cs.AppsV1().Deployments(d.opts.Namespace).Get(ctx, d.resourceName(id), metav1.GetOptions{})
		if err != nil {
			return err
		}
		if dep.Spec.Replicas == nil || *dep.Spec.Replicas != 0 {
			return fmt.Errorf("sandbox %s stop was superseded by a start request", id)
		}
		stopped, err := d.observedStopped(ctx, id, dep)
		if err != nil {
			return err
		}
		if stopped {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("sandbox %s process exit has not been confirmed: %w", id, ctx.Err())
		case <-ticker.C:
		}
	}
}
