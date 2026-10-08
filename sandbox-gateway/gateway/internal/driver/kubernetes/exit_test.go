package kubernetes

import (
	"context"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func sandboxPod(d *Driver, name string, cs corev1.ContainerStatus) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "sandboxes", Labels: d.selector("abc")},
		Spec: corev1.PodSpec{Containers: []corev1.Container{{
			Name:      sandboxContainer,
			Resources: corev1.ResourceRequirements{Limits: corev1.ResourceList{corev1.ResourceMemory: resource.MustParse("8192Mi")}},
		}}},
		Status: corev1.PodStatus{ContainerStatuses: []corev1.ContainerStatus{cs}},
	}
}

func TestLastExit(t *testing.T) {
	ctx := context.Background()
	d := testDriver(t, false)
	if e, err := d.LastExit(ctx, "abc"); e != nil || err != nil {
		t.Fatalf("no pods: e=%+v err=%v", e, err)
	}

	t0 := time.Date(2026, 10, 7, 22, 30, 0, 0, time.UTC)
	running := corev1.ContainerStatus{
		Name:         sandboxContainer,
		RestartCount: 1,
		State:        corev1.ContainerState{Running: &corev1.ContainerStateRunning{}},
		LastTerminationState: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{
			Reason: "OOMKilled", ExitCode: 137, FinishedAt: metav1.NewTime(t0),
		}},
	}
	if _, err := d.cs.CoreV1().Pods("sandboxes").Create(ctx, sandboxPod(d, "p1", running), metav1.CreateOptions{}); err != nil {
		t.Fatal(err)
	}
	e, err := d.LastExit(ctx, "abc")
	if err != nil || e == nil || !e.OOMKilled || e.ExitCode != 137 || e.Restarts != 1 || e.MemoryMB != 8192 || !e.At.Equal(t0) {
		t.Fatalf("e=%+v err=%v", e, err)
	}

	down := corev1.ContainerStatus{
		Name: sandboxContainer,
		State: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{
			ExitCode: 2, FinishedAt: metav1.NewTime(t0.Add(time.Minute)),
		}},
	}
	other := corev1.ContainerStatus{Name: "sidecar", State: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{
		Reason: "OOMKilled", FinishedAt: metav1.NewTime(t0.Add(time.Hour)),
	}}}
	p2 := sandboxPod(d, "p2", down)
	p2.Status.ContainerStatuses = append(p2.Status.ContainerStatuses, other)
	if _, err := d.cs.CoreV1().Pods("sandboxes").Create(ctx, p2, metav1.CreateOptions{}); err != nil {
		t.Fatal(err)
	}
	if e, _ := d.LastExit(ctx, "abc"); e == nil || e.Reason != "Exited" || e.ExitCode != 2 || e.OOMKilled {
		t.Fatalf("latest sandbox-container exit: e=%+v", e)
	}

	ev := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "p3", Namespace: "sandboxes", Labels: d.selector("abc")},
		Status: corev1.PodStatus{Reason: "Evicted", Message: "low on memory", Conditions: []corev1.PodCondition{
			{Type: corev1.PodReady, LastTransitionTime: metav1.NewTime(t0.Add(2 * time.Minute))},
		}},
	}
	if _, err := d.cs.CoreV1().Pods("sandboxes").Create(ctx, ev, metav1.CreateOptions{}); err != nil {
		t.Fatal(err)
	}
	if e, _ := d.LastExit(ctx, "abc"); e == nil || e.Reason != "Evicted" || e.Message != "low on memory" {
		t.Fatalf("evicted: e=%+v", e)
	}
}

func TestPodEndTimeFallsBackToCreation(t *testing.T) {
	c := metav1.NewTime(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	if got := podEndTime(&corev1.Pod{ObjectMeta: metav1.ObjectMeta{CreationTimestamp: c}}); !got.Equal(&c) {
		t.Fatalf("got %v", got)
	}
	if containerMemoryMB(&corev1.Pod{}) != 0 {
		t.Fatal("no container → 0")
	}
}
