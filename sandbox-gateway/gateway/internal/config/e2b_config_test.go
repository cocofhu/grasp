package config

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func configuredE2BForTest(t *testing.T) *Config {
	t.Helper()
	cfg := Default()
	cfg.Driver = "e2b"
	cfg.Image.Ref = "approved/full:release"
	cfg.E2B.APIKey = "synthetic-config-secret"
	cfg.E2B.OwnerID = "gateway-test"
	cfg.E2B.StateDir = filepath.Join(t.TempDir(), "state")
	cfg.E2B.Templates = []E2BTemplateConfig{
		{Image: "approved/small:release", TemplateID: "small-template", CPUCores: 1, MemoryMB: 512, DiskGi: 1},
		{Image: "approved/full:release", TemplateID: "team/full:release", CPUCores: 2, MemoryMB: 3072, DiskGi: 20},
	}
	return cfg
}

func writeE2BConfigForTest(t *testing.T, cfg *Config) string {
	t.Helper()
	data, err := yaml.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	filename := filepath.Join(t.TempDir(), "gateway.yaml")
	if err = os.WriteFile(filename, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return filename
}

func TestE2BConfigLoadsExplicitTemplatesWithoutKubernetesDefaults(t *testing.T) {
	cfg := configuredE2BForTest(t)
	loaded, err := Load(writeE2BConfigForTest(t, cfg))
	if err != nil {
		t.Fatal(err)
	}
	if loaded.E2B.APIURL != "" || loaded.E2B.SandboxURL != "" || loaded.E2B.Domain != "e2b.app" {
		t.Fatal("Cloud defaults were replaced by a fabricated local proxy")
	}
	if loaded.E2B.BootTimeoutSeconds != 600 || loaded.E2B.LeaseSeconds != 3600 || loaded.E2B.RenewIntervalSeconds != 300 {
		t.Fatal("unexpected E2B timeout defaults")
	}
	resources := loaded.ResourceDefaults()
	if resources.DefaultCPUCores != 2 || resources.DefaultMemoryMB != 3072 || resources.DefaultDiskGi != 20 ||
		resources.MaxCPUCores != 2 || resources.MaxMemoryMB != 3072 || resources.MaxDiskGi != 20 {
		t.Fatalf("E2B policy used another driver's resources: %+v", resources)
	}
	if resources.DefaultMemoryMB == loaded.K8s.MemoryMB || resources.DefaultDiskGi == loaded.K8s.DataDiskGi {
		t.Fatal("E2B inherited Kubernetes defaults")
	}
}

func TestE2BConfigEnvOverridesOriginsCredentialsAndTimingOnly(t *testing.T) {
	cfg := configuredE2BForTest(t)
	filename := writeE2BConfigForTest(t, cfg)
	for key, value := range map[string]string{
		"SBGW_E2B_API_KEY": "synthetic-env-secret", "SBGW_E2B_API_URL": "http://127.0.0.1:3000",
		"SBGW_E2B_SANDBOX_URL": "http://127.0.0.1:3002", "SBGW_E2B_DOMAIN": "selfhost.example",
		"SBGW_E2B_OWNER_ID": "gateway-env", "SBGW_E2B_STATE_DIR": filepath.Join(t.TempDir(), "persistent"),
		"SBGW_E2B_BOOT_TIMEOUT_SEC": "120", "SBGW_E2B_LEASE_SEC": "600", "SBGW_E2B_RENEW_INTERVAL_SEC": "120",
	} {
		t.Setenv(key, value)
	}
	loaded, err := Load(filename)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.E2B.APIKey != "synthetic-env-secret" || loaded.E2B.APIURL != "http://127.0.0.1:3000" ||
		loaded.E2B.SandboxURL != "http://127.0.0.1:3002" || loaded.E2B.Domain != "selfhost.example" ||
		loaded.E2B.OwnerID != "gateway-env" || loaded.E2B.StateDir == cfg.E2B.StateDir ||
		loaded.E2B.BootTimeoutSeconds != 120 || loaded.E2B.LeaseSeconds != 600 || loaded.E2B.RenewIntervalSeconds != 120 {
		t.Fatal("E2B environment did not override YAML")
	}
	if len(loaded.E2B.Templates) != 2 || loaded.E2B.Templates[1].TemplateID != "team/full:release" {
		t.Fatal("an environment override silently changed the prepared template profile")
	}
	data, err := json.Marshal(loaded.E2B)
	if err != nil || strings.Contains(string(data), "synthetic-env-secret") {
		t.Fatal("config JSON exposes the provider API key")
	}
}

func TestE2BConfigRejectsUnsafeOrIncompleteDeployment(t *testing.T) {
	cases := map[string]func(*Config){
		"missing key":               func(c *Config) { c.E2B.APIKey = "" },
		"header injection":          func(c *Config) { c.E2B.APIKey = "secret\nheader" },
		"missing owner":             func(c *Config) { c.E2B.OwnerID = "" },
		"owner control character":   func(c *Config) { c.E2B.OwnerID = "owner\nnew" },
		"missing state":             func(c *Config) { c.E2B.StateDir = "" },
		"relative state":            func(c *Config) { c.E2B.StateDir = "relative-state" },
		"root state":                func(c *Config) { c.E2B.StateDir = "/" },
		"state null":                func(c *Config) { c.E2B.StateDir = "/run/state\x00bad" },
		"origin credentials":        func(c *Config) { c.E2B.APIURL = "https://user:synthetic-config-secret@api.example" },
		"origin query":              func(c *Config) { c.E2B.APIURL = "https://api.example?secret=synthetic-config-secret" },
		"origin path":               func(c *Config) { c.E2B.APIURL = "https://api.example/control" },
		"origin scheme":             func(c *Config) { c.E2B.APIURL = "file:///private/file" },
		"proxy origin":              func(c *Config) { c.E2B.SandboxURL = "https://proxy.example/#fragment" },
		"origin port":               func(c *Config) { c.E2B.SandboxURL = "http://proxy.example:65536" },
		"zero boot timeout":         func(c *Config) { c.E2B.BootTimeoutSeconds = 0 },
		"short lease":               func(c *Config) { c.E2B.LeaseSeconds = 29 },
		"late renewal":              func(c *Config) { c.E2B.RenewIntervalSeconds = 1800 },
		"zero renewal":              func(c *Config) { c.E2B.RenewIntervalSeconds = 0 },
		"no templates":              func(c *Config) { c.E2B.Templates = nil },
		"unknown default image":     func(c *Config) { c.Image.Ref = "unapproved-image" },
		"empty image":               func(c *Config) { c.E2B.Templates[0].Image = "" },
		"empty template":            func(c *Config) { c.E2B.Templates[0].TemplateID = "" },
		"duplicate image":           func(c *Config) { c.E2B.Templates[1].Image = c.E2B.Templates[0].Image },
		"zero CPU":                  func(c *Config) { c.E2B.Templates[0].CPUCores = 0 },
		"fractional CPU":            func(c *Config) { c.E2B.Templates[0].CPUCores = 0.5 },
		"NaN CPU":                   func(c *Config) { c.E2B.Templates[0].CPUCores = math.NaN() },
		"infinite CPU":              func(c *Config) { c.E2B.Templates[0].CPUCores = math.Inf(1) },
		"zero memory":               func(c *Config) { c.E2B.Templates[0].MemoryMB = 0 },
		"zero initial capacity":     func(c *Config) { c.E2B.Templates[0].DiskGi = 0 },
		"overflow initial capacity": func(c *Config) { c.E2B.Templates[0].DiskGi = 1<<30 + 1 },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			cfg := configuredE2BForTest(t)
			mutate(cfg)
			err := cfg.validate()
			if err == nil {
				t.Fatal("accepted unsafe or ambiguous E2B configuration")
			}
			if strings.Contains(err.Error(), "synthetic-config-secret") {
				t.Fatal("configuration validation echoes a provider credential")
			}
		})
	}
}

func TestE2BConfigMalformedEnvironmentDurationFailsInsteadOfUsingDefault(t *testing.T) {
	for _, key := range []string{"SBGW_E2B_BOOT_TIMEOUT_SEC", "SBGW_E2B_LEASE_SEC", "SBGW_E2B_RENEW_INTERVAL_SEC"} {
		t.Run(key, func(t *testing.T) {
			t.Setenv(key, "not-a-duration")
			_, err := Load(writeE2BConfigForTest(t, configuredE2BForTest(t)))
			if err == nil {
				t.Fatal("invalid lease or bootstrap bound was silently ignored")
			}
		})
	}
}

func TestE2BConfigHasNoImplicitBaseTemplateOrEnvironmentProfile(t *testing.T) {
	t.Setenv("SBGW_DRIVER", "e2b")
	t.Setenv("SBGW_E2B_API_KEY", "synthetic-key")
	t.Setenv("SBGW_E2B_OWNER_ID", "owner")
	t.Setenv("SBGW_E2B_STATE_DIR", t.TempDir())
	t.Setenv("SBGW_E2B_TEMPLATE_ID", "base")
	if _, err := Load(""); err == nil {
		t.Fatal("unprepared base template bypassed explicit resource profile configuration")
	}
}

func TestE2BResourcesUseSelectedDefaultAndCapsAcrossPreparedProfiles(t *testing.T) {
	cfg := configuredE2BForTest(t)
	cfg.Image.Ref = "approved/small:release"
	resources := cfg.ResourceDefaults()
	cpu, memory, disk, err := resources.Resolve(0, 0, 0)
	if err != nil || cpu != 1 || memory != 512 || disk != 1 || resources.MaxCPUCores != 2 || resources.MaxMemoryMB != 3072 || resources.MaxDiskGi != 20 {
		t.Fatal("default profile and global approved caps were not selected independently")
	}
	if _, _, _, err := resources.Resolve(3, 512, 1); err == nil {
		t.Fatal("unapproved CPU cap accepted")
	}
	if _, _, _, err := resources.Resolve(1, 4096, 1); err == nil {
		t.Fatal("unapproved memory cap accepted")
	}
	if _, _, _, err := resources.Resolve(1, 512, 21); err == nil {
		t.Fatal("unapproved initial-capacity cap accepted")
	}
	// Exact profile matching belongs to the E2B driver, including combinations
	// within these independent upper bounds. This policy never pretends to resize.
}

func TestE2BConfigurationDoesNotChangeDockerOrKubernetesResources(t *testing.T) {
	for _, name := range []string{"docker", "kubernetes"} {
		cfg := configuredE2BForTest(t)
		cfg.Driver = name
		cfg.E2B.APIKey = "" // E2B is irrelevant when another driver is selected.
		if err := cfg.validate(); err != nil {
			t.Fatal(err)
		}
		r := cfg.ResourceDefaults()
		if r.DefaultCPUCores != cfg.K8s.CPUCores || r.DefaultMemoryMB != cfg.K8s.MemoryMB || r.DefaultDiskGi != cfg.K8s.DataDiskGi || r.MaxDiskGi != cfg.K8s.MaxDataDiskGi {
			t.Fatal("E2B profile configuration changed an existing driver")
		}
	}
}

func TestE2BAutoPauseDefaultsOnAndEnvOverrides(t *testing.T) {
	if !Default().E2B.AutoPause {
		t.Fatal("E2B autoPause must default to on")
	}
	cfg := Default()
	t.Setenv("SBGW_E2B_AUTO_PAUSE", "false")
	applyEnv(cfg)
	if cfg.E2B.AutoPause {
		t.Fatal("SBGW_E2B_AUTO_PAUSE=false did not disable autoPause")
	}
}
