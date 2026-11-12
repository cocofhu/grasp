package config

import (
	"math"
	"strings"
	"testing"
)

const e2bYAML = `driver: e2b
e2b:
  apiURL: https://api.example.test
  sandboxURL: https://sandbox.example.test
  apiKey: test-key
  ownerID: test-owner
  stateDir: /var/lib/gateway/e2b
  templates:
    standard: {templateID: template-one, cpuCores: 2, memoryMB: 3072, diskGi: 20}
`

func TestE2BSelectedTemplateResources(t *testing.T) {
	c, err := loadYAML(t, e2bYAML)
	if err != nil {
		t.Fatal(err)
	}
	if c.DefaultImage() != "standard" || c.Docker != nil || c.Kubernetes != nil || !c.E2B.AutoPause {
		t.Fatal("wrong backend defaults")
	}
	c.E2B.Templates["large"] = E2BTemplateConfig{TemplateID: "template-two", CPUCores: 8, MemoryMB: 16384, DiskGi: 80}
	for _, name := range []string{"standard", "large"} {
		cpu, mem, disk, err := c.ResourcePolicy().Resolve(name, 0, 0, 0)
		want := c.E2B.Templates[name]
		if err != nil || cpu != want.CPUCores || mem != want.MemoryMB || disk != want.DiskGi {
			t.Fatalf("selected template defaults wrong: %v", err)
		}
		if c.EnvironmentProfiles()[name].Image != name {
			t.Fatal("discovery lost template name")
		}
		if _, _, _, err = c.ResourcePolicy().Resolve(name, cpu, mem, disk); err != nil {
			t.Fatal(err)
		}
		if _, _, _, err = c.ResourcePolicy().Resolve(name, 0, 8192, 0); err == nil {
			t.Fatal("incompatible memory accepted")
		}
	}
	if names := c.TemplateNames(); len(names) != 2 || names[0] != "large" {
		t.Fatal("unordered template names")
	}
	for _, cpu := range []float64{-1, math.NaN(), math.Inf(1)} {
		if _, _, _, err := c.ResourcePolicy().Resolve("standard", cpu, 0, 0); err == nil {
			t.Fatal("invalid CPU accepted")
		}
	}
	if _, _, _, err := c.ResourcePolicy().Resolve("missing", 0, 0, 0); err == nil {
		t.Fatal("unknown template accepted")
	}
	if _, err := loadYAML(t, e2bYAML+"    large: {templateID: two, cpuCores: 4, memoryMB: 8192, diskGi: 25}\n"); err == nil {
		t.Fatal("multiple templates require defaultTemplate")
	}
	if _, err := loadYAML(t, e2bYAML+"  defaultTemplate: standard\n    "); err != nil {
		t.Fatal(err)
	}
}
func TestE2BValidation(t *testing.T) {
	for name, change := range map[string]func(*E2BConfig){
		"api key": func(e *E2BConfig) { e.APIKey = "" }, "header": func(e *E2BConfig) { e.APIKey = "secret\nvalue" },
		"owner": func(e *E2BConfig) { e.OwnerID = "" }, "state": func(e *E2BConfig) { e.StateDir = "relative" }, "root": func(e *E2BConfig) { e.StateDir = "/" },
		"API": func(e *E2BConfig) { e.APIURL = "" }, "origin": func(e *E2BConfig) { e.APIURL = "https://user:secret@api.example/path" }, "port": func(e *E2BConfig) { e.SandboxURL = "http://proxy:65536" },
		"boot": func(e *E2BConfig) { e.BootTimeoutSeconds = 0 }, "overflow": func(e *E2BConfig) { e.LeaseSeconds = math.MaxInt }, "lease": func(e *E2BConfig) { e.LeaseSeconds = 29 }, "renewal": func(e *E2BConfig) { e.RenewIntervalSeconds = e.LeaseSeconds / 2 },
		"missing": func(e *E2BConfig) { e.Templates = nil }, "default": func(e *E2BConfig) { e.DefaultTemplate = "missing" },
		"template": func(e *E2BConfig) {
			e.Templates["bad"] = E2BTemplateConfig{TemplateID: "x", CPUCores: .5, MemoryMB: 1, DiskGi: 1}
		},
	} {
		t.Run(name, func(t *testing.T) {
			c, err := loadYAML(t, e2bYAML)
			if err != nil {
				t.Fatal(err)
			}
			change(c.E2B)
			if err = c.validate(); err == nil || strings.Contains(err.Error(), "secret") {
				t.Fatal("invalid configuration accepted or leaked")
			}
		})
	}
}
func TestE2BEnvironmentAndCloudOrigins(t *testing.T) {
	for k, v := range map[string]string{"SBGW_E2B_API_URL": "https://api.e2b.app", "SBGW_E2B_SANDBOX_URL": "https://proxy.example", "SBGW_E2B_API_KEY": "test-key", "SBGW_E2B_OWNER_ID": "new-owner", "SBGW_E2B_STATE_DIR": "/var/lib/e2b", "SBGW_E2B_BOOT_TIMEOUT_SEC": "120", "SBGW_E2B_LEASE_SEC": "1800", "SBGW_E2B_RENEW_INTERVAL_SEC": "60", "SBGW_E2B_AUTO_PAUSE": "false"} {
		t.Setenv(k, v)
	}
	c, err := loadYAML(t, e2bYAML)
	if err != nil {
		t.Fatal(err)
	}
	if c.E2B.APIURL != "https://api.e2b.app" || c.E2B.AutoPause || c.E2B.LeaseSeconds != 1800 || c.E2B.OwnerID != "new-owner" {
		t.Fatal("overrides not applied")
	}
	c.E2B.SandboxURL = ""
	if err := c.validate(); err != nil {
		t.Fatal("provider-returned domain must be supported")
	}
}
