package handlers_test

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/blob"
	"github.com/cocofhu/grasp/internal/engine"
	"github.com/cocofhu/grasp/internal/gateshare"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/runtime"
)

const headerShareVisitor = "X-Gate-Share-Visitor"

// laneProvider is a parked review session whose backend hosts visitor chats.
type laneProvider struct {
	fakeProvider
	mu      sync.Mutex
	visitor map[string][]string // lane -> human messages
	retired map[string]bool
}

func (p *laneProvider) ReviseInPlace(context.Context, runtime.NodeReq, []models.ReactMessage, string, []models.PromptImage) runtime.ReactTurn {
	return runtime.ReactTurn{Msg: "node reply"}
}
func (*laneProvider) OfferCommitOnConfirm(context.Context, runtime.NodeReq) runtime.ReactTurn {
	return runtime.ReactTurn{}
}
func (*laneProvider) ReconcileOnConfirm(context.Context, runtime.NodeReq) runtime.ReactTurn {
	return runtime.ReactTurn{}
}
func (*laneProvider) HasLiveSession(string, string) bool { return true }
func (*laneProvider) RetireSession(string, string)       {}

func (p *laneProvider) VisitorTurn(_ context.Context, _ runtime.NodeReq, lane, _, human string, _ []models.PromptImage, _ func([]models.AcpEvent, bool)) runtime.ReactTurn {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.visitor == nil {
		p.visitor = map[string][]string{}
	}
	p.visitor[lane] = append(p.visitor[lane], human)
	return runtime.ReactTurn{Msg: "visitor reply: " + human}
}
func (*laneProvider) CancelVisitorTurn(string, string, string) {}
func (p *laneProvider) RetireVisitorLane(_, _, lane string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.retired == nil {
		p.retired = map[string]bool{}
	}
	p.retired[lane] = true
}

func newLaneShare(t *testing.T) (*harness, *laneProvider, string) {
	t.Helper()
	h := newHarness(t)
	runID, nodeID := "run-lanes", "design"
	seedInboxReview(t, h, runID, nodeID, true)
	provider := &laneProvider{}
	eng := engine.New(h.db, provider, h.host, h.h.Arts, 5)
	eng.SetBlobStore(blob.NewMemory())
	h.h.Eng = eng
	t.Cleanup(eng.Close)
	created := parseJSON(t, h.do(http.MethodPost, "/api/runs/"+runID+"/reviews/"+nodeID+"/share-link", map[string]any{"ttlTier": "24h"}))
	url, _ := created["url"].(string)
	i := strings.Index(url, "#t=")
	if i < 0 {
		t.Fatalf("share link: %v", created)
	}
	return h, provider, url[i+len("#t="):]
}

func visitorID(n int) string { return fmt.Sprintf("%032x", n) }

func visitorReply(h *harness, token, visitor, text string) *httpResponse {
	hdr := map[string]string{headerShareRequest: "1", "Origin": "http://" + publicHost}
	if visitor != "" {
		hdr[headerShareVisitor] = visitor
	}
	w := h.doPublic(http.MethodPost, "/public/gate-approvals/reply", map[string]any{"token": token, "text": text}, hdr)
	return &httpResponse{code: w.Code, body: w.Body.String()}
}

type httpResponse struct {
	code int
	body string
}

func visitorPreviewTurns(t *testing.T, h *harness, token, visitor string) string {
	t.Helper()
	w := h.doPublic(http.MethodGet, "/public/gate-approvals/preview", nil, map[string]string{
		headerShareToken: token, headerShareVisitor: visitor,
	})
	if w.Code != http.StatusOK {
		t.Fatalf("preview: %d %s", w.Code, w.Body.String())
	}
	return w.Body.String()
}

func waitPreviewContains(t *testing.T, h *harness, token, visitor, want string) string {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	var body string
	for time.Now().Before(deadline) {
		body = visitorPreviewTurns(t, h, token, visitor)
		if strings.Contains(body, want) && !strings.Contains(body, `"sessionBusy":true`) {
			return body
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("preview never showed %q: %s", want, body)
	return ""
}

func TestPublicShareVisitorsGetSeparateConversations(t *testing.T) {
	h, provider, token := newLaneShare(t)
	a, b := visitorID(0xa), visitorID(0xb)

	if r := visitorReply(h, token, "", "anonymous"); r.code != http.StatusBadRequest || !strings.Contains(r.body, "visitor_required") {
		t.Fatalf("reply without a visitor id: %d %s", r.code, r.body)
	}
	if r := visitorReply(h, token, a, "message from A"); r.code != http.StatusOK {
		t.Fatalf("reply A: %d %s", r.code, r.body)
	}
	waitPreviewContains(t, h, token, a, "visitor reply: message from A")
	if r := visitorReply(h, token, b, "message from B"); r.code != http.StatusOK {
		t.Fatalf("reply B: %d %s", r.code, r.body)
	}
	bodyB := waitPreviewContains(t, h, token, b, "visitor reply: message from B")
	if strings.Contains(bodyB, "message from A") {
		t.Fatalf("visitor B sees visitor A's conversation: %s", bodyB)
	}
	bodyA := visitorPreviewTurns(t, h, token, a)
	if strings.Contains(bodyA, "message from B") || !strings.Contains(bodyA, "message from A") {
		t.Fatalf("visitor A transcript: %s", bodyA)
	}
	fresh := visitorPreviewTurns(t, h, token, visitorID(0xc))
	if strings.Contains(fresh, "message from A") || strings.Contains(fresh, "message from B") {
		t.Fatalf("a new visitor sees other visitors: %s", fresh)
	}

	var conv models.ReactConversation
	if err := h.db.Where("run_id = ? AND node_id = ?", "run-lanes", "design").First(&conv).Error; err != nil {
		t.Fatal(err)
	}
	for _, m := range conv.Messages {
		if strings.Contains(m.Text, "message from") {
			t.Fatalf("visitor message leaked into the node's own dialogue: %+v", conv.Messages)
		}
	}
	provider.mu.Lock()
	lanes := len(provider.visitor)
	provider.mu.Unlock()
	if lanes != 2 {
		t.Fatalf("expected two visitor chats, got %d", lanes)
	}
}

func TestPublicShareVisitorsFull(t *testing.T) {
	h, _, token := newLaneShare(t)
	for i := 1; i <= gateshare.MaxVisitorLanesPerLink; i++ {
		v := visitorID(i)
		if r := visitorReply(h, token, v, fmt.Sprintf("hi %d", i)); r.code != http.StatusOK {
			t.Fatalf("visitor %d: %d %s", i, r.code, r.body)
		}
		waitPreviewContains(t, h, token, v, fmt.Sprintf("visitor reply: hi %d", i))
	}
	r := visitorReply(h, token, visitorID(100), "one too many")
	if r.code != http.StatusTooManyRequests || !strings.Contains(r.body, "visitors_full") {
		t.Fatalf("visitor past the cap: %d %s", r.code, r.body)
	}
}

func TestPublicShareDecideRetiresVisitorChats(t *testing.T) {
	h, provider, token := newLaneShare(t)
	a := visitorID(0xa)
	if r := visitorReply(h, token, a, "edit please"); r.code != http.StatusOK {
		t.Fatalf("reply: %d %s", r.code, r.body)
	}
	waitPreviewContains(t, h, token, a, "visitor reply: edit please")

	prev := parseJSON(t, h.doPublic(http.MethodGet, "/public/gate-approvals/preview", nil, map[string]string{
		headerShareToken: token, headerShareVisitor: a,
	}))
	nonce, _ := prev["nonce"].(string)
	if nonce == "" {
		t.Fatalf("preview issued no nonce: %v", prev)
	}
	w := h.doPublic(http.MethodPost, "/public/gate-approvals/decide", map[string]any{
		"token": token, "action": "confirm", "nonce": nonce,
	}, map[string]string{headerShareRequest: "1", "Origin": "http://" + publicHost, headerShareVisitor: a})
	if w.Code != http.StatusOK {
		t.Fatalf("decide: %d %s", w.Code, w.Body.String())
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		provider.mu.Lock()
		n := len(provider.retired)
		provider.mu.Unlock()
		if n == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("visitor chat not retired after decide (retired %d)", n)
		}
		time.Sleep(20 * time.Millisecond)
	}
	var link models.GateShareLink
	if err := h.db.Where("run_id = ? AND node_id = ?", "run-lanes", "design").Order("created_at desc").First(&link).Error; err != nil {
		t.Fatal(err)
	}
	if link.UsedAt == nil || link.UsedLane != gateshare.VisitorLane(link.ID, a) {
		t.Fatalf("decision lane not recorded: usedAt=%v usedLane=%q", link.UsedAt, link.UsedLane)
	}
}
