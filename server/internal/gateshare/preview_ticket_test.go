package gateshare

import (
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/models"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestTicketStoreIssueLookupInvalidate(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:ticket_store?mode=memory&cache=shared"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.GateSharePreviewTicket{}); err != nil {
		t.Fatal(err)
	}
	s := NewTicketStore(db)
	ticket, exp, err := s.Issue("hash1", "run1", "node1", 5173, PreviewPurposeVNC)
	if err != nil {
		t.Fatal(err)
	}
	if ticket == "" || exp.Before(time.Now()) {
		t.Fatalf("ticket=%q exp=%v", ticket, exp)
	}
	claims, ok := s.Lookup(ticket)
	if !ok || claims.Port != 5173 || claims.Purpose != PreviewPurposeVNC {
		t.Fatalf("lookup: ok=%v %+v", ok, claims)
	}
	if _, ok := s.Lookup("deadbeefdeadbeefdeadbeefdeadbeef"); ok {
		t.Fatal("expected miss")
	}
	s.InvalidateByTokenHash("hash1")
	if _, ok := s.Lookup(ticket); ok {
		t.Fatal("expected invalidate")
	}
}

func TestInferPreviewMode(t *testing.T) {
	if InferPreviewMode("API · 8080") != PreviewPurposeVNC {
		t.Fatal("api")
	}
	if InferPreviewMode("Web · 5173") != PreviewPurposeVNC {
		t.Fatal("vnc")
	}
}
