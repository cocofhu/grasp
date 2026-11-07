package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDefault(t *testing.T) {
	c := Default()
	if c.Server.Listen != ":8080" {
		t.Fatalf("listen=%q", c.Server.Listen)
	}
	if c.Driver != "docker" {
		t.Fatalf("driver=%q", c.Driver)
	}
	if c.Database.Driver != "sqlite" || c.Database.Path != "gateway.db" {
		t.Fatalf("db=%+v", c.Database)
	}
	if c.Image.Ref == "" {
		t.Fatal("image.ref empty")
	}
	if c.Docker.BindIP != "127.0.0.1" || c.Docker.NamePrefix != "sbx-" {
		t.Fatalf("docker=%+v", c.Docker)
	}
	if c.K8s.Namespace != "sandboxes" || !c.K8s.EnableLoadBalancer {
		t.Fatalf("k8s=%+v", c.K8s)
	}
}

func TestEnvironmentProfilesValidateBeforeStartup(t *testing.T) {
	for _, profiles := range []map[string]ProfileConfig{
		{"": {Image: "approved-image"}},
		{"universal": {Image: " "}},
	} {
		cfg := Default()
		cfg.Profiles = profiles
		if err := cfg.validate(); err == nil {
			t.Fatal("invalid environment profile accepted")
		}
	}
	cfg := Default()
	cfg.Profiles = map[string]ProfileConfig{"universal": {Image: "approved-image"}}
	if err := cfg.validate(); err != nil {
		t.Fatal(err)
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

func TestResourceDefaults(t *testing.T) {
	c := Default()
	rd := c.ResourceDefaults()
	if rd.DefaultCPUCores != c.K8s.CPUCores || rd.MaxDiskGi != c.K8s.MaxDataDiskGi {
		t.Fatalf("%+v vs %+v", rd, c.K8s)
	}
}

func TestLoadMissingFile(t *testing.T) {
	cfg, err := Load(filepath.Join(t.TempDir(), "no-such.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Driver != "docker" {
		t.Fatalf("defaults expected, got %q", cfg.Driver)
	}
}

func TestLoadEmptyPath(t *testing.T) {
	cfg, err := Load("")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Server.Listen != ":8080" {
		t.Fatalf("%+v", cfg.Server)
	}
}

func TestLoadValidYAML(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cfg.yaml")
	content := `
server:
  listen: ":9090"
driver: kubernetes
image:
  ref: my/img:1
database:
  driver: sqlite
  path: /tmp/x.db
docker:
  bindIP: "10.0.0.1"
kubernetes:
  namespace: ns1
  cpuRequestRatio: 2.5
auth:
  apiKeys: ["a", "b"]
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Server.Listen != ":9090" || cfg.Driver != "kubernetes" {
		t.Fatalf("%+v", cfg)
	}
	if cfg.Image.Ref != "my/img:1" {
		t.Fatalf("image=%q", cfg.Image.Ref)
	}
	if cfg.Docker.BindIP != "10.0.0.1" {
		t.Fatalf("bindIP=%q", cfg.Docker.BindIP)
	}
	if cfg.K8s.Namespace != "ns1" {
		t.Fatalf("ns=%q", cfg.K8s.Namespace)
	}
	// normalizeResourcePolicy clamps ratio > 1
	if cfg.K8s.CPURequestRatio != 1 {
		t.Fatalf("ratio clamp=%v", cfg.K8s.CPURequestRatio)
	}
	if len(cfg.Auth.APIKeys) != 2 {
		t.Fatalf("keys=%v", cfg.Auth.APIKeys)
	}
}

func TestLoadPVCAnnotations(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cfg.yaml")
	content := `
driver: kubernetes
image:
  ref: my/img:1
kubernetes:
  pvcAnnotations:
    csi.ugreen.com/deletion-policy: "purge"
    example.com/owner: sandbox-gateway
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.K8s.PVCAnnotations["csi.ugreen.com/deletion-policy"]; got != "purge" {
		t.Fatalf("deletion-policy=%q want purge; annotations=%v", got, cfg.K8s.PVCAnnotations)
	}
	if got := cfg.K8s.PVCAnnotations["example.com/owner"]; got != "sandbox-gateway" {
		t.Fatalf("owner=%q; annotations=%v", got, cfg.K8s.PVCAnnotations)
	}
}

func TestLoadBadYAML(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bad.yaml")
	if err := os.WriteFile(path, []byte("driver: [unterminated"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Load(path)
	if err == nil {
		t.Fatal("expected parse error")
	}
	if !strings.Contains(err.Error(), "parse config") {
		t.Fatalf("err=%v", err)
	}
}

func TestLoadInvalidDriver(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cfg.yaml")
	if err := os.WriteFile(path, []byte("driver: firecracker\nimage:\n  ref: x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), "invalid driver") {
		t.Fatalf("want invalid driver, got %v", err)
	}
}

func TestApplyEnv(t *testing.T) {
	t.Setenv("SBGW_LISTEN", ":7777")
	t.Setenv("SBGW_DRIVER", "kubernetes")
	t.Setenv("SBGW_DB_DRIVER", "mysql")
	t.Setenv("SBGW_DB_PATH", "/tmp/p.db")
	t.Setenv("SBGW_DB_DSN", "user:pass@tcp(h:3306)/db")
	t.Setenv("SBGW_DB_HOST", "dbhost")
	t.Setenv("SBGW_DB_PORT", "3307")
	t.Setenv("SBGW_DB_USER", "u")
	t.Setenv("SBGW_DB_PASSWORD", "p")
	t.Setenv("SBGW_DB_NAME", "n")
	t.Setenv("SBGW_IMAGE", "env/img:latest")
	t.Setenv("SBGW_API_KEYS", "k1, k2, ,k3")
	t.Setenv("SBGW_DOCKER_BIND_IP", "1.2.3.4")
	t.Setenv("SBGW_DOCKER_NETWORK", "mynet")
	t.Setenv("SBGW_K8S_IN_CLUSTER", "true")
	t.Setenv("SBGW_K8S_KUBECONFIG", "/tmp/kube")
	t.Setenv("SBGW_K8S_NAMESPACE", "fromenv")
	t.Setenv("SBGW_K8S_ENABLE_LB", "false")
	t.Setenv("SBGW_K8S_STORAGE_CLASS", "fast")
	t.Setenv("SBGW_ORPHAN_GC_ENABLED", "true")
	t.Setenv("SBGW_ORPHAN_GC_INTERVAL_SEC", "120")
	t.Setenv("SBGW_ORPHAN_GC_MIN_AGE_SEC", "60")

	cfg, err := Load("")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Server.Listen != ":7777" || cfg.Driver != "kubernetes" {
		t.Fatalf("%+v %+v", cfg.Server, cfg.Driver)
	}
	if cfg.Database.Driver != "mysql" || cfg.Database.Host != "dbhost" || cfg.Database.Port != 3307 {
		t.Fatalf("%+v", cfg.Database)
	}
	if cfg.Image.Ref != "env/img:latest" {
		t.Fatalf("image=%q", cfg.Image.Ref)
	}
	if len(cfg.Auth.APIKeys) != 3 || cfg.Auth.APIKeys[0] != "k1" {
		t.Fatalf("keys=%v", cfg.Auth.APIKeys)
	}
	if cfg.Docker.BindIP != "1.2.3.4" || cfg.Docker.Network != "mynet" {
		t.Fatalf("%+v", cfg.Docker)
	}
	if !cfg.K8s.InCluster || cfg.K8s.Namespace != "fromenv" || cfg.K8s.EnableLoadBalancer {
		t.Fatalf("%+v", cfg.K8s)
	}
	if cfg.K8s.StorageClass != "fast" || cfg.K8s.Kubeconfig != "/tmp/kube" {
		t.Fatalf("%+v", cfg.K8s)
	}
	if !cfg.OrphanGC.Enabled || cfg.OrphanGC.IntervalSeconds != 120 || cfg.OrphanGC.MinAgeSeconds != 60 {
		t.Fatalf("orphanGC=%+v", cfg.OrphanGC)
	}
}

func TestSplitCSV(t *testing.T) {
	got := splitCSV(" a, b ,,c ")
	if len(got) != 3 || got[0] != "a" || got[2] != "c" {
		t.Fatalf("%v", got)
	}
	if len(splitCSV("")) != 0 {
		t.Fatal("empty")
	}
}

func TestParseBool(t *testing.T) {
	if !parseBool("true", false) {
		t.Fatal("true")
	}
	if parseBool("false", true) {
		t.Fatal("false")
	}
	if !parseBool("nope", true) {
		t.Fatal("def true")
	}
	if parseBool("nope", false) {
		t.Fatal("def false")
	}
}

func TestNormalizeResourcePolicyRatioClampViaLoad(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cfg.yaml")
	yaml := `
driver: docker
image:
  ref: x
kubernetes:
  cpuRequestRatio: 5
  memoryRequestRatio: 9
  cpuCores: 0
  memoryMB: 0
  dataDiskGi: 0
`
	if err := os.WriteFile(path, []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.K8s.CPURequestRatio != 1 || cfg.K8s.MemoryRequestRatio != 1 {
		t.Fatalf("ratios: %v %v", cfg.K8s.CPURequestRatio, cfg.K8s.MemoryRequestRatio)
	}
	if cfg.K8s.CPUCores != 2 || cfg.K8s.MemoryMB != 4096 || cfg.K8s.DataDiskGi != 25 {
		t.Fatalf("defaults filled: %+v", cfg.K8s)
	}
}

func TestValidateInvalidDBDriver(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cfg.yaml")
	if err := os.WriteFile(path, []byte("driver: docker\nimage:\n  ref: x\ndatabase:\n  driver: postgres\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), "database.driver") {
		t.Fatalf("got %v", err)
	}
}

func TestValidateMissingImageRef(t *testing.T) {
	cfg := Default()
	cfg.Image.Ref = ""
	if err := cfg.validate(); err == nil {
		t.Fatal("expected error")
	}
}
