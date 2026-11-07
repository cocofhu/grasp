package service

import (
	"context"
	"path/filepath"
	"testing"

	"sandbox-gateway/internal/config"
	"sandbox-gateway/internal/database"
	k8sdriver "sandbox-gateway/internal/driver/kubernetes"
	"sandbox-gateway/internal/models"
	"sandbox-gateway/internal/store"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	k8sfake "k8s.io/client-go/kubernetes/fake"
)

func TestConnectionTargetPreservesPublishedKubernetesPorts(t *testing.T) {
	ctx := context.Background()
	const namespace, id = "transport", "live"
	replicas := int32(1)
	port := func(name string, number int) corev1.ServicePort {
		return corev1.ServicePort{Name: name, Port: int32(number), TargetPort: intstr.FromInt(number), Protocol: corev1.ProtocolTCP}
	}
	public := []corev1.ServicePort{port("session", 8765), port("published-preview", 18080)}
	cs := k8sfake.NewSimpleClientset(
		&appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "sbx-" + id, Namespace: namespace}, Spec: appsv1.DeploymentSpec{Replicas: &replicas}, Status: appsv1.DeploymentStatus{ReadyReplicas: 1}},
		&corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: "sbx-" + id, Namespace: namespace}, Spec: corev1.ServiceSpec{Ports: append(append([]corev1.ServicePort{}, public...), port("private-cdp", 9222))}},
		&corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: "sbx-" + id + "-lb", Namespace: namespace}, Spec: corev1.ServiceSpec{Type: corev1.ServiceTypeLoadBalancer, Ports: public}, Status: corev1.ServiceStatus{LoadBalancer: corev1.LoadBalancerStatus{Ingress: []corev1.LoadBalancerIngress{{IP: "192.0.2.100"}}}}},
	)
	drv := k8sdriver.NewFromClient(cs, k8sdriver.Options{Namespace: namespace, NamePrefix: "sbx-", EnableLoadBalancer: true, PublicPorts: []int{8765}, InternalPorts: []int{9222}})
	db, err := database.Open(config.DBConfig{Driver: "sqlite", Path: filepath.Join(t.TempDir(), "gateway.db")})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	st := store.New(db)
	if err := st.Create(ctx, &models.Sandbox{ID: id, Name: "sbx-" + id, Status: models.StatusRunning}); err != nil {
		t.Fatal(err)
	}
	svc := New(drv, st, Config{})
	// 18080 was published after creation; it is deliberately absent from
	// Options.PublicPorts. Opening a channel must not reconcile it away.
	for range 2 {
		addr, err := svc.ConnectionTarget(ctx, id, 18080)
		if err != nil || addr != "192.0.2.100:18080" {
			t.Fatalf("published preview lost while opening its channel: %q %v", addr, err)
		}
	}
	for _, name := range []string{"sbx-" + id, "sbx-" + id + "-lb"} {
		svc, err := cs.CoreV1().Services(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, p := range svc.Spec.Ports {
			found = found || p.Port == 18080
			if name == "sbx-"+id+"-lb" && p.Port == 9222 {
				t.Fatal("private CDP endpoint became public")
			}
		}
		if !found {
			t.Fatalf("channel establishment removed a published port from %s", name)
		}
	}
}
