package kubernetes

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"

	"sandbox-gateway/internal/driver"
)

var _ driver.ExitReporter = (*Driver)(nil)

// LastExit reports the latest termination of the sandbox container across all
// of the sandbox's pods: an in-place restart (lastState), a container that is
// down right now (state), or an evicted pod.
func (d *Driver) LastExit(ctx context.Context, id string) (*driver.ExitInfo, error) {
	list, err := d.cs.CoreV1().Pods(d.opts.Namespace).List(ctx, metav1.ListOptions{
		LabelSelector: labels.Set(d.selector(id)).String(),
	})
	if err != nil {
		return nil, fmt.Errorf("list pods for sandbox %s: %w", id, err)
	}
	var best *driver.ExitInfo
	keep := func(e *driver.ExitInfo) {
		if e != nil && (best == nil || e.At.After(best.At)) {
			best = e
		}
	}
	for i := range list.Items {
		pod := &list.Items[i]
		if pod.Status.Reason == "Evicted" {
			keep(&driver.ExitInfo{Reason: "Evicted", Message: pod.Status.Message, At: podEndTime(pod).Time})
		}
		for _, cs := range pod.Status.ContainerStatuses {
			if cs.Name != sandboxContainer {
				continue
			}
			mem := containerMemoryMB(pod)
			for _, t := range []*corev1.ContainerStateTerminated{cs.State.Terminated, cs.LastTerminationState.Terminated} {
				keep(exitFromTerminated(t, int(cs.RestartCount), mem))
			}
		}
	}
	return best, nil
}

func exitFromTerminated(t *corev1.ContainerStateTerminated, restarts int, memMB int64) *driver.ExitInfo {
	if t == nil {
		return nil
	}
	reason := t.Reason
	if reason == "" {
		reason = "Exited"
	}
	return &driver.ExitInfo{
		Reason:    reason,
		ExitCode:  int(t.ExitCode),
		OOMKilled: reason == "OOMKilled",
		Message:   t.Message,
		Restarts:  restarts,
		MemoryMB:  memMB,
		At:        t.FinishedAt.Time,
	}
}

func containerMemoryMB(pod *corev1.Pod) int64 {
	for _, c := range pod.Spec.Containers {
		if c.Name != sandboxContainer {
			continue
		}
		if q, ok := c.Resources.Limits[corev1.ResourceMemory]; ok {
			return q.Value() / (1 << 20)
		}
	}
	return 0
}

func podEndTime(pod *corev1.Pod) (t metav1.Time) {
	for _, c := range pod.Status.Conditions {
		if c.LastTransitionTime.After(t.Time) {
			t = c.LastTransitionTime
		}
	}
	if t.IsZero() {
		t = pod.CreationTimestamp
	}
	return t
}
