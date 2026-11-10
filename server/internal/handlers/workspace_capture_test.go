package handlers

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/services"
	"github.com/gin-gonic/gin"
)

type captureAPIProbe struct {
	input                     services.WorkspaceCaptureInput
	actor, kind, scopeID, id  string
	admissions, reads, checks int
}

func (p *captureAPIProbe) Admit(_ context.Context, actor string, input services.WorkspaceCaptureInput) (services.WorkspaceSnapshotMetadata, error) {
	p.actor, p.input, p.admissions = actor, input, p.admissions+1
	return services.WorkspaceSnapshotMetadata{ID: "snapshot-original", State: "capturing"}, nil
}

func (p *captureAPIProbe) Resolve(_ context.Context, actor, kind, scopeID, id string) (services.WorkspaceSnapshotMetadata, error) {
	p.actor, p.kind, p.scopeID, p.id, p.reads = actor, kind, scopeID, id, p.reads+1
	return services.WorkspaceSnapshotMetadata{ID: "snapshot-original", State: "unknown"}, nil
}

func (p *captureAPIProbe) Reconcile(_ context.Context, actor, kind, scopeID, id string) (services.WorkspaceSnapshotMetadata, error) {
	p.actor, p.kind, p.scopeID, p.id, p.checks = actor, kind, scopeID, id, p.checks+1
	return services.WorkspaceSnapshotMetadata{ID: "snapshot-original", State: "unknown"}, nil
}

func TestWorkspaceCaptureAPIAdmitsImmutableSelectionButRejectsCallerAuthority(t *testing.T) {
	h, _, _ := restoreAPIFixture(t)
	probe := &captureAPIProbe{}
	h.WorkspaceCapture = probe
	body := `{"clientRequestId":"request-original","sandboxId":7,"expectedBinding":"opaque-original-binding","paths":["."],"includeIgnored":[],"durationMillis":60000}`
	c, w := restoreAPIContext(http.MethodPost, body, false)
	h.AdmitSoloWorkspaceSnapshot(c)
	if w.Code != http.StatusAccepted || probe.admissions != 1 || probe.actor != "alice" || probe.input.ScopeKind != "solo" || probe.input.ScopeID != "solo-original" || probe.input.SandboxID != 7 || probe.input.ExpectedBinding != "opaque-original-binding" || probe.input.Duration != time.Minute || probe.input.Binding != nil || probe.reads != 0 || probe.checks != 0 {
		t.Fatal("capture changed identity or ran a request-scoped worker", w.Code, w.Body, probe)
	}
	for _, extra := range []string{`,"actorId":"bob"`, `,"scopeId":"different"`, `,"binding":{"id":"caller-selected-root"}`, `,"destination":"/caller/path"`} {
		c, w = restoreAPIContext(http.MethodPost, strings.TrimSuffix(body, "}")+extra+"}", false)
		h.AdmitSoloWorkspaceSnapshot(c)
		if w.Code != http.StatusBadRequest || probe.admissions != 1 {
			t.Fatal("caller changed trusted capture authority", w.Code, extra, probe)
		}
	}
	c, w = restoreAPIContext(http.MethodPost, body+`{}`, false)
	h.AdmitSoloWorkspaceSnapshot(c)
	if w.Code != http.StatusBadRequest || probe.admissions != 1 {
		t.Fatal("trailing JSON accepted", w.Code, probe)
	}
	c, w = restoreAPIContext(http.MethodPost, body, true)
	h.AdmitSoloWorkspaceSnapshot(c)
	if w.Code != http.StatusForbidden || probe.admissions != 1 {
		t.Fatal("embedded chat received capture authority", w.Code, probe)
	}
}

func TestWorkspaceCaptureAPIResolutionNeverAdmitsAndReconcileUsesOriginalRun(t *testing.T) {
	h, _, _ := restoreAPIFixture(t)
	probe := &captureAPIProbe{}
	h.WorkspaceCapture = probe
	c, w := restoreAPIContext(http.MethodGet, "", false)
	c.Params = gin.Params{{Key: "id", Value: "run-original"}, {Key: "requestId", Value: "nonce-original"}}
	h.ResolveRunWorkspaceSnapshot(c)
	if w.Code != http.StatusOK || probe.reads != 1 || probe.checks != 0 || probe.admissions != 0 || probe.actor != "alice" || probe.kind != "run" || probe.scopeID != "run-original" || probe.id != "nonce-original" {
		t.Fatal("resolving a nonce executed capture or changed scope", w.Code, probe)
	}
	c, w = restoreAPIContext(http.MethodPost, "", false)
	c.Params = gin.Params{{Key: "id", Value: "run-original"}, {Key: "snapshotId", Value: "snapshot-original"}}
	h.ReconcileRunWorkspaceSnapshot(c)
	if w.Code != http.StatusOK || probe.checks != 1 || probe.admissions != 0 || probe.scopeID != "run-original" || probe.id != "snapshot-original" {
		t.Fatal("reconcile did not inspect the original capture", w.Code, probe)
	}
}
