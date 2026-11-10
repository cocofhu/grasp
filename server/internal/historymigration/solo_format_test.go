package historymigration

import (
	"context"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/models"
)

func TestCurrentSoloFormatCheckUsesOnlyMetadataAndAllowsHistoryDeletion(t *testing.T) {
	db, owner, files := fixture(t)
	objects := &memoryObjects{objects: map[string][]byte{}}
	if err := files.SetObjectStore(objects); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"a", "b"} {
		insertSource(t, db, id, 2, time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC))
	}
	if _, err := ImportSoloEvents(context.Background(), db, owner); err != nil {
		t.Fatal(err)
	}
	objects.gets.Store(0)
	objects.puts.Store(0)
	report, err := CheckSoloEventsFormat(context.Background(), db)
	if err != nil || report.Messages != 2 || report.Events != 4 || objects.gets.Load() != 0 || objects.puts.Load() != 0 {
		t.Fatalf("startup check performed object IO or lost canonical history: %+v gets=%d puts=%d %v", report, objects.gets.Load(), objects.puts.Load(), err)
	}
	if err := db.Where("id = ?", "a").Delete(&models.SoloMessage{}).Error; err != nil {
		t.Fatal(err)
	}
	report, err = CheckSoloEventsFormat(context.Background(), db)
	if err != nil || report.Messages != 1 || report.Events != 2 || objects.gets.Load() != 0 || objects.puts.Load() != 0 {
		t.Fatalf("legitimate deletion required the old aggregate or fetched source objects: %+v %v", report, err)
	}
	if err := db.Table("chat_event_chunks").Where("1 = 1").UpdateColumn("acp_count", 7).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := CheckSoloEventsFormat(context.Background(), db); err == nil || objects.gets.Load() != 0 || objects.puts.Load() != 0 {
		t.Fatal("broken exact range was repaired or accepted", err)
	}
}
