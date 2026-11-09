package payloadstore

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"github.com/cocofhu/grasp/internal/filestore"
	"github.com/cocofhu/grasp/internal/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// closureReferenceStamp repeats exactly VerifyReferences' raw-cell ordering and
// digest, without opening any payload. It detects changes during offline GETs
// and supplies the real fixed-seed digest for an empty first primary binding.
func closureReferenceStamp(ctx context.Context, db *gorm.DB) (ReferenceReport, error) {
	var report ReferenceReport
	h := sha256.New()
	encoder := json.NewEncoder(h)
	for _, model := range models.AllModels() {
		stmt := &gorm.Statement{DB: db}
		if err := stmt.Parse(model); err != nil {
			return report, err
		}
		skip, err := filestore.SkipMissingPayloadTable(ctx, db, stmt.Schema.Table)
		if err != nil {
			return report, err
		}
		if skip {
			continue
		}
		fields, err := filestore.PayloadFields(stmt.Schema)
		if err != nil {
			return report, err
		}
		columns := []string{}
		active := map[string]filestore.PayloadField{}
		for _, field := range fields {
			if field.Explicit && !db.Migrator().HasColumn(stmt.Schema.Table, field.Column) {
				if filestore.OptionalPayloadTable(stmt.Schema.Table) {
					return report, fmt.Errorf("existing payload table %s is missing column %s", stmt.Schema.Table, field.Column)
				}
				continue
			}
			columns = append(columns, field.Column)
			active[field.Column] = field
		}
		if len(columns) == 0 {
			continue
		}
		if len(stmt.Schema.PrimaryFields) != 1 {
			return report, fmt.Errorf("reference scan requires primary cursor: %s", stmt.Schema.Table)
		}
		pk := stmt.Schema.PrimaryFields[0].DBName
		selected := []string{stmt.Quote(pk)}
		for _, column := range columns {
			selected = append(selected, stmt.Quote(column))
		}
		var after any
		for {
			query := db.WithContext(ctx).Table(stmt.Schema.Table).Select(selected).
				Order(clause.OrderByColumn{Column: clause.Column{Name: pk}}).Limit(1)
			if after != nil {
				query = query.Where(clause.Gt{Column: clause.Column{Name: pk}, Value: after})
			}
			var rows []map[string]any
			if err := query.Find(&rows).Error; err != nil {
				return report, err
			}
			if len(rows) == 0 {
				break
			}
			row := rows[0]
			after = row[pk]
			for _, column := range columns {
				raw, err := storedBytes(row[column])
				if err != nil {
					return report, err
				}
				if len(raw) == 0 || !active[column].Explicit && string(raw) == "null" {
					continue
				}
				if err := encoder.Encode([]string{stmt.Schema.Table, fmt.Sprint(after), column, string(raw)}); err != nil {
					return report, err
				}
				report.Cells++
			}
		}
	}
	report.SHA256 = hex.EncodeToString(h.Sum(nil))
	return report, nil
}
