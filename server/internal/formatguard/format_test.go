package formatguard

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/config"
	"github.com/cocofhu/grasp/internal/database"
	"github.com/cocofhu/grasp/internal/filestore"
	"github.com/cocofhu/grasp/internal/historymigration"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/storageguard"
	"github.com/google/uuid"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func freshFixture(t *testing.T) (config.DatabaseConfig, *gorm.DB) {
	t.Helper()
	root := t.TempDir()
	cfg := config.DatabaseConfig{Driver: "sqlite", Path: filepath.Join(root, "history.sqlite")}
	runtime, err := database.OpenRuntime(cfg, filepath.Join(root, "payloads"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtime.Close() })
	return cfg, runtime.DB
}

func normalizedFixture(t *testing.T) (config.DatabaseConfig, *gorm.DB) {
	t.Helper()
	cfg, db := freshFixture(t)
	if err := db.Migrator().DropTable(&database.FreshOriginSeal{}); err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.SoloSession{ID: "old-session", UserID: "owner", Status: models.SoloStatusClosed}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.SoloMessage{ID: "old-message", SessionID: "old-session", Role: "assistant", Content: "original answer",
		Events: []json.RawMessage{json.RawMessage(`{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"original"}}`)}, CreatedAt: time.Date(2026, 11, 2, 3, 4, 5, 0, time.UTC)}).Error; err != nil {
		t.Fatal(err)
	}
	owner := uuid.NewString()
	if err := storageguard.Enter(context.Background(), db, owner, []string{runtimeID(t, db)}); err != nil {
		t.Fatal(err)
	}
	report, err := historymigration.ImportSoloEvents(context.Background(), db, owner)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&database.OfflineUpgradeReceipt{}); err != nil {
		t.Fatal(err)
	}
	stamp := strings.Repeat("a", 64)
	receipt := database.OfflineUpgradeReceipt{ID: owner, Protocol: "grasp-offline-normalization/v1", SourceVersion: "unrecorded",
		SourceDatabaseSHA256: stamp, ConfigurationSHA256: stamp, ReferenceSHA256: stamp, HistoryProtocol: report.Protocol,
		HistorySHA256: report.SHA256, HistoryMessages: report.Messages, HistoryEvents: report.Events, CompletedAt: time.Now().UTC()}
	if err := db.Create(&receipt).Error; err != nil {
		t.Fatal(err)
	}
	if err := storageguard.Leave(context.Background(), db, owner); err != nil {
		t.Fatal(err)
	}
	return cfg, db
}

func runtimeID(t *testing.T, db *gorm.DB) string {
	t.Helper()
	var row storageguard.Runtime
	if err := db.Take(&row).Error; err != nil {
		t.Fatal(err)
	}
	return row.ID
}

func treeDigest(t *testing.T, root string) map[string]string {
	t.Helper()
	result := map[string]string{}
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		hash := sha256.Sum256(body)
		result[strings.TrimPrefix(path, root)] = hex.EncodeToString(hash[:])
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func TestCleanupRejectsUnsealedOldSchemaBeforeAnyMutation(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "old.sqlite")
	db, err := gorm.Open(sqlite.Open(path), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("PRAGMA journal_mode=WAL").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("CREATE TABLE runs (id text PRIMARY KEY, status text)").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("INSERT INTO runs VALUES ('old-request','queued')").Error; err != nil {
		t.Fatal(err)
	}
	conn, _ := db.DB()
	_ = conn.Close()
	if err := os.Mkdir(filepath.Join(root, "objects"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "objects", "evidence"), []byte("original attachment"), 0o600); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(root, "alias.sqlite")
	if err := os.Symlink(path, alias); err != nil {
		t.Fatal(err)
	}
	before := treeDigest(t, root)
	uri := (&url.URL{Scheme: "file", Path: path, RawQuery: "_foreign_keys=on"}).String()
	for _, configured := range []string{path, path + "?_busy_timeout=5000", uri, alias} {
		if err := Check(context.Background(), config.DatabaseConfig{Driver: "sqlite", Path: configured}); !errors.Is(err, ErrMigrationRequired) {
			t.Fatalf("old schema was not rejected through supported SQLite path syntax: %v", err)
		}
	}
	if after := treeDigest(t, root); !reflect.DeepEqual(before, after) {
		t.Fatalf("rejected startup changed SQL, objects or created migration files: before=%v after=%v", before, after)
	}
	missing := filepath.Join(root, "not-created", "new.sqlite")
	if err := Check(context.Background(), config.DatabaseConfig{Driver: "sqlite", Path: missing}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Dir(missing)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("fresh inspection created directories")
	}
}

func TestCleanupFreshOriginAllowsNewCanonicalWorkAndJSONMetadata(t *testing.T) {
	cfg, db := freshFixture(t)
	for i := range 300 {
		if err := db.Create(&models.SoloMessage{ID: uuid.NewString(), SessionID: "new-session", Role: "user", Content: "normal new work " + strings.Repeat("x", i)}).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Table("state_runs").Create(map[string]any{"id": 1, "run_id": "run", "usage": `{"inputTokens":17}`, "usage_by_model": `{"model":{"inputTokens":17}}`}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.Artifact{ID: "image", Kind: "image", Content: "aW1hZ2U="}).Error; err != nil {
		t.Fatal(err)
	}
	files, err := filestore.FromDB(db)
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := files.Prepare(context.Background(), strings.NewReader("original file bytes"), filestore.Binary)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Table("artifacts").Create(map[string]any{"id": "file", "kind": "file", "content": prepared.Ref()}).Error; err != nil {
		t.Fatal(err)
	}
	if err := Check(context.Background(), cfg); err != nil {
		t.Fatal("fresh evidence rejected growing canonical work or current inline JSON metadata", err)
	}
	if err := db.Table("solo_messages").Where("id = ?", "unknown").UpdateColumn("content", "no matched row").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Table("solo_messages").Create(map[string]any{"id": "bad-inline", "session_id": "new-session", "role": "user", "content": "old inline payload"}).Error; err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(cfg.Path + "-wal"); err != nil || info.Size() == 0 {
		t.Fatal("fixture must retain the new uncheckpointed payload in WAL", err)
	}
	if err := Check(context.Background(), cfg); !errors.Is(err, ErrMigrationRequired) {
		t.Fatal("fresh seal concealed a noncanonical owned payload", err)
	}
}

func TestCleanupUpgradeAcceptsNewReferencesAndDeletionWithoutReusingFrozenSetHash(t *testing.T) {
	cfg, db := normalizedFixture(t)
	if err := Check(context.Background(), cfg); err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.SoloMessage{ID: "new-message", SessionID: "old-session", Role: "user", Content: "legitimate new reference"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := Check(context.Background(), cfg); err != nil {
		t.Fatal("normal new references were compared to the frozen completion digest", err)
	}
	if err := db.Where("id = ?", "old-message").Delete(&models.SoloMessage{}).Error; err != nil {
		t.Fatal(err)
	}
	if err := Check(context.Background(), cfg); err != nil {
		t.Fatal("legitimate history deletion was compared to the original import aggregate", err)
	}
}

func TestCleanupImportedSourceMustKeepExactOriginAndContiguousRange(t *testing.T) {
	for _, mutation := range []string{"missing-pointer", "origin", "missing-chunk", "chunk-count", "chunk-identity", "source-encoding", "missing-table"} {
		t.Run(mutation, func(t *testing.T) {
			cfg, db := normalizedFixture(t)
			var err error
			switch mutation {
			case "missing-pointer":
				err = db.Table("solo_messages").Where("id = ?", "old-message").UpdateColumn("event_log", nil).Error
			case "origin":
				var row struct{ EventLog string }
				if err := db.Table("solo_messages").Select("event_log").Where("id = ?", "old-message").Take(&row).Error; err != nil {
					t.Fatal(err)
				}
				var pointer models.ChatEventLog
				if err := json.Unmarshal([]byte(row.EventLog), &pointer); err != nil {
					t.Fatal(err)
				}
				pointer.Legacy.MessageID = "different-message"
				body, _ := json.Marshal(pointer)
				err = db.Table("solo_messages").Where("id = ?", "old-message").UpdateColumn("event_log", string(body)).Error
			case "missing-chunk":
				err = db.Where("1 = 1").Delete(&models.ChatEventChunk{}).Error
			case "chunk-count":
				err = db.Table("chat_event_chunks").Where("1 = 1").UpdateColumn("count", 4).Error
			case "chunk-identity":
				err = db.Table("chat_event_chunks").Where("1 = 1").UpdateColumn("id", uuid.NewString()).Error
			case "source-encoding":
				err = db.Table("solo_messages").Where("id = ?", "old-message").UpdateColumn("events", filestore.Prefix+filestore.Text+"/"+strings.Repeat("a", 64)).Error
			case "missing-table":
				err = db.Migrator().DropTable(&models.WorkflowEventSource{})
			}
			if err != nil {
				t.Fatal(err)
			}
			if err := Check(context.Background(), cfg); !errors.Is(err, ErrMigrationRequired) {
				t.Fatalf("damaged migration was accepted: %v", err)
			}
		})
	}
}

func TestCleanupRetainedSoloPendingRequiresCurrentDatasetQuarantine(t *testing.T) {
	cfg, db := normalizedFixture(t)
	for _, surface := range []string{"workflow.review", "solo"} {
		if err := db.Create(&models.ChatPendingTurn{ID: surface, Surface: surface, SessionKey: "old-session", Started: true, Position: 4, Payload: []byte("original unknown effect")}).Error; err != nil {
			t.Fatal(err)
		}
		if surface == "workflow.review" {
			if err := Check(context.Background(), cfg); err != nil {
				t.Fatal("formal non-Solo pending was mistaken for an obsolete queue", err)
			}
		}
	}
	if err := Check(context.Background(), cfg); !errors.Is(err, ErrMigrationRequired) {
		t.Fatal("unquarantined Solo source accepted", err)
	}
	dataset := uuid.NewString()
	if err := db.Create(&models.ExecutionDataset{ID: "primary", DatasetID: dataset}).Error; err != nil {
		t.Fatal(err)
	}
	quarantine := models.ExecutionQuarantine{ID: "retained", DatasetID: uuid.NewString(), EntityKind: "legacy_pending", EntityID: "solo", ReviewState: "pending"}
	if err := db.Create(&quarantine).Error; err != nil {
		t.Fatal(err)
	}
	if err := Check(context.Background(), cfg); !errors.Is(err, ErrMigrationRequired) {
		t.Fatal("quarantine from an unrelated dataset was accepted", err)
	}
	if err := db.Model(&models.ExecutionQuarantine{}).Where("id = ?", quarantine.ID).UpdateColumn("dataset_id", dataset).Error; err != nil {
		t.Fatal(err)
	}
	if err := Check(context.Background(), cfg); err != nil {
		t.Fatal("retained isolated original sources blocked cleanup", err)
	}
	var old models.ChatPendingTurn
	if err := db.Take(&old, "id = ?", "solo").Error; err != nil || !old.Started || old.Position != 4 || string(old.Payload) != "original unknown effect" {
		t.Fatal("format check modified source evidence", err)
	}
}

func TestCleanupAllowsOnlyGenuineEmptyExecutionBootstrap(t *testing.T) {
	root := t.TempDir()
	cfg := config.DatabaseConfig{Driver: "sqlite", Path: filepath.Join(root, "fresh.sqlite")}
	db, err := database.ConnectWithFiles(cfg, filepath.Join(root, "payloads"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = database.Close(db) }()
	proof, err := database.CaptureFreshOrigin(context.Background(), db)
	if err != nil || !proof.Empty() {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.Run{}, &models.StateRun{}, &models.ExecutionDataset{}, &models.ExecutionQuarantine{}); err != nil {
		t.Fatal(err)
	}
	if err := database.SealFreshOrigin(context.Background(), db, proof, "execution-init"); err != nil {
		t.Fatal(err)
	}
	if err := Check(context.Background(), cfg); err != nil {
		t.Fatal("valid migration-version fresh execution-init could not reach first runtime startup", err)
	}
	if err := db.Table("runs").Create(map[string]any{"id": "unexpected-old-work", "status": "queued"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := Check(context.Background(), cfg); !errors.Is(err, ErrMigrationRequired) {
		t.Fatal("partial schema with business work was mistaken for a fresh bootstrap", err)
	}
}
