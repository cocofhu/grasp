package handlers_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/auth"
	"github.com/cocofhu/grasp/internal/models"
	"gorm.io/gorm"
)

func TestSandboxProxyDistinguishesMissingIDEFromLookupFailure(t *testing.T) {
	for _, tc := range []struct {
		name string
		want int
	}{
		{"missing", http.StatusNotFound},
		{"no code-server", http.StatusNotFound},
		{"database failure", http.StatusServiceUnavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			hn := newHarness(t)
			id := uint(999)
			if tc.name != "missing" {
				row := models.Sandbox{Name: "lookup-console", Purpose: "test", Status: "running"}
				if tc.name == "database failure" {
					row.CodeServerPort = 8080
				}
				if err := hn.db.Create(&row).Error; err != nil {
					t.Fatal(err)
				}
				id = row.ID
			}
			if tc.name == "database failure" {
				if err := hn.db.Callback().Query().Before("gorm:query").Register("console-lookup-failure", func(tx *gorm.DB) {
					if tx.Statement.Table == "sandboxes" {
						_ = tx.AddError(errors.New("sandbox database unavailable"))
					}
				}); err != nil {
					t.Fatal(err)
				}
			}
			res := hn.do(http.MethodGet, fmt.Sprintf("/sandbox/%d/", id), nil)
			if res.Code != tc.want {
				t.Fatalf("lookup response = %d %s, want %d", res.Code, res.Body, tc.want)
			}
		})
	}
}

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
