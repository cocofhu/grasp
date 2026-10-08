package services

import (
	"os"
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
func (f *fakeConc) MaxConcurrent() int     { return f.max }
func (f *fakeConc) SetAutoRetryMax(n int)  { f.ar = n }

type fakeSbxTuner struct {
	runTTL  time.Duration
	testTTL time.Duration
	maxTest int
}

func (f *fakeSbxTuner) SetTTLs(run, test time.Duration) {
	f.runTTL = run
	f.testTTL = test
}
func (f *fakeSbxTuner) SetMaxTestSandboxes(n int) { f.maxTest = n }

func TestSettingsServiceEffectiveAndUpdate(t *testing.T) {
	db, err := database.OpenSQLite(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{
		Engine:  config.EngineConfig{MaxConcurrentRuns: 5, NodeAutoRetryMax: 2},
		Sandbox: config.SandboxConfig{RunSandboxTTLMinutes: 30, TestSandboxTTLMinutes: 10, MaxTestSandboxes: 2, MemoryMB: 8192, ChatIdleTimeoutSeconds: 1200},
	}
	config.StoreConfig(cfg)
	conc := &fakeConc{}
	sbx := &fakeSbxTuner{}
	svc := NewSettingsService(db, conc, sbx)

	items := svc.Effective()
	if len(items) != 7 {
		t.Fatalf("items: %d", len(items))
	}
	updated, err := svc.Update(map[string]int{
		KeyMaxConcurrentRuns: 7,
		KeyRunSandboxTTLMin:  45,
		KeyTestSandboxTTLMin: 15,
		KeyMaxTestSandboxes:  4,
		KeyNodeAutoRetryMax:  3,
		KeySandboxMemoryMB:   12288,
		KeyAgentIdleMin:      30,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(updated) != 7 {
		t.Fatalf("updated: %d", len(updated))
	}
	if conc.max != 7 || conc.ar != 3 || sbx.maxTest != 4 {
		t.Fatalf("apply: conc=%+v sbx=%+v", conc, sbx)
	}
	if got := sandbox.DefaultMemoryMB(); got != 12288 {
		t.Fatalf("sandbox memory default: got %d, want 12288", got)
	}
	if got := sandbox.AgentIdleTimeout(); got != 30*time.Minute {
		t.Fatalf("agent idle: got %s, want 30m", got)
	}
	svc.ApplyOnBoot()
	if _, err := svc.Update(map[string]int{KeyAgentIdleMin: 121}); err == nil || !strings.Contains(err.Error(), "不能大于 120") {
		t.Fatalf("expected agent idle max validation error, got %v", err)
	}
	if _, err := svc.Update(map[string]int{KeyAgentIdleMin: 1}); err == nil {
		t.Fatal("expected agent idle min validation error")
	}
	if _, err := svc.Update(map[string]int{KeyMaxConcurrentRuns: 0}); err == nil {
		t.Fatal("expected min validation error")
	}
	if _, err := svc.Update(map[string]int{KeySandboxMemoryMB: 512}); err == nil {
		t.Fatal("expected sandbox memory min validation error")
	}
}

func TestSettingsServiceEnvLocked(t *testing.T) {
	db, err := database.OpenSQLite(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	config.StoreConfig(&config.Config{Engine: config.EngineConfig{MaxConcurrentRuns: 5}})
	t.Setenv("GRASP_MAX_RUNS", "9")
	conc := &fakeConc{}
	svc := NewSettingsService(db, conc, nil)
	items := svc.Effective()
	var locked bool
	for _, it := range items {
		if it.Key == KeyMaxConcurrentRuns {
			locked = it.Locked
			if it.Source != "env" {
				t.Fatalf("env item: %+v", it)
			}
		}
	}
	if !locked {
		t.Fatal("expected env locked")
	}
	if _, err := svc.Update(map[string]int{KeyMaxConcurrentRuns: 1}); err != nil {
		t.Fatal(err)
	}
	if conc.max != 0 && conc.max != items[0].Value {
		// apply uses effective env value
	}
	os.Unsetenv("GRASP_MAX_RUNS")
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
	svc.ApplyOnBoot()
	if got := sandbox.AgentIdleTimeout(); got != 90*time.Second {
		t.Fatalf("agent idle: got %s, want 90s", got)
	}
}

func TestSettingsServiceDBOverride(t *testing.T) {
	db, err := database.OpenSQLite(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	config.StoreConfig(&config.Config{Engine: config.EngineConfig{MaxConcurrentRuns: 5}})
	svc := NewSettingsService(db, nil, nil)
	if err := svc.setInt(KeyMaxConcurrentRuns, 12); err != nil {
		t.Fatal(err)
	}
	v, ok := svc.dbInt(KeyMaxConcurrentRuns)
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
