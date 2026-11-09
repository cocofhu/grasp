package kubernetes

import (
	"context"
	"errors"
	"reflect"
	"strconv"
	"testing"

	"sandbox-gateway/internal/driver"

	appsv1 "k8s.io/api/apps/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes/fake"
	kt "k8s.io/client-go/testing"
)

// Deployment's real strategy increments generation for annotation changes;
// the controller's later /status acknowledgement increments RV again.
func coldControllerGenerations(t *testing.T, cs *fake.Clientset) {
	t.Helper()
	cs.PrependReactor("update", "deployments", func(a kt.Action) (bool, runtime.Object, error) {
		next := a.(kt.UpdateAction).GetObject().(*appsv1.Deployment)
		obj, err := cs.Tracker().Get(appsv1.SchemeGroupVersion.WithResource("deployments"), "sandboxes", next.Name)
		if err != nil {
			return true, nil, err
		}
		old := obj.(*appsv1.Deployment)
		if !reflect.DeepEqual(next.Spec, old.Spec) || !reflect.DeepEqual(next.Annotations, old.Annotations) {
			next.Generation = old.Generation + 1
		}
		if *next.Spec.Replicas == 1 && old.Status.ObservedGeneration != old.Generation {
			t.Error("start was dispatched before stopped generation was observed")
		}
		return false, nil, nil
	})
}

func TestK8sColdConfigurationAcceptsControllerStatusRVAfterReservedGeneration(t *testing.T) {
	d, cs, opts := coldK8sFixture(t)
	coldControllerGenerations(t, cs)
	reads, controllerWrites := 0, 0
	cs.PrependReactor("get", "deployments", func(a kt.Action) (bool, runtime.Object, error) {
		obj, err := cs.Tracker().Get(appsv1.SchemeGroupVersion.WithResource("deployments"), a.GetNamespace(), a.(kt.GetAction).GetName())
		if err != nil {
			return true, nil, err
		}
		dep := obj.(*appsv1.Deployment).DeepCopy()
		if proof, err := coldProof(dep); err == nil && proof.State == "configuring" {
			reads++
			if reads == 2 {
				dep.Status.ObservedGeneration = dep.Generation
				rv, _ := strconv.Atoi(dep.ResourceVersion)
				dep.ResourceVersion = strconv.Itoa(rv + 1)
				if err := cs.Tracker().Update(appsv1.SchemeGroupVersion.WithResource("deployments"), dep, a.GetNamespace()); err != nil {
					return true, nil, err
				}
				controllerWrites++
			}
		}
		return true, dep, nil
	})
	receipt, err := d.StartConfigured(t.Context(), "original", opts)
	if err != nil || receipt.State != "started" || reads < 2 || controllerWrites != 1 {
		t.Fatalf("controller reconciliation blocked cold start: %+v %v reads=%d statusWrites=%d", receipt, err, reads, controllerWrites)
	}
	dep, _ := cs.AppsV1().Deployments("sandboxes").Get(t.Context(), "sbx-original", metav1.GetOptions{})
	if *dep.Spec.Replicas != 1 || dep.UID != "deployment-original" || dep.ResourceVersion != "4" {
		t.Fatalf("final start did not CAS the fresh observed version: RV=%s replicas=%d", dep.ResourceVersion, *dep.Spec.Replicas)
	}
}

func TestK8sColdConfigurationRejectsSpecABAAfterReservation(t *testing.T) {
	d, cs, opts := coldK8sFixture(t)
	coldControllerGenerations(t, cs)
	changedAndRestored := false
	cs.PrependReactor("get", "deployments", func(a kt.Action) (bool, runtime.Object, error) {
		resource := appsv1.SchemeGroupVersion.WithResource("deployments")
		obj, err := cs.Tracker().Get(resource, a.GetNamespace(), a.(kt.GetAction).GetName())
		if err != nil {
			return true, nil, err
		}
		dep := obj.(*appsv1.Deployment).DeepCopy()
		if proof, err := coldProof(dep); err == nil && proof.State == "configuring" && !changedAndRestored {
			// Model two accepted external spec writes. Even when the original
			// spec is restored and its latest generation observed stopped,
			// this is no longer the reservation that authorized cold start.
			original := dep.Spec.DeepCopy()
			rv, _ := strconv.Atoi(dep.ResourceVersion)
			dep.Spec.Template.Spec.Containers[0].Image = "unreviewed-image"
			dep.Generation++
			dep.ResourceVersion = strconv.Itoa(rv + 1)
			if err := cs.Tracker().Update(resource, dep.DeepCopy(), a.GetNamespace()); err != nil {
				return true, nil, err
			}
			dep.Spec = *original
			dep.Generation++
			dep.ResourceVersion = strconv.Itoa(rv + 2)
			dep.Status.ObservedGeneration = dep.Generation
			if err := cs.Tracker().Update(resource, dep.DeepCopy(), a.GetNamespace()); err != nil {
				return true, nil, err
			}
			changedAndRestored = true
		}
		return true, dep, nil
	})
	if _, err := d.StartConfigured(t.Context(), "original", opts); !errors.Is(err, driver.ErrColdStartConflict) {
		t.Fatal("restored spec authorized a newer generation", err)
	}
	obj, _ := cs.Tracker().Get(appsv1.SchemeGroupVersion.WithResource("deployments"), "sandboxes", "sbx-original")
	dep := obj.(*appsv1.Deployment)
	if !changedAndRestored || dep.Generation != 4 || *dep.Spec.Replicas != 0 || dep.Spec.Template.Spec.Containers[0].Image != "environment:known" {
		t.Fatalf("spec ABA was not fenced before start: changed=%t generation=%d replicas=%d image=%s", changedAndRestored, dep.Generation, *dep.Spec.Replicas, dep.Spec.Template.Spec.Containers[0].Image)
	}
	cs.ClearActions()
	got, err := d.GetColdStart(t.Context(), "original", opts.ColdConfig.RequestID)
	if err != nil || got.State != "unknown" || coldNativeWrites(cs) != 0 {
		t.Fatal("spec ABA receipt lookup replayed the original intent", got, err)
	}
}

func TestK8sColdConfigurationUnobservedReservationExpiresWithoutDispatch(t *testing.T) {
	d, cs, opts := coldK8sFixture(t)
	coldControllerGenerations(t, cs)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	cs.PrependReactor("get", "deployments", func(a kt.Action) (bool, runtime.Object, error) {
		obj, err := cs.Tracker().Get(appsv1.SchemeGroupVersion.WithResource("deployments"), a.GetNamespace(), a.(kt.GetAction).GetName())
		if err == nil {
			if proof, err := coldProof(obj.(*appsv1.Deployment)); err == nil && proof.State == "configuring" {
				cancel()
			}
		}
		return false, nil, nil
	})
	if _, err := d.StartConfigured(ctx, "original", opts); !errors.Is(err, context.Canceled) {
		t.Fatal("unobserved generation became permission to start", err)
	}
	obj, _ := cs.Tracker().Get(appsv1.SchemeGroupVersion.WithResource("deployments"), "sandboxes", "sbx-original")
	if *obj.(*appsv1.Deployment).Spec.Replicas != 0 {
		t.Fatal("unobserved reservation dispatched processes")
	}
	cs.ClearActions()
	got, err := d.GetColdStart(t.Context(), "original", opts.ColdConfig.RequestID)
	if err != nil || got.State != "unknown" || coldNativeWrites(cs) != 0 {
		t.Fatal("unknown GET changed the original reservation", got, err)
	}
}

func TestK8sColdConfigurationFinalStatusRaceFailsCASWithoutStarting(t *testing.T) {
	d, cs, opts := coldK8sFixture(t)
	cs.PrependReactor("update", "deployments", func(a kt.Action) (bool, runtime.Object, error) {
		next := a.(kt.UpdateAction).GetObject().(*appsv1.Deployment)
		if *next.Spec.Replicas == 1 {
			return true, nil, apierrors.NewConflict(schema.GroupResource{Resource: "deployments"}, next.Name, errors.New("controller advanced after final read"))
		}
		return false, nil, nil
	})
	if _, err := d.StartConfigured(t.Context(), "original", opts); !apierrors.IsConflict(err) {
		t.Fatal("final concurrent update was ignored", err)
	}
	obj, _ := cs.Tracker().Get(appsv1.SchemeGroupVersion.WithResource("deployments"), "sandboxes", "sbx-original")
	if *obj.(*appsv1.Deployment).Spec.Replicas != 0 {
		t.Fatal("failed CAS started the original workload")
	}
	cs.ClearActions()
	got, err := d.GetColdStart(t.Context(), "original", opts.ColdConfig.RequestID)
	if err != nil || got.State != "unknown" || coldNativeWrites(cs) != 0 {
		t.Fatal("CAS failure was replayed or hidden", got, err)
	}
	if _, err := d.StartConfigured(t.Context(), "original", opts); !errors.Is(err, driver.ErrColdStartUnknown) || coldNativeWrites(cs) != 0 {
		t.Fatal("unknown start was dispatched again", err)
	}
}
