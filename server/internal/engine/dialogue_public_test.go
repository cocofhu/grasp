package engine

import (
	"encoding/json"
	"sort"
	"strings"
	"testing"

	"github.com/cocofhu/grasp/internal/gateshare"
	"github.com/cocofhu/grasp/internal/models"
)

// publicDialogueKeys is every key a share-link dialogue may carry: the public
// turn / queue item fields the old public frames already expose, plus row
// and turn identity, phase and version.
var publicDialogueKeys = map[string]bool{
	// state
	"type": true, "nodeId": true, "iteration": true, "epoch": true, "seq": true, "mode": true,
	"rows": true, "turns": true, "orphan": true, "busy": true, "done": true, "observe": true, "desynced": true,
	// gateshare.PreviewTurn / PreviewActiveItem / PreviewImage / PreviewAnnotation
	"role": true, "text": true, "at": true, "interrupted": true, "images": true, "annotations": true,
	"questions": true, "forms": true, "narration": true, "live": true, "tools": true, "parts": true,
	"mimeType": true, "name": true, "index": true, "selector": true, "jsonPath": true, "label": true,
	"note": true, "quote": true, "title": true, "status": true, "kind": true, "id": true, "item": true,
	// identity and phase
	"turnId": true, "clientMessageId": true, "phase": true, "rev": true, "outcome": true, "handoff": true,
	"thought": true, "streaming": true, "position": true, "code": true, "autoRetryOnResume": true,
}

func collectKeys(v any, into map[string]bool) {
	switch t := v.(type) {
	case map[string]any:
		for k, x := range t {
			into[k] = true
			collectKeys(x, into)
		}
	case []any:
		for _, x := range t {
			collectKeys(x, into)
		}
	}
}

func TestPublicDialogueRedacts(t *testing.T) {
	pos := 0
	st := DialogueState{Type: "dialogue", RunID: "run-secret", NodeID: "producer-secret", Iteration: 2,
		Epoch: 7, Seq: 9, Mode: "tail", PrefixLen: 1, PrefixDigest: "1:abc",
		Rows: []DialogueRow{
			{ReactMessage: models.ReactMessage{Role: "human", Text: "改按钮", TurnID: "t1", ClientMessageID: "c1",
				Images: []models.PromptImage{{Ref: "blob:secret-ref", MimeType: "image/png", Name: "a.png"}}},
				ID: "t1:h", Phase: rowSealed, Rev: 3},
			{ReactMessage: models.ReactMessage{Role: "agent", Text: "好", TurnID: "t1", OpID: "op-secret",
				Outcome: models.OutcomeFailed, Error: "dial tcp 10.0.0.3: secret",
				Tools:     []models.ReactTool{{Title: "Read /home/secret/a.go", Status: "completed"}},
				Execution: &models.ChatExecution{}},
				ID: "t1:a0", Phase: rowSealed, Rev: 4},
			{ReactMessage: models.ReactMessage{Role: "agent", TurnID: "orphan:op-secret"},
				ID: "orphan:op-secret:a0", Phase: rowOrphan, Rev: 5, Thought: "想", Streaming: true},
		},
		Turns: []DialogueTurn{
			{TurnID: "t1", Phase: turnEnded, Outcome: models.OutcomeFailed, Error: "dial tcp 10.0.0.3: secret"},
			{TurnID: "t2", Phase: turnQueued, Position: &pos, Item: map[string]any{"id": "t2", "text": "下一条",
				"images": []models.PromptImage{{Ref: "blob:queued-ref", MimeType: "image/png"}}}},
			{TurnID: "orphan:op-secret", Phase: turnOrphan, RunningOpID: "op-secret"},
		},
		Orphan: &DialogueOrphan{RunningOpID: "op-secret", Desynced: true},
	}
	pub := PublicDialogue(st)
	raw, err := json.Marshal(pub)
	if err != nil {
		t.Fatal(err)
	}
	body := string(raw)
	for _, leak := range []string{"run-secret", "producer-secret", "blob:", "10.0.0.3", "/home/secret", "runningOpId", "\"error\"", "execution", "opId", "prefixDigest", "op-secret"} {
		if strings.Contains(body, leak) {
			t.Errorf("public dialogue leaks %q: %s", leak, body)
		}
	}
	var m any
	_ = json.Unmarshal(raw, &m)
	keys := map[string]bool{}
	collectKeys(m, keys)
	var extra []string
	for k := range keys {
		if !publicDialogueKeys[k] {
			extra = append(extra, k)
		}
	}
	sort.Strings(extra)
	if len(extra) > 0 {
		t.Fatalf("public dialogue has non-public keys %v", extra)
	}
	if pub.NodeID != gateshare.PublicDialogueNodeID || pub.Mode != "full" || pub.Seq != 9 || len(pub.Rows) != 3 {
		t.Fatalf("pub=%+v", pub)
	}
	// Opaque image indexes follow the catalogue order: rows, then queue.
	if len(pub.Rows[0].Images) != 1 || pub.Rows[0].Images[0].Index != 0 {
		t.Fatalf("row image %+v", pub.Rows[0].Images)
	}
	if it := pub.Turns[1].Item; it == nil || len(it.Images) != 1 || it.Images[0].Index != 1 {
		t.Fatalf("queued item %+v", pub.Turns[1].Item)
	}
	if pub.Rows[1].Tools[0].Title != "Read" || pub.Rows[1].Outcome != models.OutcomeFailed {
		t.Fatalf("reply %+v", pub.Rows[1])
	}
	// Row ids and revisions are kept, so a viewer's prefix digest matches.
	if dialoguePrefixDigest(st.Rows[:2]) != dialoguePrefixDigest([]DialogueRow{{ID: pub.Rows[0].ID, Rev: pub.Rows[0].Rev}, {ID: pub.Rows[1].ID, Rev: pub.Rows[1].Rev}}) {
		t.Fatal("public rows changed identity")
	}
}
