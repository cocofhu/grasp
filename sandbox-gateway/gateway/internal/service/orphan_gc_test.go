package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"sandbox-gateway/internal/config"
	"sandbox-gateway/internal/database"
	k8sdriver "sandbox-gateway/internal/driver/kubernetes"
	"sandbox-gateway/internal/models"
	"sandbox-gateway/internal/store"

	"gorm.io/gorm"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	k8sfake "k8s.io/client-go/kubernetes/fake"
	k8sscheme "k8s.io/client-go/kubernetes/scheme"
	k8stesting "k8s.io/client-go/testing"
)

func orphanGCTestService(t *testing.T) (*SandboxService, *k8sfake.Clientset, *gorm.DB) {
	t.Helper()
	db, err := database.Open(config.DBConfig{Driver: "sqlite", Path: "file:" + t.Name() + "?mode=memory&cache=shared"})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	client := k8sfake.NewSimpleClientset()
	drv := k8sdriver.NewFromClient(client, k8sdriver.Options{Namespace: "sandboxes"})
	return New(drv, store.New(db), Config{OrphanGCMinAge: 10 * time.Minute}), client, db
}

func orphanGCObjects(id string, created time.Time) []runtime.Object {
	meta := func(suffix string) metav1.ObjectMeta {
		return metav1.ObjectMeta{
			Name: "sbx-" + id + suffix, Namespace: "sandboxes", CreationTimestamp: metav1.NewTime(created),
			Labels: map[string]string{"app.kubernetes.io/managed-by": "sandbox-gateway", "sandbox-gateway.io/id": id},
		}
	}
	return []runtime.Object{
		&appsv1.Deployment{ObjectMeta: meta("")},
		&corev1.PersistentVolumeClaim{ObjectMeta: meta("-data")},
		&corev1.Service{ObjectMeta: meta("")},
		&corev1.Service{ObjectMeta: meta("-lb")},
		&corev1.Secret{ObjectMeta: meta("-env")},
	}
}

func TestSweepOrphansKubernetes(t *testing.T) {
	svc, client, _ := orphanGCTestService(t)
	ctx := context.Background()
	cases := []struct {
		id      string
		tracked bool
		keep    bool
		objects []runtime.Object
	}{
		{id: "old", objects: orphanGCObjects("old", time.Now().Add(-time.Hour))},
		{id: "volume-only", objects: orphanGCObjects("volume-only", time.Now().Add(-time.Hour))[1:]},
		{id: "young", keep: true, objects: orphanGCObjects("young", time.Now().Add(-time.Minute))},
		{id: "tracked", tracked: true, keep: true, objects: orphanGCObjects("tracked", time.Now().Add(-time.Hour))},
		{id: "unmanaged", keep: true, objects: orphanGCObjects("unmanaged", time.Now().Add(-time.Hour))},
		{id: "other-namespace", keep: true, objects: orphanGCObjects("other-namespace", time.Now().Add(-time.Hour))},
	}
	for _, tc := range cases {
		for _, obj := range tc.objects {
			meta := obj.(metav1.Object)
			if tc.id == "unmanaged" {
				meta.SetLabels(nil)
			}
			if tc.id == "other-namespace" {
				meta.SetNamespace("other")
			}
			if err := client.Tracker().Add(obj); err != nil {
				t.Fatal(err)
			}
		}
		if tc.tracked {
			if err := svc.store.Create(ctx, &models.Sandbox{ID: tc.id, Status: models.StatusStopped}); err != nil {
				t.Fatal(err)
			}
		}
	}
	if n, err := svc.SweepOrphans(ctx); err != nil || n != 2 {
		t.Fatalf("removed=%d want 2, err=%v", n, err)
	}
	resources := map[string]schema.GroupVersionResource{
		"Deployment":            appsv1.SchemeGroupVersion.WithResource("deployments"),
		"PersistentVolumeClaim": corev1.SchemeGroupVersion.WithResource("persistentvolumeclaims"),
		"Service":               corev1.SchemeGroupVersion.WithResource("services"),
		"Secret":                corev1.SchemeGroupVersion.WithResource("secrets"),
	}
	for _, tc := range cases {
		for _, obj := range tc.objects {
			meta := obj.(metav1.Object)
			gvks, _, err := k8sscheme.Scheme.ObjectKinds(obj)
			if err != nil {
				t.Fatal(err)
			}
			_, err = client.Tracker().Get(resources[gvks[0].Kind], meta.GetNamespace(), meta.GetName())
			if (tc.keep && err != nil) || (!tc.keep && !apierrors.IsNotFound(err)) {
				t.Fatalf("%s %s keep=%v, err=%v", gvks[0].Kind, meta.GetName(), tc.keep, err)
			}
		}
	}
	for _, action := range client.Actions() {
		if action.Matches("delete", "deployments") {
			policy := action.(k8stesting.DeleteAction).GetDeleteOptions().PropagationPolicy
			if policy == nil || *policy != metav1.DeletePropagationForeground {
				t.Fatal("deployment deletion must also garbage-collect its Pods")
			}
		}
	}
	svc.cfg.OrphanGCMinAge = 0
	if n, err := svc.SweepOrphans(ctx); err != nil || n != 1 {
		t.Fatalf("without age gate removed=%d want 1, err=%v", n, err)
	}
	if n, err := svc.SweepOrphans(ctx); err != nil || n != 0 {
		t.Fatalf("repeated sweep removed=%d want 0, err=%v", n, err)
	}
}

func TestSweepOrphansKubernetesReadFailuresDoNotDelete(t *testing.T) {
	for _, stage := range []string{"driver list", "database list", "database recheck"} {
		t.Run(stage, func(t *testing.T) {
			svc, client, db := orphanGCTestService(t)
			for _, obj := range orphanGCObjects("old", time.Now().Add(-time.Hour)) {
				if err := client.Tracker().Add(obj); err != nil {
					t.Fatal(err)
				}
			}
			wantErr := errors.New("read unavailable")
			if stage == "driver list" {
				client.PrependReactor("list", "deployments", func(k8stesting.Action) (bool, runtime.Object, error) {
					return true, nil, wantErr
				})
			} else if err := db.Callback().Query().Before("gorm:query").Register("orphan-read-error", func(tx *gorm.DB) {
				_, single := tx.Statement.Dest.(*models.Sandbox)
				if stage == "database list" || single {
					tx.AddError(wantErr)
				}
			}); err != nil {
				t.Fatal(err)
			}
			if n, err := svc.SweepOrphans(context.Background()); n != 0 || !errors.Is(err, wantErr) {
				t.Fatalf("removed=%d err=%v, want read failure", n, err)
			}
			for _, action := range client.Actions() {
				if action.GetVerb() == "delete" {
					t.Fatalf("read failure caused deletion: %v", action)
				}
			}
		})
	}
}

func TestSweepOrphansKubernetesRechecksNewDatabaseRecord(t *testing.T) {
	svc, client, db := orphanGCTestService(t)
	for _, obj := range orphanGCObjects("new-record", time.Now().Add(-time.Hour)) {
		if err := client.Tracker().Add(obj); err != nil {
			t.Fatal(err)
		}
	}
	// Insert after the inventory query, before the candidate can be deleted.
	if err := db.Callback().Query().After("gorm:query").Register("new-sandbox-record", func(tx *gorm.DB) {
		if _, list := tx.Statement.Dest.(*[]models.Sandbox); list {
			tx.AddError(svc.store.Create(context.Background(), &models.Sandbox{ID: "new-record"}))
		}
	}); err != nil {
		t.Fatal(err)
	}
	if n, err := svc.SweepOrphans(context.Background()); err != nil || n != 0 {
		t.Fatalf("removed=%d want 0, err=%v", n, err)
	}
	for _, action := range client.Actions() {
		if action.GetVerb() == "delete" {
			t.Fatal("sandbox registered during the sweep was deleted")
		}
	}
}

func TestRunOrphanGCKubernetesDeletesNewOrphans(t *testing.T) {
	svc, client, _ := orphanGCTestService(t)
	svc.cfg.OrphanGCInterval = 10 * time.Millisecond
	deleted := make(chan string, 2)
	client.PrependReactor("delete", "deployments", func(action k8stesting.Action) (bool, runtime.Object, error) {
		deleted <- action.(k8stesting.DeleteAction).GetName()
		return false, nil, nil
	})
	seed := func(id string) {
		t.Helper()
		for _, obj := range orphanGCObjects(id, time.Now().Add(-time.Hour)) {
			if err := client.Tracker().Add(obj); err != nil {
				t.Fatal(err)
			}
		}
	}
	seed("initial")
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		svc.RunOrphanGC(ctx)
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Error("orphan GC did not stop on cancellation")
		}
	})
	for _, id := range []string{"initial", "periodic"} {
		if id == "periodic" {
			seed(id)
		}
		select {
		case name := <-deleted:
			if name != "sbx-"+id {
				t.Fatalf("deleted=%s want sbx-%s", name, id)
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("orphan %s was not cleaned automatically", id)
		}
	}
}
