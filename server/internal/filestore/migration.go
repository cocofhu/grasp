package filestore

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type MigrationReport struct {
	Migrated int      `json:"migrated"`
	Verified int      `json:"verified"`
	Deferred int      `json:"deferred"`
	Bytes    int64    `json:"bytes"`
	Failures []string `json:"failures,omitempty"`
}

// Migrate externalizes legacy columns in bounded batches. SQL is updated with
// a compare-and-swap against the original cell, so a concurrent edit wins.
// Updates bypass model Save: migration never changes revisions or timestamps.
// Historical snapshots retain their exact bytes, including truncated traces.
func Migrate(ctx context.Context, db *gorm.DB, models ...any) (MigrationReport, error) {
	var report MigrationReport
	store, err := FromDB(db)
	if err != nil {
		return report, err
	}
	for _, model := range models {
		stmt := &gorm.Statement{DB: db}
		if err := stmt.Parse(model); err != nil {
			return report, err
		}
		if len(stmt.Schema.PrimaryFields) != 1 {
			continue
		}
		table := stmt.Schema.Table
		pk := stmt.Schema.PrimaryFields[0].DBName
		columns := []string{pk}
		encodings := map[string]string{}
		explicit := map[string]bool{}
		fields, err := PayloadFields(stmt.Schema)
		if err != nil {
			return report, err
		}
		for _, field := range fields {
			if field.Encoding == "" {
				continue
			}
			// A frozen pre-upgrade SQL backup cannot contain this newly
			// introduced raw-ref column. Do not invent it while verifying it.
			if field.Explicit && !db.Migrator().HasColumn(stmt.Schema.Table, field.Column) {
				continue
			}
			encodings[field.Column], explicit[field.Column] = field.Encoding, field.Explicit
			columns = append(columns, field.Column)
		}
		if len(encodings) == 0 {
			continue
		}
		if table == "artifacts" || table == "artifact_versions" {
			columns = append(columns, "kind")
		}
		var after any
		for {
			if err := ctx.Err(); err != nil {
				return report, err
			}
			// A legacy row may contain a large base64 body. Load one row at a
			// time rather than multiplying peak memory by a fixed batch size.
			q := db.WithContext(ctx).Table(table).Select(columns).Order(pk).Limit(1)
			if after != nil {
				q = q.Where(pk+" > ?", after)
			}
			var rows []map[string]any
			if err := q.Find(&rows).Error; err != nil {
				return report, err
			}
			if len(rows) == 0 {
				break
			}
			for _, row := range rows {
				after = row[pk]
				for column, encoding := range encodings {
					raw, e := rawString(row[column])
					if e != nil {
						report.Failures = append(report.Failures, fmt.Sprintf("%s/%v/%s: %v", table, after, column, e))
						continue
					}
					if raw == "" || (encoding == JSON && raw == "null") {
						continue
					}
					if IsRef(raw) {
						actual, _, _ := Parse(raw)
						if explicit[column] && actual != encoding {
							e = fmt.Errorf("stored payload encoding disagrees with its owner")
						} else {
							e = store.Verify(ctx, raw)
						}
						if e != nil {
							report.Failures = append(report.Failures, fmt.Sprintf("%s/%v/%s: %v", table, after, column, e))
						} else {
							report.Verified++
						}
						continue
					}
					if explicit[column] {
						report.Failures = append(report.Failures, fmt.Sprintf("%s/%v/%s: explicit payload requires a prepared reference", table, after, column))
						continue
					}
					if strings.HasPrefix(raw, Prefix) {
						report.Failures = append(report.Failures, fmt.Sprintf("%s/%v/%s: invalid stored file reference", table, after, column))
						continue
					}
					data := []byte(raw)
					kind, _ := rawString(row["kind"])
					if encoding == JSON && !json.Valid(data) {
						report.Failures = append(report.Failures, fmt.Sprintf("%s/%v/%s: invalid legacy JSON", table, after, column))
						continue
					}
					if column == "content" && kind == "image" {
						data, e = base64.StdEncoding.DecodeString(strings.TrimSpace(raw))
						encoding = Image
						if e != nil {
							report.Failures = append(report.Failures, fmt.Sprintf("%s/%v/%s: invalid legacy image", table, after, column))
							continue
						}
					}
					ref, size, e := store.Put(ctx, bytes.NewReader(data), encoding)
					if e == nil {
						e = store.Verify(ctx, ref)
					}
					if e != nil {
						report.Failures = append(report.Failures, fmt.Sprintf("%s/%v/%s: %v", table, after, column, e))
						continue
					}
					q := db.WithContext(ctx).Table(table).Where(clause.Eq{Column: clause.Column{Name: pk}, Value: after})
					if db.Dialector.Name() == "mysql" {
						// A case-insensitive collation must not overwrite a concurrent
						// edit which differs only by case or trailing spaces.
						q = q.Where(clause.Expr{SQL: "BINARY ? = BINARY ?", Vars: []any{clause.Column{Name: column}, row[column]}})
					} else {
						q = q.Where(clause.Eq{Column: clause.Column{Name: column}, Value: row[column]})
					}
					result := q.UpdateColumn(column, ref)
					if result.Error != nil {
						return report, result.Error
					}
					if result.RowsAffected == 0 {
						report.Deferred++
					} else {
						report.Migrated++
						report.Bytes += size
					}
				}
			}
		}
	}
	return report, nil
}
