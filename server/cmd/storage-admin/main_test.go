package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/blob"
	"github.com/cocofhu/grasp/internal/config"
	"github.com/cocofhu/grasp/internal/database"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/objectstore"
	"github.com/cocofhu/grasp/internal/payloadstore"
	"github.com/cocofhu/grasp/internal/storageguard"
	"github.com/google/uuid"
	"gopkg.in/yaml.v3"
)

func TestAdministrationCannotAccidentallyUseDefaultDatabaseOrOnlineMode(t *testing.T) {
	for _, opts := range []options{
		{mode: "copy", target: "destination", workdir: "new"},
		{mode: "restore", offline: true, workdir: "existing"},
		{mode: "verify", offline: true, target: "destination", workdir: "existing", activate: true},
		{mode: "unknown"},
	} {
		if err := validateOptions(opts); err == nil {
			t.Fatalf("unsafe mode admitted: %+v", opts)
		}
	}
	if err := run(context.Background(), options{config: filepath.Join(t.TempDir(), "missing.yaml"), mode: "status"}, io.Discard); err == nil {
		t.Fatal("missing maintenance config fell back to defaults")
	}
}

func TestCheckpointCannotBeReusedForDifferentIntent(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target.yaml")
	writeYAML(t, target, &config.Config{Storage: config.StorageConfig{Driver: "s3", S3: config.S3StorageConfig{
		Endpoint: "http://objects.test", Bucket: "private", Prefix: "backup"}}})
	opts := options{mode: "copy", target: target, workdir: filepath.Join(dir, "replica"), offline: true}
	state, err := prepareCheckpoint(opts, &config.Config{Storage: config.StorageConfig{Driver: "local", BlobsRoot: filepath.Join(dir, "source")}})
	if err != nil || state.Header.ID == "" {
		t.Fatal(err)
	}
	if _, err := prepareCheckpoint(opts, &config.Config{}); err == nil {
		t.Fatal("existing checkpoint overwritten without resume")
	}
	opts.resume, opts.activate = true, true
	if _, err := prepareCheckpoint(opts, &config.Config{}); err == nil {
		t.Fatal("resume changed backup into cutover")
	}
}

// The native SDK and command implementation operate on an isolated prefix in a
// real SeaweedFS/S3 service. No application bucket objects outside it are read
// or changed. The default unit suite remains fully offline.
func TestLiveStorageAdminMigrationAndColdBackupRestore(t *testing.T) {
	endpoint := os.Getenv("GRASP_S3_TEST_ENDPOINT")
	if endpoint == "" {
		t.Skip("set GRASP_S3_TEST_ENDPOINT and business credentials for live storage administration verification")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	dir := t.TempDir()
	pathStyle := true
	prefix := "grasp-verification/storage-admin-" + uuid.NewString()
	cfg := &config.Config{Database: config.DatabaseConfig{Driver: "sqlite", Path: filepath.Join(dir, "source.db")},
		Storage: config.StorageConfig{Driver: "s3", BlobsRoot: filepath.Join(dir, "source-local"), MaxFileBytes: 32 << 20,
			S3: config.S3StorageConfig{Endpoint: endpoint, Bucket: os.Getenv("GRASP_S3_TEST_BUCKET"), Region: "us-east-1", Prefix: prefix + "/source", PathStyle: &pathStyle,
				AccessKeyID: os.Getenv("GRASP_S3_TEST_ACCESS_KEY_ID"), SecretAccessKey: os.Getenv("GRASP_S3_TEST_SECRET_ACCESS_KEY")}}}
	if cfg.Storage.S3.Bucket == "" {
		cfg.Storage.S3.Bucket = "grasp"
	}
	db, err := database.OpenWithFiles(cfg.Database, filepath.Join(cfg.Storage.BlobsRoot, "payloads"))
	if err != nil {
		t.Fatal(err)
	}
	storage, err := payloadstore.Open(ctx, db, cfg)
	if err != nil {
		_ = database.Close(db)
		t.Fatal(err)
	}
	target := cfg.Storage
	target.S3.Prefix = prefix + "/replica"
	destination, err := payloadstore.OpenObjects(ctx, target)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		for _, store := range []objectstore.Store{storage.Objects, destination} {
			page, err := store.List(context.Background(), "", "")
			if err != nil || page.Next != "" {
				t.Errorf("isolated fixture cleanup inventory: %v", err)
				continue
			}
			for _, item := range page.Objects {
				if err := store.Delete(context.Background(), item.Key); err != nil {
					t.Errorf("fixture cleanup: %v", err)
				}
			}
		}
	})
	ref, err := storage.Blobs.Put(ctx, strings.NewReader("attachment\x00\xff"), blob.Meta{Name: "迁移附件.bin"})
	if err != nil {
		t.Fatal(err)
	}
	large := strings.Repeat("multipart\x00数据", 700000)
	for _, row := range []any{
		&models.SoloMessage{ID: "chat", SessionID: "owner", Content: "历史更正与中文", Images: []models.PromptImage{{Ref: ref.String()}}, WorkflowContext: "workflow-result-and-instructions"},
		&models.ChatPendingTurn{ID: "queued", Payload: []byte(`{"images":[{"ref":"` + ref.String() + `"}]}`)},
		&models.WorkflowInstruction{ID: "supplement", MailboxID: 1, Generation: "one", ClientMessageID: "manual", Text: "保留用户的补充要求"},
		&models.Artifact{ID: "multipart", Kind: "text", Content: large},
		&models.ArtifactVersion{ArtifactID: "multipart", Revision: 1, Content: "previous revision"},
	} {
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := database.Close(db); err != nil {
		t.Fatal(err)
	}
	before, err := storage.Objects.List(ctx, "", "")
	if err != nil || len(before.Objects) < 8 {
		t.Fatalf("nonempty source required: %d %v", len(before.Objects), err)
	}
	first := before.Objects[0]
	body, info, err := storage.Objects.Open(ctx, first.Key)
	if err != nil {
		t.Fatal(err)
	}
	file, err := objectstore.DownloadVerified(ctx, body, info.Size, info.Metadata["grasp-sha256"])
	_ = body.Close()
	if err != nil {
		t.Fatal(err)
	}
	info.Metadata["fixture-conflict"] = "must-not-overwrite"
	if err := destination.Put(ctx, first.Key, file, info); err != nil {
		t.Fatal(err)
	}
	_ = file.Close()
	sourceConfig, targetConfig := filepath.Join(dir, "source.yaml"), filepath.Join(dir, "target.yaml")
	writeYAML(t, sourceConfig, cfg)
	writeYAML(t, targetConfig, &config.Config{Storage: target})
	if err := run(ctx, options{config: sourceConfig, mode: "execution-init", offline: true, workdir: filepath.Join(dir, "execution-init")}, io.Discard); err != nil {
		t.Fatal(err)
	}
	opts := options{config: sourceConfig, target: targetConfig, mode: "copy", offline: true, activate: true, workdir: filepath.Join(dir, "replica")}
	if err := run(ctx, opts, io.Discard); err == nil {
		t.Fatal("conflicting target accepted")
	}
	control, err := database.ConnectWithFiles(cfg.Database, filepath.Join(dir, "control-local"))
	if err != nil {
		t.Fatal(err)
	}
	status, err := storageguard.Inspect(ctx, control)
	if err != nil || status.MaintenanceID == "" {
		t.Fatalf("failed copy released maintenance: %+v %v", status, err)
	}
	if _, err := storageguard.Register(ctx, control); err == nil {
		t.Fatal("startup passed interrupted migration fence")
	}
	_ = database.Close(control)
	if err := destination.Delete(ctx, first.Key); err != nil {
		t.Fatal(err)
	}
	opts.resume = true
	var report bytes.Buffer
	if err := run(ctx, opts, &report); err != nil {
		t.Fatal(err)
	}
	after, err := storage.Objects.List(ctx, "", "")
	if err != nil || len(before.Objects) != len(after.Objects) {
		t.Fatal("migration deleted source objects")
	}
	backupBytes, err := os.ReadFile(filepath.Join(opts.workdir, "database.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	beginRestore := options{config: sourceConfig, target: targetConfig, mode: "restore-begin", offline: true, workdir: opts.workdir}
	if err := run(ctx, beginRestore, io.Discard); err != nil {
		t.Fatal("SQL restore barrier before import", err)
	}
	restoredCfg := *cfg
	restoredCfg.Database.Path = filepath.Join(dir, "restored.db")
	restoredCfg.Storage.BlobsRoot = filepath.Join(dir, "cold-empty-root")
	if err := os.WriteFile(restoredCfg.Database.Path, backupBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	restoredConfig := filepath.Join(dir, "restored.yaml")
	writeYAML(t, restoredConfig, &restoredCfg)
	// Remove this fixture's source only after proving the command retained it.
	// Restore must now work solely from SQL backup + the verified destination.
	for _, item := range after.Objects {
		if err := storage.Objects.Delete(ctx, item.Key); err != nil {
			t.Fatal(err)
		}
	}
	restore := options{config: restoredConfig, target: targetConfig, mode: "restore-finalize", offline: true, workdir: opts.workdir}
	if err := run(ctx, restore, io.Discard); err != nil {
		t.Fatalf("cold restore without source bytes: %v", err)
	}
	restoredCfg.Storage.S3 = target.S3
	cold, err := database.OpenRuntime(restoredCfg.Database, filepath.Join(restoredCfg.Storage.BlobsRoot, "payloads"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cold.Close() }()
	coldStorage, err := payloadstore.Open(ctx, cold.DB, &restoredCfg)
	if err != nil {
		t.Fatal(err)
	}
	var message models.SoloMessage
	var artifact models.Artifact
	var supplement models.WorkflowInstruction
	if err := cold.DB.First(&message, "id = ?", "chat").Error; err != nil || message.Content != "历史更正与中文" || message.WorkflowContext != "workflow-result-and-instructions" {
		t.Fatalf("cold chat: %v", err)
	}
	if err := cold.DB.First(&artifact, "id = ?", "multipart").Error; err != nil || artifact.Content != large {
		t.Fatalf("cold multipart: %v", err)
	}
	if err := cold.DB.First(&supplement, "id = ?", "supplement").Error; err != nil || supplement.Text != "保留用户的补充要求" {
		t.Fatalf("cold supplement: %v", err)
	}
	read, meta, err := coldStorage.Blobs.Open(ctx, ref)
	if err != nil {
		t.Fatal(err)
	}
	attachment, err := io.ReadAll(read)
	_ = read.Close()
	if err != nil || string(attachment) != "attachment\x00\xff" || meta.Name != "迁移附件.bin" {
		t.Fatalf("cold attachment: %v", err)
	}
	var result map[string]any
	if err := json.Unmarshal(report.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	result["interruptedMigrationBlocksStartup"] = true
	result["coldRestoreWithoutSourceOrLocalFiles"] = true
	result["sourceObjectsBeforeOperatorCleanup"] = len(after.Objects)
	result["multipartPayloadBytes"] = len(large)
	result["paidModelCalls"] = 0
	result["status"] = "passed"
	if reportPath := os.Getenv("GRASP_STORAGE_ADMIN_TEST_REPORT"); reportPath != "" {
		raw, _ := json.MarshalIndent(result, "", "  ")
		if err := os.WriteFile(reportPath, raw, 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

func writeYAML(t *testing.T, path string, value any) {
	t.Helper()
	raw, err := yaml.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
}
