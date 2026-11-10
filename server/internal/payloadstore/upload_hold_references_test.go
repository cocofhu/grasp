package payloadstore

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/config"
	"github.com/cocofhu/grasp/internal/database"
	"github.com/cocofhu/grasp/internal/filestore"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/objectstore"
	"github.com/cocofhu/grasp/internal/payloadhold"
	"github.com/cocofhu/grasp/internal/storageguard"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type uploadHoldAuthority struct{}

func (uploadHoldAuthority) Verify(ctx context.Context, _ string) error { return ctx.Err() }

type holdReplicaObjects struct {
	*replicaStore
	failKey string
	puts    atomic.Int32
}

func (s *holdReplicaObjects) Put(ctx context.Context, key string, body io.ReadSeeker, info objectstore.Info) error {
	s.puts.Add(1)
	if key == s.failKey {
		return errors.New("second manifest was not published")
	}
	return s.replicaStore.Put(ctx, key, body, info)
}

func TestUploadHoldOnlyRootsSurviveDestinationOnlyColdRestore(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	cfg := config.DatabaseConfig{Driver: "sqlite", Path: filepath.Join(dir, "live.db")}
	db, err := database.OpenWithFiles(cfg, filepath.Join(dir, "live-files"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close(db) })
	source, target := &replicaStore{objects: map[string]replicaBytes{}}, &replicaStore{objects: map[string]replicaBytes{}}
	partialManifest := `{"version":1,"entries":[],"capture":"partial"}`
	partialSHA := sha256.Sum256([]byte(partialManifest))
	partialDigest := hex.EncodeToString(partialSHA[:])
	objects := &holdReplicaObjects{replicaStore: source, failKey: "payloads/sha256/" + partialDigest[:2] + "/" + partialDigest}
	files, _ := filestore.FromDB(db)
	if err := files.SetObjectStore(objects); err != nil {
		t.Fatal(err)
	}
	sourceLocation, destination, err := Locations(config.StorageConfig{Driver: "s3", S3: config.S3StorageConfig{Endpoint: "https://source.test", Bucket: "private", Prefix: "holds"}}, config.StorageConfig{Driver: "s3", S3: config.S3StorageConfig{Endpoint: "https://destination.test", Bucket: "private", Prefix: "backup"}})
	if err != nil {
		t.Fatal(err)
	}
	if err := seedPriorPrimary(db, sourceLocation); err != nil {
		t.Fatal(err)
	}
	m, err := payloadhold.New(db, files, payloadhold.Options{StorageFingerprint: sourceLocation.Fingerprint, Authority: uploadHoldAuthority{}, Authorize: func(ctx context.Context, _ *gorm.DB, _ payloadhold.Identity) error { return ctx.Err() }})
	if err != nil {
		t.Fatal(err)
	}
	baseline, err := VerifyReferences(ctx, db, source)
	if err != nil {
		t.Fatal(err)
	}
	opener := func(body string) payloadhold.Opener {
		return func(context.Context, int64) (io.ReadCloser, error) { return io.NopCloser(strings.NewReader(body)), nil }
	}
	for _, state := range []string{"verified-unpinned", "partial-unknown", "planned-only"} {
		id := payloadhold.Identity{ScopeKind: "solo", ScopeID: "solo-1", ActorID: "owner", OperationID: state, SnapshotID: "snapshot-" + state, Lineage: "old-external-lineage", Deadline: time.Now().Add(time.Minute)}
		manifest := `{"version":1,"entries":[],"literal":"fileref:v1/binary/` + strings.Repeat("0", 64) + `"}`
		if state == "partial-unknown" {
			manifest = partialManifest
		}
		plan, err := m.Stage(ctx, opener("opaque archive for "+state+"\x00\xff"), opener(manifest), payloadhold.DefaultLimits())
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = plan.Close() })
		lease, err := m.BeginHold(ctx, id, plan)
		if err != nil {
			t.Fatal(err)
		}
		if state == "planned-only" {
			continue
		}
		_, err = m.UploadAndVerify(ctx, lease, plan)
		if state == "verified-unpinned" && err != nil || state == "partial-unknown" && err == nil {
			t.Fatalf("unexpected capture fixture state %s: %v", state, err)
		}
	}
	if report, err := VerifyReferences(ctx, db, source); err != nil || report.Payloads != 3 || report.Cells != baseline.Cells+3 {
		t.Fatalf("unpublished holds were absent from the production catalogue: %+v %v", report, err)
	}
	beforePuts := objects.puts.Load()
	if report, err := filestore.Migrate(ctx, db, &models.PayloadUploadHold{}); err != nil || len(report.Failures) != 0 || report.Migrated != 0 || report.Verified != 3 || objects.puts.Load() != beforePuts {
		t.Fatalf("migration reparsed/reuploaded raw refs or planned absent objects: %+v %v", report, err)
	}
	owner := uuid.NewString()
	if err := storageguard.Enter(ctx, db, owner, nil); err != nil {
		t.Fatal(err)
	}
	backup := filepath.Join(dir, "backup.db")
	if err := database.Snapshot(ctx, db, cfg, owner, backup); err != nil {
		t.Fatal(err)
	}
	databaseHash, err := FileSHA256(backup)
	if err != nil {
		t.Fatal(err)
	}
	header := ReplicaHeader{Version: 1, ID: owner, Source: sourceLocation, Destination: destination, DatabaseSHA256: databaseHash, CreatedAt: time.Now().UTC()}
	var manifest bytes.Buffer
	summary, err := WriteReplica(ctx, db, source, target, header, &manifest)
	if err != nil || summary.References.Payloads != 3 || summary.Objects.Files != 3 {
		t.Fatalf("hold-only roots omitted from replica: %+v %v", summary, err)
	}
	backupBytes, err := os.ReadFile(backup)
	if err != nil {
		t.Fatal(err)
	}
	restoredPath := filepath.Join(dir, "restored.db")
	if err := os.WriteFile(restoredPath, backupBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	restored, err := database.ConnectWithFiles(config.DatabaseConfig{Driver: "sqlite", Path: restoredPath}, filepath.Join(dir, "empty-cold-files"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close(restored) })
	clear(source.objects)
	if _, verified, err := VerifyReplica(ctx, restored, target, bytes.NewReader(manifest.Bytes())); err != nil || verified.References != summary.References {
		t.Fatalf("destination alone could not verify unpinned/partial roots: %+v %v", verified, err)
	}
	var holds []models.PayloadUploadHold
	if err := restored.Find(&holds).Error; err != nil || len(holds) != 3 {
		t.Fatalf("cold SQL lost pending intent ownership: %+v %v", holds, err)
	}
	var rawRef string
	for _, row := range holds {
		if row.ReservedBytes <= 0 || row.PinnedAt != nil {
			t.Fatal("cold restore dropped reservations or fabricated a pin")
		}
		if row.OperationID == "partial-unknown" && (row.State != payloadhold.StateUnknown || row.ArchiveRef == "" || row.ManifestRef != "") {
			t.Fatal("cold restore changed partial verification semantics")
		}
		if row.OperationID == "planned-only" && (row.ArchiveRef != "" || row.ManifestRef != "") {
			t.Fatal("planned absent objects became false verified roots")
		}
		if row.ArchiveRef != "" {
			rawRef = row.ArchiveRef
		}
	}
	_, digest, _ := filestore.Parse(rawRef)
	delete(target.objects, "payloads/sha256/"+digest[:2]+"/"+digest)
	if _, err := VerifyReferences(ctx, restored, target); err == nil {
		t.Fatal("missing unpinned hold root escaped destination-only verification")
	}
}

func TestUploadHoldCatalogueOnlySkipsTrulyPreUpgradeTables(t *testing.T) {
	ctx := context.Background()
	db, err := database.OpenWithFiles(config.DatabaseConfig{Driver: "sqlite", Path: filepath.Join(t.TempDir(), "catalogue.db")}, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close(db) })
	objects := &replicaStore{objects: map[string]replicaBytes{}}
	files, _ := filestore.FromDB(db)
	if err := files.SetObjectStore(objects); err != nil {
		t.Fatal(err)
	}
	before, err := VerifyReferences(ctx, db, objects)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Migrator().DropTable(&models.PayloadUploadHold{}, &models.PayloadUploadBudget{}); err != nil {
		t.Fatal(err)
	}
	legacy, err := VerifyReferences(ctx, db, objects)
	if err != nil || legacy != before {
		t.Fatalf("pre-upgrade SQL without hold tables changed reference hash: %+v want=%+v %v", legacy, before, err)
	}
	if report, err := filestore.Migrate(ctx, db, &models.PayloadUploadHold{}); err != nil || report.Migrated != 0 || len(report.Failures) != 0 {
		t.Fatalf("old SQL cannot skip the absent new table: %+v %v", report, err)
	}
	if err := db.AutoMigrate(&models.PayloadUploadBudget{}); err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.PayloadUploadBudget{ID: strings.Repeat("b", 64), HoldCount: 1, ReservedBytes: 10}).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := VerifyReferences(ctx, db, objects); err == nil {
		t.Fatal("budget marker was ignored when its hold roots were missing")
	}
	if _, err := filestore.Migrate(ctx, db, &models.PayloadUploadHold{}); err == nil {
		t.Fatal("migration ignored a live reservation's missing hold table")
	}
	if err := db.Migrator().DropColumn(&models.PayloadUploadBudget{}, "HoldCount"); err != nil {
		t.Fatal(err)
	}
	if _, err := VerifyReferences(ctx, db, objects); err == nil {
		t.Fatal("existing reservation table without its ownership marker was treated as old SQL")
	}
	if err := db.AutoMigrate(&models.PayloadUploadHold{}, &models.PayloadUploadBudget{}); err != nil {
		t.Fatal(err)
	}
	if err := db.Migrator().DropColumn(&models.PayloadUploadHold{}, "ManifestRef"); err != nil {
		t.Fatal(err)
	}
	if _, err := VerifyReferences(ctx, db, objects); err == nil {
		t.Fatal("existing hold table without a typed root column was accepted")
	}
	if _, err := filestore.Migrate(ctx, db, &models.PayloadUploadHold{}); err == nil {
		t.Fatal("migration hid a missing typed hold reference")
	}
}
