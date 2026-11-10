package api_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"sandbox-gateway/internal/api"
	"sandbox-gateway/internal/config"
	"sandbox-gateway/internal/database"
	gatewayk8s "sandbox-gateway/internal/driver/kubernetes"
	"sandbox-gateway/internal/models"
	"sandbox-gateway/internal/service"
	"sandbox-gateway/internal/store"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

// Opt-in real infrastructure proof. It creates and removes only a fresh,
// uniquely owned namespace and UID-preconditioned resources. No model runs.
func TestEnvironmentReleaseRealKubernetesUIDAndVolumeReceipt(t *testing.T) {
	kubeconfig := os.Getenv("GRASP_RELEASE_KUBECONFIG")
	if kubeconfig == "" {
		t.Skip("set GRASP_RELEASE_KUBECONFIG for the isolated real Kubernetes deletion proof")
	}
	storageClass := os.Getenv("GRASP_RELEASE_STORAGE_CLASS")
	if storageClass == "" {
		t.Fatal("explicit GRASP_RELEASE_STORAGE_CLASS required")
	}
	cfg, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	if err != nil {
		t.Fatal("cannot load explicit test cluster configuration")
	}
	client, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
	var random [6]byte
	if _, err := rand.Read(random[:]); err != nil {
		t.Fatal(err)
	}
	suffix := hex.EncodeToString(random[:])
	namespace := "grasp-release-proof-" + suffix
	id := "owned-" + suffix
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Minute)
	defer cancel()
	ns, err := client.CoreV1().Namespaces().Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace, Labels: map[string]string{"grasp-release-proof": suffix}}}, metav1.CreateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanup, stop := context.WithTimeout(context.Background(), 60*time.Second)
		defer stop()
		uid := ns.UID
		_ = client.CoreV1().Namespaces().Delete(cleanup, namespace, metav1.DeleteOptions{Preconditions: &metav1.Preconditions{UID: &uid}})
	})
	labels := map[string]string{"app.kubernetes.io/managed-by": "sandbox-gateway", "sandbox-gateway.io/id": id}
	name := "sbx-" + id
	claim, err := client.CoreV1().PersistentVolumeClaims(namespace).Create(ctx, &corev1.PersistentVolumeClaim{ObjectMeta: metav1.ObjectMeta{Name: name + "-data", Namespace: namespace, Labels: labels}, Spec: corev1.PersistentVolumeClaimSpec{StorageClassName: &storageClass, AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce}, Resources: corev1.VolumeResourceRequirements{Requests: corev1.ResourceList{corev1.ResourceStorage: resource.MustParse("1Gi")}}}}, metav1.CreateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	marker := "native-release-proof-" + suffix
	markerSHA := sha256.Sum256([]byte(marker))
	expectedSHA := hex.EncodeToString(markerSHA[:])
	replicas := int32(1)
	image := "busybox:1.37"
	if v := os.Getenv("GRASP_RELEASE_FIXTURE_IMAGE"); v != "" {
		image = v
	}
	deployment, err := client.AppsV1().Deployments(namespace).Create(ctx, &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace, Labels: labels}, Spec: appsv1.DeploymentSpec{Replicas: &replicas, Selector: &metav1.LabelSelector{MatchLabels: labels}, Template: corev1.PodTemplateSpec{ObjectMeta: metav1.ObjectMeta{Labels: labels}, Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "proof", Image: image, Command: []string{"sh", "-c", "printf '%s' '" + marker + "' > /workspace/marker; sha256sum /workspace/marker; exec sleep 240"}, VolumeMounts: []corev1.VolumeMount{{Name: "disk", MountPath: "/workspace"}}, Resources: corev1.ResourceRequirements{Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("10m"), corev1.ResourceMemory: resource.MustParse("8Mi")}, Limits: corev1.ResourceList{corev1.ResourceMemory: resource.MustParse("32Mi")}}}}, Volumes: []corev1.Volume{{Name: "disk", VolumeSource: corev1.VolumeSource{PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: claim.Name}}}}}}}}, metav1.CreateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	wait := func(description string, check func() (bool, error)) {
		t.Helper()
		ticker := time.NewTicker(500 * time.Millisecond)
		defer ticker.Stop()
		for {
			ok, err := check()
			if err != nil {
				t.Fatalf("%s: %v", description, err)
			}
			if ok {
				return
			}
			select {
			case <-ctx.Done():
				t.Fatalf("%s: timeout", description)
			case <-ticker.C:
			}
		}
	}
	var podName string
	wait("owned file writer ready", func() (bool, error) {
		pods, e := client.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{LabelSelector: "sandbox-gateway.io/id=" + id})
		if e != nil {
			return false, e
		}
		for _, p := range pods.Items {
			for _, c := range p.Status.ContainerStatuses {
				if c.Ready {
					podName = p.Name
					return true, nil
				}
			}
		}
		return false, nil
	})
	logs, err := client.CoreV1().Pods(namespace).GetLogs(podName, &corev1.PodLogOptions{Container: "proof"}).DoRaw(ctx)
	if err != nil || !strings.Contains(string(logs), expectedSHA) {
		t.Fatal("owned disk marker SHA was not confirmed")
	}
	native := gatewayk8s.NewFromClient(client, gatewayk8s.Options{Namespace: namespace, NamePrefix: "sbx-", StorageClass: storageClass})
	if err := native.Stop(ctx, id); err != nil {
		t.Fatal(err)
	}
	plan, err := native.PreviewEnvironmentRelease(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if !plan.Valid() {
		t.Fatal("native release plan lacks complete UID proof")
	}
	for _, o := range plan.Objects {
		if o.Kind == "Deployment" && o.UID != string(deployment.UID) {
			t.Fatal("deployment incarnation changed")
		}
		if o.Kind == "PersistentVolumeClaim" && o.UID != string(claim.UID) {
			t.Fatal("claim incarnation changed")
		}
	}
	db, err := database.Open(config.DBConfig{Driver: "sqlite", Path: filepath.Join(t.TempDir(), "gateway.db")})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := db.DB()
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = sqlDB.Close() })
	sb := models.Sandbox{ID: id, Name: id, Status: models.StatusStopped}
	sb.SetEnv(map[string]string{"GRASP_EXECUTION_LINEAGE": "real-release-proof"})
	if err := db.Create(&sb).Error; err != nil {
		t.Fatal(err)
	}
	svc := service.New(native, store.New(db), service.Config{})
	releases := service.NewEnvironmentReleases(db, svc, service.EnvironmentReleaseOptions{})
	routerCfg := config.Default()
	routerCfg.Driver = "kubernetes"
	routerCfg.Auth.APIKeys = []string{"synthetic-isolated-release-key"}
	httpServer := httptest.NewServer(api.NewRouter(api.NewHandler(svc, config.PortsConfig{}), routerCfg))
	defer httpServer.Close()
	request := func(method, path string, input any, out any) int {
		t.Helper()
		var body io.Reader
		if input != nil {
			data, _ := json.Marshal(input)
			body = bytes.NewReader(data)
		}
		req, err := http.NewRequestWithContext(ctx, method, httpServer.URL+path, body)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Authorization", "Bearer synthetic-isolated-release-key")
		req.Header.Set("Content-Type", "application/json")
		response, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		data, err := io.ReadAll(io.LimitReader(response.Body, 128<<10))
		if err != nil {
			t.Fatal(err)
		}
		if out != nil && response.StatusCode >= 200 && response.StatusCode < 300 {
			if err := json.Unmarshal(data, out); err != nil {
				t.Fatal(err)
			}
		}
		return response.StatusCode
	}
	base := "/api/v1/sandboxes/" + id + "/releases"
	operation := "manual-release-" + suffix
	var view service.EnvironmentReleaseView
	if code := request("POST", base, service.EnvironmentReleasePrepare{ID: operation, Lineage: "real-release-proof"}, &view); code != 200 {
		t.Fatalf("native HTTP preview %d", code)
	}
	originalPlan := view.Plan
	confirm := service.EnvironmentReleaseConfirm{PlanSHA256: view.Plan.SHA256, Nonce: "manual-loss-confirm-" + suffix, Confirmed: true, LossAccepted: true, Owner: "owned-worker-" + suffix, Generation: 1, ExpiresAt: time.Now().UTC().Add(44 * time.Second), Deadline: time.Now().UTC().Add(4 * time.Minute)}
	if code := request("POST", base+"/"+operation+"/confirm", confirm, &view); code != 202 {
		t.Fatalf("native HTTP confirm %d", code)
	}
	// Native dispatch belongs to the application worker, never HTTP requests.
	if _, err := client.AppsV1().Deployments(namespace).Get(ctx, name, metav1.GetOptions{}); err != nil {
		t.Fatal("confirmation request executed deletion")
	}
	if err := releases.RunOne(ctx, operation); err != nil {
		t.Fatal(err)
	}
	wait("actual UID resources and reclaiming volume gone", func() (bool, error) {
		if code := request("GET", base+"/"+operation, nil, &view); code != 200 {
			return false, fmt.Errorf("receipt HTTP %d", code)
		}
		return view.State == "released", nil
	})
	if view.Receipt == nil || view.Receipt.PlanSHA256 != originalPlan.SHA256 || len(view.Receipt.Objects) != len(originalPlan.Objects) {
		t.Fatal("native receipt lost exact original scope")
	}
	gone, err := native.EnvironmentReleaseGone(ctx, originalPlan)
	if err != nil || !gone {
		t.Fatal("physical resources still present", err)
	}
	for _, o := range originalPlan.Objects {
		if o.Kind == "PersistentVolume" {
			if _, err := client.CoreV1().PersistentVolumes().Get(ctx, o.Name, metav1.GetOptions{}); !apierrors.IsNotFound(err) {
				t.Fatal("PV deletion was not independently confirmed", err)
			}
		}
	}
	// Repeated original confirmation/get are receipts, not deletion requests.
	if code := request("POST", base+"/"+operation+"/confirm", confirm, &view); code != 202 {
		t.Fatalf("original confirmation no longer idempotent %d", code)
	}
	if err := releases.RunOne(ctx, operation); err == nil {
		t.Fatal("released native job was replayable")
	}
	if path := os.Getenv("GRASP_RELEASE_PROOF_OUTPUT"); path != "" {
		proof := map[string]any{"namespace": namespace, "namespaceUID": ns.UID, "resource": id, "markerSHA256": expectedSHA, "plan": originalPlan, "receipt": view.Receipt, "physicalGone": true, "modelRequests": 0, "costBoundary": "real Kubernetes API and disk operations; no model inference"}
		body, _ := json.MarshalIndent(proof, "", "  ")
		if err := os.WriteFile(path, append(body, '\n'), 0600); err != nil {
			t.Fatal(err)
		}
	}
}
