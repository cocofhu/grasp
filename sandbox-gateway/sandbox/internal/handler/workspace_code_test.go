package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func codeRequest(generation uint64, expires time.Time) (*gin.Context, *httptest.ResponseRecorder) {
	response := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(response)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/workspace/code/imports/test", nil)
	if generation != 0 {
		c.Request.Header.Set("X-Grasp-Operation-Generation", strconv.FormatUint(generation, 10))
	}
	if !expires.IsZero() {
		c.Request.Header.Set("X-Grasp-Operation-Expires-At", expires.UTC().Format(time.RFC3339Nano))
	}
	return c, response
}

func TestWorkspaceCodePhysicalFenceRejectsMissingExpiredAndSupersededRequests(t *testing.T) {
	h := NewWorkspaceCode(t.TempDir())
	for _, test := range []struct {
		generation uint64
		expires    time.Time
	}{
		{0, time.Time{}}, {1, time.Now().Add(-time.Second)}, {1, time.Now().Add(2 * time.Minute)},
	} {
		c, response := codeRequest(test.generation, test.expires)
		if _, _, ok := h.operation(c); ok || response.Code != http.StatusConflict {
			t.Fatal("invalid fence reached workspace mutation")
		}
	}
	c, _ := codeRequest(5, time.Now().Add(30*time.Second))
	old, cancel, ok := h.operation(c)
	if !ok {
		t.Fatal("valid acknowledged lease rejected")
	}
	defer cancel()
	c, response := codeRequest(4, time.Now().Add(30*time.Second))
	if _, _, ok := h.operation(c); ok || response.Code != http.StatusConflict {
		t.Fatal("stale generation was allowed to write")
	}
	c, _ = codeRequest(6, time.Now().Add(30*time.Second))
	type successor struct {
		cancel context.CancelFunc
		ok     bool
	}
	started := make(chan successor, 1)
	go func() { _, release, ok := h.operation(c); started <- successor{release, ok} }()
	select {
	case <-old.Done():
	case <-time.After(time.Second):
		t.Fatal("superseded runtime request was not cancelled")
	}
	select {
	case <-started:
		t.Fatal("successor entered before the original operation exited")
	case <-time.After(20 * time.Millisecond):
	}
	cancel()
	select {
	case next := <-started:
		if !next.ok {
			t.Fatal("successor was rejected after the original exited")
		}
		defer next.cancel()
	case <-time.After(time.Second):
		t.Fatal("successor did not enter after physical release")
	}
}

func TestWorkspaceCodePhysicalFenceExpiresWithoutServerConnection(t *testing.T) {
	h := NewWorkspaceCode(t.TempDir())
	c, _ := codeRequest(1, time.Now().Add(30*time.Millisecond))
	ctx, cancel, ok := h.operation(c)
	if !ok {
		t.Fatal("live short lease rejected")
	}
	defer cancel()
	select {
	case <-ctx.Done():
		if context.Cause(ctx) != context.DeadlineExceeded {
			t.Fatal(context.Cause(ctx))
		}
	case <-time.After(time.Second):
		t.Fatal("runtime survived acknowledged lease expiry")
	}
}

func TestWorkspaceCodeRenewalExtendsOnlyTheLiveOriginalGeneration(t *testing.T) {
	h := NewWorkspaceCode(t.TempDir())
	initial := time.Now().Add(200 * time.Millisecond)
	c, _ := codeRequest(7, initial)
	ctx, release, ok := h.operation(c)
	if !ok {
		t.Fatal("original operation was rejected")
	}
	defer release()
	for _, generation := range []uint64{6, 8} {
		request, response := codeRequest(generation, time.Now().Add(time.Second))
		h.Renew(request)
		if response.Code != http.StatusConflict {
			t.Fatal("different generation renewed the original operation")
		}
	}
	next := time.Now().Add(600 * time.Millisecond)
	request, response := codeRequest(7, next)
	h.Renew(request)
	if response.Code != http.StatusOK {
		t.Fatal("current generation was not renewed")
	}
	select {
	case <-ctx.Done():
		t.Fatal("renewed operation stopped at its initial deadline")
	case <-time.After(time.Until(initial) + 30*time.Millisecond):
	}
	select {
	case <-ctx.Done():
		if context.Cause(ctx) != context.DeadlineExceeded {
			t.Fatal(context.Cause(ctx))
		}
	case <-time.After(time.Second):
		t.Fatal("operation survived its last acknowledged expiry")
	}
	request, response = codeRequest(7, time.Now().Add(time.Second))
	h.Renew(request)
	if response.Code != http.StatusConflict {
		t.Fatal("expired operation was revived by renewal")
	}
}

func TestWorkspaceCodeImportRequiresFenceBeforeReadingArchive(t *testing.T) {
	h := NewWorkspaceCode(t.TempDir())
	c, response := codeRequest(0, time.Time{})
	h.Import(c)
	if response.Code != http.StatusConflict {
		t.Fatal("unfenced import was accepted")
	}
}
