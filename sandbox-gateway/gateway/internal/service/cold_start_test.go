package service

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"sandbox-gateway/internal/driver"
	"sandbox-gateway/internal/models"
	"sandbox-gateway/internal/store"
)

type coldServiceDriver struct {
	driver.Driver
	mu               sync.Mutex
	starts, reads    int
	receipt          *driver.ColdStartReceipt
	failACK          bool
	entered, proceed chan struct{}
}

func (d *coldServiceDriver) PreviewColdStart(context.Context, string) (*driver.ColdStartTarget, error) {
	return &driver.ColdStartTarget{NativeIdentity: "original-native", WorkspaceDir: "/root/workspace"}, nil
}
func (d *coldServiceDriver) StartConfigured(ctx context.Context, id string, opts driver.StartOptions) (*driver.ColdStartReceipt, error) {
	d.mu.Lock()
	d.starts++
	d.mu.Unlock()
	if d.entered != nil {
		close(d.entered)
		select {
		case <-d.proceed:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	if err := d.Driver.Start(ctx, id); err != nil {
		return nil, err
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	d.receipt = &driver.ColdStartReceipt{RequestID: opts.ColdConfig.RequestID, InputSHA256: opts.ColdConfig.InputSHA256, PayloadSHA256: opts.ColdConfig.PayloadSHA256, NativeIdentity: "original-native", WorkspaceDir: "/root/workspace", State: "started"}
	if d.failACK {
		return nil, errors.New("native start ACK lost")
	}
	copy := *d.receipt
	return &copy, nil
}
func (d *coldServiceDriver) GetColdStart(context.Context, string, string) (*driver.ColdStartReceipt, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.reads++
	if d.receipt == nil {
		return nil, driver.ErrColdStartUnknown
	}
	copy := *d.receipt
	return &copy, nil
}

func serviceColdOptions() driver.StartOptions {
	env := map[string]string{"ROOT_PASSWORD": "synthetic-current", "ACP_BRIDGE_PASSWORD": "synthetic-current", "CONFIG_ROOT": "/root/.cursor", "WORKSPACE_DIR": "/root/workspace", "GIT_REPOS": "", "GRASP_RUNTIME_URL": "http://platform/runtime", "SANDBOX_INJECT_HEADERS": "Authorization: Bearer synthetic", "SANDBOX_INJECT": "", "MODEL_TOKEN": "synthetic-current"}
	c := &driver.ColdStartConfig{ExpectedNativeIdentity: "original-native", RequestID: "review-request-cold", InputSHA256: strings.Repeat("a", 64), Env: env, Injections: []driver.ColdConfigInjection{{URL: "http://platform/git", Destination: "/tmp/grasp-ssh-inject", Bytes: 10, SHA256: strings.Repeat("b", 64)}, {URL: "http://platform/config", Destination: env["CONFIG_ROOT"], Bytes: 20, SHA256: strings.Repeat("c", 64)}}}
	opts := driver.StartOptions{BootstrapScript: "#!/bin/bash\ntrue\n", RuntimeURL: env["GRASP_RUNTIME_URL"], RuntimeHeaders: env["SANDBOX_INJECT_HEADERS"], ColdConfig: c}
	c.PayloadSHA256 = driver.ColdStartPayloadSHA256(opts)
	return opts
}

func TestColdStartGatewayRestartRecoversLostACKWithoutAnotherProviderStart(t *testing.T) {
	svc, drv, st := testService(t)
	ctx := context.Background()
	sb, err := svc.Create(ctx, CreateRequest{Env: map[string]string{"GRASP_RUNTIME_URL": "http://old", "REVOKED": "synthetic-old"}})
	if err != nil {
		t.Fatal(err)
	}
	waitSvcRunning(t, svc, sb.ID)
	if err := svc.Stop(ctx, sb.ID); err != nil {
		t.Fatal(err)
	}
	native := &coldServiceDriver{Driver: drv, failACK: true}
	svc.drv = native
	opts := serviceColdOptions()
	if _, err := svc.StartConfigured(ctx, sb.ID, opts); !errors.Is(err, driver.ErrColdStartUnknown) {
		t.Fatalf("native ambiguity lost: %v", err)
	}
	// A fresh service over the same SQL and provider observes the original ID.
	restarted := New(native, st, svc.cfg)
	receipt, err := restarted.StartConfigured(ctx, sb.ID, opts)
	if err != nil || receipt.State != "started" {
		t.Fatalf("accepted nonce lost after gateway restart: %+v %v", receipt, err)
	}
	waitSvcRunning(t, restarted, sb.ID)
	row, err := st.Get(ctx, sb.ID)
	if err != nil {
		t.Fatal(err)
	}
	if row.Env()["REVOKED"] != "" || row.Env()["MODEL_TOKEN"] != "synthetic-current" || row.ID != sb.ID {
		t.Fatal("gateway merged stale authorization or changed original binding")
	}
	if _, err := restarted.GetColdStart(ctx, sb.ID, "another-request"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("foreign nonce resolved: %v", err)
	}
	for _, change := range []string{"input", "payload"} {
		modified := serviceColdOptions()
		if change == "input" {
			modified.ColdConfig.InputSHA256 = strings.Repeat("d", 64)
		} else {
			modified.ColdConfig.Env["MODEL_TOKEN"] = "synthetic-changed"
		}
		modified.ColdConfig.PayloadSHA256 = driver.ColdStartPayloadSHA256(modified)
		if _, err := restarted.StartConfigured(ctx, sb.ID, modified); !errors.Is(err, driver.ErrColdStartConflict) {
			t.Fatalf("changed %s accepted original nonce: %v", change, err)
		}
	}
	native.mu.Lock()
	defer native.mu.Unlock()
	if native.starts != 1 {
		t.Fatalf("provider start replayed %d times", native.starts)
	}
}

func TestColdStartPendingFencesOtherLifecycleAndConcurrentServiceRequests(t *testing.T) {
	svc, drv, st := testService(t)
	ctx := context.Background()
	sb, err := svc.Create(ctx, CreateRequest{Env: map[string]string{"GRASP_RUNTIME_URL": "http://old"}})
	if err != nil {
		t.Fatal(err)
	}
	waitSvcRunning(t, svc, sb.ID)
	if err := svc.Stop(ctx, sb.ID); err != nil {
		t.Fatal(err)
	}
	native := &coldServiceDriver{Driver: drv, entered: make(chan struct{}), proceed: make(chan struct{})}
	svc.drv = native
	opts := serviceColdOptions()
	done := make(chan error, 1)
	go func() { _, err := svc.StartConfigured(ctx, sb.ID, opts); done <- err }()
	<-native.entered
	other := New(native, st, svc.cfg)
	var workers sync.WaitGroup
	for range 16 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			if _, err := other.StartConfigured(ctx, sb.ID, opts); !errors.Is(err, driver.ErrColdStartUnknown) {
				t.Errorf("pending same nonce result: %v", err)
			}
		}()
	}
	workers.Wait()
	if err := other.Start(ctx, sb.ID); !errors.Is(err, driver.ErrColdStartUnknown) {
		t.Fatalf("ordinary Start escaped pending full authorization: %v", err)
	}
	if err := other.Destroy(ctx, sb.ID); !errors.Is(err, driver.ErrColdStartUnknown) {
		t.Fatalf("Destroy erased unknown original disk: %v", err)
	}
	if err := other.Reinstall(ctx, sb.ID, false); !errors.Is(err, driver.ErrColdStartUnknown) {
		t.Fatalf("Reinstall erased unknown original disk: %v", err)
	}
	if _, err := other.PreviewColdStart(ctx, sb.ID); !errors.Is(err, driver.ErrColdStartUnknown) {
		t.Fatalf("pending provider was offered as new cold target: %v", err)
	}
	row, _ := st.Get(ctx, sb.ID)
	if row == nil || row.Status != models.StatusStopped {
		t.Fatal("pending dispatch lost original stopped resource")
	}
	close(native.proceed)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	waitSvcRunning(t, svc, sb.ID)
	native.mu.Lock()
	defer native.mu.Unlock()
	if native.starts != 1 {
		t.Fatalf("concurrent service instances dispatched %d starts", native.starts)
	}
}

func TestColdStartUnsupportedHasNoMetadataOrProviderMutation(t *testing.T) {
	svc, _, st := testService(t)
	ctx := context.Background()
	sb, err := svc.Create(ctx, CreateRequest{})
	if err != nil {
		t.Fatal(err)
	}
	waitSvcRunning(t, svc, sb.ID)
	if _, err := svc.StartConfigured(ctx, sb.ID, serviceColdOptions()); !errors.Is(err, driver.ErrColdStartUnsupported) {
		t.Fatal(err)
	}
	if _, err := svc.PreviewColdStart(ctx, sb.ID); !errors.Is(err, driver.ErrColdStartUnsupported) {
		t.Fatal(err)
	}
	if _, err := svc.GetColdStart(ctx, sb.ID, "review-request-cold"); !errors.Is(err, driver.ErrColdStartUnsupported) {
		t.Fatal(err)
	}
	if _, err := st.ColdStart(ctx, sb.ID, "review-request-cold"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("unsupported driver reserved a cold operation: %v", err)
	}
}

func TestColdReceiptCannotPublishMismatchedNativeProof(t *testing.T) {
	svc, drv, st := testService(t)
	ctx := context.Background()
	sb, err := svc.Create(ctx, CreateRequest{})
	if err != nil {
		t.Fatal(err)
	}
	waitSvcRunning(t, svc, sb.ID)
	if err := svc.Stop(ctx, sb.ID); err != nil {
		t.Fatal(err)
	}
	svc.drv = &coldServiceDriver{Driver: drv}
	opts := serviceColdOptions()
	c := opts.ColdConfig
	row, _, err := st.ReserveColdStart(ctx, models.ColdStart{Resource: sb.ID, RequestID: c.RequestID, InputSHA256: c.InputSHA256, PayloadSHA256: c.PayloadSHA256, State: "dispatching"}, c.Env)
	if err != nil {
		t.Fatal(err)
	}
	for _, change := range []string{"nil", "nonce", "input", "payload", "missing-native", "replaced-native"} {
		t.Run(change, func(t *testing.T) {
			candidate := &driver.ColdStartReceipt{RequestID: row.RequestID, InputSHA256: row.InputSHA256, PayloadSHA256: row.PayloadSHA256, NativeIdentity: "original-native", State: "started"}
			expected := row
			switch change {
			case "nil":
				candidate = nil
			case "nonce":
				candidate.RequestID = "other-request"
			case "input":
				candidate.InputSHA256 = strings.Repeat("d", 64)
			case "payload":
				candidate.PayloadSHA256 = strings.Repeat("e", 64)
			case "missing-native":
				candidate.NativeIdentity = ""
			case "replaced-native":
				expected.NativeIdentity = "prior-native"
			}
			if _, err := svc.confirmColdStart(ctx, expected, candidate); !errors.Is(err, driver.ErrColdStartConflict) {
				t.Fatalf("mismatched proof accepted: %v", err)
			}
			saved, _ := st.ColdStart(ctx, sb.ID, row.RequestID)
			if saved.State != "dispatching" || saved.NativeIdentity != "" {
				t.Fatal("mismatched proof changed original pending receipt")
			}
		})
	}
	invalid := serviceColdOptions()
	invalid.ColdConfig.PayloadSHA256 = "invalid"
	if _, err := svc.StartConfigured(ctx, sb.ID, invalid); !errors.Is(err, driver.ErrColdStartConflict) {
		t.Fatal("invalid proof reached native dispatcher", err)
	}
}

func TestColdPendingCannotBeReleasedUsingAnEarlierStoppedPreview(t *testing.T) {
	r, native, db := releaseServiceFixture(t, EnvironmentReleaseOptions{})
	ctx := context.Background()
	preview, err := r.Preview(ctx, "environment-one", EnvironmentReleasePrepare{ID: "before-cold-review", Lineage: "lineage-one"})
	if err != nil {
		t.Fatal(err)
	}
	st := store.New(db)
	_, _, err = st.ReserveColdStart(ctx, models.ColdStart{Resource: "environment-one", RequestID: "review-cold-original", State: "dispatching"}, map[string]string{"GRASP_EXECUTION_LINEAGE": "lineage-one"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.Preview(ctx, "environment-one", EnvironmentReleasePrepare{ID: "during-cold-review", Lineage: "lineage-one"}); !errors.Is(err, driver.ErrColdStartUnknown) {
		t.Fatalf("unknown authorization was offered for loss confirmation: %v", err)
	}
	_, err = r.Confirm(ctx, "environment-one", preview.ID, EnvironmentReleaseConfirm{PlanSHA256: preview.Plan.SHA256, Nonce: "explicit-loss-original", Confirmed: true, LossAccepted: true, Owner: "owner-original", Generation: 1, ExpiresAt: time.Now().UTC().Add(r.options.LeaseTTL - time.Second), Deadline: time.Now().UTC().Add(r.options.MaxDuration - time.Second)})
	if !errors.Is(err, driver.ErrColdStartUnknown) {
		t.Fatalf("old stopped preview bypassed a pending cold operation: %v", err)
	}
	if native.deletes.Load() != 0 {
		t.Fatal("unknown cold operation lost its original disk")
	}
}
