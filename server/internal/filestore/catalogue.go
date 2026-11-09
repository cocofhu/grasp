package filestore

import (
	"context"
	"fmt"

	"gorm.io/gorm"
	"gorm.io/gorm/schema"
)

// PayloadField describes SQL-owned content without registering an ORM hook.
// Explicit raw fields contain references only; their encoding is part of the
// owning schema. Legacy serializer fields retain their historical scan rules.
type PayloadField struct {
	Column     string
	Encoding   string
	Serializer string
	Explicit   bool
}

// OptionalPayloadTable is a compatibility allowlist for tables introduced
// after the first portable SQL snapshots. An absent table in that immutable
// snapshot cannot own references. Existing tables/columns still fail closed.
func OptionalPayloadTable(table string) bool {
	return table == "workflow_event_sources" || table == "payload_upload_holds" || table == "workflow_visit_checkpoints"
}

// SkipMissingPayloadTable accepts an absent new table only when the frozen SQL
// contains no generation that owns its rows. A current manifest must fail
// verification if its source table was lost, even after body materialization.
func SkipMissingPayloadTable(ctx context.Context, db *gorm.DB, table string) (bool, error) {
	if !OptionalPayloadTable(table) {
		return false, nil
	}
	if table == "workflow_visit_checkpoints" {
		if db.Migrator().HasTable(table) {
			return false, nil
		}
		if db.Migrator().HasColumn("state_runs", "workspace_checkpoint_id") {
			var owners int64
			if err := db.WithContext(ctx).Table("state_runs").Where("COALESCE(workspace_checkpoint_id, '') <> ''").Count(&owners).Error; err != nil {
				return false, err
			}
			if owners != 0 {
				return false, fmt.Errorf("workflow visits require missing payload table %s", table)
			}
		}
		return true, nil
	}
	if table == "payload_upload_holds" {
		hasHolds, hasBudgets := db.Migrator().HasTable(table), db.Migrator().HasTable("payload_upload_budgets")
		if hasBudgets && !db.Migrator().HasColumn("payload_upload_budgets", "hold_count") {
			return false, fmt.Errorf("existing upload reservation table is missing hold_count")
		}
		if hasHolds {
			if !hasBudgets {
				var owners int64
				if err := db.WithContext(ctx).Table(table).Count(&owners).Error; err != nil {
					return false, err
				}
				if owners > 0 {
					return false, fmt.Errorf("upload holds require missing reservation table")
				}
			}
			return false, nil
		}
		if hasBudgets {
			var owners int64
			if err := db.WithContext(ctx).Table("payload_upload_budgets").Where("COALESCE(hold_count, 0) <> 0").Count(&owners).Error; err != nil {
				return false, err
			}
			if owners > 0 {
				return false, fmt.Errorf("upload reservations require missing payload table %s", table)
			}
		}
		return true, nil
	}
	if db.Migrator().HasTable(table) {
		return false, nil
	}
	if db.Migrator().HasColumn("workflow_conversation_events", "source_version") {
		var owners int64
		if err := db.WithContext(ctx).Table("workflow_conversation_events").Where("COALESCE(source_version, 0) <> 0").Count(&owners).Error; err != nil {
			return false, err
		}
		if owners > 0 {
			return false, fmt.Errorf("immutable workflow manifests require missing payload table %s", table)
		}
	}
	return true, nil
}

// PayloadFields preserves schema order, so legacy backup reference hashes do
// not change when callers adopt explicit raw references in existing columns.
func PayloadFields(model *schema.Schema) ([]PayloadField, error) {
	var fields []PayloadField
	for _, field := range model.Fields {
		if field.DBName == "" || !field.Readable {
			continue
		}
		serializer, encoding := field.TagSettings["SERIALIZER"], ""
		switch serializer {
		case "filetext":
			encoding = Text
		case "filejson":
			encoding = JSON
		case "filebytes":
			encoding = Bytes
		}
		explicit := field.TagSettings["PAYLOAD"]
		if explicit != "" {
			switch explicit {
			case Text, JSON, Bytes, Binary, Image:
			default:
				return nil, fmt.Errorf("unknown payload encoding on %s.%s", model.Table, field.DBName)
			}
			if serializer != "" && encoding != explicit {
				return nil, fmt.Errorf("conflicting payload descriptions on %s.%s", model.Table, field.DBName)
			}
			encoding = explicit
		}
		if serializer != "" || explicit != "" {
			fields = append(fields, PayloadField{Column: field.DBName, Encoding: encoding, Serializer: serializer, Explicit: explicit != ""})
		}
	}
	return fields, nil
}
