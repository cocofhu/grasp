package config

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

func loadYAML(t *testing.T, value string) (*Config, error) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "gateway.yaml")
	if err := os.WriteFile(path, []byte(value), 0600); err != nil {
		t.Fatal(err)
	}
	return Load(path)
}
func TestBackendDefaultsAndIsolation(t *testing.T) {
	for _, driver := range []string{"docker", "kubernetes"} {
		c, err := loadYAML(t, "driver: "+driver)
		if err != nil {
			t.Fatal(err)
		}
		if c.DefaultImage() != "universal-sandbox:local" || c.Server.Listen != ":8080" || c.Database.Driver != "sqlite" || c.Database.Path != "gateway.db" {
			t.Fatal("incorrect defaults")
		}
		cpu, mem, disk, err := c.ResourcePolicy().Resolve(c.DefaultImage(), 0, 0, 0)
		if err != nil || cpu != 2 || mem != 4096 || disk != 25 {
			t.Fatal("incorrect resources")
		}
		if c.FinalizeTimeout() != 20*time.Minute || c.OrphanGCInterval() != 5*time.Minute || c.OrphanGCMinAge() != 10*time.Minute {
			t.Fatal("incorrect timeouts")
		}
		if driver == "docker" && (c.Kubernetes != nil || c.E2B != nil || c.Docker.BindIP != "127.0.0.1") {
			t.Fatal("backend defaults leaked")
		}
		if driver == "kubernetes" && (c.Docker != nil || c.E2B != nil || c.Kubernetes.StorageClass != "") {
			t.Fatal("backend defaults leaked")
		}
	}
	if _, err := Load(""); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(filepath.Join(t.TempDir(), "missing.yaml")); err == nil {
		t.Fatal("missing explicit file accepted")
	}
	if _, err := Load(t.TempDir()); err == nil {
		t.Fatal("directory accepted")
	}
}
func TestStrictConfiguration(t *testing.T) {
	for _, body := range []string{
		"driver: unknown", "driver: [broken", "driver: docker\n---\ndriver: docker",
		"driver: docker\ne2b: {}", "driver: kubernetes\ndocker: {}", "driver: docker\nkubernetes: {}", "driver: docker\ndocker: null",
		"image: {ref: old}", "profiles: {}", "driver: docker\ndocker: {imag: typo}",
		"driver: e2b\ne2b: {domain: e2b.app}", "database: {driver: invalid}",
		"docker: {image: ''}", "docker: {profiles: {universal: {image: ''}}}", "docker: {profiles: {'': {image: x}}}",
		"docker: {resources: {default: {memoryMB: 0}}}", "docker: {resources: {max: {cpuCores: 1}}}",
		"docker: {resources: {default: {cpuCores: .nan}}}", "docker: {resources: {max: {diskGi: -1}}}",
		"server: {listen: ''}", "server: {finalizeTimeoutSeconds: 0}", "server: {finalizeTimeoutSeconds: 99999999999}",
		"orphanGC: {intervalSeconds: 0}", "orphanGC: {minAgeSeconds: -1}", "database: {port: 65536}",
		"driver: kubernetes\nkubernetes: {cpuRequestCores: -1}",
		"driver: kubernetes\nkubernetes: {memoryRequestRatio: 2}",
		"driver: kubernetes\nkubernetes: {imagePullPolicy: sometimes}",
	} {
		t.Run(body, func(t *testing.T) {
			if _, err := loadYAML(t, body); err == nil {
				t.Fatal("accepted invalid config")
			}
		})
	}
	if _, err := loadYAML(t, "server: {finalizeTimeoutSeconds: secret-value}"); err == nil || strings.Contains(err.Error(), "secret-value") || !strings.Contains(err.Error(), "line 1") {
		t.Fatalf("error must locate the value without leaking it: %v", err)
	}
	if _, err := loadYAML(t, "driver: docker\ndocker:\n  imag: typo-secret"); err == nil || !strings.Contains(err.Error(), "line 3: field imag not found") || strings.Contains(err.Error(), "typo-secret") {
		t.Fatalf("unknown field must name its line and key: %v", err)
	}
	if _, err := loadYAML(t, "driver: docker\n  bad: [secret"); err == nil || !strings.Contains(err.Error(), "line") || strings.Contains(err.Error(), "secret") {
		t.Fatalf("syntax error must keep only its line: %v", err)
	}
}

func TestRemovedEnvironmentNamesTheirReplacement(t *testing.T) {
	t.Setenv("SBGW_IMAGE", "old")
	if _, err := Load(""); err == nil || !strings.Contains(err.Error(), "SBGW_DOCKER_IMAGE") {
		t.Fatalf("removed SBGW_IMAGE needs a replacement hint: %v", err)
	}
	_ = os.Unsetenv("SBGW_IMAGE")
	t.Setenv("SBGW_E2B_DOMAIN", "e2b.app")
	if _, err := Load(""); err == nil || !strings.Contains(err.Error(), "SBGW_E2B_API_URL") {
		t.Fatalf("removed SBGW_E2B_DOMAIN needs a replacement hint: %v", err)
	}
}
func TestBackendYAMLAndEnvironment(t *testing.T) {
	t.Setenv("SBGW_DRIVER", "kubernetes")
	t.Setenv("SBGW_LISTEN", ":7777")
	for k, v := range map[string]string{
		"SBGW_DB_DRIVER": "mysql", "SBGW_DB_PATH": "custom.db", "SBGW_DB_DSN": "test-dsn", "SBGW_DB_HOST": "db", "SBGW_DB_PORT": "3307", "SBGW_DB_USER": "user", "SBGW_DB_PASSWORD": "test-password", "SBGW_DB_NAME": "name",
		"SBGW_API_KEYS": "first, second, ,third", "SBGW_K8S_IMAGE": "custom:image", "SBGW_K8S_IN_CLUSTER": "true", "SBGW_K8S_KUBECONFIG": "/kube", "SBGW_K8S_NAMESPACE": "custom", "SBGW_K8S_ENABLE_LB": "false", "SBGW_K8S_STORAGE_CLASS": "fast", "SBGW_K8S_IMAGE_PULL_POLICY": "Always",
		"SBGW_ORPHAN_GC_ENABLED": "true", "SBGW_ORPHAN_GC_INTERVAL_SEC": "60", "SBGW_ORPHAN_GC_MIN_AGE_SEC": "0", "SBGW_FINALIZE_TIMEOUT_SEC": "100", "SBGW_LOG_LEVEL": "info", "SBGW_LOG_PRETTY": "false", "SBGW_CONFIG": "unused",
	} {
		t.Setenv(k, v)
	}
	c, err := loadYAML(t, `driver: kubernetes
kubernetes:
  image: overridden:image
  cpuRequestCores: 0
  memoryRequestMB: 0
  pvcAnnotations: {example.com/owner: gateway}
  profiles: {universal: {image: custom:image}}
  resources:
    default: {cpuCores: 4, memoryMB: 8192, diskGi: 50}
`)
	if err != nil {
		t.Fatal(err)
	}
	if c.DefaultImage() != "custom:image" || c.Server.Listen != ":7777" || c.Database.Driver != "mysql" || c.Database.Port != 3307 || len(c.Auth.APIKeys) != 3 {
		t.Fatal("environment overlay failed")
	}
	if c.Kubernetes.CPURequestCores != 0 || c.Kubernetes.MemoryRequestMB != 0 || c.Kubernetes.Resources.Default.DiskGi != 50 || c.Kubernetes.PVCAnnotations["example.com/owner"] != "gateway" {
		t.Fatal("YAML overwritten by defaults")
	}
	if !c.Kubernetes.InCluster || c.Kubernetes.EnableLoadBalancer || c.Kubernetes.StorageClass != "fast" || c.EnvironmentProfiles()["universal"].Image != "custom:image" {
		t.Fatal("backend overlay failed")
	}
	if _, err := loadYAML(t, "driver: docker"); err == nil {
		t.Fatal("conflicting driver accepted")
	}
}
func TestEnvironmentRejectsTyposInactiveBackendsAndBadValues(t *testing.T) {
	for k, v := range map[string]string{"SBGW_IMAGE": "old", "SBGW_DOKCER_IMAGE": "typo", "SBGW_E2B_API_URL": "https://api.example", "SBGW_DB_PORT": "bad", "SBGW_ORPHAN_GC_ENABLED": "perhaps"} {
		t.Run(k, func(t *testing.T) {
			t.Setenv(k, v)
			if _, err := Load(""); err == nil || !strings.Contains(err.Error(), k) {
				t.Fatal("invalid environment accepted")
			}
		})
	}
	t.Setenv("SBGW_DOCKER_IMAGE", "custom:image")
	t.Setenv("SBGW_DOCKER_NETWORK", "test-net")
	t.Setenv("SBGW_DOCKER_BIND_IP", "0.0.0.0")
	t.Setenv("SBGW_DB_PORT", "")
	t.Setenv("SBGW_ORPHAN_GC_ENABLED", "")
	t.Setenv("SBGW_API_KEYS", "")
	c, err := Load("")
	if err != nil || c.DefaultImage() != "custom:image" || c.Docker.Network != "test-net" || c.Docker.BindIP != "0.0.0.0" {
		t.Fatal("docker overrides failed")
	}
}
func TestPortsConfigAll(t *testing.T) {
	p := PortsConfig{
		Session: 8765, CodeServer: 8744, SSH: 22, CDP: 9222, NoVNC: 6080,
		App: []int{80, 80, 0, -1, 443},
	}
	assertUnique := func(t *testing.T, got []int, want []int, forbid []int) {
		t.Helper()
		seen := map[int]int{}
		for _, v := range got {
			seen[v]++
		}
		for _, w := range want {
			if seen[w] != 1 {
				t.Fatalf("port %d count=%d in %v", w, seen[w], got)
			}
		}
		for _, f := range forbid {
			if seen[f] != 0 {
				t.Fatalf("port %d must not be in publish set %v", f, got)
			}
		}
		if len(got) != len(want) {
			t.Fatalf("len=%d want %d %v", len(got), len(want), got)
		}
	}

	// All/Public are the external publish set: session/ide/ssh/app only.
	// 9222/6080 must not be treated as host/LB publish ports (g1.1).
	assertUnique(t, p.Public(), []int{8765, 8744, 22, 80, 443}, []int{9222, 6080})
	assertUnique(t, p.All(), []int{8765, 8744, 22, 80, 443}, []int{9222, 6080})

	internal := p.Internal()
	seenI := map[int]int{}
	for _, v := range internal {
		seenI[v]++
	}
	if seenI[9222] != 1 || seenI[6080] != 1 || len(internal) != 2 {
		t.Fatalf("Internal=%v want 9222 and 6080 once each", internal)
	}

	listen := p.Listen()
	seenL := map[int]int{}
	for _, v := range listen {
		seenL[v]++
	}
	for _, w := range []int{8765, 8744, 22, 80, 443, 9222, 6080} {
		if seenL[w] != 1 {
			t.Fatalf("Listen missing %d in %v", w, listen)
		}
	}
}

func TestPublishedBackendExamplesLoad(t *testing.T) {
	for driver, path := range map[string]string{
		"docker":     "../../config.docker.example.yaml",
		"kubernetes": "../../config.kubernetes.example.yaml",
		"e2b":        "../../../deploy/e2b/gateway.example.yaml",
	} {
		t.Run(driver, func(t *testing.T) {
			if driver == "e2b" {
				t.Setenv("SBGW_E2B_API_KEY", "test-key")
			}
			c, err := Load(path)
			if err != nil || c.Driver != driver {
				t.Fatalf("example failed: %v", err)
			}
			if _, _, _, err := c.ResourcePolicy().Resolve(c.DefaultImage(), 0, 0, 0); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestKubernetesDeploymentConfigLoads(t *testing.T) {
	f, err := os.Open("../../../deploy/k8s/stack.example.yaml")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	decoder := yaml.NewDecoder(f)
	for {
		var manifest struct {
			Kind     string
			Metadata struct{ Name string }
			Data     map[string]string
		}
		if err := decoder.Decode(&manifest); err == io.EOF {
			t.Fatal("deployment is missing the gateway ConfigMap")
		} else if err != nil {
			t.Fatal(err)
		}
		if manifest.Kind != "ConfigMap" || manifest.Metadata.Name != "sandbox-gateway-config" {
			continue
		}
		c, err := loadYAML(t, manifest.Data["config.yaml"])
		if err != nil {
			t.Fatal(err)
		}
		if c.Driver != "kubernetes" || c.DefaultImage() != "ghcr.io/cocofhu/universal-sandbox:3.3.0" || c.Kubernetes.EnableLoadBalancer {
			t.Fatal("incorrect deployment configuration")
		}
		cpu, memory, disk, err := c.ResourcePolicy().Resolve(c.DefaultImage(), 0, 0, 0)
		if err != nil || cpu != 2 || memory != 4096 || disk != 25 {
			t.Fatal("incorrect deployment resource defaults")
		}
		return
	}
}
