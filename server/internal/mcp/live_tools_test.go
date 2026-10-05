package mcp

import (
	"errors"
	"strings"
	"testing"

	"github.com/cocofhu/grasp/internal/models"
)

type fakeLiveUpdater struct {
	enabled bool
	got     []LiveReport
	sess    *models.LiveSession
	err     error
}

func (f *fakeLiveUpdater) LiveEnabled(_, _ string) bool { return f.enabled }

func (f *fakeLiveUpdater) ApplyLiveReport(_, _ string, r LiveReport) (*models.LiveSession, error) {
	f.got = append(f.got, r)
	return f.sess, f.err
}

func liveHost(t *testing.T, caps *models.AgentCapabilities, enabled bool) (*Host, string, *fakeLiveUpdater) {
	t.Helper()
	h := NewHost(&memStore{})
	tok := h.RegisterRun("r1")
	h.SetActiveNode("r1", "p1", caps)
	h.SetActiveReview("r1", caps.ReviewEnabled())
	u := &fakeLiveUpdater{enabled: enabled, sess: &models.LiveSession{ID: "sid001", State: models.LiveStateReady, Variants: []models.LiveVariant{{N: 1}, {N: 2}}}}
	h.SetLiveUpdater(u)
	return h, tok, u
}

func liveCall(t *testing.T, h *Host, tok, args string) (string, bool) {
	t.Helper()
	return toolText(t, call(t, h, "r1", tok, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"live_update","arguments":`+args+`}}`))
}

func TestLiveUpdateListedOnlyWhenEnabled(t *testing.T) {
	h, tok, u := liveHost(t, capsPreview, true)
	if !listedNames(t, h, tok)["live_update"] {
		t.Fatal("live_update should be listed")
	}
	u.enabled = false
	if listedNames(t, h, tok)["live_update"] {
		t.Fatal("disabled node must not list live_update")
	}
	h2, tok2, _ := liveHost(t, capsWriting(models.SchemaPlan), true)
	if listedNames(t, h2, tok2)["live_update"] {
		t.Fatal("an Agent without set_preview must not list live_update")
	}
	h3 := NewHost(&memStore{})
	tok3 := h3.RegisterRun("r1")
	h3.SetActiveNode("r1", "p1", capsPreview)
	if listedNames(t, h3, tok3)["live_update"] {
		t.Fatal("no updater: not listed")
	}
}

func TestLiveUpdateCall(t *testing.T) {
	h, tok, u := liveHost(t, capsPreview, true)
	txt, isErr := liveCall(t, h, tok, `{"session_id":"sid001","state":"ready","file":"src/App.vue","variants":[{"n":1,"label":"层级"},{"n":2}]}`)
	if isErr || !strings.Contains(txt, "ready") || !strings.Contains(txt, "2 个变体") {
		t.Fatalf("txt=%q isErr=%v", txt, isErr)
	}
	r := u.got[0]
	if r.SID != "sid001" || r.File != "src/App.vue" || len(r.Variants) != 2 || r.Variants[0].Label != "层级" {
		t.Fatalf("report=%+v", r)
	}
	u.sess = &models.LiveSession{ID: "sid001", State: models.LiveStateAccepted}
	if txt, _ := liveCall(t, h, tok, `{"session_id":"sid001","state":"accepted"}`); !strings.Contains(txt, "已结束") {
		t.Fatalf("accepted txt=%q", txt)
	}
	u.err = errors.New("nope")
	if txt, isErr := liveCall(t, h, tok, `{"session_id":"sid001","state":"ready"}`); !isErr || !strings.Contains(txt, "nope") {
		t.Fatalf("err txt=%q", txt)
	}
}

func TestLiveUpdateValidation(t *testing.T) {
	h, tok, u := liveHost(t, capsPreview, true)
	for _, args := range []string{`{}`, `{"session_id":"sid001"}`, `{"session_id":"sid001","state":"ready","variants":"x"}`, `{"session_id":"sid001","state":"ready","variants":[1]}`} {
		if _, isErr := liveCall(t, h, tok, args); !isErr {
			t.Fatalf("args %s should fail", args)
		}
	}
	if len(u.got) != 0 {
		t.Fatal("invalid calls must not reach the updater")
	}
	u.enabled = false
	if txt, isErr := liveCall(t, h, tok, `{"session_id":"sid001","state":"ready"}`); !isErr {
		t.Fatalf("disabled txt=%q", txt)
	}
	if txt, isErr := liveCall(t, h, "bad-token", `{"session_id":"sid001","state":"ready"}`); !isErr || txt == "" {
		t.Fatalf("unauthorized txt=%q", txt)
	}
	if formatLiveReportResult(nil) != "ok" {
		t.Fatal("nil session result")
	}
}

func TestLiveChatBeginToolProtocol(t *testing.T) {
	h, tok, u := liveHost(t, capsPreview, true)
	u.sess = &models.LiveSession{ID: "sid001", State: models.LiveStateRefining, Selected: 2}
	text, isErr := liveCall(t, h, tok, `{"session_id":"sid001","state":"refining","variant":2}`)
	if isErr || !strings.Contains(text, "只修改变体 2") || u.got[0].Variant != 2 {
		t.Fatalf("explicit ordinal missing: text=%q report=%+v isErr=%v", text, u.got, isErr)
	}
	u.sess = &models.LiveSession{ID: "sid001", State: models.LiveStateAccepting, Selected: 1, FinalParams: map[string]any{"gap": "24px"}}
	text, isErr = liveCall(t, h, tok, `{"session_id":"sid001","state":"accepting"}`)
	if isErr || !strings.Contains(text, "变体 1") || !strings.Contains(text, `"gap":"24px"`) {
		t.Fatalf("adoption snapshot missing: %q %v", text, isErr)
	}
	for _, variant := range []string{"0", "1.5", "9", `"2"`} {
		if _, isErr := liveCall(t, h, tok, `{"session_id":"sid001","state":"refining","variant":`+variant+`}`); !isErr {
			t.Fatalf("invalid ordinal %s was accepted", variant)
		}
	}
}

func TestLiveToolsAvailableInClarifyDialogue(t *testing.T) {
	for _, caps := range []*models.AgentCapabilities{capsClarify} {
		t.Run(caps.Interaction, func(t *testing.T) {
			h, tok, u := liveHost(t, caps, true)
			if !listedNames(t, h, tok)["live_update"] {
				t.Fatal("enabled clarify Agent must list live_update")
			}
			if txt, isErr := liveCall(t, h, tok, `{"session_id":"sid001","state":"ready"}`); isErr {
				t.Fatalf("live_update: %s", txt)
			}
			u.enabled = false
			if listedNames(t, h, tok)["live_update"] {
				t.Fatal("disabled clarify Agent must not list live_update")
			}
		})
	}
}
