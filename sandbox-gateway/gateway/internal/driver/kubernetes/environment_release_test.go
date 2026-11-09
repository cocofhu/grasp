package kubernetes

import (
	"context"
	"errors"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
	"sandbox-gateway/internal/driver"
)

func releaseFixture(t *testing.T) (*Driver, driver.EnvironmentReleasePlan) {
	t.Helper()
	d := testDriver(t, false)
	ctx := context.Background()
	zero := int32(0)
	objects := []runtime.Object{
		&appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "sbx-release", Namespace: "sandboxes", UID: "dep-original", Labels: d.selector("release"), Generation: 1}, Spec: appsv1.DeploymentSpec{Replicas: &zero}, Status: appsv1.DeploymentStatus{ObservedGeneration: 1}},
		&corev1.PersistentVolumeClaim{ObjectMeta: metav1.ObjectMeta{Name: "sbx-release-data", Namespace: "sandboxes", UID: "pvc-original", Labels: d.selector("release")}, Spec: corev1.PersistentVolumeClaimSpec{VolumeName: "pv-release"}, Status: corev1.PersistentVolumeClaimStatus{Phase: corev1.ClaimBound}},
		&corev1.PersistentVolume{ObjectMeta: metav1.ObjectMeta{Name: "pv-release", UID: "pv-original"}, Spec: corev1.PersistentVolumeSpec{PersistentVolumeReclaimPolicy: corev1.PersistentVolumeReclaimDelete, ClaimRef: &corev1.ObjectReference{Namespace: "sandboxes", Name: "sbx-release-data", UID: "pvc-original"}}},
		&corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: "sbx-release", Namespace: "sandboxes", UID: "service-original", Labels: d.selector("release")}},
		&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "sbx-release-env", Namespace: "sandboxes", UID: "secret-original", Labels: d.selector("release")}, Data: map[string][]byte{"secret": []byte("must-never-enter-plan")}},
	}
	client := d.cs.(*fake.Clientset)
	for _, object := range objects {
		if err := client.Tracker().Add(object); err != nil {
			t.Fatal(err)
		}
	}
	p, err := d.PreviewEnvironmentRelease(ctx, "release")
	if err != nil {
		t.Fatal(err)
	}
	return d, p
}
func TestEnvironmentReleaseUIDProofAndVolumeReclamation(t *testing.T) {
	d, p := releaseFixture(t)
	ctx := context.Background()
	if !p.Valid() || len(p.Objects) != 5 {
		t.Fatalf("invalid frozen scope: %+v", p)
	}
	client := d.cs.(*fake.Clientset)
	deleted := 0
	client.PrependReactor("delete", "*", func(a k8stesting.Action) (bool, runtime.Object, error) {
		opts := a.(k8stesting.DeleteAction).GetDeleteOptions()
		if opts.Preconditions == nil || opts.Preconditions.UID == nil || *opts.Preconditions.UID == "" {
			t.Fatal("native deletion lacked UID precondition")
		}
		if a.GetResource().Resource == "persistentvolumes" {
			t.Fatal("PV must be reclaimed by its PVC policy")
		}
		deleted++
		return false, nil, nil
	})
	if err := d.DeleteEnvironmentRelease(ctx, p, func(context.Context) error { return nil }); err != nil {
		t.Fatal(err)
	}
	if deleted != 4 {
		t.Fatalf("native deletes=%d", deleted)
	}
	if gone, err := d.EnvironmentReleaseGone(ctx, p); err != nil || gone {
		t.Fatalf("PVC deletion did not prove PV reclamation: %v %v", gone, err)
	}
	if err := client.Tracker().Delete(corev1.SchemeGroupVersion.WithResource("persistentvolumes"), "", "pv-release"); err != nil {
		t.Fatal(err)
	}
	if gone, err := d.EnvironmentReleaseGone(ctx, p); err != nil || !gone {
		t.Fatalf("complete resource proof: %v %v", gone, err)
	}
	if deleted != 4 {
		t.Fatal("read-only reconciliation replayed deletion")
	}
}
func TestEnvironmentReleaseRefusesUnknownRunningAndRetainVolumes(t *testing.T) {
	for _, kind := range []string{"running", "terminating-pod", "retained-pv", "foreign-pv", "unbound", "read-failed", "changed-uid", "foreign-service"} {
		t.Run(kind, func(t *testing.T) {
			d, p := releaseFixture(t)
			ctx := context.Background()
			client := d.cs.(*fake.Clientset)
			switch kind {
			case "running":
				dep, _ := d.cs.AppsV1().Deployments("sandboxes").Get(ctx, "sbx-release", metav1.GetOptions{})
				one := int32(1)
				dep.Spec.Replicas = &one
				_, _ = d.cs.AppsV1().Deployments("sandboxes").Update(ctx, dep, metav1.UpdateOptions{})
			case "terminating-pod":
				_, _ = d.cs.CoreV1().Pods("sandboxes").Create(ctx, &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "sbx-release-running", Namespace: "sandboxes", Labels: d.selector("release")}, Status: corev1.PodStatus{Phase: corev1.PodRunning}}, metav1.CreateOptions{})
			case "retained-pv", "foreign-pv":
				pv, _ := d.cs.CoreV1().PersistentVolumes().Get(ctx, "pv-release", metav1.GetOptions{})
				if kind == "retained-pv" {
					pv.Spec.PersistentVolumeReclaimPolicy = corev1.PersistentVolumeReclaimRetain
				} else {
					pv.Spec.ClaimRef.UID = "foreign-claim"
				}
				_, _ = d.cs.CoreV1().PersistentVolumes().Update(ctx, pv, metav1.UpdateOptions{})
			case "unbound":
				v, _ := d.cs.CoreV1().PersistentVolumeClaims("sandboxes").Get(ctx, "sbx-release-data", metav1.GetOptions{})
				v.Spec.VolumeName = ""
				_, _ = d.cs.CoreV1().PersistentVolumeClaims("sandboxes").Update(ctx, v, metav1.UpdateOptions{})
			case "read-failed":
				client.PrependReactor("get", "persistentvolumes", func(k8stesting.Action) (bool, runtime.Object, error) {
					return true, nil, errors.New("apiserver unavailable")
				})
			case "changed-uid":
				v, _ := d.cs.CoreV1().PersistentVolumeClaims("sandboxes").Get(ctx, "sbx-release-data", metav1.GetOptions{})
				v.UID = "new-incarnation"
				_, _ = d.cs.CoreV1().PersistentVolumeClaims("sandboxes").Update(ctx, v, metav1.UpdateOptions{})
			case "foreign-service":
				v, _ := d.cs.CoreV1().Services("sandboxes").Get(ctx, "sbx-release", metav1.GetOptions{})
				v.Labels = nil
				_, _ = d.cs.CoreV1().Services("sandboxes").Update(ctx, v, metav1.UpdateOptions{})
			}
			if err := d.CheckEnvironmentRelease(ctx, p); err == nil {
				t.Fatal("changed or unknown scope accepted")
			}
			count := 0
			client.PrependReactor("delete", "*", func(k8stesting.Action) (bool, runtime.Object, error) { count++; return false, nil, nil })
			if err := d.DeleteEnvironmentRelease(ctx, p, func(context.Context) error { return nil }); err == nil || count != 0 {
				t.Fatalf("scope mutated after rejection: %d %v", count, err)
			}
		})
	}
}
func TestEnvironmentReleaseLossStopsFurtherMutationsAndNeverDeletesReplacement(t *testing.T) {
	d, p := releaseFixture(t)
	client := d.cs.(*fake.Clientset)
	ctx := context.Background()
	calls := 0
	lost := errors.New("owner lost")
	if err := d.DeleteEnvironmentRelease(ctx, p, func(context.Context) error {
		calls++
		if calls == 3 {
			return lost
		}
		return nil
	}); !errors.Is(err, lost) {
		t.Fatalf("loss was hidden: %v", err)
	}
	// A replacement with the same name must never count as successful release.
	replacement := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "sbx-release", Namespace: "sandboxes", UID: "new-deployment", Labels: d.selector("release")}}
	if _, err := client.AppsV1().Deployments("sandboxes").Create(ctx, replacement, metav1.CreateOptions{}); err != nil {
		t.Fatal(err)
	}
	if gone, err := d.EnvironmentReleaseGone(ctx, p); gone || !errors.Is(err, driver.ErrEnvironmentReleaseConflict) {
		t.Fatalf("replacement was mistaken for deleted scope: %v %v", gone, err)
	}
}
func TestEnvironmentReleaseDeleteUIDRaceAndReadOnlyUnknown(t *testing.T) {
	d, p := releaseFixture(t)
	client := d.cs.(*fake.Clientset)
	ctx := context.Background()
	deletes := 0
	client.PrependReactor("delete", "deployments", func(a k8stesting.Action) (bool, runtime.Object, error) {
		deletes++
		if uid := a.(k8stesting.DeleteAction).GetDeleteOptions().Preconditions.UID; uid == nil || *uid != types.UID("dep-original") {
			t.Fatal("wrong original UID")
		}
		return true, nil, apierrors.NewConflict(schema.GroupResource{Group: "apps", Resource: "deployments"}, "sbx-release", errors.New("UID changed"))
	})
	if err := d.DeleteEnvironmentRelease(ctx, p, func(context.Context) error { return nil }); !errors.Is(err, driver.ErrEnvironmentReleaseConflict) {
		t.Fatalf("native UID race hidden: %v", err)
	}
	for range 3 {
		gone, err := d.EnvironmentReleaseGone(ctx, p)
		if gone || err != nil {
			t.Fatalf("unknown must remain pending: %v %v", gone, err)
		}
	}
	if deletes != 1 {
		t.Fatalf("unknown read replayed native delete: %d", deletes)
	}
}

func TestEnvironmentReleaseMaterializedReplicaSetsAndCompletedPodsAreInScope(t *testing.T) {
	d, _ := releaseFixture(t)
	ctx := t.Context()
	client := d.cs.(*fake.Clientset)
	zero := int32(0)
	controller := true
	rs := &appsv1.ReplicaSet{ObjectMeta: metav1.ObjectMeta{Name: "sbx-release-rs", Namespace: "sandboxes", UID: "rs-original", Labels: d.selector("release"), OwnerReferences: []metav1.OwnerReference{{Kind: "Deployment", UID: "dep-original", Controller: &controller}}}, Spec: appsv1.ReplicaSetSpec{Replicas: &zero}}
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "sbx-release-rs-done", Namespace: "sandboxes", UID: "pod-original", Labels: d.selector("release"), OwnerReferences: []metav1.OwnerReference{{Kind: "ReplicaSet", UID: rs.UID, Controller: &controller}}}, Status: corev1.PodStatus{Phase: corev1.PodSucceeded}}
	for _, o := range []runtime.Object{rs, pod, &corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: "sbx-release-lb", Namespace: "sandboxes", UID: "lb-original", Labels: d.selector("release")}}} {
		if err := client.Tracker().Add(o); err != nil {
			t.Fatal(err)
		}
	}
	plan, err := d.PreviewEnvironmentRelease(ctx, "release")
	if err != nil || len(plan.Objects) != 8 {
		t.Fatal(plan, err)
	}
	if err := d.DeleteEnvironmentRelease(ctx, plan, func(context.Context) error { return nil }); err != nil {
		t.Fatal(err)
	}
	if err := client.Tracker().Delete(corev1.SchemeGroupVersion.WithResource("persistentvolumes"), "", "pv-release"); err != nil {
		t.Fatal(err)
	}
	if gone, err := d.EnvironmentReleaseGone(ctx, plan); err != nil || !gone {
		t.Fatal(gone, err)
	}
}
func TestEnvironmentReleaseRejectsUnownedControllersAndReadFailures(t *testing.T) {
	for _, kind := range []string{"unowned-rs", "unowned-pod", "running-rs", "foreign-deployment", "missing-deployment", "missing-claim", "missing-pv", "secret-owner", "get-service-error", "get-secret-error", "list-rs-error", "list-pod-error"} {
		t.Run(kind, func(t *testing.T) {
			d, _ := releaseFixture(t)
			client := d.cs.(*fake.Clientset)
			ctx := t.Context()
			zero := int32(0)
			switch kind {
			case "unowned-rs", "running-rs":
				rs := &appsv1.ReplicaSet{ObjectMeta: metav1.ObjectMeta{Name: "sbx-release-rs", Namespace: "sandboxes", UID: "rs", Labels: d.selector("release")}, Spec: appsv1.ReplicaSetSpec{Replicas: &zero}}
				if kind == "running-rs" {
					one := int32(1)
					rs.Spec.Replicas = &one
				}
				_ = client.Tracker().Add(rs)
			case "unowned-pod":
				_ = client.Tracker().Add(&corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "sbx-release-pod", Namespace: "sandboxes", UID: "pod", Labels: d.selector("release")}, Status: corev1.PodStatus{Phase: corev1.PodSucceeded}})
			case "foreign-deployment":
				v, _ := client.AppsV1().Deployments("sandboxes").Get(ctx, "sbx-release", metav1.GetOptions{})
				v.Labels = nil
				_, _ = client.AppsV1().Deployments("sandboxes").Update(ctx, v, metav1.UpdateOptions{})
			case "missing-deployment":
				_ = client.Tracker().Delete(appsv1.SchemeGroupVersion.WithResource("deployments"), "sandboxes", "sbx-release")
			case "missing-claim":
				_ = client.Tracker().Delete(corev1.SchemeGroupVersion.WithResource("persistentvolumeclaims"), "sandboxes", "sbx-release-data")
			case "missing-pv":
				_ = client.Tracker().Delete(corev1.SchemeGroupVersion.WithResource("persistentvolumes"), "", "pv-release")
			case "secret-owner":
				v, _ := client.CoreV1().Secrets("sandboxes").Get(ctx, "sbx-release-env", metav1.GetOptions{})
				v.Labels = nil
				_, _ = client.CoreV1().Secrets("sandboxes").Update(ctx, v, metav1.UpdateOptions{})
			case "get-service-error", "get-secret-error":
				resource := "services"
				if kind == "get-secret-error" {
					resource = "secrets"
				}
				client.PrependReactor("get", resource, func(k8stesting.Action) (bool, runtime.Object, error) {
					return true, nil, errors.New("temporary read failure")
				})
			case "list-rs-error", "list-pod-error":
				resource := "replicasets"
				if kind == "list-pod-error" {
					resource = "pods"
				}
				client.PrependReactor("list", resource, func(k8stesting.Action) (bool, runtime.Object, error) {
					return true, nil, errors.New("temporary list failure")
				})
			}
			if _, err := d.PreviewEnvironmentRelease(ctx, "release"); err == nil {
				t.Fatal("unknown/unowned scope accepted")
			}
			for _, a := range client.Actions() {
				if a.GetVerb() == "delete" {
					t.Fatal("preview mutated resource")
				}
			}
		})
	}
}
func TestEnvironmentReleaseRejectsForgedPlanAndCannotHideReadErrors(t *testing.T) {
	d, plan := releaseFixture(t)
	ctx := t.Context()
	if err := d.DeleteEnvironmentRelease(ctx, plan, nil); err == nil {
		t.Fatal("optional physical owner guard")
	}
	for _, mutate := range []func(*driver.EnvironmentReleasePlan){func(p *driver.EnvironmentReleasePlan) { p.Resource = "other" }, func(p *driver.EnvironmentReleasePlan) { p.Namespace = "foreign" }, func(p *driver.EnvironmentReleasePlan) { p.Objects[0].Name = "foreign-deployment" }, func(p *driver.EnvironmentReleasePlan) { p.Objects[1].Name = "foreign-pvc" }, func(p *driver.EnvironmentReleasePlan) { p.Objects[3].Name = "foreign-service" }, func(p *driver.EnvironmentReleasePlan) { p.Objects[4].Name = "foreign-secret" }} {
		p := plan
		p.Objects = append([]driver.EnvironmentReleaseObject(nil), plan.Objects...)
		mutate(&p)
		p.SHA256 = p.Digest()
		if err := d.CheckEnvironmentRelease(ctx, p); err == nil {
			t.Fatal("forged deletion scope accepted")
		}
		if gone, err := d.EnvironmentReleaseGone(ctx, p); err == nil || gone {
			t.Fatal("forged scope treated as deleted")
		}
	}
	client := d.cs.(*fake.Clientset)
	client.PrependReactor("get", "deployments", func(k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, errors.New("API temporarily unavailable")
	})
	if gone, err := d.EnvironmentReleaseGone(ctx, plan); err == nil || gone {
		t.Fatal("read failure became deletion proof")
	}
}
