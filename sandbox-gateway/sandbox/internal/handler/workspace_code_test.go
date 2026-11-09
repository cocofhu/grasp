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
	_, newerCancel, ok := h.operation(c)
	if !ok {
		t.Fatal("successor was rejected")
	}
	defer newerCancel()
	if old.Err() != context.Canceled {
		t.Fatal("superseded runtime request was not cancelled")
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
		if ctx.Err() != context.DeadlineExceeded {
			t.Fatal(ctx.Err())
		}
	case <-time.After(time.Second):
		t.Fatal("runtime survived acknowledged lease expiry")
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
