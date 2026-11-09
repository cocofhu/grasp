package historymigration

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/chatsession"
	"github.com/cocofhu/grasp/internal/database"
	"github.com/cocofhu/grasp/internal/filestore"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/objectstore"
	"github.com/cocofhu/grasp/internal/payloadstore"
	"github.com/cocofhu/grasp/internal/storageguard"
	"github.com/google/uuid"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func fixture(t *testing.T) (*gorm.DB, string, *filestore.Store) {
	t.Helper()
	db, err := database.OpenSQLiteTest(filepath.Join(t.TempDir(), "history.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close(db) })
	owner := uuid.NewString()
	if err := storageguard.Enter(context.Background(), db, owner, nil); err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.SoloSession{ID: "history", UserID: "alice", Status: models.SoloStatusClosed}).Error; err != nil {
		t.Fatal(err)
	}
	files, err := filestore.FromDB(db)
	if err != nil {
		t.Fatal(err)
	}
	return db, owner, files
}

func insertSource(t *testing.T, db *gorm.DB, id string, count int, at time.Time) []byte {
	t.Helper()
	events := make([]json.RawMessage, count)
	for i := range events {
		events[i] = json.RawMessage(fmt.Sprintf(`{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"%s original %d 中文 fileref:v1/json/not-a-reference"},"large":9007199254740993}`, id, i))
	}
	body, err := json.Marshal(events)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("INSERT INTO solo_messages (id,session_id,role,content,events,execution,created_at) VALUES (?,?,?,?,?,?,?)", id, "history", "assistant", "original answer "+id, string(body), `{"backend":"original-tool","modelId":"original-model"}`, at).Error; err != nil {
		t.Fatal(err)
	}
	return body
}

func collect(t *testing.T, db *gorm.DB, id string, expected int, incomplete bool) []chatsession.JournalEntry {
	t.Helper()
	after := int64(-1)
	var entries []chatsession.JournalEntry
	for {
		page, warning, err := ReadSoloMessagePage(context.Background(), db, id, "history", after)
		if err != nil || warning != incomplete {
			t.Fatalf("read %s: warning=%v err=%v", id, warning, err)
		}
		for _, entry := range page.Items {
			if entry.TurnID != "" || entry.Generation != 0 || entry.Type != chatsession.FrameAcp {
				t.Fatal("import fabricated execution provenance")
			}
		}
		entries = append(entries, page.Items...)
		if !page.HasMore {
			break
		}
		if page.Next <= after {
			t.Fatal("cursor did not advance")
		}
		after = page.Next
	}
	if len(entries) != expected {
		t.Fatalf("got %d events, want %d", len(entries), expected)
	}
	return entries
}

func TestSoloImportKeepsMessagesSeparateOriginalBytesAndResumeDigest(t *testing.T) {
	db, owner, files := fixture(t)
	at := time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)
	originals := map[string][]byte{"a": insertSource(t, db, "a", 3, at), "b": insertSource(t, db, "b", LegacyEventCap, at.Add(time.Second)), "c": insertSource(t, db, "c", 0, at.Add(2*time.Second))}
	if err := db.Create(&models.ChatPendingTurn{ID: "old-dispatched", Surface: "solo", SessionKey: "history", Started: true, Payload: []byte(`{"text":"external side effect unknown"}`)}).Error; err != nil {
		t.Fatal(err)
	}
	report, err := ImportSoloEvents(context.Background(), db, owner)
	if err != nil || report.Messages != 3 || report.Events != 6003 || report.PossiblyTruncated != 1 || report.EmptyMessages != 1 || len(report.SHA256) != 64 {
		t.Fatalf("report %+v: %v", report, err)
	}
	for id, original := range originals {
		row, err := readSource(context.Background(), db, id, "history")
		if err != nil {
			t.Fatal(err)
		}
		body, err := files.Load(context.Background(), *row.Events, filestore.JSON)
		if err != nil || !bytes.Equal(body, original) {
			t.Fatal("original array bytes changed", err)
		}
		pointer, err := parsePointer(row.EventLog)
		if err != nil || pointer.Legacy.MessageID != id || pointer.Legacy.SHA256 != digest(original) || pointer.Generation != 0 || row.TurnID != "" {
			t.Fatalf("wrong origin: %+v %v", pointer, err)
		}
		var untouched struct{ Content, Execution string }
		if err := db.Table("solo_messages").Select("content", "execution").Where("id = ?", id).Take(&untouched).Error; err != nil {
			t.Fatal(err)
		}
		if untouched.Content != "original answer "+id || untouched.Execution != `{"backend":"original-tool","modelId":"original-model"}` {
			t.Fatal("original reply/provenance was rewritten")
		}
	}
	a := collect(t, db, "a", 3, false)
	collect(t, db, "b", 6000, true)
	collect(t, db, "c", 0, false)
	if !bytes.Contains(a[0].Data, []byte(`9007199254740993`)) || !a[0].At.Equal(at) {
		t.Fatal("large integer or original message time changed")
	}
	second, err := ImportSoloEvents(context.Background(), db, owner)
	if err != nil || !reflect.DeepEqual(report, second) {
		t.Fatalf("resume digest changed: %+v %+v %v", report, second, err)
	}
	var queue models.ChatQueue
	if err := db.First(&queue, "id = ?", scope("history").ID()).Error; err != nil || queue.NextEventSequence != 6003 || queue.Generation != 0 || queue.Owner != "" {
		t.Fatalf("import acquired execution ownership or duplicated history: %+v %v", queue, err)
	}
	var pending models.ChatPendingTurn
	if err := db.First(&pending, "id = ?", "old-dispatched").Error; err != nil || !pending.Started {
		t.Fatal("old dispatch marker was changed", err)
	}
}

type memoryObjects struct {
	objectstore.Store
	mu         sync.Mutex
	objects    map[string][]byte
	puts, gets atomic.Int32
	beforeIO   func(context.Context) error
	deny       string
}

func (m *memoryObjects) Put(ctx context.Context, key string, body io.ReadSeeker, _ objectstore.Info) error {
	if m.beforeIO != nil {
		if err := m.beforeIO(ctx); err != nil {
			return err
		}
	}
	data, err := io.ReadAll(body)
	if err != nil {
		return err
	}
	m.puts.Add(1)
	m.mu.Lock()
	defer m.mu.Unlock()
	m.objects[key] = bytes.Clone(data)
	return nil
}
func (m *memoryObjects) Open(ctx context.Context, key string) (io.ReadCloser, objectstore.Info, error) {
	if m.beforeIO != nil {
		if err := m.beforeIO(ctx); err != nil {
			return nil, objectstore.Info{}, err
		}
	}
	m.gets.Add(1)
	m.mu.Lock()
	defer m.mu.Unlock()
	data, ok := m.objects[key]
	if !ok || key == m.deny {
		return nil, objectstore.Info{}, os.ErrNotExist
	}
	return io.NopCloser(bytes.NewReader(bytes.Clone(data))), objectstore.Info{Size: int64(len(data))}, nil
}
func (m *memoryObjects) Head(_ context.Context, key string) (objectstore.Info, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	data, ok := m.objects[key]
	if !ok {
		return objectstore.Info{}, os.ErrNotExist
	}
	return objectstore.Info{Size: int64(len(data))}, nil
}

func TestSoloImportObjectIOHoldsNoSQLAndReaderDoesNotLoadOldBody(t *testing.T) {
	db, owner, files := fixture(t)
	insertSource(t, db, "original", 260, time.Now().UTC())
	pool, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	pool.SetMaxOpenConns(1)
	objects := &memoryObjects{objects: map[string][]byte{}}
	objects.beforeIO = func(ctx context.Context) error {
		probe, cancel := context.WithTimeout(ctx, time.Second)
		defer cancel()
		var count int64
		return db.WithContext(probe).Table("solo_sessions").Count(&count).Error
	}
	if err := files.SetObjectStore(objects); err != nil {
		t.Fatal(err)
	}
	if _, err := ImportSoloEvents(context.Background(), db, owner); err != nil {
		t.Fatal(err)
	}
	puts := objects.puts.Load()
	verified, err := VerifySoloEvents(context.Background(), db, owner)
	if err != nil || verified.Messages != 1 || objects.puts.Load() != puts {
		t.Fatal("receipt verification mutated object storage", verified, err)
	}
	references, err := payloadstore.VerifyReferences(context.Background(), db, objects)
	if err != nil || references.Payloads < 4 {
		t.Fatalf("backup closure omitted the original Events or imported journal: %+v %v", references, err)
	}
	source, err := readSource(context.Background(), db, "original", "history")
	if err != nil {
		t.Fatal(err)
	}
	_, sha, err := filestore.Parse(*source.Events)
	if err != nil {
		t.Fatal(err)
	}
	objects.deny = "payloads/sha256/" + sha[:2] + "/" + sha
	gets := objects.gets.Load()
	collect(t, db, "original", 260, false)
	if objects.gets.Load() == gets {
		t.Fatal("test did not exercise remote journal reads")
	}
	// Original evidence is required by the offline verifier, although ordinary
	// canonical history pages do not need to download it.
	if _, err := ImportSoloEvents(context.Background(), db, owner); err == nil {
		t.Fatal("resume claimed completion with a missing source body")
	}
}

func TestSoloImportRefusesLateSourceOrMaintenanceChanges(t *testing.T) {
	for _, change := range []string{"source", "maintenance"} {
		t.Run(change, func(t *testing.T) {
			db, owner, files := fixture(t)
			original := insertSource(t, db, "original", 1, time.Now().UTC())
			objects := &memoryObjects{objects: map[string][]byte{}}
			var once sync.Once
			objects.beforeIO = func(ctx context.Context) (err error) {
				once.Do(func() {
					if change == "source" {
						err = db.WithContext(ctx).Table("solo_messages").Where("id = ?", "original").Update("events", `[{"text":"changed during upload"}]`).Error
					} else {
						err = storageguard.Leave(ctx, db, owner)
					}
				})
				return err
			}
			if err := files.SetObjectStore(objects); err != nil {
				t.Fatal(err)
			}
			if _, err := ImportSoloEvents(context.Background(), db, owner); err == nil {
				t.Fatal("late changed source or offline fence was accepted")
			}
			source, err := readSource(context.Background(), db, "original", "history")
			if err != nil || source.EventLog != nil {
				t.Fatal("late import published a pointer", err)
			}
			if change == "maintenance" && *source.Events != string(original) {
				t.Fatal("lost maintenance changed the source")
			}
			var count int64
			if err := db.Table("chat_event_chunks").Count(&count).Error; err != nil || count != 0 {
				t.Fatal("late import published chunks", count, err)
			}
		})
	}
}

func TestSoloImportedOriginAndRangeMustMatchBeforeReturningFrames(t *testing.T) {
	for _, change := range []string{"message", "source_hash", "count", "range", "generation", "warning", "missing_chunk", "wrong_chunk_owner", "bytes"} {
		t.Run(change, func(t *testing.T) {
			db, owner, _ := fixture(t)
			insertSource(t, db, "original", 260, time.Now().UTC())
			if _, err := VerifySoloEvents(context.Background(), db, owner); !errors.Is(err, ErrInvalidHistory) {
				t.Fatal("read-only verifier converted missing pointer", err)
			}
			if _, err := ImportSoloEvents(context.Background(), db, owner); err != nil {
				t.Fatal(err)
			}
			source, err := readSource(context.Background(), db, "original", "history")
			if err != nil {
				t.Fatal(err)
			}
			pointer, err := parsePointer(source.EventLog)
			if err != nil {
				t.Fatal(err)
			}
			switch change {
			case "message":
				pointer.Legacy.MessageID = "another-message"
			case "source_hash":
				pointer.Legacy.SHA256 = strings.Repeat("f", 64)
			case "count":
				pointer.Legacy.Count--
			case "range":
				pointer.From++
			case "generation":
				pointer.Generation = 1
			case "warning":
				pointer.Legacy.PossiblyTruncated = true
			case "missing_chunk":
				if err := db.Where("queue_id = ? AND first_sequence = ?", scope("history").ID(), 128).Delete(&models.ChatEventChunk{}).Error; err != nil {
					t.Fatal(err)
				}
			case "wrong_chunk_owner":
				if err := db.Table("chat_event_chunks").Where("queue_id = ?", scope("history").ID()).Update("turn_id", "claimed-live-turn").Error; err != nil {
					t.Fatal(err)
				}
			case "bytes":
				if err := db.Table("chat_event_chunks").Where("queue_id = ?", scope("history").ID()).UpdateColumn("bytes", gorm.Expr("bytes + 1")).Error; err != nil {
					t.Fatal(err)
				}
			}
			encoded, err := json.Marshal(pointer)
			if err != nil {
				t.Fatal(err)
			}
			if err := db.Table("solo_messages").Where("id = ?", "original").Update("event_log", string(encoded)).Error; err != nil {
				t.Fatal(err)
			}
			page, _, err := ReadSoloMessagePage(context.Background(), db, "original", "history", -1)
			if err == nil || len(page.Items) != 0 || page.Next != -1 {
				t.Fatalf("unverifiable range advanced cursor: %+v %v", page, err)
			}
			if _, err := VerifySoloEvents(context.Background(), db, owner); err == nil {
				t.Fatal("invalid history passed completion proof")
			}
		})
	}
}

func TestSoloImportSQLFailureLeavesOriginalAndNoPublishedRange(t *testing.T) {
	db, owner, files := fixture(t)
	original := insertSource(t, db, "original", 2, time.Now().UTC())
	objects := &memoryObjects{objects: map[string][]byte{}}
	if err := files.SetObjectStore(objects); err != nil {
		t.Fatal(err)
	}
	failure := errors.New("disk metadata write failed")
	if err := db.Callback().Create().Before("gorm:create").Register("test:import-sql-failure", func(tx *gorm.DB) {
		if tx.Statement.Table == "chat_event_chunks" {
			_ = tx.AddError(failure)
		}
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := ImportSoloEvents(context.Background(), db, owner); !errors.Is(err, failure) {
		t.Fatalf("wrong failure: %v", err)
	}
	if err := db.Callback().Create().Remove("test:import-sql-failure"); err != nil {
		t.Fatal(err)
	}
	source, err := readSource(context.Background(), db, "original", "history")
	if err != nil || source.EventLog != nil || source.Events == nil || *source.Events != string(original) {
		t.Fatalf("source changed after rollback: %+v %v", source, err)
	}
	var chunks, queues int64
	db.Table("chat_event_chunks").Count(&chunks)
	db.Table("chat_queues").Count(&queues)
	if chunks != 0 || queues != 0 || objects.puts.Load() < 2 {
		t.Fatalf("expected unreferenced objects, no SQL publication: chunks=%d queues=%d puts=%d", chunks, queues, objects.puts.Load())
	}
	if err := storageguard.Check(context.Background(), db, owner); err != nil {
		t.Fatal("failed import released maintenance", err)
	}
	if _, err := ImportSoloEvents(context.Background(), db, owner); err != nil {
		t.Fatal(err)
	}
}

func TestSoloImportRejectsMissingMaintenanceTransactionsAndInvalidSource(t *testing.T) {
	for _, raw := range []string{`null`, `{}`, `[{}] trailing`, `[`, `["` + strings.Repeat("x", chatsession.JournalChunkBytes) + `"]`} {
		t.Run(fmt.Sprintf("length_%d", len(raw)), func(t *testing.T) {
			db, owner, _ := fixture(t)
			if err := db.Exec("INSERT INTO solo_messages (id,session_id,role,events,created_at) VALUES (?,?,?,?,?)", "bad", "history", "assistant", raw, time.Now().UTC()).Error; err != nil {
				t.Fatal(err)
			}
			if raw == "null" { // SQL null-equivalent means there is no stored array to convert.
				if report, err := ImportSoloEvents(context.Background(), db, owner); err != nil || report.Messages != 0 {
					t.Fatal(report, err)
				}
				return
			}
			if _, err := ImportSoloEvents(context.Background(), db, owner); err == nil {
				t.Fatal("invalid input silently imported")
			}
			var count int64
			db.Table("chat_event_chunks").Count(&count)
			if count != 0 {
				t.Fatal("invalid source reserved a journal range")
			}
		})
	}
	db, owner, _ := fixture(t)
	insertSource(t, db, "original", 1, time.Now().UTC())
	if _, err := ImportSoloEvents(context.Background(), db, uuid.NewString()); err == nil {
		t.Fatal("unowned offline import accepted")
	}
	if err := db.Transaction(func(tx *gorm.DB) error {
		_, err := ImportSoloEvents(context.Background(), tx, owner)
		if !errors.Is(err, chatsession.ErrPayloadInTransaction) {
			t.Fatal(err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&storageguard.Runtime{ID: uuid.NewString()}).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := ImportSoloEvents(context.Background(), db, owner); err == nil {
		t.Fatal("serving runtime accepted")
	}
}

func TestSoloImportKeysetCoversOldTimestampRepresentationsAndRejectsNull(t *testing.T) {
	db, owner, _ := fixture(t)
	for i := range 130 {
		at := "2026-11-02 12:00:00+00:00"
		if i >= 65 {
			at = "2026-11-02T12:00:01Z"
		}
		if err := db.Exec("INSERT INTO solo_messages (id,session_id,role,events,created_at) VALUES (?,?,?,?,?)", fmt.Sprintf("old-%03d", i), "history", "assistant", "[]", at).Error; err != nil {
			t.Fatal(err)
		}
	}
	report, err := ImportSoloEvents(context.Background(), db, owner)
	if err != nil || report.Messages != 130 || report.EmptyMessages != 130 || report.Events != 0 {
		t.Fatal("keyset skipped/repeated old rows", report, err)
	}
	verified, err := VerifySoloEvents(context.Background(), db, owner)
	if err != nil || report != verified {
		t.Fatal("keyset verification changed set", verified, err)
	}
	if err := db.Exec("INSERT INTO solo_messages (id,session_id,role,events,created_at) VALUES (?,?,?,?,NULL)", "null-time", "history", "assistant", "[]").Error; err != nil {
		t.Fatal(err)
	}
	if _, err := ImportSoloEvents(context.Background(), db, owner); err == nil {
		t.Fatal("missing timestamps silently escaped keyset/claimed completion")
	}
}

// The opt-in integration fixture is generated by the actual v3.2.0 model and
// serializer, not by a current model with a fabricated version label.
func TestSoloImportActualV320Database(t *testing.T) {
	input := os.Getenv("GRASP_HISTORY_V320_FIXTURE")
	if input == "" {
		t.Skip("set GRASP_HISTORY_V320_FIXTURE to synthetic actual-v3.2 fixture directory")
	}
	data, err := os.ReadFile(filepath.Join(input, "legacy.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "old.sqlite")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	db, err := gorm.Open(sqlite.Open(path), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	pool, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = pool.Close() })
	if db.Migrator().HasColumn("solo_messages", "event_log") || db.Migrator().HasColumn("solo_messages", "turn_id") {
		t.Fatal("fixture is not the original v3.2 schema")
	}
	if err := filestore.Attach(db, filepath.Join(t.TempDir(), "files")); err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.SoloMessage{}, &models.ChatQueue{}, &models.ChatEventChunk{}); err != nil {
		t.Fatal(err)
	}
	owner := uuid.NewString()
	if err := storageguard.Enter(context.Background(), db, owner, nil); err != nil {
		t.Fatal(err)
	}
	report, err := ImportSoloEvents(context.Background(), db, owner)
	if err != nil || report.Messages != 1 || report.Events != 6000 || report.PossiblyTruncated != 1 {
		t.Fatalf("actual v3.2 report: %+v %v", report, err)
	}
	var baseline struct {
		EventsSHA256 string `json:"eventsSHA256"`
	}
	raw, err := os.ReadFile(filepath.Join(input, "baseline.json"))
	if err != nil || json.Unmarshal(raw, &baseline) != nil {
		t.Fatal(err)
	}
	source, err := readSource(context.Background(), db, "old-agent", "old-solo")
	if err != nil {
		t.Fatal(err)
	}
	pointer, err := parsePointer(source.EventLog)
	if err != nil || pointer.Legacy.SHA256 != baseline.EventsSHA256 || source.TurnID != "" {
		t.Fatalf("original array identity changed: %+v %v", pointer, err)
	}
	after, total := int64(-1), 0
	for {
		page, incomplete, err := ReadSoloMessagePage(context.Background(), db, "old-agent", "old-solo", after)
		if err != nil || !incomplete {
			t.Fatal(err)
		}
		total += len(page.Items)
		if !page.HasMore {
			break
		}
		after = page.Next
	}
	if total != 6000 {
		t.Fatal("actual old process was lost", total)
	}
	resumed, err := ImportSoloEvents(context.Background(), db, owner)
	if err != nil || resumed != report {
		t.Fatal("actual fixture resume mismatch", resumed, err)
	}
}
