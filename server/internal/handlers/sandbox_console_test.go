package handlers_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/auth"
	"github.com/cocofhu/grasp/internal/models"
)

func TestSandboxProxyBusyReturnsTemporaryFailureInsteadOfMissingCodeServer(t *testing.T) {
	hn := newHarness(t)
	expires := time.Now().Add(time.Minute)
	row := models.Sandbox{Name: "console-preparing", Purpose: "solo", Status: "running", CodeServerPort: 8080,
		OperationOwner: "preparing-owner", OperationKind: "turn", OperationExpiresAt: &expires}
	if err := hn.db.Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()
	req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/sandbox/%d/", row.ID), nil).WithContext(ctx)
	req.AddCookie(&http.Cookie{Name: auth.CookieName, Value: hn.cookie})
	w := httptest.NewRecorder()
	hn.r.ServeHTTP(w, req)
	if w.Code != http.StatusServiceUnavailable || strings.Contains(w.Body.String(), "no code-server") {
		t.Fatalf("busy sandbox was reported as missing its IDE: %d %s", w.Code, w.Body)
	}
}
