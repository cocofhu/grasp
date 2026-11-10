package services

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/chatsession"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/sandbox"
	"gorm.io/gorm"
)

type workspaceImportRuntime struct {
	db          *gorm.DB
	t           *testing.T
	selected    []sandbox.WorkspaceCodeSelection
	exports     int
	imports     int
	checks      int
	importError error
	importHook  func()
	receipt     sandbox.WorkspaceCodeReceipt
	inventory   *sandbox.WorkspaceCodeInventory
}

func (r *workspaceImportRuntime) leased(ctx context.Context, id uint) {
	r.t.Helper()
	var row models.Sandbox
	if err := r.db.First(&row, id).Error; err != nil {
		r.t.Fatal(err)
	}
	if row.OperationOwner == "" || row.Generation == 0 || row.OperationExpiresAt == nil || !row.OperationExpiresAt.After(time.Now()) {
		r.t.Fatal("provider called without a physical operation lease")
	}
	if _, ok := ctx.Value(sandboxOperationKey{}).(*sandboxOperation); !ok {
		r.t.Fatal("provider context lost operation fencing")
	}
}

func (r *workspaceImportRuntime) Inventory(ctx context.Context, id uint) (sandbox.WorkspaceCodeInventory, error) {
	r.leased(ctx, id)
	if r.inventory != nil {
		return *r.inventory, nil
	}
	return sandbox.WorkspaceCodeInventory{Version: 1, RenewableLease: true, Repositories: []sandbox.WorkspaceCodeRepository{{Path: "repo", Commit: strings.Repeat("a", 40), Head: strings.Repeat("a", 40), Supported: true}}, WorkingChanges: true}, nil
}

func TestWorkspaceImportInventoryAllowsOnlySafeCapabilityReasons(t *testing.T) {
	s, runtime, in := workspaceImportFixture(t)
	for _, reason := range []string{"repository_submodules", "repository_lfs", "repository_shallow", "repository_external_objects", "repository_external_worktree", "repository_custom_configuration", "repository_rewritten_history", "repository_unsafe_metadata", "repository_limit", "git failed at /private/host with secret diagnostic"} {
		runtime.inventory = &sandbox.WorkspaceCodeInventory{Version: 1, RenewableLease: true, Repositories: []sandbox.WorkspaceCodeRepository{{Path: "repo", Reason: reason}}}
		got, err := s.Inventory(context.Background(), "solo-import", "alice", in.WorkspaceImportSourceInput)
		if err != nil {
			t.Fatal(err)
		}
		want := reason
		if strings.HasPrefix(reason, "git ") {
			want = "repository_requires_snapshot"
		}
		if len(got.Repositories) != 1 || got.Repositories[0].Reason != want || got.Repositories[0].Supported {
			t.Fatalf("unsafe or incorrect reason: %+v", got.Repositories)
		}
	}
}

func codeArchive(t *testing.T, repositories []sandbox.WorkspaceCodeRepository, extra string) []byte {
	t.Helper()
	var data bytes.Buffer
	z := gzip.NewWriter(&data)
	tw := tar.NewWriter(z)
	manifest, err := json.Marshal(sandbox.WorkspaceCodeManifest{Version: 1, Repositories: repositories})
	if err != nil {
		t.Fatal(err)
	}
	write := func(name string, value []byte) {
		t.Helper()
		if err := tw.WriteHeader(&tar.Header{Name: name, Typeflag: tar.TypeReg, Mode: 0600, Size: int64(len(value))}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write(value); err != nil {
			t.Fatal(err)
		}
	}
	write("manifest.json", manifest)
	for i := range repositories {
		write("repos/"+stringJSON(i)+".bundle", []byte("# v2 git bundle\nselected immutable history"))
	}
	if extra != "" {
		write(extra, []byte("must not import"))
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := z.Close(); err != nil {
		t.Fatal(err)
	}
	return data.Bytes()
}

func (r *workspaceImportRuntime) Export(ctx context.Context, id uint, selected []sandbox.WorkspaceCodeSelection) (io.ReadCloser, error) {
	r.leased(ctx, id)
	r.exports++
	r.selected = selected
	repositories := []sandbox.WorkspaceCodeRepository{}
	for _, repo := range selected {
		repositories = append(repositories, sandbox.WorkspaceCodeRepository{Path: repo.Path, Commit: repo.Commit, Head: repo.Commit, Dirty: true, Untracked: true, Files: 1, Bytes: 32, Supported: true})
	}
	return io.NopCloser(bytes.NewReader(codeArchive(r.t, repositories, ""))), nil
}

func (r *workspaceImportRuntime) Import(ctx context.Context, id uint, request, digest string, archive io.Reader) (sandbox.WorkspaceCodeReceipt, error) {
	r.leased(ctx, id)
	r.imports++
	if _, err := io.Copy(io.Discard, archive); err != nil {
		r.t.Fatal(err)
	}
	repositories := []sandbox.WorkspaceCodeRepository{}
	for _, repo := range r.selected {
		repositories = append(repositories, sandbox.WorkspaceCodeRepository{Path: repo.Path, Commit: repo.Commit, Head: repo.Commit, Dirty: true, Untracked: true, Files: 1, Bytes: 32, Supported: true})
	}
	r.receipt = sandbox.WorkspaceCodeReceipt{ID: request, ArchiveSHA256: digest, Directory: sandbox.WorkspaceCodeDirectory + "/" + request, Repositories: repositories}
	if r.importHook != nil {
		r.importHook()
	}
	return r.receipt, r.importError
}

func (r *workspaceImportRuntime) Receipt(ctx context.Context, id uint, request string) (sandbox.WorkspaceCodeReceipt, error) {
	r.leased(ctx, id)
	r.checks++
	return r.receipt, nil
}

func workspaceImportFixture(t *testing.T) (*WorkspaceImportService, *workspaceImportRuntime, WorkspaceImportPreviewInput) {
	t.Helper()
	db := newTestDB(t)
	sbx := newSandboxService(t, db, &dockerState{})
	solo := NewSoloService(db, sbx, nil, NewProjectService(db))
	target := models.Sandbox{Name: "solo-target", Purpose: SandboxPurposeSolo, ThreadID: "solo-import", Status: "running"}
	source := models.Sandbox{Name: "workflow-source", Purpose: "run", RunID: "run-import", NodeID: "implement", Iteration: 2, Status: "running"}
	for _, row := range []any{&target, &source} {
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	for _, row := range []any{
		&models.SoloSession{ID: "solo-import", UserID: "alice", SandboxID: target.ID, Status: models.SoloStatusRunning},
		&models.Run{ID: "run-import", CreatedBy: "bob", Status: "completed", OriginSoloID: "bobs-private-chat"},
		&models.SoloWorkflowLink{ID: "authorized-reference", SoloID: "solo-import", RunID: "run-import", UserID: "alice", Kind: "reference"},
		&models.StateRun{RunID: "run-import", NodeID: "implement", Iteration: 2, Status: "completed"},
	} {
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	runtime := &workspaceImportRuntime{db: db, t: t}
	s := NewWorkspaceImportService(db, solo, sbx)
	s.runtime = runtime
	in := WorkspaceImportPreviewInput{WorkspaceImportSourceInput: WorkspaceImportSourceInput{RunID: "run-import", NodeID: "implement", Iteration: 2, Attempt: 1}, ClientRequestID: "preview-request-1", Repositories: []sandbox.WorkspaceCodeSelection{{Path: "repo", Commit: strings.Repeat("a", 40)}}}
	return s, runtime, in
}

func importCode(t *testing.T, err error, code string) {
	t.Helper()
	var typed *SoloError
	if !errors.As(err, &typed) || typed.Code != code {
		t.Fatalf("wanted %s, got %v", code, err)
	}
}

func TestWorkspaceImportPreviewConfirmPersistsPrivateReceiptAndChatResult(t *testing.T) {
	s, runtime, in := workspaceImportFixture(t)
	ctx := context.Background()
	sources, err := s.Sources(ctx, "solo-import", "alice", in.RunID)
	if err != nil || len(sources) != 1 || sources[0].NodeID != in.NodeID || sources[0].Attempt != 1 {
		t.Fatalf("sources: %+v %v", sources, err)
	}
	inventory, err := s.Inventory(ctx, "solo-import", "alice", in.WorkspaceImportSourceInput)
	if err != nil || inventory.WorkingChanges || len(inventory.Repositories) != 1 {
		t.Fatalf("capabilities exaggerated: %+v %v", inventory, err)
	}
	preview, err := s.Preview(ctx, "solo-import", "alice", in)
	if err != nil {
		t.Fatal(err)
	}
	if preview.State != "prepared" || runtime.imports != 0 || preview.Directory != "" || len(preview.Warnings) < 4 {
		t.Fatalf("preview mutated or hid boundaries: %+v", preview)
	}
	if _, err := s.Preview(ctx, "solo-import", "alice", in); err != nil || runtime.exports != 1 {
		t.Fatalf("retry exported new source: %d %v", runtime.exports, err)
	}
	changed := in
	changed.Repositories = []sandbox.WorkspaceCodeSelection{{Path: "repo", Commit: strings.Repeat("b", 40)}}
	_, err = s.Preview(ctx, "solo-import", "alice", changed)
	importCode(t, err, "workspace_import_request_conflict")
	_, err = s.Get(ctx, "solo-import", "mallory", preview.ID)
	importCode(t, err, "workspace_import_not_found")
	data, err := json.Marshal(preview)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"sandboxId", "Generation", "archiveRef", "fileref:", "bobs-private-chat"} {
		if strings.Contains(string(data), secret) {
			t.Fatalf("private implementation field leaked: %s", data)
		}
	}
	// The frozen bundle survives source retention expiry; confirmation does not
	// reconnect to a source or merge subsequent files from that workspace.
	if err := s.db.Where("run_id = ? AND purpose = ?", in.RunID, "run").Delete(&models.Sandbox{}).Error; err != nil {
		t.Fatal(err)
	}
	confirmed := WorkspaceImportConfirmInput{ClientRequestID: "confirm-request-1", Confirmed: true}
	result, err := s.Confirm(ctx, "solo-import", "alice", preview.ID, confirmed)
	if err != nil {
		t.Fatal(err)
	}
	if result.State != "applied" || len(result.Paths) != 1 || result.Paths[0] != result.Directory+"/repo" || runtime.imports != 1 {
		t.Fatalf("wrong import receipt: %+v", result)
	}
	if _, err := s.Confirm(ctx, "solo-import", "alice", preview.ID, confirmed); err != nil || runtime.imports != 1 {
		t.Fatalf("confirmation replayed native import: %d %v", runtime.imports, err)
	}
	confirmed.ClientRequestID = "other-confirmation"
	_, err = s.Confirm(ctx, "solo-import", "alice", preview.ID, confirmed)
	importCode(t, err, "workspace_import_confirmation_conflict")
	messages, err := s.solo.Messages("solo-import", "alice")
	if err != nil || len(messages) != 1 || !strings.Contains(messages[0].Content, result.Paths[0]) || !strings.Contains(messages[0].Content, in.Repositories[0].Commit) {
		t.Fatalf("import result not referenceable in Solo: %+v %v", messages, err)
	}
	var session models.SoloSession
	if err := s.db.First(&session, "id = ?", "solo-import").Error; err != nil {
		t.Fatal(err)
	}
	if !session.ConversationContextPending || session.WorkflowContextVersion != 1 {
		t.Fatal("next Solo turn did not receive import context")
	}
	preamble, err := s.solo.contextPreambleChecked("solo-import", "next-turn", "Continue the existing chat")
	if err != nil || !strings.Contains(preamble, result.Paths[0]) {
		t.Fatalf("continuing model cannot locate imported code: %q %v", preamble, err)
	}
}

func TestWorkspaceImportUnknownReconcilesWithoutRepeatingNativeImport(t *testing.T) {
	s, runtime, in := workspaceImportFixture(t)
	ctx := context.Background()
	preview, err := s.Preview(ctx, "solo-import", "alice", in)
	if err != nil {
		t.Fatal(err)
	}
	runtime.importError = errors.New("connection lost after checkout and receipt")
	confirm := WorkspaceImportConfirmInput{ClientRequestID: "confirm-unknown", Confirmed: true}
	_, err = s.Confirm(ctx, "solo-import", "alice", preview.ID, confirm)
	importCode(t, err, "workspace_import_unknown")
	current, err := s.Get(ctx, "solo-import", "alice", preview.ID)
	if err != nil || current.State != "unknown" {
		t.Fatalf("uncertain operation claimed failure/success: %+v %v", current, err)
	}
	_, err = s.Confirm(ctx, "solo-import", "alice", preview.ID, confirm)
	importCode(t, err, "workspace_import_reconcile_required")
	result, err := s.Reconcile(ctx, "solo-import", "alice", preview.ID)
	if err != nil || result.State != "applied" || runtime.imports != 1 || runtime.checks != 1 {
		t.Fatalf("reconcile repeated operation: %+v %d %d %v", result, runtime.imports, runtime.checks, err)
	}
	if _, err := s.Reconcile(ctx, "solo-import", "alice", preview.ID); err != nil || runtime.checks != 1 {
		t.Fatalf("complete reconciliation was not idempotent: %v", err)
	}
}

func TestWorkspaceImportRejectsBusyChangedTargetAndUnconfirmedWrites(t *testing.T) {
	for _, test := range []string{"busy", "target_changed", "closed", "not_confirmed", "archive_missing"} {
		t.Run(test, func(t *testing.T) {
			s, runtime, in := workspaceImportFixture(t)
			ctx := context.Background()
			preview, err := s.Preview(ctx, "solo-import", "alice", in)
			if err != nil {
				t.Fatal(err)
			}
			want := ""
			confirm := WorkspaceImportConfirmInput{ClientRequestID: "confirm-rejected", Confirmed: true}
			switch test {
			case "busy":
				_, err = s.solo.durable.Admit(ctx, soloQueueScope("solo-import"), chatsession.QueueIntent{ID: "pending-turn", ActorID: "alice", ClientMessageID: "pending-request", Fingerprint: strings.Repeat("a", 64), Payload: []byte(`{"ID":"pending-turn","Text":"keep editing"}`)}, 0, nil)
				want = "session_busy"
			case "target_changed":
				err = s.db.Model(&models.SoloSession{}).Where("id = ?", "solo-import").Update("sandbox_id", 999).Error
				want = "workspace_target_changed"
			case "closed":
				err = s.db.Model(&models.SoloSession{}).Where("id = ?", "solo-import").Update("status", models.SoloStatusClosed).Error
				want = "workspace_target_changed"
			case "not_confirmed":
				confirm.Confirmed = false
				want = "workspace_import_confirmation_required"
			case "archive_missing":
				err = s.db.Model(&models.WorkspaceImport{}).Where("id = ?", preview.ID).Update("archive_ref", "fileref:v1/binary/missing").Error
				want = "workspace_archive_unavailable"
			}
			if err != nil {
				t.Fatal(err)
			}
			_, err = s.Confirm(ctx, "solo-import", "alice", preview.ID, confirm)
			importCode(t, err, want)
			if test == "busy" {
				var rejected *SoloError
				if !errors.As(err, &rejected) || rejected.Params["confirmationAccepted"] != false || rejected.Params["importId"] != preview.ID || rejected.Params["clientRequestId"] != confirm.ClientRequestID {
					t.Fatalf("known queue rejection did not return exact pre-admission proof: %#v", rejected)
				}
			}
			if runtime.imports != 0 {
				t.Fatal("provider changed files after rejected confirmation")
			}
			current, err := s.Get(ctx, "solo-import", "alice", preview.ID)
			if err != nil || current.State != "prepared" {
				t.Fatalf("rejected operation was accepted: %+v %v", current, err)
			}
		})
	}
}

func TestWorkspaceImportLostPhysicalFenceCannotPublishSuccess(t *testing.T) {
	s, runtime, in := workspaceImportFixture(t)
	ctx := context.Background()
	preview, err := s.Preview(ctx, "solo-import", "alice", in)
	if err != nil {
		t.Fatal(err)
	}
	runtime.importHook = func() {
		if err := s.db.Model(&models.Sandbox{}).Where("purpose = ? AND thread_id = ?", SandboxPurposeSolo, "solo-import").Updates(map[string]any{"operation_owner": "successor", "generation": gorm.Expr("generation + 1"), "operation_expires_at": time.Now().Add(time.Minute)}).Error; err != nil {
			t.Fatal(err)
		}
	}
	_, err = s.Confirm(ctx, "solo-import", "alice", preview.ID, WorkspaceImportConfirmInput{ClientRequestID: "confirm-lost-fence", Confirmed: true})
	if !errors.Is(err, errSandboxOperationLost) {
		t.Fatalf("stale provider result acknowledged: %v", err)
	}
	current, err := s.Get(ctx, "solo-import", "alice", preview.ID)
	if err != nil || current.State != "applying" || current.Directory != "" {
		t.Fatalf("lost worker rewrote state: %+v %v", current, err)
	}
	var count int64
	if err := s.db.Model(&models.SoloMessage{}).Count(&count).Error; err != nil || count != 0 {
		t.Fatal("lost worker published a success notice")
	}
}

func TestWorkspaceImportSourceAuthorizationDoesNotUseCreatedByAsOwner(t *testing.T) {
	s, runtime, in := workspaceImportFixture(t)
	ctx := context.Background()
	// The fixture is Alice's authorized reference to Bob's workspace Run. Its
	// source code is allowed; Bob's private originating chat is never requested.
	if _, err := s.Inventory(ctx, "solo-import", "alice", in.WorkspaceImportSourceInput); err != nil {
		t.Fatal(err)
	}
	if err := s.db.Where("solo_id = ?", "solo-import").Delete(&models.SoloWorkflowLink{}).Error; err != nil {
		t.Fatal(err)
	}
	_, err := s.Preview(ctx, "solo-import", "alice", in)
	importCode(t, err, "workflow_not_found")
	if runtime.exports != 0 {
		t.Fatal("unauthorized source reached a provider")
	}
}

func TestWorkspaceImportArchiveRejectsAdditionalEntriesAndMismatchedCommit(t *testing.T) {
	repos := []sandbox.WorkspaceCodeRepository{{Path: "repo", Commit: strings.Repeat("a", 40), Head: strings.Repeat("a", 40), Supported: true, Files: 1, Bytes: 12}}
	for _, extra := range []string{"../escape", "other-file", "repos/9.bundle"} {
		file, _, _, err := canonicalCodeArchive(context.Background(), bytes.NewReader(codeArchive(t, repos, extra)))
		if file != nil {
			_ = file.Close()
		}
		if err == nil {
			t.Fatalf("extra archive path accepted: %s", extra)
		}
	}
	repos[0].Commit = "HEAD"
	file, _, _, err := canonicalCodeArchive(context.Background(), bytes.NewReader(codeArchive(t, repos, "")))
	if file != nil {
		_ = file.Close()
	}
	if err == nil {
		t.Fatal("mutable branch name accepted as frozen commit")
	}
}
