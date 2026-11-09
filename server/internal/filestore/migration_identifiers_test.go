package filestore_test

import (
	"context"
	"testing"

	"github.com/cocofhu/grasp/internal/filestore"
	"gorm.io/gorm/clause"
)

type reservedPayloadRow struct {
	Key   string   `gorm:"column:select;primaryKey"`
	Usage string   `gorm:"column:usage;serializer:filetext"`
	Group []string `gorm:"column:group;serializer:filejson"`
}

func TestMigrationQuotesReservedPayloadAndPrimaryNamesAcrossPages(t *testing.T) {
	db := fileDB(t)
	if err := db.AutoMigrate(&reservedPayloadRow{}); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"a", "b"} {
		if err := db.Table("reserved_payload_rows").Create(map[string]any{"select": key, "usage": "history " + key, "group": `["original"]`}).Error; err != nil {
			t.Fatal(err)
		}
	}
	report, err := filestore.Migrate(context.Background(), db, &reservedPayloadRow{})
	if err != nil || report.Migrated != 4 || report.Deferred != 0 || len(report.Failures) != 0 {
		t.Fatalf("reserved column or paginated primary cursor failed: %+v %v", report, err)
	}
	var rows []reservedPayloadRow
	if err := db.Order(clause.OrderByColumn{Column: clause.Column{Name: "select"}}).Find(&rows).Error; err != nil || len(rows) != 2 {
		t.Fatal(err)
	}
	for i, key := range []string{"a", "b"} {
		if rows[i].Key != key || rows[i].Usage != "history "+key || len(rows[i].Group) != 1 || rows[i].Group[0] != "original" {
			t.Fatalf("quoted migration changed logical history: %+v", rows[i])
		}
	}
}
