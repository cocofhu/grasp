package kubernetes

import (
	"context"
	"errors"
	"testing"
	"time"

	"sandbox-gateway/internal/driver"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

func TestDestroyPropagatesResourceCleanupFailuresAndRetries(t *testing.T) {
	for _, tc := range []struct{ resource, name string }{
		{"deployments", "sbx-cleanup"},
		{"services", "sbx-cleanup"},
		{"services", "sbx-cleanup-lb"},
		{"secrets", "sbx-cleanup-env"},
		{"persistentvolumeclaims", "sbx-cleanup-data"},
	} {
		t.Run(tc.name+"-"+tc.resource, func(t *testing.T) {
			drv := testDriver(t, true)
			if _, err := drv.Create(t.Context(), driver.Spec{ID: "cleanup", Image: "image", Ports: []int{8765}, Env: map[string]string{"TEST": "1"}}); err != nil {
				t.Fatal(err)
			}
			client := drv.cs.(*fake.Clientset)
			failure := errors.New("cleanup temporarily rejected")
			fail := true
			client.PrependReactor("delete", tc.resource, func(action k8stesting.Action) (bool, runtime.Object, error) {
				if fail && action.(k8stesting.DeleteAction).GetName() == tc.name {
					return true, nil, failure
				}
				return false, nil, nil
			})
			if err := drv.Destroy(t.Context(), "cleanup"); !errors.Is(err, failure) {
				t.Fatalf("cleanup failure hidden: %v", err)
			}
			fail = false
			if err := drv.Destroy(t.Context(), "cleanup"); err != nil {
				t.Fatalf("retry failed: %v", err)
			}
			if _, err := client.AppsV1().Deployments("sandboxes").Get(t.Context(), "sbx-cleanup", metav1.GetOptions{}); !apierrors.IsNotFound(err) {
				t.Fatalf("deployment remains: %v", err)
			}
			if _, err := client.CoreV1().PersistentVolumeClaims("sandboxes").Get(t.Context(), "sbx-cleanup-data", metav1.GetOptions{}); !apierrors.IsNotFound(err) {
				t.Fatalf("PVC remains: %v", err)
			}
			for _, name := range []string{"sbx-cleanup", "sbx-cleanup-lb"} {
				if _, err := client.CoreV1().Services("sandboxes").Get(t.Context(), name, metav1.GetOptions{}); !apierrors.IsNotFound(err) {
					t.Fatalf("service remains: %s %v", name, err)
				}
			}
			if _, err := client.CoreV1().Secrets("sandboxes").Get(t.Context(), "sbx-cleanup-env", metav1.GetOptions{}); !apierrors.IsNotFound(err) {
				t.Fatalf("secret remains: %v", err)
			}
			if err := drv.Destroy(t.Context(), "cleanup"); err != nil {
				t.Fatalf("idempotent deletion failed: %v", err)
			}
		})
	}
}

func TestDestroyWaitsForPVCDeletionInsteadOfOnlyAcceptingDeleteRequest(t *testing.T) {
	drv := testDriver(t, false)
	if _, err := drv.Create(t.Context(), driver.Spec{ID: "finalizing", Image: "image"}); err != nil {
		t.Fatal(err)
	}
	client := drv.cs.(*fake.Clientset)
	pending := true
	client.PrependReactor("delete", "persistentvolumeclaims", func(k8stesting.Action) (bool, runtime.Object, error) {
		if pending {
			return true, nil, nil
		} // DELETE accepted, CSI finalizer still owns the PVC.
		return false, nil, nil
	})
	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	if err := drv.Destroy(ctx, "finalizing"); err == nil {
		t.Fatal("reported resource release while PVC still exists")
	}
	if _, err := client.CoreV1().PersistentVolumeClaims("sandboxes").Get(t.Context(), "sbx-finalizing-data", metav1.GetOptions{}); err != nil {
		t.Fatalf("fixture lost pending PVC: %v", err)
	}
	pending = false
	if err := drv.Destroy(t.Context(), "finalizing"); err != nil {
		t.Fatalf("retry after finalizer completion: %v", err)
	}
}

func TestDestroyWaitsForOwnedPodReleaseAndIgnoresOtherSandboxes(t *testing.T) {
	drv := testDriver(t, false)
	if _, err := drv.Create(t.Context(), driver.Spec{ID: "pod-cleanup", Image: "image"}); err != nil {
		t.Fatal(err)
	}
	pods := drv.cs.CoreV1().Pods("sandboxes")
	for _, pair := range [][2]string{{"owned-pod", "pod-cleanup"}, {"unrelated-pod", "other"}} {
		if _, err := pods.Create(t.Context(), &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: pair[0], Namespace: "sandboxes", Labels: drv.selector(pair[1])}, Status: corev1.PodStatus{Phase: corev1.PodRunning}}, metav1.CreateOptions{}); err != nil {
			t.Fatal(err)
		}
	}
	pending := true
	drv.cs.(*fake.Clientset).PrependReactor("delete", "pods", func(action k8stesting.Action) (bool, runtime.Object, error) {
		if pending && action.(k8stesting.DeleteAction).GetName() == "owned-pod" {
			return true, nil, nil
		}
		return false, nil, nil
	})
	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	if err := drv.Destroy(ctx, "pod-cleanup"); err == nil {
		t.Fatal("reported release while the sandbox Pod still runs")
	}
	pending = false
	if err := drv.Destroy(t.Context(), "pod-cleanup"); err != nil {
		t.Fatalf("unrelated Pod blocked cleanup: %v", err)
	}
	if _, err := pods.Get(t.Context(), "unrelated-pod", metav1.GetOptions{}); err != nil {
		t.Fatalf("cleanup touched unrelated sandbox: %v", err)
	}
}
