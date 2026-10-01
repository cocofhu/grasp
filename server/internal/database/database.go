// Package database opens the configured store (SQLite or MySQL), runs
// migrations, and seeds initial demo data so a fresh boot mirrors the frontend
// prototype.
package database

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/cocofhu/grasp/internal/config"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/tokenledger"

	"github.com/rs/zerolog/log"
	"gorm.io/driver/mysql"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// Open opens the database for the configured driver and migrates the schema.
// SQLite is the zero-dependency default; MySQL is used when configured (driver
// == "mysql" or a DSN is present, resolved in config.setDefaults).
func Open(cfg config.DatabaseConfig) (*gorm.DB, error) {
	switch cfg.Driver {
	case "mysql":
		return openMySQL(cfg.DSN)
	default:
		return OpenSQLite(cfg.Path)
	}
}

// OpenSQLite opens (or creates) the SQLite database at path. WAL + a busy
// timeout plus a single writer connection keep the concurrent FSM goroutines
// and API readers from tripping over SQLite write locks. Exported so tests can
// spin up a file/memory DB without constructing a full DatabaseConfig.
func OpenSQLite(path string) (*gorm.DB, error) {
	if path != "" && path != ":memory:" && !strings.HasPrefix(path, "file:") {
		display := path
		if abs, absErr := filepath.Abs(path); absErr == nil {
			display = abs
		}
		_, err := os.Stat(path)
		status := "new"
		if err == nil {
			status = "existing"
		}
		log.Info().Str("path", display).Str("database", status).Msg("opening sqlite database")
	}
	db, err := openSQLiteConn(path)
	if err != nil {
		return nil, err
	}
	return finalize(db)
}

var (
	sqliteTemplateOnce sync.Once
	sqliteTemplate     []byte
	sqliteTemplateErr  error
)

// OpenSQLiteTest opens a migrated SQLite DB at path by cloning a process-wide
// schema template. Unit tests that open a fresh DB per case should prefer this
// over OpenSQLite: AutoMigrate of the full model set is multi-second on cold
// disks and routinely pushes engine/handlers packages near the default 10m
// go-test timeout when every case pays that cost.
func OpenSQLiteTest(path string) (*gorm.DB, error) {
	if err := ensureSQLiteTemplate(); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	if err := os.WriteFile(path, sqliteTemplate, 0o644); err != nil {
		return nil, err
	}
	return openSQLiteConn(path)
}

func ensureSQLiteTemplate() error {
	sqliteTemplateOnce.Do(func() {
		dir, err := os.MkdirTemp("", "grasp-schema-*")
		if err != nil {
			sqliteTemplateErr = err
			return
		}
		defer os.RemoveAll(dir)
		path := filepath.Join(dir, "template.db")
		// Build the template with DELETE journal so the schema lives in a single
		// file (no -wal/-shm) and a byte-for-byte copy is a complete DB.
		db, err := openSQLiteConn(path + "?_journal_mode=DELETE&_busy_timeout=5000&_foreign_keys=on")
		if err != nil {
			sqliteTemplateErr = err
			return
		}
		if _, err := finalize(db); err != nil {
			sqliteTemplateErr = err
			_ = closeGorm(db)
			return
		}
		if err := closeGorm(db); err != nil {
			sqliteTemplateErr = err
			return
		}
		sqliteTemplate, sqliteTemplateErr = os.ReadFile(path)
	})
	return sqliteTemplateErr
}

func closeGorm(db *gorm.DB) error {
	sqlDB, err := db.DB()
	if err != nil {
		return err
	}
	return sqlDB.Close()
}

func openSQLiteConn(path string) (*gorm.DB, error) {
	dsn := path
	if !strings.Contains(dsn, "?") {
		dsn += "?_journal_mode=WAL&_busy_timeout=5000&_foreign_keys=on"
	}
	db, err := gorm.Open(sqlite.Open(dsn), gormConfig())
	if err != nil {
		return nil, err
	}
	sqlDB, err := db.DB()
	if err != nil {
		return nil, err
	}
	// Serialize writes: SQLite allows only one writer; a single connection
	// avoids "database is locked" stalls under concurrent runs.
	sqlDB.SetMaxOpenConns(1)
	return db, nil
}

// openMySQL opens the MySQL database at dsn. Unlike SQLite it supports genuine
// concurrent writers, so the FSM goroutines each get their own pooled
// connection instead of being serialized behind a single one.
func openMySQL(dsn string) (*gorm.DB, error) {
	if strings.TrimSpace(dsn) == "" {
		return nil, fmt.Errorf("mysql driver selected but database.dsn is empty")
	}
	db, err := gorm.Open(mysql.Open(dsn), gormConfig())
	if err != nil {
		return nil, err
	}
	sqlDB, err := db.DB()
	if err != nil {
		return nil, err
	}
	sqlDB.SetMaxOpenConns(20)
	sqlDB.SetMaxIdleConns(10)
	sqlDB.SetConnMaxLifetime(time.Hour)
	return finalize(db)
}

func gormConfig() *gorm.Config {
	return &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)}
}

// finalize runs the shared post-open steps (migrate + backfill) for any driver.
func finalize(db *gorm.DB) (*gorm.DB, error) {
	if err := db.AutoMigrate(models.AllModels()...); err != nil {
		return nil, err
	}
	dropNotificationReadPrefs(db)
	migrateProjectMemoryIndexes(db)
	migrateChannelMultiPerProject(db)
	backfillChannelPrimaryAndAgent(db)
	backfillNotifyPolicyChannelIDs(db)
	backfillLegacyProjectMemories(db)
	backfillWorkflowIDs(db)
	backfillGateShareLinkKind(db)
	ensureDefaultProject(db)
	if err := tokenledger.BackfillOnce(db); err != nil {
		return nil, fmt.Errorf("token ledger backfill: %w", err)
	}
	return db, nil
}

// dropNotificationReadPrefs removes the #427 one-row-per-user JSON array table.
// AutoMigrate will not drop leftover tables; this is a hard cut (no JSON→row migrate).
func dropNotificationReadPrefs(db *gorm.DB) {
	_ = db.Exec("DROP TABLE IF EXISTS notification_read_prefs").Error
}

func backfillGateShareLinkKind(db *gorm.DB) {
	_ = db.Exec(
		"UPDATE gate_share_links SET kind = ? WHERE kind IS NULL OR kind = ''",
		models.ShareLinkKindHumanGate,
	).Error
}

// migrateProjectMemoryIndexes drops legacy unique indexes that predate
// agent_name scoping (project_id, title) so AutoMigrate can create
// idx_pm_mem_proj_agent_title cleanly.
func migrateProjectMemoryIndexes(db *gorm.DB) {
	legacy := []string{
		"idx_pm_mem_proj_title",
		"idx_project_memory_items_project_id_title",
		"udx_project_memory_items_project_id_title",
	}
	switch db.Dialector.Name() {
	case "mysql":
		for _, name := range legacy {
			_ = db.Exec("DROP INDEX `" + name + "` ON `project_memory_items`").Error
		}
	default:
		for _, name := range legacy {
			_ = db.Exec("DROP INDEX IF EXISTS `" + name + "`").Error
		}
	}
}

// migrateChannelMultiPerProject drops the legacy "one channel per project"
// unique index and adds multi-channel constraints. Must NOT collapse or
// delete secondary rows (the old migrateChannelUniqueProject did that).
func migrateChannelMultiPerProject(db *gorm.DB) {
	switch db.Dialector.Name() {
	case "mysql":
		_ = db.Exec("DROP INDEX udx_channel_configs_project_id ON channel_configs").Error
		// MySQL 8 functional unique indexes: NULL expression values do not
		// collide, so secondaries (is_primary=0) and empty agent_name are free.
		// Best-effort — older MySQL falls back to ChannelConfigService tx locks.
		_ = db.Exec(`CREATE UNIQUE INDEX udx_channel_configs_primary ON channel_configs
			((CASE WHEN is_primary = 1 THEN project_id ELSE NULL END))`).Error
		_ = db.Exec(`CREATE UNIQUE INDEX udx_channel_configs_agent_name ON channel_configs
			((CASE WHEN agent_name IS NOT NULL AND agent_name != '' THEN agent_name ELSE NULL END))`).Error
	default:
		_ = db.Exec("DROP INDEX IF EXISTS udx_channel_configs_project_id").Error
		// Partial uniques: empty agent_name allowed during backfill; only one
		// primary (is_primary=1) per project.
		_ = db.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS udx_channel_configs_agent_name
			ON channel_configs (agent_name) WHERE agent_name != '' AND agent_name IS NOT NULL`).Error
		_ = db.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS udx_channel_configs_primary
			ON channel_configs (project_id) WHERE is_primary = 1`).Error
	}
}

// channelPmMCPAllowed mirrors services.FilterPmEnabledMcps without importing
// services (database must not depend on that package).
var channelPmMCPAllowed = map[string]bool{
	"pm-progress": true, "pm-workflow-read": true, "pm-workflow-write": true,
	"pm-agent-fs": true, "pm-prd-manager": true,
}

// snapshotChannelEnabledMcps copies Project.PmEnabledMcps onto a legacy Channel
// whose EnabledMcps is still nil. nil project → nil channel (both mean
// platform defaults). Non-nil project (incl. explicit empty) → filtered copy
// so channel turns keep the pre-upgrade GetBinding() permission set.
func snapshotChannelEnabledMcps(projectMcps []string) []string {
	if projectMcps == nil {
		return nil
	}
	var out []string
	seen := map[string]bool{}
	for _, id := range projectMcps {
		id = strings.TrimSpace(id)
		if !channelPmMCPAllowed[id] || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	if out == nil {
		return []string{}
	}
	return out
}

// backfillChannelPrimaryAndAgent marks each project's earliest channel as
// primary (when none is marked), binds empty AgentName to the project's
// PmLeaderAgent when available, and copies Project.PmEnabledMcps onto Channels
// whose EnabledMcps is still nil (upgrade-compat for channel-turn MCP scope).
// Existing credentials/fields are preserved.
func backfillChannelPrimaryAndAgent(db *gorm.DB) {
	var rows []models.ChannelConfig
	if err := db.Order("project_id asc, created_at asc, id asc").Find(&rows).Error; err != nil {
		return
	}
	type projState struct {
		hasPrimary bool
		firstID    string
	}
	byProj := map[string]*projState{}
	for _, r := range rows {
		st := byProj[r.ProjectID]
		if st == nil {
			st = &projState{firstID: r.ID}
			byProj[r.ProjectID] = st
		}
		if r.IsPrimary {
			st.hasPrimary = true
		}
	}
	for pid, st := range byProj {
		if st.hasPrimary || st.firstID == "" {
			continue
		}
		if err := db.Model(&models.ChannelConfig{}).Where("id = ?", st.firstID).
			Update("is_primary", true).Error; err != nil {
			log.Warn().Err(err).Str("channel", st.firstID).Str("project", pid).
				Msg("channel backfill: failed to mark primary")
		}
	}
	// Bind empty agent_name → project PmLeaderAgent and snapshot EnabledMcps.
	var projects []models.Project
	if err := db.Select("id", "pm_leader_agent", "pm_enabled_mcps").Find(&projects).Error; err != nil {
		return
	}
	pmByID := map[string]string{}
	mcpsByID := map[string][]string{}
	for _, p := range projects {
		if a := strings.TrimSpace(p.PmLeaderAgent); a != "" {
			pmByID[p.ID] = a
		}
		// Preserve nil vs non-nil: only non-nil project lists are snapshotted.
		if p.PmEnabledMcps != nil {
			mcpsByID[p.ID] = snapshotChannelEnabledMcps(p.PmEnabledMcps)
		}
	}
	// Track agents already claimed so we do not violate udx_channel_configs_agent_name.
	claimed := map[string]bool{}
	for _, r := range rows {
		if a := strings.TrimSpace(r.AgentName); a != "" {
			claimed[a] = true
		}
	}
	for _, r := range rows {
		if strings.TrimSpace(r.AgentName) != "" {
			continue
		}
		agent := pmByID[r.ProjectID]
		if agent == "" || claimed[agent] {
			continue
		}
		if err := db.Model(&models.ChannelConfig{}).Where("id = ?", r.ID).
			Update("agent_name", agent).Error; err != nil {
			log.Warn().Err(err).Str("channel", r.ID).Str("agent", agent).
				Msg("channel backfill: failed to bind default agent")
			continue
		}
		claimed[agent] = true
	}
	// EnabledMcps nil → copy project PmEnabledMcps snapshot (at least primary;
	// apply to every nil channel so secondary leftovers stay consistent).
	for _, r := range rows {
		if r.EnabledMcps != nil {
			continue
		}
		snap, ok := mcpsByID[r.ProjectID]
		if !ok {
			// Project also nil → leave channel nil (Effective = defaults).
			continue
		}
		// Must use Updates(struct) so serializer:json persists valid JSON
		// (column Update with []string would store Go fmt text).
		if err := db.Model(&models.ChannelConfig{}).Where("id = ?", r.ID).
			Select("EnabledMcps").
			Updates(models.ChannelConfig{EnabledMcps: snap}).Error; err != nil {
			log.Warn().Err(err).Str("channel", r.ID).
				Msg("channel backfill: failed to snapshot enabled_mcps")
		}
	}
}

// backfillNotifyPolicyChannelIDs migrates legacy single-target notify semantics:
// enabled notify → select the primary channel; disabled → empty ChannelIDs.
// Skips projects that already have an explicit ChannelIDs list (incl. empty
// after a deliberate save — we only backfill when the JSON field is absent /
// null-equivalent: ChannelIDs == nil).
func backfillNotifyPolicyChannelIDs(db *gorm.DB) {
	var projects []models.Project
	if err := db.Find(&projects).Error; err != nil {
		return
	}
	for _, p := range projects {
		pol := p.NotifyPolicy
		if pol.ChannelIDs != nil {
			continue
		}
		if !pol.IsEnabled() {
			empty := []string{}
			pol.ChannelIDs = empty
			_ = db.Model(&models.Project{}).Where("id = ?", p.ID).
				Update("notify_policy", pol).Error
			continue
		}
		var primary models.ChannelConfig
		err := db.Where("project_id = ? AND is_primary = ?", p.ID, true).
			Order("created_at asc").First(&primary).Error
		if err != nil {
			// Fall back to earliest channel when primary flag not yet set.
			err = db.Where("project_id = ?", p.ID).Order("created_at asc").First(&primary).Error
		}
		if err != nil {
			pol.ChannelIDs = []string{}
		} else {
			pol.ChannelIDs = []string{primary.ID}
		}
		if err := db.Model(&models.Project{}).Where("id = ?", p.ID).
			Update("notify_policy", pol).Error; err != nil {
			log.Warn().Err(err).Str("project", p.ID).Msg("notify policy channelIds backfill failed")
		}
	}
}

// backfillLegacyProjectMemories assigns empty agent_name memories to each
// project's bound PM Leader agent once at startup (not on every GetBinding).
func backfillLegacyProjectMemories(db *gorm.DB) {
	var projects []models.Project
	if err := db.Where("pm_leader_agent != ? AND pm_leader_agent IS NOT NULL", "").Find(&projects).Error; err != nil {
		return
	}
	for _, p := range projects {
		agent := strings.TrimSpace(p.PmLeaderAgent)
		if agent == "" {
			continue
		}
		_ = db.Model(&models.ProjectMemoryItem{}).
			Where("project_id = ? AND (agent_name = '' OR agent_name IS NULL)", p.ID).
			Update("agent_name", agent).Error
	}
}

// ensureDefaultProject creates「默认项目」when none exist and backfills any
// workflow_defs rows still missing a project_id. The default project has no
// special protection afterward (rename/delete-when-empty like any other).
func ensureDefaultProject(db *gorm.DB) {
	var count int64
	db.Model(&models.Project{}).Count(&count)
	if count == 0 {
		now := time.Now()
		_ = db.Create(&models.Project{
			ID:           models.DefaultProjectID,
			Name:         models.DefaultProjectName,
			SandboxEnv:   []models.EnvEntry{},
			Variables:    []models.ProjectVariable{},
			NotifyPolicy: models.DefaultProjectNotifyPolicy(),
			CreatedAt:    now,
			UpdatedAt:    now,
		}).Error
	}
	var defaultID string
	var p models.Project
	if err := db.Where("id = ?", models.DefaultProjectID).First(&p).Error; err == nil {
		defaultID = p.ID
	} else if err := db.Order("created_at asc").First(&p).Error; err == nil {
		defaultID = p.ID
	}
	if defaultID == "" {
		return
	}
	db.Exec("UPDATE workflow_defs SET project_id = ? WHERE project_id IS NULL OR project_id = ''", defaultID)
}

// backfillWorkflowIDs populates the workflow_id column on gates/artifacts rows
// created before the column existed, resolving it from each row's run. Best
// effort: errors are ignored (a missing run simply leaves the field empty).
func backfillWorkflowIDs(db *gorm.DB) {
	for _, table := range []string{"gates", "artifacts"} {
		db.Exec("UPDATE " + table + " SET workflow_id = (SELECT workflow_id FROM runs WHERE runs.id = " + table + ".run_id) " +
			"WHERE (workflow_id IS NULL OR workflow_id = '') AND run_id IN (SELECT id FROM runs)")
	}
}
