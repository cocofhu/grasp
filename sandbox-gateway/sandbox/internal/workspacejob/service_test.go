package workspacejob

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func jobService(t *testing.T, executor Executor) *Service {
	t.Helper()
	cfg := Config{Root: t.TempDir(), Resource: "resource-one", Lineage: "lineage-original", MaxLease: 2 * time.Second, MaxDuration: 10 * time.Second}
	if executor != nil {
		cfg.Executors = map[string]Executor{"control_probe": executor}
	}
	s, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := s.Close(ctx); err != nil {
			t.Error(err)
		}
	})
	return s
}

func jobRequest(t *testing.T, id string, generation uint64, ttl time.Duration) CreateRequest {
	t.Helper()
	spec, hash, err := Fingerprint(Spec{ID: id, Kind: "control_probe", Resource: "resource-one", Lineage: "lineage-original", Nonce: "nonce-original", Deadline: time.Now().Add(8 * time.Second), Body: json.RawMessage(`{"probe":true}`)})
	if err != nil {
		t.Fatal(err)
	}
	return CreateRequest{Spec: spec, SHA256: hash, Fence: Fence{Owner: "owner-original", Generation: generation, ExpiresAt: time.Now().Add(ttl)}}
}

func renewal(req CreateRequest, ttl time.Duration) RenewRequest {
	return RenewRequest{SHA256: req.SHA256, Lineage: req.Spec.Lineage, Fence: Fence{Owner: req.Fence.Owner, Generation: req.Fence.Generation, ExpiresAt: time.Now().Add(ttl)}}
}

func waitJob(t *testing.T, s *Service, id, state string) Receipt {
	t.Helper()
	until := time.Now().Add(4 * time.Second)
	for time.Now().Before(until) {
		receipt, err := s.Get(id)
		if err == nil && receipt.State == state {
			return receipt
		}
		time.Sleep(5 * time.Millisecond)
	}
	receipt, err := s.Get(id)
	t.Fatalf("job state = %s (%v), wanted %s", receipt.State, err, state)
	return Receipt{}
}

func TestWorkspaceJobCapabilitiesAreExplicitAndRequestsImmutable(t *testing.T) {
	s := jobService(t, nil)
	caps := s.Capabilities()
	if !caps.PersistentGeneration || !caps.RenewableLease || len(caps.Kinds) != 0 || caps.FileCapture || caps.FileRestore {
		t.Fatalf("foundation advertised an unavailable collector: %+v", caps)
	}
	req := jobRequest(t, "job-original", 1, time.Second)
	if _, err := s.Create(req); !errors.Is(err, ErrUnsupported) {
		t.Fatal(err)
	}
	if _, err := New(Config{Root: s.config.Root, Resource: s.config.Resource, Lineage: s.config.Lineage}); !errors.Is(err, ErrInUse) {
		t.Fatalf("journal admitted a second runtime: %v", err)
	}
	for _, body := range []string{`{`, `{} {}`, strings.Repeat("x", 65537)} {
		req.Spec.Body = json.RawMessage(body)
		if _, _, err := Fingerprint(req.Spec); err == nil {
			t.Fatal("accepted malformed or oversized spec")
		}
	}
}

func TestWorkspaceJobMultipleRenewalsPermitLongerThanOneLease(t *testing.T) {
	finish := make(chan struct{})
	var starts atomic.Int32
	s := jobService(t, func(ctx context.Context, h *Handle, _ json.RawMessage) (json.RawMessage, error) {
		starts.Add(1)
		select {
		case <-ctx.Done():
			return nil, context.Cause(ctx)
		case <-finish:
		}
		return json.RawMessage(`{"checked":true}`), h.WriteFile("proof.txt", strings.NewReader("retained"), 128)
	})
	req := jobRequest(t, "job-original", 1, 400*time.Millisecond)
	if _, err := s.Create(req); err != nil {
		t.Fatal(err)
	}
	waitJob(t, s, req.Spec.ID, "running")
	for range 6 {
		time.Sleep(90 * time.Millisecond)
		if _, err := s.Renew(req.Spec.ID, renewal(req, 400*time.Millisecond)); err != nil {
			t.Fatal(err)
		}
	}
	close(finish)
	receipt := waitJob(t, s, req.Spec.ID, "completed")
	if starts.Load() != 1 || string(receipt.Result) != `{"checked":true}` {
		t.Fatal("operation replayed or lost result")
	}
	data, err := os.ReadFile(filepath.Join(s.config.Root, "data", req.Spec.ID, "proof.txt"))
	if err != nil || string(data) != "retained" {
		t.Fatalf("staging file: %q %v", data, err)
	}
}

func TestWorkspaceJobIdempotentCreateDoesNotRenewOrRevive(t *testing.T) {
	s := jobService(t, func(ctx context.Context, _ *Handle, _ json.RawMessage) (json.RawMessage, error) {
		<-ctx.Done()
		return nil, context.Cause(ctx)
	})
	req := jobRequest(t, "job-original", 1, 250*time.Millisecond)
	first, err := s.Create(req)
	if err != nil {
		t.Fatal(err)
	}
	waitJob(t, s, req.Spec.ID, "running")
	retry := req
	retry.Fence.ExpiresAt = time.Now().Add(time.Second)
	second, err := s.Create(retry)
	if err != nil || !second.ExpiresAt.Equal(first.ExpiresAt) {
		t.Fatal("retry-create silently extended the lease")
	}
	waitJob(t, s, req.Spec.ID, "unknown")
	if _, err := s.Renew(req.Spec.ID, renewal(req, time.Second)); !errors.Is(err, ErrFence) {
		t.Fatalf("expired owner revived: %v", err)
	}
	if receipt, err := s.Create(retry); err != nil || receipt.State != "unknown" {
		t.Fatalf("terminal unknown was replayed: %+v %v", receipt, err)
	}
}

func TestWorkspaceJobSupersessionWaitsForPredecessorAndRejectsLateResult(t *testing.T) {
	started := make(chan string, 2)
	release := make(chan struct{})
	var old *Handle
	s := jobService(t, func(ctx context.Context, h *Handle, _ json.RawMessage) (json.RawMessage, error) {
		if h.id == "job-original" {
			old = h
		}
		started <- h.id
		if h.id == "job-original" {
			<-ctx.Done()
			<-release // simulate a native helper draining after cancellation
		}
		return json.RawMessage(`{"late":true}`), nil
	})
	a := jobRequest(t, "job-original", 10, time.Second)
	if _, err := s.Create(a); err != nil {
		t.Fatal(err)
	}
	<-started
	b := jobRequest(t, "job-successor", 11, time.Second)
	if _, err := s.Create(b); err != nil {
		t.Fatal(err)
	}
	if err := old.Check(); !errors.Is(err, ErrFence) {
		t.Fatalf("old generation still allowed writes: %v", err)
	}
	select {
	case <-started:
		t.Fatal("successor began before predecessor joined")
	case <-time.After(30 * time.Millisecond):
	}
	close(release)
	if id := <-started; id != b.Spec.ID {
		t.Fatal(id)
	}
	waitJob(t, s, b.Spec.ID, "completed")
	if receipt := waitJob(t, s, a.Spec.ID, "unknown"); len(receipt.Result) != 0 {
		t.Fatal("superseded owner published late result")
	}
}

func TestWorkspaceJobBlockedRenewalCannotExtendAcknowledgedDeadline(t *testing.T) {
	cancelled := make(chan struct{})
	s := jobService(t, func(ctx context.Context, _ *Handle, _ json.RawMessage) (json.RawMessage, error) {
		<-ctx.Done()
		close(cancelled)
		return nil, context.Cause(ctx)
	})
	req := jobRequest(t, "job-original", 1, 300*time.Millisecond)
	if _, err := s.Create(req); err != nil {
		t.Fatal(err)
	}
	waitJob(t, s, req.Spec.ID, "running")
	blocked, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	s.mu.Lock()
	s.write = func(root *os.Root, name string, value any) error {
		if row, ok := value.(*record); ok && row.Receipt.ExpiresAt.After(req.Fence.ExpiresAt) {
			once.Do(func() { close(blocked); <-release })
		}
		return writeRecord(root, name, value)
	}
	s.mu.Unlock()
	done := make(chan error, 1)
	go func() { _, err := s.Renew(req.Spec.ID, renewal(req, time.Second)); done <- err }()
	<-blocked
	select {
	case <-cancelled:
	case <-time.After(time.Second):
		close(release)
		t.Fatal("blocked fsync kept native work alive after its acknowledged lease")
	}
	close(release)
	if err := <-done; !errors.Is(err, ErrFence) {
		t.Fatal("late renewal revived expired operation", err)
	}
	waitJob(t, s, req.Spec.ID, "unknown")
}

type jobBlockingReader struct{ started, release chan struct{} }

func (r jobBlockingReader) Read(p []byte) (int, error) {
	close(r.started)
	<-r.release
	return copy(p, "must not publish"), io.EOF
}

func TestWorkspaceJobFileChunkCannotResumeAfterExpiry(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	done := make(chan error, 1)
	s := jobService(t, func(_ context.Context, h *Handle, _ json.RawMessage) (json.RawMessage, error) {
		err := h.WriteFile("partial", jobBlockingReader{started, release}, 128)
		done <- err
		return nil, err
	})
	req := jobRequest(t, "job-original", 1, 250*time.Millisecond)
	if _, err := s.Create(req); err != nil {
		t.Fatal(err)
	}
	<-started
	time.Sleep(time.Until(req.Fence.ExpiresAt) + 20*time.Millisecond)
	close(release)
	if err := <-done; !errors.Is(err, ErrFence) {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(s.config.Root, "data", req.Spec.ID, "partial"))
	if err != nil || len(data) != 0 {
		t.Fatalf("post-expiry reader wrote bytes: %q %v", data, err)
	}
	waitJob(t, s, req.Spec.ID, "unknown")
}

func TestWorkspaceJobRestartNeverReplaysAndRetainsGenerationAndLineage(t *testing.T) {
	var starts atomic.Int32
	s := jobService(t, func(ctx context.Context, _ *Handle, _ json.RawMessage) (json.RawMessage, error) {
		starts.Add(1)
		<-ctx.Done()
		return nil, context.Cause(ctx)
	})
	req := jobRequest(t, "job-original", 77, time.Second)
	if _, err := s.Create(req); err != nil {
		t.Fatal(err)
	}
	waitJob(t, s, req.Spec.ID, "running")
	if err := s.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	// Model a process death with a fsynced dispatch/running journal. Startup
	// must classify it unknown without invoking the registered executor.
	root, err := os.OpenRoot(filepath.Join(s.config.Root, "jobs"))
	if err != nil {
		t.Fatal(err)
	}
	var row record
	if err := readRecord(root, req.Spec.ID+".json", &row); err != nil {
		t.Fatal(err)
	}
	row.Receipt.State, row.Receipt.Error = "running", ""
	if err := writeRecord(root, req.Spec.ID+".json", &row); err != nil {
		t.Fatal(err)
	}
	_ = root.Close()
	restarted, err := New(s.config)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = restarted.Close(context.Background()) }()
	if receipt, err := restarted.Get(req.Spec.ID); err != nil || receipt.State != "unknown" || starts.Load() != 1 {
		t.Fatalf("restart replayed work: %+v %v starts=%d", receipt, err, starts.Load())
	}
	stale := jobRequest(t, "job-different", 77, time.Second)
	if _, err := restarted.Create(stale); !errors.Is(err, ErrFence) {
		t.Fatal("lost durable high water", err)
	}
	if err := restarted.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	changed := s.config
	changed.Lineage = "lineage-restored"
	if _, err := New(changed); !errors.Is(err, ErrLineage) {
		t.Fatalf("SQL-only restore could take over retained runtime: %v", err)
	}
}

func TestWorkspaceJobCancellationAndPanicAreReceiptedWithoutReplay(t *testing.T) {
	for _, panics := range []bool{false, true} {
		t.Run(stringJSONBool(panics), func(t *testing.T) {
			s := jobService(t, func(ctx context.Context, _ *Handle, _ json.RawMessage) (json.RawMessage, error) {
				if panics {
					panic("executor implementation bug")
				}
				<-ctx.Done()
				return nil, context.Cause(ctx)
			})
			req := jobRequest(t, "job-original", 1, time.Second)
			if _, err := s.Create(req); err != nil {
				t.Fatal(err)
			}
			state := "failed"
			if !panics {
				waitJob(t, s, req.Spec.ID, "running")
				if _, err := s.Cancel(req.Spec.ID, renewal(req, time.Second)); err != nil {
					t.Fatal(err)
				}
				state = "cancelled"
			}
			waitJob(t, s, req.Spec.ID, state)
		})
	}
}

func stringJSONBool(v bool) string {
	if v {
		return "panic"
	}
	return "cancel"
}
