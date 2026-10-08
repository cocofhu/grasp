package services

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/cocofhu/grasp/internal/config"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/sandbox"

	"gorm.io/gorm"
)

// Setting keys (the fixed set of UI-editable platform scheduling knobs).
const (
	KeyMaxConcurrentRuns = "max_concurrent_runs"
	KeyRunSandboxTTLMin  = "run_sandbox_ttl_minutes"
	KeyTestSandboxTTLMin = "test_sandbox_ttl_minutes"
	KeyMaxTestSandboxes  = "max_test_sandboxes"
	KeyNodeAutoRetryMax  = "node_auto_retry_max"
	KeySandboxMemoryMB   = "sandbox_memory_mb"
	KeyAgentIdleMin      = "agent_idle_timeout_minutes"
	KeyBrandProductName  = "brand_product_name"
	KeyBrandHomeSubtitle = "brand_home_subtitle"

	BrandProductNameMaxLength  = 40
	BrandHomeSubtitleMaxLength = 80
)

type BrandSettings struct {
	ProductName  string `json:"product_name"`
	HomeSubtitle string `json:"home_subtitle"`
}

type BrandPatch struct {
	ProductName  *string
	HomeSubtitle *string
}

// ConcurrencyController is the slice of the engine the settings layer drives:
// changing the live max_concurrent_runs. Defined here (consumer side) so the
// engine need not import services.
type ConcurrencyController interface {
	SetMaxConcurrent(n int)
	MaxConcurrent() int
}

// AutoRetryController is the optional slice of the engine that accepts the live
// node auto-retry cap. The concrete engine implements it; the settings layer
// type-asserts its ConcurrencyController to this so no constructor signature
// changes and test fakes that don't implement it are simply skipped.
type AutoRetryController interface {
	SetAutoRetryMax(n int)
}

// SandboxTuner is the slice of the sandbox service the settings layer drives.
type SandboxTuner interface {
	SetTTLs(runTTL, testTTL time.Duration)
	SetMaxTestSandboxes(n int)
}

// SettingsService is the DB override layer for platform scheduling params. It
// resolves effective values (env > DB > config-file > default), persists UI
// edits, and applies them to the running engine / sandbox service.
type SettingsService struct {
	db   *gorm.DB
	conc ConcurrencyController
	sbx  SandboxTuner
}

func NewSettingsService(db *gorm.DB, conc ConcurrencyController, sbx SandboxTuner) *SettingsService {
	return &SettingsService{db: db, conc: conc, sbx: sbx}
}

// knob describes one tunable: how to label it, its floor, its optional env
// lock, and how to read the config-file/default fallback value.
type knob struct {
	key     string
	label   string
	unit    string
	min     int
	max     int    // 0 = no ceiling
	envVar  string // "" when there is no env override for this knob
	fromCfg func(*config.Config) int
}

func knobs() []knob {
	return []knob{
		{KeyMaxConcurrentRuns, "最大并发运行数", "", 1, 0, "GRASP_MAX_RUNS",
			func(c *config.Config) int { return c.Engine.MaxConcurrentRuns }},
		{KeyRunSandboxTTLMin, "运行沙箱保留时长", "分钟", 1, 0, "",
			func(c *config.Config) int { return c.Sandbox.RunSandboxTTLMinutes }},
		{KeyTestSandboxTTLMin, "测试沙箱空闲 TTL", "分钟", 1, 0, "",
			func(c *config.Config) int { return c.Sandbox.TestSandboxTTLMinutes }},
		{KeyMaxTestSandboxes, "最大测试沙箱数", "", 1, 0, "",
			func(c *config.Config) int { return c.Sandbox.MaxTestSandboxes }},
		{KeyNodeAutoRetryMax, "节点自动重试次数", "次", 0, 0, "GRASP_NODE_AUTO_RETRY",
			func(c *config.Config) int { return c.Engine.NodeAutoRetryMax }},
		{KeySandboxMemoryMB, "沙箱内存上限", "MiB", 1024, 0, "GRASP_SANDBOX_MEMORY_MB",
			func(c *config.Config) int { return c.Sandbox.MemoryMB }},
		{KeyAgentIdleMin, "Agent 无动作时限", "分钟", 2, 120, "GRASP_CHAT_IDLE_SEC",
			func(c *config.Config) int { return (c.Sandbox.ChatIdleTimeoutSeconds + 59) / 60 }},
	}
}

// SettingItem is the API shape for one effective knob.
type SettingItem struct {
	Key    string `json:"key"`
	Label  string `json:"label"`
	Unit   string `json:"unit,omitempty"`
	Value  int    `json:"value"`
	Min    int    `json:"min"`
	Max    int    `json:"max,omitempty"`
	Source string `json:"source"` // env | db | config
	Locked bool   `json:"locked"` // true when pinned by an env var (UI read-only)
}

// Effective returns every knob's current effective value with provenance.
func (s *SettingsService) Effective() []SettingItem {
	cfg := config.GetConfig()
	out := make([]SettingItem, 0, len(knobs()))
	for _, k := range knobs() {
		val, src, locked := s.resolve(k, cfg)
		out = append(out, SettingItem{
			Key: k.key, Label: k.label, Unit: k.unit, Value: val,
			Min: k.min, Max: k.max, Source: src, Locked: locked,
		})
	}
	return out
}

// Brand returns normalized instance-level brand overrides. Empty values mean
// callers should use their built-in, locale-aware defaults.
func (s *SettingsService) Brand() BrandSettings {
	name := strings.TrimSpace(s.dbString(KeyBrandProductName))
	return BrandSettings{
		ProductName:  name,
		HomeSubtitle: s.dbString(KeyBrandHomeSubtitle),
	}
}

// resolve applies the precedence env > DB > config(file/default) for one knob.
// The config snapshot already folds env over file over default, so when a knob
// is env-locked its config value is exactly the env value.
func (s *SettingsService) resolve(k knob, cfg *config.Config) (value int, source string, locked bool) {
	cfgVal := k.fromCfg(cfg)
	if k.envVar != "" && os.Getenv(k.envVar) != "" {
		return cfgVal, "env", true
	}
	if v, ok := s.dbInt(k.key); ok {
		return v, "db", false
	}
	return cfgVal, "config", false
}

// Update validates and persists the given patch (only keys present are
// changed), skipping env-locked knobs, then applies the new effective values
// to the running engine / sandbox service.
func (s *SettingsService) Update(patch map[string]int) ([]SettingItem, error) {
	return s.UpdateWithBrand(patch, BrandPatch{})
}

// UpdateWithBrand validates the complete patch before writing, then persists
// scheduling and brand values in one transaction.
func (s *SettingsService) UpdateWithBrand(patch map[string]int, brand BrandPatch) ([]SettingItem, error) {
	for _, k := range knobs() {
		v, ok := patch[k.key]
		if !ok {
			continue
		}
		if v < k.min {
			return nil, fmt.Errorf("%s 不能小于 %d", k.label, k.min)
		}
		if k.max > 0 && v > k.max {
			return nil, fmt.Errorf("%s 不能大于 %d", k.label, k.max)
		}
	}
	if err := validateBrandPatch(brand); err != nil {
		return nil, err
	}
	if err := s.db.Transaction(func(tx *gorm.DB) error {
		for _, k := range knobs() {
			v, ok := patch[k.key]
			if !ok || (k.envVar != "" && os.Getenv(k.envVar) != "") {
				continue
			}
			if err := setSetting(tx, k.key, strconv.Itoa(v)); err != nil {
				return err
			}
		}
		if brand.ProductName != nil {
			if err := setSetting(tx, KeyBrandProductName, strings.TrimSpace(*brand.ProductName)); err != nil {
				return err
			}
		}
		if brand.HomeSubtitle != nil {
			if err := setSetting(tx, KeyBrandHomeSubtitle, strings.TrimSpace(*brand.HomeSubtitle)); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		return nil, err
	}
	s.apply()
	return s.Effective(), nil
}

func validateBrandPatch(patch BrandPatch) error {
	if patch.ProductName != nil && len([]rune(strings.TrimSpace(*patch.ProductName))) > BrandProductNameMaxLength {
		return fmt.Errorf("产品名不能超过 %d 个字符", BrandProductNameMaxLength)
	}
	if patch.HomeSubtitle != nil && len([]rune(strings.TrimSpace(*patch.HomeSubtitle))) > BrandHomeSubtitleMaxLength {
		return fmt.Errorf("首页副标题不能超过 %d 个字符", BrandHomeSubtitleMaxLength)
	}
	return nil
}

// ApplyOnBoot pushes the persisted (DB) overrides onto the runtime components
// at startup, so UI-configured values survive a restart.
func (s *SettingsService) ApplyOnBoot() { s.apply() }

// apply computes the effective values and drives the runtime components.
func (s *SettingsService) apply() {
	m := map[string]int{}
	idleFromCfg := true
	for _, it := range s.Effective() {
		m[it.Key] = it.Value
		if it.Key == KeyAgentIdleMin {
			idleFromCfg = it.Source != "db"
		}
	}
	if s.conc != nil {
		s.conc.SetMaxConcurrent(m[KeyMaxConcurrentRuns])
		// The concrete engine also accepts the live node auto-retry cap.
		if ar, ok := s.conc.(AutoRetryController); ok {
			ar.SetAutoRetryMax(m[KeyNodeAutoRetryMax])
		}
	}
	if s.sbx != nil {
		s.sbx.SetTTLs(
			time.Duration(m[KeyRunSandboxTTLMin])*time.Minute,
			time.Duration(m[KeyTestSandboxTTLMin])*time.Minute,
		)
		s.sbx.SetMaxTestSandboxes(m[KeyMaxTestSandboxes])
	}
	sandbox.SetDefaultMemoryMB(m[KeySandboxMemoryMB])
	if idleFromCfg {
		// The config value is in seconds; keep it exact.
		sandbox.SetAgentIdleTimeout(config.GetConfig().ChatIdleTimeout())
	} else {
		sandbox.SetAgentIdleTimeout(time.Duration(m[KeyAgentIdleMin]) * time.Minute)
	}
}

func (s *SettingsService) dbInt(key string) (int, bool) {
	var row models.Setting
	if err := s.db.First(&row, "key = ?", key).Error; err != nil {
		return 0, false
	}
	v, err := strconv.Atoi(strings.TrimSpace(row.Value))
	if err != nil {
		return 0, false
	}
	return v, true
}

func (s *SettingsService) setInt(key string, v int) error {
	// Key is the primary key, so Save upserts (update when present, else insert).
	return setSetting(s.db, key, strconv.Itoa(v))
}

func (s *SettingsService) dbString(key string) string {
	var row models.Setting
	if err := s.db.First(&row, "key = ?", key).Error; err != nil {
		return ""
	}
	return strings.TrimSpace(row.Value)
}

func setSetting(db *gorm.DB, key, value string) error {
	return db.Save(&models.Setting{Key: key, Value: value, UpdatedAt: time.Now()}).Error
}
