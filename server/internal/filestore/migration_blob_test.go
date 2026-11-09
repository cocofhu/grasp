package filestore_test

import (
	"bytes"
	"context"
	"io"
	"testing"

	"github.com/cocofhu/grasp/internal/filestore"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/objectstore"
)

func TestMigrationCanonicalizesActualSQLiteBlobWithoutChangingIntent(t *testing.T) {
	db := fileDB(t)
	if err := db.AutoMigrate(&models.ChatPendingTurn{}); err != nil {
		t.Fatal(err)
	}
	body := []byte{'o', 'l', 'd', 0, 255, '\n'}
	if err := db.Table("chat_pending_turns").Create(map[string]any{"id": "old", "surface": "solo", "session_key": "owner", "started": true, "position": 19, "payload": body}).Error; err != nil {
		t.Fatal(err)
	}
	var beforeType string
	if err := db.Raw("SELECT typeof(payload) FROM chat_pending_turns WHERE id = ?", "old").Scan(&beforeType).Error; err != nil || beforeType != "blob" {
		t.Fatal("fixture does not contain a real SQLite BLOB", beforeType, err)
	}
	report, err := filestore.Migrate(context.Background(), db, &models.ChatPendingTurn{})
	if err != nil || report.Migrated != 1 || report.Deferred != 0 || len(report.Failures) != 0 {
		t.Fatalf("BLOB normalization deferred in an offline database: %+v %v", report, err)
	}
	var row models.ChatPendingTurn
	if err := db.First(&row, "id = ?", "old").Error; err != nil || !bytes.Equal(row.Payload, body) || !row.Started || row.Position != 19 || row.SessionKey != "owner" {
		t.Fatalf("intent changed: %+v %v", row, err)
	}
}

type migrationCASBackend struct {
	objectBackend
	beforePut func()
}

func (s *migrationCASBackend) Put(ctx context.Context, key string, body io.ReadSeeker, info objectstore.Info) error {
	if s.beforePut != nil {
		callback := s.beforePut
		s.beforePut = nil
		callback()
	}
	return s.objectBackend.Put(ctx, key, body, info)
}

func TestMigrationCASPreservesConcurrentSQLiteValueAndStorageClassChanges(t *testing.T) {
	for _, test := range []struct {
		name     string
		original any
		changed  any
		wantType string
	}{
		{"blob-value", []byte("original"), []byte("changed"), "blob"},
		{"blob-to-text", []byte("same bytes"), "same bytes", "text"},
		{"text-to-blob", "same bytes", []byte("same bytes"), "blob"},
	} {
		t.Run(test.name, func(t *testing.T) {
			db := fileDB(t)
			if err := db.AutoMigrate(&models.ChatPendingTurn{}); err != nil {
				t.Fatal(err)
			}
			if err := db.Table("chat_pending_turns").Create(map[string]any{"id": "race", "payload": test.original}).Error; err != nil {
				t.Fatal(err)
			}
			store, err := filestore.FromDB(db)
			if err != nil {
				t.Fatal(err)
			}
			backend := &migrationCASBackend{beforePut: func() {
				if err := db.Table("chat_pending_turns").Where("id = ?", "race").UpdateColumn("payload", test.changed).Error; err != nil {
					t.Fatal(err)
				}
			}}
			if err := store.SetObjectStore(backend); err != nil {
				t.Fatal(err)
			}
			report, err := filestore.Migrate(context.Background(), db, &models.ChatPendingTurn{})
			if err != nil || report.Deferred != 1 || report.Migrated != 0 || len(report.Failures) != 0 {
				t.Fatalf("migration overwrote a concurrent edit: %+v %v", report, err)
			}
			var actualType string
			var actual []byte
			if err := db.Raw("SELECT typeof(payload), payload FROM chat_pending_turns WHERE id = ?", "race").Row().Scan(&actualType, &actual); err != nil || actualType != test.wantType {
				t.Fatal("concurrent storage class was lost", actualType, err)
			}
			want := test.changed
			if text, ok := want.(string); ok {
				want = []byte(text)
			}
			if !bytes.Equal(actual, want.([]byte)) {
				t.Fatal("concurrent source bytes changed")
			}
		})
	}
}
