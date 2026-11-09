package kubernetes

import (
	"context"
	"errors"
	"testing"

	"sandbox-gateway/internal/driver"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	kt "k8s.io/client-go/testing"
)

func TestConvergeRetainsPublishedAppsAndRecoversNativePreviewAfterGatewayRestart(t *testing.T) {
	d := isolationDriver(t)
	ctx := t.Context()
	if _, err := d.Create(ctx, driver.Spec{ID: "retained", Image: "environment:known", Env: map[string]string{driver.EnvPreviewDirect: "1"}, Ports: d.opts.PublicPorts, InternalPorts: d.opts.InternalPorts}); err != nil {
		t.Fatal(err)
	}
	secret, _ := d.cs.CoreV1().Secrets(d.opts.Namespace).Get(ctx, d.secretName("retained"), metav1.GetOptions{})
	secret.Data = map[string][]byte{driver.EnvPreviewDirect: []byte("1"), driver.EnvPreviewPort: []byte("18080")}
	secret.StringData = nil
	if _, err := d.cs.CoreV1().Secrets(d.opts.Namespace).Update(ctx, secret, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	// Reproduce an older gateway's startup: its global catalogue omitted the
	// preview port, so it removed that platform-created Service entry.
	services := d.cs.CoreV1().Services(d.opts.Namespace)
	for _, name := range []string{d.resourceName("retained"), d.lbName("retained")} {
		svc, _ := services.Get(ctx, name, metav1.GetOptions{})
		svc.Spec.Ports = servicePorts([]int{22, 8765, 9222, 6080})
		if _, err := services.Update(ctx, svc, metav1.UpdateOptions{}); err != nil {
			t.Fatal(err)
		}
	}
	assertSurface := func() {
		t.Helper()
		cluster, _ := services.Get(ctx, d.resourceName("retained"), metav1.GetOptions{})
		lb, _ := services.Get(ctx, d.lbName("retained"), metav1.GetOptions{})
		for _, svc := range []map[int32]struct{}{servicePortSet(cluster.Spec.Ports), servicePortSet(lb.Spec.Ports)} {
			if _, ok := svc[18080]; !ok {
				t.Fatal("retained platform preview was dropped")
			}
			if _, ok := svc[3017]; !ok {
				t.Fatal("explicitly published application port was dropped")
			}
		}
		for _, p := range lb.Spec.Ports {
			if p.Port == 9222 || p.Port == 6080 {
				t.Fatal("retaining apps exposed an internal endpoint")
			}
		}
		for _, p := range []int32{9222, 6080} {
			if _, ok := servicePortSet(cluster.Spec.Ports)[p]; !ok {
				t.Fatal("private gateway transport lost its internal endpoint")
			}
		}
	}
	if err := d.PublishPort(ctx, "retained", 3017); err != nil {
		t.Fatal(err)
	}
	for _, converge := range []func() error{
		func() error { return d.ConvergePublishSurface(ctx, "retained") },
		func() error { _, err := d.Get(ctx, "retained"); return err },
		func() error { return d.Start(ctx, "retained") },
	} {
		if err := converge(); err != nil {
			t.Fatal(err)
		}
		assertSurface()
	}
}

func TestConvergePreviewConfigurationFailureDoesNotRewriteService(t *testing.T) {
	for _, fault := range []string{"foreign-secret", "invalid-preview", "private-preview", "unreadable-secret"} {
		t.Run(fault, func(t *testing.T) {
			d := isolationDriver(t)
			ctx := context.Background()
			if _, err := d.Create(ctx, driver.Spec{ID: "retained", Image: "environment:known", Env: map[string]string{driver.EnvPreviewDirect: "1"}, Ports: d.opts.PublicPorts, InternalPorts: d.opts.InternalPorts}); err != nil {
				t.Fatal(err)
			}
			secret, _ := d.cs.CoreV1().Secrets(d.opts.Namespace).Get(ctx, d.secretName("retained"), metav1.GetOptions{})
			// The client-go fake stores StringData without apiserver conversion.
			secret.Data = map[string][]byte{driver.EnvPreviewDirect: []byte("1"), driver.EnvPreviewPort: []byte("18080")}
			secret.StringData = nil
			switch fault {
			case "foreign-secret":
				secret.Labels["sandbox-gateway.io/id"] = "other"
			case "invalid-preview":
				secret.Data[driver.EnvPreviewPort] = []byte("not-a-port")
			case "private-preview":
				secret.Data[driver.EnvPreviewPort] = []byte("9222")
			}
			if _, err := d.cs.CoreV1().Secrets(d.opts.Namespace).Update(ctx, secret, metav1.UpdateOptions{}); err != nil {
				t.Fatal(err)
			}
			cs := d.cs.(interface {
				PrependReactor(string, string, kt.ReactionFunc)
				ClearActions()
				Actions() []kt.Action
			})
			if fault == "unreadable-secret" {
				cs.PrependReactor("get", "secrets", func(kt.Action) (bool, runtime.Object, error) {
					return true, nil, errors.New("secret observation unavailable")
				})
			}
			cs.ClearActions()
			if err := d.ConvergePublishSurface(ctx, "retained"); err == nil {
				t.Fatal("untrusted preview configuration was accepted")
			}
			for _, a := range cs.Actions() {
				if a.GetVerb() != "get" && a.GetVerb() != "list" {
					t.Fatal("failed preview proof mutated a Service")
				}
			}
		})
	}
}
