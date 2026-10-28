package database

import (
	"encoding/json"
	"strings"

	"gorm.io/gorm"
)

// projectRuntimeColumns are the old projects columns that stored access and
// environment. AutoMigrate does not drop them; startup does, and a second
// pass is a no-op when they are already gone.
var projectRuntimeColumns = []string{
	"git_credential_type",
	"git_credential_id",
	"ssh_hosts_credential_id",
	"env",
	"credential_bindings",
}

// projectRuntimeJSONKeys are the same settings inside Solo session snapshots.
// They are removed in process so SQLite and MySQL share one path and no
// driver-specific JSON function is required.
var projectRuntimeJSONKeys = []string{
	"gitCredentialType",
	"gitCredentialId",
	"sshHostsCredentialId",
	"env",
	"credentialBindings",
}

// stripProjectRuntimeConfig drops repository access and environment from
// projects and from both Solo snapshot shapes. Missing columns, empty
// snapshots and already-clean rows are success. A second call does not
// rewrite rows that no longer contain the keys.
func stripProjectRuntimeConfig(db *gorm.DB) error {
	if err := dropProjectRuntimeColumns(db); err != nil {
		return err
	}
	return stripSessionRuntimeSnapshots(db)
}

func dropProjectRuntimeColumns(db *gorm.DB) error {
	if db == nil || !db.Migrator().HasTable("projects") {
		return nil
	}
	cols, err := db.Migrator().ColumnTypes("projects")
	if err != nil {
		return err
	}
	present := map[string]bool{}
	for _, col := range cols {
		present[strings.ToLower(col.Name())] = true
	}
	for _, name := range projectRuntimeColumns {
		if !present[name] {
			continue
		}
		if err := db.Exec("ALTER TABLE projects DROP COLUMN " + name).Error; err != nil {
			return err
		}
	}
	return nil
}

type sessionSnapshotRow struct {
	ID              string `gorm:"column:id"`
	ProjectSnapshot string `gorm:"column:project_snapshot"`
	Repos           string `gorm:"column:repos"`
}

func stripSessionRuntimeSnapshots(db *gorm.DB) error {
	if db == nil || !db.Migrator().HasTable("solo_sessions") {
		return nil
	}
	var rows []sessionSnapshotRow
	if err := db.Table("solo_sessions").Select("id, project_snapshot, repos").Find(&rows).Error; err != nil {
		return err
	}
	for _, row := range rows {
		snap, snapChanged, err := stripRuntimeJSON(row.ProjectSnapshot)
		if err != nil {
			return err
		}
		repos, reposChanged, err := stripRuntimeJSON(row.Repos)
		if err != nil {
			return err
		}
		if !snapChanged && !reposChanged {
			continue
		}
		updates := map[string]any{}
		if snapChanged {
			updates["project_snapshot"] = snap
		}
		if reposChanged {
			updates["repos"] = repos
		}
		if err := db.Table("solo_sessions").Where("id = ?", row.ID).Updates(updates).Error; err != nil {
			return err
		}
	}
	return nil
}

// stripRuntimeJSON removes the retired keys from a project object or a
// session-repo array. Blank and JSON null are left untouched.
func stripRuntimeJSON(raw string) (string, bool, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "null" {
		return raw, false, nil
	}
	var value any
	if err := json.Unmarshal([]byte(raw), &value); err != nil {
		return "", false, err
	}
	if !stripRuntimeValue(value) {
		return raw, false, nil
	}
	out, err := json.Marshal(value)
	if err != nil {
		return "", false, err
	}
	return string(out), true, nil
}

func stripRuntimeValue(value any) bool {
	switch typed := value.(type) {
	case map[string]any:
		changed := false
		for _, key := range projectRuntimeJSONKeys {
			if _, ok := typed[key]; ok {
				delete(typed, key)
				changed = true
			}
		}
		return changed
	case []any:
		changed := false
		for _, item := range typed {
			if stripRuntimeValue(item) {
				changed = true
			}
		}
		return changed
	default:
		return false
	}
}
