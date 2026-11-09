package objectstore

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/config"
	"github.com/google/uuid"
)

func TestIndependentLocalControlCASAcrossInstancesAndRestart(t *testing.T) {
	root := t.TempDir()
	first, err := NewLocalControl(root)
	if err != nil {
		t.Fatal(err)
	}
	second, err := NewLocalControl(root)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := ProbeConditional(ctx, first); err != nil {
		t.Fatal(err)
	}
	a := []byte(`{"lineage":"original","revision":1}`)
	info, err := first.PutIfAbsent(ctx, "namespace.json", bytes.NewReader(a), conditionalInfo(a))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := second.PutIfAbsent(ctx, "namespace.json", bytes.NewReader(a), conditionalInfo(a)); !errors.Is(err, ErrPrecondition) {
		t.Fatal("another local instance overwrote the original namespace", err)
	}
	type outcome struct{ err error }
	results := make(chan outcome, 2)
	start := make(chan struct{})
	for i, store := range []*LocalControl{first, second} {
		go func() {
			<-start
			body := []byte(`{"lineage":"replacement-` + strconv.Itoa(i) + `","revision":2}`)
			_, err := store.CompareAndSwap(ctx, "namespace.json", info.ETag, bytes.NewReader(body), conditionalInfo(body))
			results <- outcome{err}
		}()
	}
	close(start)
	winners, rejected := 0, 0
	for range 2 {
		err := (<-results).err
		if err == nil {
			winners++
		} else if errors.Is(err, ErrPrecondition) {
			rejected++
		} else {
			t.Fatal(err)
		}
	}
	if winners != 1 || rejected != 1 {
		t.Fatalf("local CAS did not serialize processes: success=%d rejected=%d", winners, rejected)
	}
	reopened, err := NewLocalControl(root)
	if err != nil {
		t.Fatal(err)
	}
	body, stored, err := readConditional(ctx, reopened, "namespace.json")
	if err != nil || !strings.Contains(string(body), "replacement-") || stored.ETag == info.ETag {
		t.Fatal("independent marker did not survive restart", err)
	}
	if _, err := first.CompareAndSwap(ctx, "namespace.json", info.ETag, bytes.NewReader(a), conditionalInfo(a)); !errors.Is(err, ErrPrecondition) {
		t.Fatal("an old-generation control writer revived its namespace", err)
	}
}

func TestLocalControlRejectsCorruptionAndCanceledLockWait(t *testing.T) {
	root := t.TempDir()
	store, err := NewLocalControl(root)
	if err != nil {
		t.Fatal(err)
	}
	lock, err := os.OpenFile(filepath.Join(root, controlLockName), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = lock.Close() }()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	if _, err := store.PutIfAbsent(ctx, "namespace.json", strings.NewReader(`{}`), conditionalInfo([]byte(`{}`))); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("local control ignored its shorter waiting deadline", err)
	}
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_UN); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "namespace.json"), []byte("corrupt marker"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.PutIfAbsent(context.Background(), "namespace.json", strings.NewReader(`{}`), conditionalInfo([]byte(`{}`))); err == nil {
		t.Fatal("corrupt independent marker was silently reinitialized")
	}
	for _, key := range []string{"../escape", "/absolute", controlLockName, "child/" + controlLockName} {
		if _, err := store.PutIfAbsent(context.Background(), key, strings.NewReader(`{}`), conditionalInfo([]byte(`{}`))); err == nil {
			t.Fatal("control key escaped its independent namespace", key)
		}
	}
}

type conditionalHTTPObject struct {
	body []byte
	etag string
}

func conditionalHTTPFixture(t *testing.T, enforce bool) *S3 {
	t.Helper()
	var mu sync.Mutex
	objects := map[string]conditionalHTTPObject{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		current, exists := objects[r.URL.Path]
		switch r.Method {
		case http.MethodPut:
			if enforce && (r.Header.Get("If-None-Match") == "*" && exists || r.Header.Get("If-Match") != "" && (!exists || r.Header.Get("If-Match") != current.etag)) {
				w.WriteHeader(http.StatusPreconditionFailed)
				_, _ = io.WriteString(w, `<Error><Code>PreconditionFailed</Code><Message>private-upstream-secret</Message></Error>`)
				return
			}
			body, err := io.ReadAll(r.Body)
			if err != nil {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			hash := sha256.Sum256(body)
			etag := `"` + hex.EncodeToString(hash[:]) + `"`
			objects[r.URL.Path] = conditionalHTTPObject{body: body, etag: etag}
			w.Header().Set("ETag", etag)
		case http.MethodGet:
			if !exists {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			w.Header().Set("Content-Length", strconv.Itoa(len(current.body)))
			w.Header().Set("ETag", current.etag)
			_, _ = w.Write(current.body)
		case http.MethodDelete:
			delete(objects, r.URL.Path)
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	}))
	t.Cleanup(server.Close)
	store, err := NewS3(config.S3StorageConfig{Endpoint: server.URL, Bucket: "private", Prefix: "independent-control", AccessKeyID: "test-access", SecretAccessKey: "test-secret"})
	if err != nil {
		t.Fatal(err)
	}
	return store
}

func TestConditionalS3UsesRealHeadersAndRejectsIgnoringBackend(t *testing.T) {
	for _, enforce := range []bool{true, false} {
		t.Run(strconv.FormatBool(enforce), func(t *testing.T) {
			store := conditionalHTTPFixture(t, enforce)
			err := ProbeConditional(context.Background(), store)
			if enforce && err != nil || !enforce && !errors.Is(err, ErrConditionalUnsupported) {
				t.Fatalf("unexpected conditional backend capability: enforce=%t %v", enforce, err)
			}
			if err != nil && strings.Contains(err.Error(), "private-upstream-secret") {
				t.Fatal("conditional error exposed an upstream response body")
			}
		})
	}
}

func TestConditionalS3RejectsStaleAndOversizeBeforePublishing(t *testing.T) {
	store := conditionalHTTPFixture(t, true)
	ctx := context.Background()
	if _, err := store.CompareAndSwap(ctx, "marker", "", strings.NewReader(`{}`), conditionalInfo([]byte(`{}`))); !errors.Is(err, ErrPrecondition) {
		t.Fatal("empty condition became an unconditional write")
	}
	if _, err := store.PutIfAbsent(ctx, "marker", strings.NewReader("too long"), Info{Size: 1}); err == nil {
		t.Fatal("declared size mismatch was accepted")
	}
	large := bytes.Repeat([]byte("a"), int(MaxConditionalBytes+1))
	if _, err := store.PutIfAbsent(ctx, "marker", bytes.NewReader(large), conditionalInfo(large)); err == nil {
		t.Fatal("unbounded control object accepted")
	}
	if _, err := store.PutIfAbsent(ctx, "marker", strings.NewReader(`{}`), conditionalInfo([]byte(`{}`))); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CompareAndSwap(ctx, "marker", `"stale"`, strings.NewReader(`{}`), conditionalInfo([]byte(`{}`))); !errors.Is(err, ErrPrecondition) || strings.Contains(err.Error(), "private-upstream-secret") {
		t.Fatal("conditional conflict was not safely classified", err)
	}
}

func TestS3LiveIndependentControlConditionalWrites(t *testing.T) {
	endpoint := os.Getenv("GRASP_S3_TEST_ENDPOINT")
	if endpoint == "" {
		t.Skip("set GRASP_S3_TEST_ENDPOINT and business credentials for native conditional-write verification")
	}
	pathStyle := true
	store, err := NewS3(config.S3StorageConfig{Endpoint: endpoint, Bucket: os.Getenv("GRASP_S3_TEST_BUCKET"), Region: "us-east-1", Prefix: "control-test/" + uuid.NewString(), PathStyle: &pathStyle, AccessKeyID: os.Getenv("GRASP_S3_TEST_ACCESS_KEY_ID"), SecretAccessKey: os.Getenv("GRASP_S3_TEST_SECRET_ACCESS_KEY")})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := ProbeConditional(ctx, store); err != nil {
		t.Fatal("native backend cannot safely host independent execution control", err)
	}
}
