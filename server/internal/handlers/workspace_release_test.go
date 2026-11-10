package handlers

import (
	"context"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cocofhu/grasp/internal/auth"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/services"
	"github.com/gin-gonic/gin"
)

type releaseHandlerFixture struct {
	calls           int
	actor, solo, id string
	input           services.WorkspaceReleaseConfirmInput
	err             error
}

func (f *releaseHandlerFixture) List(_ context.Context, actor, solo string) ([]services.WorkspaceReleaseView, error) {
	f.calls++
	f.actor, f.solo = actor, solo
	return []services.WorkspaceReleaseView{}, f.err
}
func (f *releaseHandlerFixture) Preview(_ context.Context, actor, solo string, _ services.WorkspaceReleasePreviewInput) (services.WorkspaceReleaseView, error) {
	f.calls++
	f.actor, f.solo = actor, solo
	return services.WorkspaceReleaseView{ID: "opaque"}, f.err
}
func (f *releaseHandlerFixture) Get(_ context.Context, actor, solo, id string) (services.WorkspaceReleaseView, error) {
	f.calls++
	f.actor, f.solo, f.id = actor, solo, id
	return services.WorkspaceReleaseView{ID: id}, f.err
}
func (f *releaseHandlerFixture) Confirm(_ context.Context, actor, solo, id string, in services.WorkspaceReleaseConfirmInput) (services.WorkspaceReleaseView, error) {
	f.calls++
	f.actor, f.solo, f.id, f.input = actor, solo, id, in
	return services.WorkspaceReleaseView{ID: id, State: "confirmed"}, f.err
}
func (f *releaseHandlerFixture) Reconcile(_ context.Context, actor, solo, id string) (services.WorkspaceReleaseView, error) {
	f.calls++
	f.actor, f.solo, f.id = actor, solo, id
	return services.WorkspaceReleaseView{ID: id}, f.err
}
func TestWorkspaceReleaseCookieIdentityStrictIntentAndReceiptProjection(t *testing.T) {
	service := &releaseHandlerFixture{}
	h := &Handlers{Auth: &auth.Service{}, WorkspaceReleases: service}
	request := func(cookie, embed bool, body string, handler func(*gin.Context)) *httptest.ResponseRecorder {
		t.Helper()
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest("POST", "/api/solo/sessions/owned/environment-releases/original/confirm?actorId=mallory", strings.NewReader(body))
		c.Params = gin.Params{{Key: "id", Value: "owned"}, {Key: "releaseId", Value: "original"}}
		if cookie {
			c.Set("auth_session", &models.Session{Username: "alice"})
		}
		if embed {
			c.Set(soloEmbedUserKey, "alice")
		}
		handler(c)
		return w
	}
	valid := `{"clientRequestId":"manual-nonce","confirmed":true,"lossAccepted":true}`
	if w := request(false, false, valid, h.ConfirmWorkspaceRelease); w.Code != 401 || service.calls != 0 {
		t.Fatal("cookie required", w.Code)
	}
	if w := request(true, true, valid, h.ConfirmWorkspaceRelease); w.Code != 403 || service.calls != 0 {
		t.Fatal("embed released environment", w.Code)
	}
	for _, body := range []string{`{"sandboxId":9,"clientRequestId":"manual-nonce"}`, valid + `{}`, `{"owner":"forged","generation":99}`, strings.Repeat(" ", 9000) + valid} {
		if w := request(true, false, body, h.ConfirmWorkspaceRelease); w.Code != 400 || service.calls != 0 {
			t.Fatal("untrusted scope or malformed body reached service", w.Code)
		}
	}
	if w := request(true, false, valid, h.ConfirmWorkspaceRelease); w.Code != 202 || service.calls != 1 || service.actor != "alice" || service.solo != "owned" || service.id != "original" || !service.input.LossAccepted {
		t.Fatal("canonical owner/route intent lost", w.Code, service)
	}
	if w := request(true, false, "", h.ReconcileWorkspaceRelease); w.Code != 200 || service.calls != 2 {
		t.Fatal("receipt reconciliation unavailable", w.Code)
	}
	h.WorkspaceReleases = nil
	if w := request(true, false, valid, h.PreviewWorkspaceRelease); w.Code != 501 || !strings.Contains(w.Body.String(), "configuration_unavailable") {
		t.Fatal(w.Code, w.Body.String())
	}
}
func TestWorkspaceReleaseErrorsAreStructuredWithoutNativeDiagnostics(t *testing.T) {
	for _, test := range []struct {
		err    error
		status int
		code   string
	}{{services.ErrWorkspaceReleaseConflict, 409, "workspace_release_scope_changed"}, {services.ErrWorkspaceReleaseBlocked, 409, "workspace_release_blocked"}, {services.ErrWorkspaceReleaseUnknown, 409, "workspace_release_reconcile_required"}, {&services.WorkspaceReleaseUnsupportedError{Reason: "driver_not_supported"}, 501, "driver_not_supported"}, {errors.New("provider secret=private-token at private-host"), 503, "workspace_release_unavailable"}} {
		t.Run(test.code, func(t *testing.T) {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			workspaceReleaseError(c, test.err)
			if w.Code != test.status || !strings.Contains(w.Body.String(), test.code) || strings.Contains(w.Body.String(), "private") {
				t.Fatal(w.Code, w.Body.String())
			}
		})
	}
}
