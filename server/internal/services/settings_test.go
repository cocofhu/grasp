package services

import (
	"strings"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/config"
	"github.com/cocofhu/grasp/internal/database"
	"github.com/cocofhu/grasp/internal/sandbox"
)

type fakeConc struct {
	max int
	ar  int
}

func (f *fakeConc) SetMaxConcurrent(n int) { f.max = n }
func (f *fakeConc) SetAutoRetryMax(n int)  { f.ar = n }

type fakeSbxTuner struct {
	idleTTL time.Duration
	max     int
}

func (f *fakeSbxTuner) SetIdleTTL(ttl time.Duration) { f.idleTTL = ttl }
func (f *fakeSbxTuner) SetMaxSandboxes(n int)        { f.max = n }

func TestSettingsServiceEffectiveAndUpdate(t *testing.T) {
	db, err := database.OpenSQLite(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{
		Engine:  config.EngineConfig{NodeAutoRetryMax: 2},
		Sandbox: config.SandboxConfig{IdleTTLMinutes: 60, MaxSandboxes: 15, MemoryMB: 8192, ChatIdleTimeoutSeconds: 1200},
	}
	config.StoreConfig(cfg)
	conc := &fakeConc{}
	sbx := &fakeSbxTuner{}
	svc := NewSettingsService(db, conc, sbx)

	items := svc.Effective()
	if len(items) != 5 {
		t.Fatalf("items: %d", len(items))
	}
	updated, err := svc.UpdateWithBrand(map[string]int{
		KeySandboxIdleTTLMin: 45,
		KeyMaxSandboxes:      8,
		KeyNodeAutoRetryMax:  3,
		KeySandboxMemoryMB:   12288,
		KeyAgentIdleMin:      30,
	}, BrandPatch{})

	if err != nil {
		t.Fatal(err)
	}
	if len(updated) != 5 {
		t.Fatalf("updated: %d", len(updated))
	}
	if conc.max != 8 || conc.ar != 3 || sbx.max != 8 || sbx.idleTTL != 45*time.Minute {
		t.Fatalf("apply: conc=%+v sbx=%+v", conc, sbx)
	}
	if got := sandbox.DefaultMemoryMB(); got != 12288 {
		t.Fatalf("sandbox memory default: got %d, want 12288", got)
	}
	if got := sandbox.AgentIdleTimeout(); got != 30*time.Minute {
		t.Fatalf("agent idle: got %s, want 30m", got)
	}
	if err := svc.ApplyOnBoot(); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.UpdateWithBrand(map[string]int{KeyAgentIdleMin: 121}, BrandPatch{}); err == nil || !strings.Contains(err.Error(), "不能大于 120") {
		t.Fatalf("expected agent idle max validation error, got %v", err)
	}
	if _, err := svc.UpdateWithBrand(map[string]int{KeyAgentIdleMin: 1}, BrandPatch{}); err == nil {
		t.Fatal("expected agent idle min validation error")
	}
	if _, err := svc.UpdateWithBrand(map[string]int{KeyMaxSandboxes: 0}, BrandPatch{}); err == nil {
		t.Fatal("expected sandbox min validation error")
	}
	if _, err := svc.UpdateWithBrand(map[string]int{KeySandboxMemoryMB: 512}, BrandPatch{}); err == nil {
		t.Fatal("expected sandbox memory min validation error")
	}
	if _, err := svc.UpdateWithBrand(map[string]int{KeySandboxMemoryMB: 0}, BrandPatch{}); err != nil {
		t.Fatal(err)
	}
	if err := svc.ApplyOnBoot(); err != nil {
		t.Fatal(err)
	}
	if sandbox.DefaultMemoryMB() != 0 {
		t.Fatal("saved zero did not clear the memory override on update and restart")
	}

}

func TestSettingsServiceEnvLocked(t *testing.T) {
	db, err := database.OpenSQLite(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	config.StoreConfig(&config.Config{Engine: config.EngineConfig{NodeAutoRetryMax: 5}})
	t.Setenv("GRASP_NODE_AUTO_RETRY", "5")
	conc := &fakeConc{}
	svc := NewSettingsService(db, conc, nil)
	items := svc.Effective()
	var locked bool
	for _, it := range items {
		if it.Key == KeyNodeAutoRetryMax {
			locked = it.Locked
			if it.Source != "env" {
				t.Fatalf("env item: %+v", it)
			}
		}
	}
	if !locked {
		t.Fatal("expected env locked")
	}
	if _, err := svc.UpdateWithBrand(map[string]int{KeyNodeAutoRetryMax: 1}, BrandPatch{}); err != nil {
		t.Fatal(err)
	}
	if conc.ar != 5 {
		t.Fatalf("apply should keep env value, got %d", conc.ar)
	}
}

func TestSettingsServiceAgentIdleFromConfigKeepsSeconds(t *testing.T) {
	db, err := database.OpenSQLite(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	config.StoreConfig(&config.Config{Sandbox: config.SandboxConfig{ChatIdleTimeoutSeconds: 90}})
	t.Setenv("GRASP_CHAT_IDLE_SEC", "90")
	svc := NewSettingsService(db, nil, nil)
	for _, it := range svc.Effective() {
		if it.Key == KeyAgentIdleMin && (it.Value != 2 || !it.Locked || it.Max != 120) {
			t.Fatalf("idle item: %+v", it)
		}
	}
	if err := svc.ApplyOnBoot(); err != nil {
		t.Fatal(err)
	}
	if got := sandbox.AgentIdleTimeout(); got != 90*time.Second {
		t.Fatalf("agent idle: got %s, want 90s", got)
	}
}

func TestSettingsServiceDBOverride(t *testing.T) {
	db, err := database.OpenSQLite(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	config.StoreConfig(&config.Config{Sandbox: config.SandboxConfig{MaxSandboxes: 5}})
	svc := NewSettingsService(db, nil, nil)
	if err := svc.setInt(KeyMaxSandboxes, 12); err != nil {
		t.Fatal(err)
	}
	v, ok := svc.dbInt(KeyMaxSandboxes)
	if !ok || v != 12 {
		t.Fatalf("dbInt: %v %v", v, ok)
	}
	if v, ok := svc.dbInt("missing"); ok {
		t.Fatalf("missing key: %v", v)
	}
	if err := svc.setInt("bad-num", 0); err != nil {
		t.Fatal(err)
	}
	if _, ok := svc.dbInt("bad-num"); !ok {
		t.Fatal("expected stored int")
	}
}

func TestSettingsServiceBrandUpdateFallbackAndValidation(t *testing.T) {
	db, err := database.OpenSQLite(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	config.StoreConfig(&config.Config{})
	svc := NewSettingsService(db, nil, nil)
	name, subtitle := "  Acme Flow  ", "  Clarify before coding  "
	if _, err := svc.UpdateWithBrand(nil, BrandPatch{
		ProductName: &name, HomeSubtitle: &subtitle,
	}); err != nil {
		t.Fatal(err)
	}
	if got := svc.Brand(); got.ProductName != "Acme Flow" || got.HomeSubtitle != "Clarify before coding" {
		t.Fatalf("brand: %+v", got)
	}

	blank := "　 "
	if _, err := svc.UpdateWithBrand(nil, BrandPatch{ProductName: &blank}); err != nil {
		t.Fatal(err)
	}
	if got := svc.Brand().ProductName; got != "" {
		t.Fatalf("blank product name should mean fallback, got %q", got)
	}

	tooLong := strings.Repeat("界", BrandProductNameMaxLength+1)
	before := svc.Brand()
	if _, err := svc.UpdateWithBrand(nil, BrandPatch{ProductName: &tooLong}); err == nil {
		t.Fatal("expected product name length error")
	}
	if got := svc.Brand(); got != before {
		t.Fatalf("invalid patch changed brand: before=%+v after=%+v", before, got)
	}
}
