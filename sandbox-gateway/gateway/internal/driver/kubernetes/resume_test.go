package kubernetes

import (
	"context"
	"testing"

	"sandbox-gateway/internal/driver"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestPrepareStartRefreshesRuntimeAndKeepsExistingSecrets(t *testing.T) {
	cs := fake.NewClientset(&appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "sbx-original", Namespace: "sandboxes"}}, &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "sbx-original-env", Namespace: "sandboxes"}, Data: map[string][]byte{"SSH_KEY": []byte("original-key"), "SANDBOX_INJECT": []byte("expired config URL")}})
	d := NewFromClient(cs, Options{})
	if err := d.PrepareStart(context.Background(), "original", driver.StartOptions{RuntimeURL: "http://fresh/runtime", RuntimeHeaders: "Authorization: Bearer fresh"}); err != nil {
		t.Fatal(err)
	}
	secret, err := cs.CoreV1().Secrets("sandboxes").Get(context.Background(), "sbx-original-env", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if string(secret.Data["SSH_KEY"]) != "original-key" || string(secret.Data["GRASP_RUNTIME_URL"]) != "http://fresh/runtime" || string(secret.Data["SANDBOX_INJECT_HEADERS"]) != "Authorization: Bearer fresh" || len(secret.Data["SANDBOX_INJECT"]) != 0 {
		t.Fatalf("resume replaced existing config: %v", secret.Data)
	}
	if err := d.PrepareStart(context.Background(), "original", driver.StartOptions{}); err == nil {
		t.Fatal("empty runtime URL accepted")
	}
	if err := d.PrepareStart(context.Background(), "missing", driver.StartOptions{RuntimeURL: "http://fresh/runtime"}); err == nil {
		t.Fatal("missing environment accepted")
	}
}
