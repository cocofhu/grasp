package kubernetes

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"
	kt "k8s.io/client-go/testing"
	"sandbox-gateway/internal/driver"
)

func coldK8sFixture(t *testing.T) (*Driver, *fake.Clientset, driver.StartOptions) {
	t.Helper()
	ctx := context.Background()
	cs := fake.NewClientset()
	d := NewFromClient(cs, Options{})
	if _, err := d.Create(ctx, driver.Spec{ID: "original", Image: "environment:known", WorkspaceDir: "/root/workspace/retained"}); err != nil {
		t.Fatal(err)
	}
	dep, _ := cs.AppsV1().Deployments("sandboxes").Get(ctx, "sbx-original", metav1.GetOptions{})
	zero := int32(0)
	dep.UID = "deployment-original"
	dep.ResourceVersion = "1"
	dep.Generation = 1
	dep.Spec.Replicas = &zero
	dep.Status.ObservedGeneration = 1
	dep.Spec.Template.Spec.Containers[0].Env = append(dep.Spec.Template.Spec.Containers[0].Env, corev1.EnvVar{Name: "REVOKED_MODEL_TOKEN", Value: "synthetic-revoked"})
	if _, err := cs.AppsV1().Deployments("sandboxes").Update(ctx, dep, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	secret, _ := cs.CoreV1().Secrets("sandboxes").Get(ctx, "sbx-original-env", metav1.GetOptions{})
	secret.UID = "secret-original"
	secret.ResourceVersion = "1"
	secret.StringData = nil
	secret.Data = map[string][]byte{"REVOKED_TOKEN": []byte("synthetic-revoked"), "GRASP_RUNTIME_URL": []byte("http://expired/runtime")}
	if _, err := cs.CoreV1().Secrets("sandboxes").Update(ctx, secret, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	pvc, _ := cs.CoreV1().PersistentVolumeClaims("sandboxes").Get(ctx, "sbx-original-data", metav1.GetOptions{})
	if pvc == nil { // The default resource helper uses the configured name.
		pvcs, _ := cs.CoreV1().PersistentVolumeClaims("sandboxes").List(ctx, metav1.ListOptions{})
		if len(pvcs.Items) != 1 {
			t.Fatal("original PVC missing")
		}
		pvc = &pvcs.Items[0]
	}
	pvc.UID = "pvc-original"
	pvc.ResourceVersion = "1"
	pvc.Status.Phase = corev1.ClaimBound
	if _, err := cs.CoreV1().PersistentVolumeClaims("sandboxes").Update(ctx, pvc, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	// client-go's object tracker does not implement resourceVersion conflicts.
	// Model the real apiserver CAS contract explicitly rather than accepting
	// stale writes in the tests that are meant to protect concurrency.
	for _, resource := range []string{"deployments", "secrets"} {
		cs.PrependReactor("update", resource, func(action kt.Action) (bool, runtime.Object, error) {
			a := action.(kt.UpdateAction)
			object := a.GetObject()
			meta := object.(metav1.Object)
			old, err := cs.Tracker().Get(action.GetResource(), action.GetNamespace(), meta.GetName())
			if err != nil {
				return true, nil, err
			}
			before := old.(metav1.Object)
			if meta.GetResourceVersion() != before.GetResourceVersion() || meta.GetUID() != before.GetUID() {
				return true, nil, apierrors.NewConflict(schema.GroupResource{Resource: action.GetResource().Resource}, meta.GetName(), errors.New("stale version"))
			}
			next, _ := strconv.Atoi(meta.GetResourceVersion())
			meta.SetResourceVersion(strconv.Itoa(next + 1))
			if err := cs.Tracker().Update(action.GetResource(), object, action.GetNamespace()); err != nil {
				return true, nil, err
			}
			return true, object.DeepCopyObject(), nil
		})
	}
	env := map[string]string{"ROOT_PASSWORD": "synthetic-root-current", "ACP_BRIDGE_PASSWORD": "synthetic-bridge-current", "WORKSPACE_DIR": "/root/workspace/retained", "CONFIG_ROOT": "/root/.config/opencode", "GRASP_EXECUTION_LINEAGE": "lineage-current", "GRASP_SANDBOX_RESOURCE_ID": "original", "GRASP_WORKSPACE_CONTROL_ROOT": "/tmp/grasp-private-workspace/original/control", "GRASP_RUNTIME_URL": "http://platform/runtime", "SANDBOX_INJECT_HEADERS": "Authorization: Bearer synthetic-current", "SANDBOX_INJECT": "", "GIT_REPOS": "", "MODEL_TOKEN": "synthetic-current"}
	c := &driver.ColdStartConfig{ExpectedNativeIdentity: "deployment-original", RequestID: "review-original-cold", InputSHA256: strings.Repeat("a", 64), Env: env, Injections: []driver.ColdConfigInjection{{URL: "http://platform/git.tgz", Destination: "/tmp/grasp-ssh-inject", SHA256: strings.Repeat("b", 64), Bytes: 1024}, {URL: "http://platform/config.tgz", Destination: env["CONFIG_ROOT"], SHA256: strings.Repeat("c", 64), Bytes: 2048}}}
	opts := driver.StartOptions{BootstrapScript: "#!/bin/bash\ntrue\n", RuntimeURL: env["GRASP_RUNTIME_URL"], RuntimeHeaders: env["SANDBOX_INJECT_HEADERS"], ColdConfig: c}
	c.PayloadSHA256 = driver.ColdStartPayloadSHA256(opts)
	cs.ClearActions()
	return d, cs, opts
}

func coldNativeWrites(cs *fake.Clientset) int {
	n := 0
	for _, a := range cs.Actions() {
		if a.GetVerb() != "get" && a.GetVerb() != "list" {
			n++
		}
	}
	return n
}

func coldObserveReady(t *testing.T, cs *fake.Clientset) {
	t.Helper()
	resource := appsv1.SchemeGroupVersion.WithResource("deployments")
	obj, err := cs.Tracker().Get(resource, "sandboxes", "sbx-original")
	if err != nil {
		t.Fatal(err)
	}
	dep := obj.(*appsv1.Deployment).DeepCopy()
	dep.Status = appsv1.DeploymentStatus{ObservedGeneration: dep.Generation, Replicas: 1, ReadyReplicas: 1, AvailableReplicas: 1}
	if err := cs.Tracker().Update(resource, dep, "sandboxes"); err != nil {
		t.Fatal(err)
	}
}

func TestK8sColdConfigurationPreservesResourceAndReplacesAllAuthorizationBeforeStart(t *testing.T) {
	d, cs, opts := coldK8sFixture(t)
	ctx := context.Background()
	target, err := d.PreviewColdStart(ctx, "original")
	if err != nil || target.NativeIdentity != "deployment-original" || target.WorkspaceDir != "/root/workspace/retained" || coldNativeWrites(cs) != 0 {
		t.Fatalf("preview changed original target: %+v %v", target, err)
	}
	cs.PrependReactor("update", "secrets", func(a kt.Action) (bool, runtime.Object, error) {
		obj, _ := cs.Tracker().Get(appsv1.SchemeGroupVersion.WithResource("deployments"), "sandboxes", "sbx-original")
		dep := obj.(*appsv1.Deployment)
		if *dep.Spec.Replicas != 0 || dep.Annotations[coldAnnotation] == "" {
			t.Error("authorization written before the original workload was fenced cold")
		}
		return false, nil, nil
	})
	receipt, err := d.StartConfigured(ctx, "original", opts)
	if err != nil || receipt.State != "started" || receipt.NativeIdentity != "deployment-original" {
		t.Fatalf("cold start: %+v %v", receipt, err)
	}
	secret, _ := cs.CoreV1().Secrets("sandboxes").Get(ctx, "sbx-original-env", metav1.GetOptions{})
	if secret.UID != "secret-original" || string(secret.Data["MODEL_TOKEN"]) != "synthetic-current" || secret.Data["REVOKED_TOKEN"] != nil || len(secret.StringData) != 0 {
		t.Fatal("old authorization survived full Secret replacement")
	}
	dep, _ := cs.AppsV1().Deployments("sandboxes").Get(ctx, "sbx-original", metav1.GetOptions{})
	c := dep.Spec.Template.Spec.Containers[0]
	if dep.UID != "deployment-original" || *dep.Spec.Replicas != 1 || len(c.Env) != 0 || len(c.EnvFrom) != 1 || c.EnvFrom[0].SecretRef.Name != "sbx-original-env" || len(c.Command) != 5 || c.Command[4] != "fetch" {
		t.Fatal("cold startup replaced the resource or retained old inline env")
	}
	pvc, _ := cs.CoreV1().PersistentVolumeClaims("sandboxes").Get(ctx, d.pvcName("original"), metav1.GetOptions{})
	if pvc.UID != "pvc-original" {
		t.Fatal("original writable disk replaced")
	}
	cs.ClearActions()
	got, err := d.GetColdStart(ctx, "original", opts.ColdConfig.RequestID)
	if err != nil || got.State != "unknown" || coldNativeWrites(cs) != 0 {
		t.Fatalf("accepted but not running was represented as ready: %+v %v", got, err)
	}
	coldObserveReady(t, cs)
	got, err = d.GetColdStart(ctx, "original", opts.ColdConfig.RequestID)
	if err != nil || *got != *receipt || coldNativeWrites(cs) != 0 {
		t.Fatalf("receipt lookup changed provider: %+v %v", got, err)
	}
	if _, err := d.StartConfigured(ctx, "original", opts); !errors.Is(err, driver.ErrColdStartUnknown) {
		t.Fatalf("repeated native dispatch accepted: %v", err)
	}
	if coldNativeWrites(cs) != 0 {
		t.Fatal("same native request was replayed")
	}
	obj, _ := cs.Tracker().Get(appsv1.SchemeGroupVersion.WithResource("deployments"), "sandboxes", "sbx-original")
	stopped := obj.(*appsv1.Deployment).DeepCopy()
	zero := int32(0)
	stopped.Spec.Replicas = &zero
	stopped.Status = appsv1.DeploymentStatus{ObservedGeneration: stopped.Generation}
	if err := cs.Tracker().Update(appsv1.SchemeGroupVersion.WithResource("deployments"), stopped, "sandboxes"); err != nil {
		t.Fatal(err)
	}
	got, err = d.GetColdStart(ctx, "original", opts.ColdConfig.RequestID)
	if err != nil || got.State != "unknown" || coldNativeWrites(cs) != 0 {
		t.Fatalf("later stop caused replay or false running proof: %+v %v", got, err)
	}
}

func TestK8sColdConfigurationRefusesUnsafeOriginalStateWithoutWrites(t *testing.T) {
	for _, name := range []string{"live-pod", "unobserved", "foreign-secret", "wrong-root", "foreign-native", "custom-command", "extra-mount", "bad-payload"} {
		t.Run(name, func(t *testing.T) {
			d, cs, opts := coldK8sFixture(t)
			ctx := context.Background()
			dep, _ := cs.AppsV1().Deployments("sandboxes").Get(ctx, "sbx-original", metav1.GetOptions{})
			switch name {
			case "live-pod":
				_, _ = cs.CoreV1().Pods("sandboxes").Create(ctx, &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "old-process", Labels: d.selector("original")}, Status: corev1.PodStatus{Phase: corev1.PodRunning}}, metav1.CreateOptions{})
			case "unobserved":
				dep.Status.ObservedGeneration = 0
				_, _ = cs.AppsV1().Deployments("sandboxes").Update(ctx, dep, metav1.UpdateOptions{})
			case "foreign-secret":
				secret, _ := cs.CoreV1().Secrets("sandboxes").Get(ctx, "sbx-original-env", metav1.GetOptions{})
				secret.Labels = map[string]string{}
				_, _ = cs.CoreV1().Secrets("sandboxes").Update(ctx, secret, metav1.UpdateOptions{})
			case "wrong-root":
				opts.ColdConfig.Env["WORKSPACE_DIR"] = "/root/workspace/other"
			case "foreign-native":
				opts.ColdConfig.ExpectedNativeIdentity = "foreign-deployment"
			case "custom-command":
				dep.Spec.Template.Spec.Containers[0].Command = []string{"unsafe-other-entrypoint"}
				_, _ = cs.AppsV1().Deployments("sandboxes").Update(ctx, dep, metav1.UpdateOptions{})
			case "extra-mount":
				dep.Spec.Template.Spec.Containers[0].VolumeMounts = append(dep.Spec.Template.Spec.Containers[0].VolumeMounts, corev1.VolumeMount{Name: "data", MountPath: "/root/.config", SubPath: "old-authorization"})
				_, _ = cs.AppsV1().Deployments("sandboxes").Update(ctx, dep, metav1.UpdateOptions{})
			}
			opts.ColdConfig.PayloadSHA256 = driver.ColdStartPayloadSHA256(opts)
			if name == "bad-payload" {
				opts.ColdConfig.PayloadSHA256 = strings.Repeat("f", 64)
			}
			cs.ClearActions()
			if _, err := d.StartConfigured(ctx, "original", opts); err == nil {
				t.Fatal("unsafe cold configuration accepted")
			}
			if coldNativeWrites(cs) != 0 {
				t.Fatal("rejected cold operation changed original provider resources")
			}
		})
	}
}

func TestK8sColdConfigurationConcurrentChangePreservesStoppedUnknownAndNeverReplays(t *testing.T) {
	for _, fault := range []string{"secret-cas", "parent-change", "late-pod", "start-ack-loss"} {
		t.Run(fault, func(t *testing.T) {
			d, cs, opts := coldK8sFixture(t)
			ctx := context.Background()
			dispatched := 0
			cs.PrependReactor("update", "deployments", func(a kt.Action) (bool, runtime.Object, error) {
				dep := a.(kt.UpdateAction).GetObject().(*appsv1.Deployment)
				if *dep.Spec.Replicas != 1 {
					return false, nil, nil
				}
				dispatched++
				if fault == "start-ack-loss" {
					_ = cs.Tracker().Update(appsv1.SchemeGroupVersion.WithResource("deployments"), dep, "sandboxes")
					return true, nil, errors.New("response lost after apiserver accepted start")
				}
				return false, nil, nil
			})
			cs.PrependReactor("update", "secrets", func(a kt.Action) (bool, runtime.Object, error) {
				switch fault {
				case "secret-cas":
					return true, nil, apierrors.NewConflict(schema.GroupResource{Resource: "secrets"}, "sbx-original-env", errors.New("concurrent secret update"))
				case "parent-change":
					obj, _ := cs.Tracker().Get(appsv1.SchemeGroupVersion.WithResource("deployments"), "sandboxes", "sbx-original")
					dep := obj.(*appsv1.Deployment).DeepCopy()
					dep.ResourceVersion = "99"
					dep.Spec.Template.Spec.Containers[0].Image = "unreviewed-image"
					_ = cs.Tracker().Update(appsv1.SchemeGroupVersion.WithResource("deployments"), dep, "sandboxes")
				case "late-pod":
					_ = cs.Tracker().Add(&corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "late-original-process", Namespace: "sandboxes", Labels: d.selector("original")}, Status: corev1.PodStatus{Phase: corev1.PodRunning}})
				}
				return false, nil, nil
			})
			if _, err := d.StartConfigured(ctx, "original", opts); err == nil {
				t.Fatal("injected provider ambiguity was hidden")
			}
			if fault == "start-ack-loss" {
				coldObserveReady(t, cs)
			}
			cs.ClearActions()
			got, err := d.GetColdStart(ctx, "original", opts.ColdConfig.RequestID)
			if err != nil {
				t.Fatal(err)
			}
			if fault == "start-ack-loss" {
				if got.State != "started" || dispatched != 1 {
					t.Fatalf("accepted native receipt lost: %+v %d", got, dispatched)
				}
			} else if got.State != "unknown" || dispatched != 0 {
				t.Fatalf("failed preparation was started: %+v %d", got, dispatched)
			}
			if err := d.Start(ctx, "original"); fault != "start-ack-loss" && !errors.Is(err, driver.ErrColdStartUnknown) {
				t.Fatalf("ordinary Start bypassed pending authorization: %v", err)
			}
			if fault != "start-ack-loss" && coldNativeWrites(cs) != 0 {
				t.Fatal("unknown configuration replayed or started the provider")
			}
		})
	}
}

func TestK8sColdReceiptRejectsReplacedNativeIdentityAndChangedAuthorization(t *testing.T) {
	for _, changed := range []string{"deployment", "secret", "pvc", "env", "command"} {
		t.Run(changed, func(t *testing.T) {
			d, cs, opts := coldK8sFixture(t)
			ctx := context.Background()
			if _, err := d.StartConfigured(ctx, "original", opts); err != nil {
				t.Fatal(err)
			}
			switch changed {
			case "deployment", "command":
				obj, _ := cs.Tracker().Get(appsv1.SchemeGroupVersion.WithResource("deployments"), "sandboxes", "sbx-original")
				dep := obj.(*appsv1.Deployment).DeepCopy()
				if changed == "deployment" {
					dep.UID = types.UID("replaced")
				} else {
					dep.Spec.Template.Spec.Containers[0].Command[2] = "exit 0"
				}
				_ = cs.Tracker().Update(appsv1.SchemeGroupVersion.WithResource("deployments"), dep, "sandboxes")
			case "secret", "env":
				secret, _ := cs.CoreV1().Secrets("sandboxes").Get(ctx, "sbx-original-env", metav1.GetOptions{})
				if changed == "secret" {
					secret.UID = "replaced"
				} else {
					secret.Data["MODEL_TOKEN"] = []byte("unreviewed")
				}
				_ = cs.Tracker().Update(corev1.SchemeGroupVersion.WithResource("secrets"), secret, "sandboxes")
			case "pvc":
				pvc, _ := cs.CoreV1().PersistentVolumeClaims("sandboxes").Get(ctx, d.pvcName("original"), metav1.GetOptions{})
				pvc.UID = "replaced"
				_ = cs.Tracker().Update(corev1.SchemeGroupVersion.WithResource("persistentvolumeclaims"), pvc, "sandboxes")
			}
			cs.ClearActions()
			if _, err := d.GetColdStart(ctx, "original", opts.ColdConfig.RequestID); err == nil {
				t.Fatal("foreign native/config proof accepted")
			}
			if coldNativeWrites(cs) != 0 {
				t.Fatal("receipt verification mutated provider")
			}
		})
	}
}

func TestK8sColdConfigurationReadFailuresNeverBecomePermissionToStart(t *testing.T) {
	for _, point := range []string{"deployment-first", "secret-first", "pvc-first", "deployment-after-publish", "secret-after-publish", "pvc-after-publish"} {
		t.Run(point, func(t *testing.T) {
			d, cs, opts := coldK8sFixture(t)
			ctx := context.Background()
			resource := "deployments"
			if strings.HasPrefix(point, "secret") {
				resource = "secrets"
			}
			if strings.HasPrefix(point, "pvc") {
				resource = "persistentvolumeclaims"
			}
			at := 1
			if strings.HasSuffix(point, "after-publish") {
				at = 2
			}
			reads := 0
			cs.PrependReactor("get", resource, func(kt.Action) (bool, runtime.Object, error) {
				reads++
				if reads == at {
					return true, nil, errors.New("provider observation unavailable")
				}
				return false, nil, nil
			})
			if _, err := d.StartConfigured(ctx, "original", opts); err == nil {
				t.Fatal("unreadable provider state was accepted")
			}
			obj, _ := cs.Tracker().Get(appsv1.SchemeGroupVersion.WithResource("deployments"), "sandboxes", "sbx-original")
			if *obj.(*appsv1.Deployment).Spec.Replicas != 0 {
				t.Fatal("failed observation started original processes")
			}
		})
	}
}
