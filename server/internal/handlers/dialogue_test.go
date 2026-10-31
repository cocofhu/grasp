package handlers_test

import (
	"encoding/json"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/models"

	"github.com/gorilla/websocket"
)

type dialogueBody struct {
	Type   string `json:"type"`
	NodeID string `json:"nodeId"`
	Epoch  int64  `json:"epoch"`
	Seq    uint64 `json:"seq"`
	Mode   string `json:"mode"`
	Rows   []struct {
		ID      string `json:"id"`
		Role    string `json:"role"`
		Phase   string `json:"phase"`
		Outcome string `json:"outcome"`
	} `json:"rows"`
}

func seedDialogueRun(h *harness, runID string) {
	h.db.Create(&models.Run{ID: runID, Status: "waiting_human", StartedAt: time.Now(), Graph: models.Graph{
		Nodes: []models.Node{{ID: "react", Type: "agent", Caps: testClarifyCaps, Label: "澄清"}},
	}})
	h.db.Create(&models.ReactConversation{RunID: runID, NodeID: "react", Iteration: 1, Messages: []models.ReactMessage{
		{Role: "agent", Text: "要做什么？"},
		{Role: "human", Text: "登录页", TurnID: "t1"},
		{Role: "agent", Text: "", TurnID: "t1"},
	}})
	h.db.Create(&models.StateRun{RunID: runID, NodeID: "react", Iteration: 1, Status: "waiting_human"})
}

func checkSeededDialogue(t *testing.T, d dialogueBody) {
	t.Helper()
	if d.Type != "dialogue" || d.NodeID != "react" || d.Mode != "full" || d.Seq == 0 || len(d.Rows) != 3 {
		t.Fatalf("dialogue=%+v", d)
	}
	if d.Rows[0].ID != "i1:0" || d.Rows[1].ID != "t1:h" || d.Rows[2].ID != "t1:a0" {
		t.Fatalf("row ids %+v", d.Rows)
	}
	if d.Rows[2].Phase != "sealed" || d.Rows[2].Outcome != models.OutcomeEmpty || d.Rows[0].Outcome != models.OutcomeOK {
		t.Fatalf("outcomes %+v", d.Rows)
	}
}

func TestDialogueRESTAndRunDTOShareOneState(t *testing.T) {
	h := newHarness(t)
	seedDialogueRun(h, "rd-1")

	w := h.do("GET", "/api/runs/rd-1/nodes/react/dialogue", nil)
	if w.Code != 200 {
		t.Fatalf("dialogue: %d %s", w.Code, w.Body.String())
	}
	var d dialogueBody
	if err := json.Unmarshal(w.Body.Bytes(), &d); err != nil {
		t.Fatal(err)
	}
	checkSeededDialogue(t, d)

	w = h.do("GET", "/api/runs/rd-1", nil)
	var run struct {
		DialogueByNode map[string]dialogueBody `json:"dialogueByNode"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &run); err != nil {
		t.Fatal(err)
	}
	got := run.DialogueByNode["react"]
	checkSeededDialogue(t, got)
	if got.Epoch != d.Epoch || got.Seq != d.Seq {
		t.Fatalf("run detail version %d/%d, endpoint %d/%d: one unchanged state must keep one version", got.Epoch, got.Seq, d.Epoch, d.Seq)
	}

	if w := h.do("GET", "/api/runs/rd-1/nodes/none/dialogue", nil); w.Code != 404 {
		t.Fatalf("unknown node: %d", w.Code)
	}
	if w := h.do("GET", "/api/runs/nope/nodes/react/dialogue", nil); w.Code != 404 {
		t.Fatalf("unknown run: %d", w.Code)
	}
}

func TestRunEventsWSSendsFullDialogueOnConnect(t *testing.T) {
	h := newHarness(t)
	seedDialogueRun(h, "rd-ws")
	srv := httptest.NewServer(h.r)
	defer srv.Close()
	c, _, err := websocket.DefaultDialer.Dial(wsURL(srv.URL, "/api/runs/rd-ws/events"), h.wsHeader())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer c.Close()
	_ = c.SetReadDeadline(time.Now().Add(3 * time.Second))
	for i := 0; i < 10; i++ {
		_, msg, err := c.ReadMessage()
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		var d dialogueBody
		if json.Unmarshal(msg, &d) == nil && d.Type == "dialogue" {
			checkSeededDialogue(t, d)
			return
		}
	}
	t.Fatal("no dialogue frame on connect")
}
