package kubernetes

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
	"sandbox-gateway/internal/driver"
)

func TestStopConfirmsControllerAndPodExit(t *testing.T) {
	d := testDriver(t, false)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if _, err := d.Create(ctx, driver.Spec{ID: "stopping", Image: "img"}); err != nil {
		t.Fatal(err)
	}
	deps := d.cs.AppsV1().Deployments("sandboxes")
	dep, _ := deps.Get(ctx, "sbx-stopping", metav1.GetOptions{})
	dep.Generation = 2
	dep.Status = appsv1.DeploymentStatus{ObservedGeneration: 1, Replicas: 1, ReadyReplicas: 1}
	if _, err := deps.Update(ctx, dep, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "still-running", Namespace: "sandboxes", Labels: d.selector("stopping")}, Status: corev1.PodStatus{Phase: corev1.PodRunning}}
	if _, err := d.cs.CoreV1().Pods("sandboxes").Create(ctx, pod, metav1.CreateOptions{}); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- d.Stop(ctx, "stopping") }()
	deadline := time.Now().Add(time.Second)
	for {
		dep, _ = deps.Get(ctx, "sbx-stopping", metav1.GetOptions{})
		if dep.Spec.Replicas != nil && *dep.Spec.Replicas == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("stop intent was not written")
		}
		time.Sleep(time.Millisecond)
	}
	if status, err := d.Status(ctx, "stopping"); err != nil || status != driver.StatusPending {
		t.Fatalf("intent is not confirmed stop: %s %v", status, err)
	}
	select {
	case err := <-done:
		t.Fatalf("stop returned while a Pod was running: %v", err)
	default:
	}
	dep.Status = appsv1.DeploymentStatus{ObservedGeneration: 2}
	if _, err := deps.UpdateStatus(ctx, dep, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	if stopped, err := d.observedStopped(ctx, "stopping", dep); err != nil || stopped {
		t.Fatalf("a live Pod survived a zero deployment status: stopped=%v err=%v", stopped, err)
	}
	for _, pod := range []*corev1.Pod{
		{ObjectMeta: metav1.ObjectMeta{Name: "finished", Namespace: "sandboxes", Labels: d.selector("stopping")}, Status: corev1.PodStatus{Phase: corev1.PodSucceeded}},
		{ObjectMeta: metav1.ObjectMeta{Name: "another-task", Namespace: "sandboxes", Labels: d.selector("other")}, Status: corev1.PodStatus{Phase: corev1.PodRunning}},
	} {
		if _, err := d.cs.CoreV1().Pods("sandboxes").Create(ctx, pod, metav1.CreateOptions{}); err != nil {
			t.Fatal(err)
		}
	}
	if err := d.cs.CoreV1().Pods("sandboxes").Delete(ctx, pod.Name, metav1.DeleteOptions{}); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("confirmed stop did not complete")
	}
	if status, err := d.Status(ctx, "stopping"); err != nil || status != driver.StatusStopped {
		t.Fatalf("process exit not reflected: %s %v", status, err)
	}
}

func TestStopDoesNotHideCancellationOrPodReadFailure(t *testing.T) {
	for _, failure := range []string{"cancel", "pod-read", "deployment-read", "superseded"} {
		t.Run(failure, func(t *testing.T) {
			d := testDriver(t, false)
			ctx := context.Background()
			if _, err := d.Create(ctx, driver.Spec{ID: "blocked", Image: "img"}); err != nil {
				t.Fatal(err)
			}
			if err := d.scale(ctx, "blocked", 0); err != nil {
				t.Fatal(err)
			}
			client := d.cs.(*fake.Clientset)
			switch failure {
			case "cancel":
				dep, _ := client.AppsV1().Deployments("sandboxes").Get(ctx, "sbx-blocked", metav1.GetOptions{})
				dep.Generation = 2
				_, _ = client.AppsV1().Deployments("sandboxes").Update(ctx, dep, metav1.UpdateOptions{})
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			case "pod-read":
				client.PrependReactor("list", "pods", func(k8stesting.Action) (bool, runtime.Object, error) { return true, nil, errors.New("pod read failed") })
			case "deployment-read":
				client.PrependReactor("get", "deployments", func(k8stesting.Action) (bool, runtime.Object, error) {
					return true, nil, errors.New("deployment read failed")
				})
			case "superseded":
				if err := d.scale(ctx, "blocked", 1); err != nil {
					t.Fatal(err)
				}
			}
			err := d.waitStopped(ctx, "blocked")
			if err == nil || failure == "superseded" && !strings.Contains(err.Error(), "superseded") {
				t.Fatalf("unconfirmed stop was accepted: %v", err)
			}
			if failure == "cancel" && !errors.Is(err, context.Canceled) {
				t.Fatalf("cancellation was hidden: %v", err)
			}
			if failure == "pod-read" {
				status, err := d.Status(context.Background(), "blocked")
				if status != driver.StatusError || err == nil {
					t.Fatalf("failed observation must not report stopped: %s %v", status, err)
				}
			}
		})
	}
}
