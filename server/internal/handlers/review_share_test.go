package handlers_test

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/gateshare"
	"github.com/cocofhu/grasp/internal/models"
)

func seedInboxReview(t *testing.T, h *harness, runID, nodeID string, withArtifact bool) {
	t.Helper()
	now := time.Now()
	h.db.Create(&models.WorkflowDef{
		ID: "wf-" + runID, ProjectID: models.DefaultProjectID, Name: "review-" + runID,
		Version: 1, PublishedVersion: 1,
	})
	h.db.Create(&models.Run{
		ID: runID, WorkflowID: "wf-" + runID, WorkflowName: "review-" + runID, Status: "waiting_human",
		StartedAt: now, Title: "复审运行",
		Graph: models.Graph{Nodes: []models.Node{
			{ID: nodeID, Type: "agent", Caps: testReviewCaps, Label: "调研"},
			{ID: "clarify", Type: "agent", Caps: testClarifyCaps, Label: "澄清"},
			{ID: "hg-r", Type: "human_gate", Label: "审",
				Config: map[string]any{"title": "审阅", "body_template": "请批准", "actions": []any{
					map[string]any{"id": "approve", "label": "批准"},
					map[string]any{"id": "revise", "label": "驳回", "requireForm": true},
				}}},
		}},
	})
	h.db.Create(&models.ReactConversation{
		RunID: runID, NodeID: nodeID, Iteration: 1, Done: false,
		Messages: []models.ReactMessage{{Role: "agent", Text: "请复审 research.json", At: now.Format(time.RFC3339)}},
	})
	h.db.Create(&models.StateRun{
		RunID: runID, NodeID: nodeID, Iteration: 1, Status: "waiting_human", NodeType: "agent",
	})
	h.db.Create(&models.Gate{
		RunID: runID, NodeID: "hg-r", Iteration: 1, WorkflowID: "wf-" + runID, WorkflowName: "review-" + runID,
		Title: "审阅视觉稿", BodyMd: "请审阅",
		Actions: []models.GateAction{
			{ID: "approve", Label: "批准"},
			{ID: "revise", Label: "驳回", RequireForm: true},
		},
		Form:        []models.GateField{{Key: "comment", Label: "意见"}},
		RequestedAt: now,
	})
	if withArtifact {
		h.h.Arts.Save(runID, nodeID, "research.json", "json", `{"title":"调研摘要","goals":["g1"],"runId":"should-hide"}`)
		h.h.Arts.Save(runID, "other", "page.html", "html", `<html><body>LEAK-OTHER</body></html>`)
	}
}

func TestReviewShareCreateLookupConfirmAndInboxStatus(t *testing.T) {
	h := newHarness(t)
	seedInboxReview(t, h, "run-rev-1", "research1", true)

	w := h.do(http.MethodPost, "/api/runs/run-rev-1/reviews/research1/share-link", map[string]any{"permissionPreset": "full", "ttlTier": "24h"})
	if w.Code != http.StatusOK {
		t.Fatalf("create: %d %s", w.Code, w.Body.String())
	}
	created := parseJSON(t, w)
	url, _ := created["url"].(string)
	if !strings.Contains(url, "/public/gate-approvals#t=") {
		t.Fatalf("url fragment: %s", url)
	}
	token := strings.TrimPrefix(url[strings.Index(url, "#t="):], "#t=")
	if !gateshare.ValidTokenShape(token) {
		t.Fatalf("token shape: %s", token)
	}
	if strings.Contains(w.Body.String(), `"token"`) && !strings.Contains(url, token) {
		t.Fatal("unexpected token field")
	}

	var row models.GateShareLink
	if err := h.db.Where("run_id = ? AND node_id = ?", "run-rev-1", "research1").First(&row).Error; err != nil {
		t.Fatalf("load link: %v", err)
	}
	if row.Kind != models.ShareLinkKindReview {
		t.Fatalf("kind=%q", row.Kind)
	}
	if row.GateID != nil {
		t.Fatalf("review link must not fake GateID: %+v", row.GateID)
	}

	st := parseJSON(t, h.do(http.MethodGet, "/api/runs/run-rev-1/reviews/research1/share-link", nil))
	if st["state"] != models.ShareLinkStateActive {
		t.Fatalf("status: %+v", st)
	}
	if _, ok := st["url"]; ok {
		t.Fatalf("status leaked url: %+v", st)
	}

	list := h.do(http.MethodGet, "/api/gates", nil)
	if list.Code != 200 {
		t.Fatalf("list: %d", list.Code)
	}
	body := list.Body.String()
	if !strings.Contains(body, `"kind":"review"`) {
		t.Fatalf("inbox missing review item: %s", body)
	}
	if !strings.Contains(body, `"shareLink"`) {
		t.Fatalf("inbox missing shareLink: %s", body)
	}
	if strings.Contains(body, token) {
		t.Fatal("inbox leaked plaintext token")
	}

	prev := h.doPublic(http.MethodGet, "/public/gate-approvals/preview", nil, map[string]string{headerShareToken: token})
	p := parseJSON(t, prev)
	if p["status"] != models.ShareLinkStateActive {
		t.Fatalf("preview: %s", prev.Body.String())
	}
	if p["kind"] != models.ShareLinkKindReview {
		t.Fatalf("preview kind: %+v", p)
	}
	if p["actions"] == nil {
		t.Fatalf("preview missing actions: %+v", p)
	}
	actions, _ := p["actions"].(map[string]any)
	if actions["confirm"] != "confirm" || actions["approve"] != nil || actions["reject"] != nil {
		t.Fatalf("review preview must not expose gate reject/approve: %+v", actions)
	}
	if strings.Contains(prev.Body.String(), "run-rev-1") || strings.Contains(prev.Body.String(), "should-hide") || strings.Contains(prev.Body.String(), "LEAK-OTHER") {
		t.Fatalf("preview leak: %s", prev.Body.String())
	}
	if p["structured"] == nil {
		t.Fatalf("expected research.json structured preview: %+v", p)
	}
	if p["productKind"] != "structured" && p["productName"] != "research.json" {
		if p["productName"] != "research.json" {
			t.Fatalf("expected structured research.json product, got kind=%v name=%v", p["productKind"], p["productName"])
		}
	}
	turns, _ := p["turns"].([]any)
	if len(turns) == 0 {
		t.Fatalf("expected sanitized turns: %+v", p)
	}

	nonce := publicPreviewNonce(t, h, token)
	dec := h.doPublic(http.MethodPost, "/public/gate-approvals/decide", map[string]any{
		"token": token, "action": "confirm", "nonce": nonce,
	}, map[string]string{headerShareRequest: "1", "Origin": "http://" + publicHost})
	out := parseJSON(t, dec)
	if dec.Code != 200 || out["status"] != "confirmed" {
		t.Fatalf("decide: %d %s", dec.Code, dec.Body.String())
	}

	prevUsed := h.doPublic(http.MethodGet, "/public/gate-approvals/preview", nil, map[string]string{headerShareToken: token})
	stUsed := parseJSON(t, prevUsed)["status"]
	if stUsed != models.ShareLinkStateUsed {
		t.Fatalf("after confirm preview=%v body=%s", stUsed, prevUsed.Body.String())
	}
	re := h.do(http.MethodPost, "/api/runs/run-rev-1/reviews/research1/share-link", map[string]any{"permissionPreset": "full", "ttlTier": "24h"})
	if re.Code != http.StatusConflict {
		t.Fatalf("recreate after confirm: %d %s", re.Code, re.Body.String())
	}
}

func TestReviewShareKindIsolationAndHumanGateUnchanged(t *testing.T) {
	h := newHarness(t)
	seedInboxReview(t, h, "run-rev-iso", "research1", true)

	rev := parseJSON(t, h.do(http.MethodPost, "/api/runs/run-rev-iso/reviews/research1/share-link", map[string]any{"permissionPreset": "full", "ttlTier": "24h"}))
	gate := parseJSON(t, h.do(http.MethodPost, "/api/runs/run-rev-iso/gates/hg-r/share-link", map[string]any{"permissionPreset": "full", "ttlTier": "8h"}))
	revURL, _ := rev["url"].(string)
	gateURL, _ := gate["url"].(string)
	revTok := strings.TrimPrefix(revURL[strings.Index(revURL, "#t="):], "#t=")
	gateTok := strings.TrimPrefix(gateURL[strings.Index(gateURL, "#t="):], "#t=")

	if w := h.do(http.MethodPost, "/api/runs/run-rev-iso/gates/research1/share-link", map[string]any{"permissionPreset": "full", "ttlTier": "24h"}); w.Code == 200 {
		t.Fatalf("gates API on research must not mint a link: %s", w.Body.String())
	} else if !strings.Contains(w.Body.String(), "not_human_gate") && !strings.Contains(w.Body.String(), "gate_not_pending") {
		t.Fatalf("gates API on research: %d %s", w.Code, w.Body.String())
	}
	if w := h.do(http.MethodPost, "/api/runs/run-rev-iso/reviews/hg-r/share-link", map[string]any{"permissionPreset": "full", "ttlTier": "24h"}); w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "not_review_session") {
		t.Fatalf("reviews API on human_gate: %d %s", w.Code, w.Body.String())
	}
	if w := h.do(http.MethodPost, "/api/runs/run-rev-iso/reviews/clarify/share-link", map[string]any{"permissionPreset": "full", "ttlTier": "24h"}); strings.Contains(w.Body.String(), "not_review_session") {
		t.Fatalf("clarify must be a shareable review session: %d %s", w.Code, w.Body.String())
	} else if w.Code != http.StatusNotFound && w.Code != http.StatusBadRequest {
		t.Fatalf("reviews API on react clarify without pending conv: %d %s", w.Code, w.Body.String())
	}

	revPrev := parseJSON(t, h.doPublic(http.MethodGet, "/public/gate-approvals/preview", nil, map[string]string{headerShareToken: revTok}))
	gatePrev := parseJSON(t, h.doPublic(http.MethodGet, "/public/gate-approvals/preview", nil, map[string]string{headerShareToken: gateTok}))
	if revPrev["kind"] != models.ShareLinkKindReview {
		t.Fatalf("review preview kind: %+v", revPrev)
	}
	if gatePrev["kind"] != models.ShareLinkKindHumanGate && gatePrev["kind"] != nil && gatePrev["kind"] != "" {
		// human_gate preview historically omitted kind or set human_gate
		if gatePrev["status"] != models.ShareLinkStateActive {
			t.Fatalf("gate preview: %+v", gatePrev)
		}
	}
	if gatePrev["status"] != models.ShareLinkStateActive {
		t.Fatalf("gate preview inactive: %+v", gatePrev)
	}
	gActions, _ := gatePrev["actions"].(map[string]any)
	if gActions["approve"] == nil || gActions["reject"] == nil {
		t.Fatalf("gate preview must keep approve/reject: %+v", gActions)
	}

	regen := h.do(http.MethodPost, "/api/runs/run-rev-iso/reviews/research1/share-link/regen", nil)
	if regen.Code != 200 {
		t.Fatalf("regen review: %d %s", regen.Code, regen.Body.String())
	}
	newRevURL, _ := parseJSON(t, regen)["url"].(string)
	newRevTok := strings.TrimPrefix(newRevURL[strings.Index(newRevURL, "#t="):], "#t=")
	gateStill := parseJSON(t, h.doPublic(http.MethodGet, "/public/gate-approvals/preview", nil, map[string]string{headerShareToken: gateTok}))
	if gateStill["status"] != models.ShareLinkStateActive {
		t.Fatalf("regen review must not revoke gate link: %+v", gateStill)
	}

	revNonce := publicPreviewNonce(t, h, newRevTok)
	bad := h.doPublic(http.MethodPost, "/public/gate-approvals/decide", map[string]any{
		"token": newRevTok, "action": "approve", "nonce": revNonce,
	}, map[string]string{headerShareRequest: "1", "Origin": "http://" + publicHost})
	if bad.Code != http.StatusBadRequest || !strings.Contains(bad.Body.String(), "unsupported_action") {
		t.Fatalf("review decide approve must fail: %d %s", bad.Code, bad.Body.String())
	}

	nonce := publicPreviewNonce(t, h, gateTok)
	dec := h.doPublic(http.MethodPost, "/public/gate-approvals/decide", map[string]any{
		"token": gateTok, "action": "approve", "comment": "可以流转", "name": "Jordan", "nonce": nonce,
	}, map[string]string{headerShareRequest: "1", "Origin": "http://" + publicHost})
	if dec.Code != 200 || parseJSON(t, dec)["status"] != "approved" {
		t.Fatalf("gate decide regression: %d %s", dec.Code, dec.Body.String())
	}
}

func TestReviewShareValidationFailureDoesNotBurnLink(t *testing.T) {
	h := newHarness(t)
	seedInboxReview(t, h, "run-rev-val", "research1", false)

	created := parseJSON(t, h.do(http.MethodPost, "/api/runs/run-rev-val/reviews/research1/share-link", map[string]any{"permissionPreset": "full", "ttlTier": "24h"}))
	url, _ := created["url"].(string)
	token := strings.TrimPrefix(url[strings.Index(url, "#t="):], "#t=")

	nonce := publicPreviewNonce(t, h, token)
	dec := h.doPublic(http.MethodPost, "/public/gate-approvals/decide", map[string]any{
		"token": token, "action": "confirm", "nonce": nonce,
	}, map[string]string{headerShareRequest: "1", "Origin": "http://" + publicHost})
	out := parseJSON(t, dec)
	if out["status"] != "validation_failed" && out["error"] != "review_validation_failed" {
		t.Fatalf("expected validation_failed: %d %s", dec.Code, dec.Body.String())
	}

	prev := h.doPublic(http.MethodGet, "/public/gate-approvals/preview", nil, map[string]string{headerShareToken: token})
	if parseJSON(t, prev)["status"] != models.ShareLinkStateActive {
		t.Fatalf("link burned after validation failure: %s", prev.Body.String())
	}

	h.h.Arts.Save("run-rev-val", "research1", "research.json", "json", `{"title":"调研摘要","goals":["g1"]}`)
	nonce2 := publicPreviewNonce(t, h, token)
	dec2 := h.doPublic(http.MethodPost, "/public/gate-approvals/decide", map[string]any{
		"token": token, "action": "confirm", "nonce": nonce2,
	}, map[string]string{headerShareRequest: "1", "Origin": "http://" + publicHost})
	if dec2.Code != 200 || parseJSON(t, dec2)["status"] != "confirmed" {
		t.Fatalf("retry after artifact: %d %s", dec2.Code, dec2.Body.String())
	}
}

func TestReviewShareLoginConfirmRevokesUnusedLink(t *testing.T) {
	h := newHarness(t)
	seedInboxReview(t, h, "run-rev-login", "research1", true)

	created := parseJSON(t, h.do(http.MethodPost, "/api/runs/run-rev-login/reviews/research1/share-link", map[string]any{"permissionPreset": "full", "ttlTier": "24h"}))
	url, _ := created["url"].(string)
	token := strings.TrimPrefix(url[strings.Index(url, "#t="):], "#t=")

	w := h.do(http.MethodPost, "/api/runs/run-rev-login/react/research1/reply", map[string]any{
		"text": "确认并流转", "force": true,
	})
	if w.Code != http.StatusOK {
		t.Fatalf("login review reply: %d %s", w.Code, w.Body.String())
	}
	prev := h.doPublic(http.MethodGet, "/public/gate-approvals/preview", nil, map[string]string{headerShareToken: token})
	st := parseJSON(t, prev)["status"]
	if st != models.ShareLinkStateRevoked && st != models.ShareLinkStateUsed {
		t.Fatalf("after login confirm preview=%v body=%s", st, prev.Body.String())
	}
	re := h.do(http.MethodPost, "/api/runs/run-rev-login/reviews/research1/share-link", map[string]any{"permissionPreset": "full", "ttlTier": "24h"})
	if re.Code != http.StatusConflict {
		t.Fatalf("recreate after login confirm: %d %s", re.Code, re.Body.String())
	}
}

func TestReviewShareDoneConversationCannotCreate(t *testing.T) {
	h := newHarness(t)
	seedInboxReview(t, h, "run-rev-done", "research1", true)
	if err := h.db.Model(&models.ReactConversation{}).Where("run_id = ?", "run-rev-done").Update("done", true).Error; err != nil {
		t.Fatalf("mark done: %v", err)
	}
	w := h.do(http.MethodPost, "/api/runs/run-rev-done/reviews/research1/share-link", map[string]any{"permissionPreset": "full", "ttlTier": "24h"})
	if w.Code != http.StatusConflict && w.Code != http.StatusNotFound {
		t.Fatalf("create on done conv: %d %s", w.Code, w.Body.String())
	}
}

func TestReviewShareReplyAndCancelDoNotConsume(t *testing.T) {
	h := newHarness(t)
	seedInboxReview(t, h, "run-rev-reply", "research1", true)
	created := parseJSON(t, h.do(http.MethodPost, "/api/runs/run-rev-reply/reviews/research1/share-link", map[string]any{"permissionPreset": "full", "ttlTier": "24h"}))
	url, _ := created["url"].(string)
	token := strings.TrimPrefix(url[strings.Index(url, "#t="):], "#t=")

	reply := h.doPublic(http.MethodPost, "/public/gate-approvals/reply", map[string]any{
		"token": token, "text": "请把摘要写短一点",
	}, map[string]string{headerShareRequest: "1", "Origin": "http://" + publicHost})
	if reply.Code == http.StatusForbidden {
		t.Fatalf("reply csrf: %s", reply.Body.String())
	}
	prev := h.doPublic(http.MethodGet, "/public/gate-approvals/preview", nil, map[string]string{headerShareToken: token})
	if parseJSON(t, prev)["status"] != models.ShareLinkStateActive {
		t.Fatalf("reply burned token: %d %s preview=%s", reply.Code, reply.Body.String(), prev.Body.String())
	}

	cancel := h.doPublic(http.MethodPost, "/public/gate-approvals/cancel", map[string]any{
		"token": token,
	}, map[string]string{headerShareRequest: "1", "Origin": "http://" + publicHost})
	if cancel.Code == http.StatusForbidden {
		t.Fatalf("cancel csrf: %s", cancel.Body.String())
	}
	prev2 := h.doPublic(http.MethodGet, "/public/gate-approvals/preview", nil, map[string]string{headerShareToken: token})
	if parseJSON(t, prev2)["status"] != models.ShareLinkStateActive {
		t.Fatalf("cancel burned token: %d %s preview=%s", cancel.Code, cancel.Body.String(), prev2.Body.String())
	}

	nonce := publicPreviewNonce(t, h, token)
	dec := h.doPublic(http.MethodPost, "/public/gate-approvals/decide", map[string]any{
		"token": token, "action": "confirm", "nonce": nonce,
	}, map[string]string{headerShareRequest: "1", "Origin": "http://" + publicHost})
	out := parseJSON(t, dec)
	if dec.Code != 200 || (out["status"] != "confirmed" && out["status"] != "validation_failed" && out["status"] != "busy") {
		t.Fatalf("decide after reply: %d %s", dec.Code, dec.Body.String())
	}
}

func seedAppPreviewReview(t *testing.T, h *harness, runID, nodeID string) {
	t.Helper()
	now := time.Now()
	h.db.Create(&models.WorkflowDef{
		ID: "wf-" + runID, ProjectID: models.DefaultProjectID, Name: "preview-" + runID,
		Version: 1, PublishedVersion: 1,
	})
	h.db.Create(&models.Run{
		ID: runID, WorkflowID: "wf-" + runID, WorkflowName: "preview-" + runID, Status: "waiting_human",
		StartedAt: now, Title: "应用预览运行",
		Graph: models.Graph{Nodes: []models.Node{
			{ID: nodeID, Type: "agent", Label: "应用预览", Caps: testPreviewCaps},
		}},
	})
	h.db.Create(&models.ReactConversation{
		RunID: runID, NodeID: nodeID, Iteration: 1, Done: false,
		Messages: []models.ReactMessage{{Role: "agent", Text: "应用预览已就绪", At: now.Format(time.RFC3339)}},
	})
	h.db.Create(&models.StateRun{
		RunID: runID, NodeID: nodeID, Iteration: 1, Status: "waiting_human", NodeType: "agent",
	})
}

// TestAppPreviewShareCreateAttachPreviewAndGateAPIRejected covers plan g1/g3/g4.2:
// waiting_human preview review can mint review share links, inbox attaches shareLink,
// public preview is productKind=app (remote-capable, leak-free), and gates API must fail.
func TestAppPreviewShareCreateAttachPreviewAndGateAPIRejected(t *testing.T) {
	h := newHarness(t)
	seedAppPreviewReview(t, h, "run-ap-share", "preview1")

	w := h.do(http.MethodPost, "/api/runs/run-ap-share/reviews/preview1/share-link", map[string]any{"permissionPreset": "full", "ttlTier": "24h"})
	if w.Code != http.StatusOK {
		t.Fatalf("create preview review share: %d %s", w.Code, w.Body.String())
	}
	created := parseJSON(t, w)
	url, _ := created["url"].(string)
	if !strings.Contains(url, "/public/gate-approvals#t=") {
		t.Fatalf("url fragment: %s", url)
	}
	token := strings.TrimPrefix(url[strings.Index(url, "#t="):], "#t=")
	if !gateshare.ValidTokenShape(token) {
		t.Fatalf("token shape: %s", token)
	}

	var row models.GateShareLink
	if err := h.db.Where("run_id = ? AND node_id = ?", "run-ap-share", "preview1").First(&row).Error; err != nil {
		t.Fatalf("load link: %v", err)
	}
	if row.Kind != models.ShareLinkKindReview {
		t.Fatalf("kind=%q want review", row.Kind)
	}
	if row.GateID != nil {
		t.Fatalf("preview review link must not fake GateID: %+v", row.GateID)
	}

	st := parseJSON(t, h.do(http.MethodGet, "/api/runs/run-ap-share/reviews/preview1/share-link", nil))
	if st["state"] != models.ShareLinkStateActive {
		t.Fatalf("status: %+v", st)
	}

	list := h.do(http.MethodGet, "/api/gates", nil)
	if list.Code != 200 {
		t.Fatalf("list: %d", list.Code)
	}
	body := list.Body.String()
	if !strings.Contains(body, `"nodeId":"preview1"`) || !strings.Contains(body, `"kind":"review"`) {
		t.Fatalf("inbox missing preview review item: %s", body)
	}
	if !strings.Contains(body, `"shareLink"`) {
		t.Fatalf("inbox missing shareLink for preview review: %s", body)
	}
	if strings.Contains(body, token) {
		t.Fatal("inbox leaked plaintext token")
	}

	prev := h.doPublic(http.MethodGet, "/public/gate-approvals/preview", nil, map[string]string{headerShareToken: token})
	p := parseJSON(t, prev)
	if p["status"] != models.ShareLinkStateActive {
		t.Fatalf("preview: %s", prev.Body.String())
	}
	if p["kind"] != models.ShareLinkKindReview {
		t.Fatalf("preview kind: %+v", p)
	}
	if p["productKind"] != "app" {
		t.Fatalf("productKind=%v want app", p["productKind"])
	}
	prevBody := prev.Body.String()
	if strings.Contains(prevBody, "run-ap-share") || strings.Contains(prevBody, "preview1") {
		t.Fatalf("public preview leaked runId/nodeId: %s", prevBody)
	}
	if strings.Contains(prevBody, "/preview/") || strings.Contains(prevBody, "proxyUrl") {
		t.Fatalf("public preview leaked internal preview path: %s", prevBody)
	}
	if strings.Contains(prevBody, "公开页仅支持只读预览") || strings.Contains(prevBody, "不提供远程桌面或取点") {
		t.Fatalf("public preview must not use legacy read-only success copy: %s", prevBody)
	}
	actions, _ := p["actions"].(map[string]any)
	if actions["confirm"] != "confirm" {
		t.Fatalf("preview actions: %+v", actions)
	}

	// g3.3: preview review must not succeed via human_gate share API
	gateW := h.do(http.MethodPost, "/api/runs/run-ap-share/gates/preview1/share-link", map[string]any{"permissionPreset": "full", "ttlTier": "24h"})
	if gateW.Code == http.StatusOK {
		t.Fatalf("gates API on preview review must fail: %s", gateW.Body.String())
	}
	gateBody := gateW.Body.String()
	if !strings.Contains(gateBody, "not_human_gate") && !strings.Contains(gateBody, "gate_not_pending") {
		t.Fatalf("gates API on preview review: %d %s", gateW.Code, gateBody)
	}

	nonce := publicPreviewNonce(t, h, token)
	dec := h.doPublic(http.MethodPost, "/public/gate-approvals/decide", map[string]any{
		"token": token, "action": "confirm", "nonce": nonce,
	}, map[string]string{headerShareRequest: "1", "Origin": "http://" + publicHost})
	out := parseJSON(t, dec)
	if dec.Code != 200 || (out["status"] != "confirmed" && out["status"] != "busy" && out["status"] != "validation_failed") {
		t.Fatalf("decide preview review: %d %s", dec.Code, dec.Body.String())
	}
}

func seedInboxClarify(t *testing.T, h *harness, runID, nodeID string) {
	t.Helper()
	now := time.Now()
	h.db.Create(&models.WorkflowDef{
		ID: "wf-" + runID, ProjectID: models.DefaultProjectID, Name: "clarify-" + runID,
		Version: 1, PublishedVersion: 1,
	})
	h.db.Create(&models.Run{
		ID: runID, WorkflowID: "wf-" + runID, WorkflowName: "clarify-" + runID, Status: "waiting_human",
		StartedAt: now, Title: "澄清运行",
		Graph: models.Graph{Nodes: []models.Node{
			{ID: nodeID, Type: "agent", Caps: testClarifyCaps, Label: "需求澄清"},
			{ID: "gate", Type: "human_gate", Label: "人工门禁"},
		}},
	})
	h.db.Create(&models.ReactConversation{
		RunID: runID, NodeID: nodeID, Iteration: 1, Done: false,
		Messages: []models.ReactMessage{{Role: "agent", Text: "请补充验收标准", At: now.Format(time.RFC3339)}},
	})
	h.db.Create(&models.StateRun{
		RunID: runID, NodeID: nodeID, Iteration: 1, Status: "waiting_human", NodeType: "agent",
	})
}

func TestClarifyShareCreatePreviewInboxAndPublicCancel(t *testing.T) {
	h := newHarness(t)
	seedInboxClarify(t, h, "run-clarify-share", "clarify1")

	if w := h.do(http.MethodPost, "/api/runs/run-clarify-share/reviews/gate/share-link", map[string]any{"permissionPreset": "full", "ttlTier": "24h"}); w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "not_review_session") {
		t.Fatalf("human_gate must stay excluded: %d %s", w.Code, w.Body.String())
	}

	w := h.do(http.MethodPost, "/api/runs/run-clarify-share/reviews/clarify1/share-link", map[string]any{"permissionPreset": "full", "ttlTier": "24h"})
	if w.Code != http.StatusOK {
		t.Fatalf("create clarify share: %d %s", w.Code, w.Body.String())
	}
	created := parseJSON(t, w)
	url, _ := created["url"].(string)
	token := strings.TrimPrefix(url[strings.Index(url, "#t="):], "#t=")
	if !gateshare.ValidTokenShape(token) {
		t.Fatalf("token: %s", token)
	}

	list := h.do(http.MethodGet, "/api/gates", nil)
	body := list.Body.String()
	if !strings.Contains(body, `"kind":"clarify"`) {
		t.Fatalf("inbox missing clarify item: %s", body)
	}
	if !strings.Contains(body, `"shareLink"`) {
		t.Fatalf("inbox clarify missing shareLink: %s", body)
	}
	if strings.Contains(body, token) {
		t.Fatal("inbox leaked plaintext token")
	}

	prev := h.doPublic(http.MethodGet, "/public/gate-approvals/preview", nil, map[string]string{headerShareToken: token})
	p := parseJSON(t, prev)
	if p["status"] != models.ShareLinkStateActive {
		t.Fatalf("preview: %s", prev.Body.String())
	}
	if p["kind"] != models.ShareLinkKindReview {
		t.Fatalf("preview kind must stay review: %+v", p)
	}
	if p["nodeType"] != "agent" || p["interaction"] != models.InteractionClarify {
		t.Fatalf("preview nodeType/interaction: %+v", p)
	}
	desc, _ := p["description"].(string)
	if !strings.Contains(desc, "外部澄清") {
		t.Fatalf("preview description: %q", desc)
	}
	if strings.Contains(desc, "待复审") || strings.Contains(prev.Body.String(), "run-clarify-share") {
		t.Fatalf("preview leak or review copy: %s", prev.Body.String())
	}
	if p["structured"] != nil {
		t.Fatalf("first-round empty product expected: %+v", p["structured"])
	}

	cancel := h.doPublic(http.MethodPost, "/public/gate-approvals/cancel", map[string]any{
		"token": token,
	}, map[string]string{headerShareRequest: "1", "Origin": "http://" + publicHost})
	if cancel.Code != 200 {
		t.Fatalf("cancel: %d %s", cancel.Code, cancel.Body.String())
	}
	prev3 := h.doPublic(http.MethodGet, "/public/gate-approvals/preview", nil, map[string]string{headerShareToken: token})
	if parseJSON(t, prev3)["status"] != models.ShareLinkStateActive {
		t.Fatalf("cancel burned token: %s", prev3.Body.String())
	}
}

func TestReviewSharePermissionPresetReactOnly(t *testing.T) {
	h := newHarness(t)
	seedInboxReview(t, h, "run-rev-preset", "research-preset", true)

	created := parseJSON(t, h.do(http.MethodPost, "/api/runs/run-rev-preset/reviews/research-preset/share-link", map[string]any{
		"ttlTier": "24h", "permissionPreset": "react_only",
	}))
	if created["permissionPreset"] != models.SharePermissionReactOnly {
		t.Fatalf("create: %+v", created)
	}
	url, _ := created["url"].(string)
	token := strings.TrimPrefix(url[strings.Index(url, "#t="):], "#t=")

	prev := parseJSON(t, h.doPublic(http.MethodGet, "/public/gate-approvals/preview", nil, map[string]string{headerShareToken: token}))
	actions, _ := prev["actions"].(map[string]any)
	if _, ok := actions["confirm"]; ok {
		t.Fatalf("review react_only preview still has confirm: %+v", actions)
	}

	nonce := publicPreviewNonce(t, h, token)
	dec := h.doPublic(http.MethodPost, "/public/gate-approvals/decide", map[string]any{
		"token": token, "action": "confirm", "nonce": nonce,
	}, map[string]string{headerShareRequest: "1", "Origin": "http://" + publicHost})
	if dec.Code != http.StatusForbidden || !strings.Contains(dec.Body.String(), "permission_denied") {
		t.Fatalf("decide: %d %s", dec.Code, dec.Body.String())
	}
	var link models.GateShareLink
	if err := h.db.Where("run_id = ? AND node_id = ?", "run-rev-preset", "research-preset").
		Order("created_at desc").First(&link).Error; err != nil {
		t.Fatalf("link: %v", err)
	}
	if link.UsedAt != nil {
		t.Fatal("must not consume on denied decide")
	}
}

func TestReviewSharePublicArtifactsListAndContent(t *testing.T) {
	h := newHarness(t)
	seedInboxReview(t, h, "run-rev-arts", "research1", true)
	feedbackNames := []string{"feedback.clarify.research1.r1.json", "feedback_index.json"}
	for _, name := range feedbackNames {
		h.h.Arts.Save("run-rev-arts", "research1", name, "json", `{"reviewer":"FEEDBACK-LEAK"}`)
	}

	created := parseJSON(t, h.do(http.MethodPost, "/api/runs/run-rev-arts/reviews/research1/share-link", map[string]any{"permissionPreset": "full", "ttlTier": "24h"}))
	url, _ := created["url"].(string)
	token := strings.TrimPrefix(url[strings.Index(url, "#t="):], "#t=")

	list := parseJSON(t, h.doPublic(http.MethodGet, "/public/gate-approvals/artifacts", nil, map[string]string{headerShareToken: token}))
	if list["status"] != models.ShareLinkStateActive {
		t.Fatalf("list status: %+v", list)
	}
	arts, _ := list["artifacts"].([]any)
	if len(arts) < 2 {
		t.Fatalf("expected run artifacts including other node, got %+v", list)
	}
	names := map[string]bool{}
	for _, raw := range arts {
		m, _ := raw.(map[string]any)
		names[m["name"].(string)] = true
		if _, ok := m["runId"]; ok {
			t.Fatalf("public list must not leak runId: %+v", m)
		}
	}
	if !names["research.json"] || !names["page.html"] {
		t.Fatalf("missing expected artifacts: %+v", names)
	}
	for _, name := range feedbackNames {
		if names[name] {
			t.Fatalf("public list leaked feedback artifact %q: %+v", name, names)
		}
		w := h.doPublic(http.MethodGet, "/public/gate-approvals/artifacts/"+name+"/content", nil, map[string]string{headerShareToken: token})
		if w.Code != http.StatusNotFound || strings.Contains(w.Body.String(), "FEEDBACK-LEAK") {
			t.Fatalf("feedback content %q must be not_found: %d %s", name, w.Code, w.Body.String())
		}
	}
	if list["nodes"] == nil {
		t.Fatal("expected sanitized graph nodes for stage filtering")
	}

	body := h.doPublic(http.MethodGet, "/public/gate-approvals/artifacts/research.json/content", nil, map[string]string{headerShareToken: token})
	if body.Code != 200 {
		t.Fatalf("content: %d %s", body.Code, body.Body.String())
	}
	content := parseJSON(t, body)
	if !strings.Contains(content["content"].(string), "调研摘要") {
		t.Fatalf("content body: %+v", content)
	}
	if strings.Contains(body.Body.String(), "run-rev-arts") {
		t.Fatalf("content leaked run id: %s", body.Body.String())
	}
}

func TestReviewSharePublicArtifactsInactiveAndHumanGateDenied(t *testing.T) {
	h := newHarness(t)
	seedInboxReview(t, h, "run-rev-arts-deny", "research1", true)

	rev := parseJSON(t, h.do(http.MethodPost, "/api/runs/run-rev-arts-deny/reviews/research1/share-link", map[string]any{"permissionPreset": "full", "ttlTier": "24h"}))
	gate := parseJSON(t, h.do(http.MethodPost, "/api/runs/run-rev-arts-deny/gates/hg-r/share-link", map[string]any{"permissionPreset": "full", "ttlTier": "8h"}))
	revTok := strings.TrimPrefix(rev["url"].(string)[strings.Index(rev["url"].(string), "#t="):], "#t=")
	gateTok := strings.TrimPrefix(gate["url"].(string)[strings.Index(gate["url"].(string), "#t="):], "#t=")

	if w := h.doPublic(http.MethodGet, "/public/gate-approvals/artifacts", nil, map[string]string{headerShareToken: gateTok}); w.Code != 403 {
		t.Fatalf("human_gate artifacts list: %d %s", w.Code, w.Body.String())
	}
	if w := h.doPublic(http.MethodGet, "/public/gate-approvals/artifacts/research.json/content", nil, map[string]string{headerShareToken: gateTok}); w.Code != 403 {
		t.Fatalf("human_gate artifact content: %d %s", w.Code, w.Body.String())
	}

	h.db.Model(&models.GateShareLink{}).Where("token_hash = ?", gateshare.HashToken(revTok)).
		Update("expires_at", time.Now().Add(-time.Hour))
	exp := parseJSON(t, h.doPublic(http.MethodGet, "/public/gate-approvals/artifacts", nil, map[string]string{headerShareToken: revTok}))
	if exp["status"] != models.ShareLinkStateExpired {
		t.Fatalf("expired list: %+v", exp)
	}
	if _, ok := exp["artifacts"]; ok {
		t.Fatalf("expired must not return artifacts: %+v", exp)
	}
}
