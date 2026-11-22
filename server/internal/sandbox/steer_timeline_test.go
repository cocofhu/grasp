package sandbox

import (
	"encoding/json"
	"os"
	"reflect"
	"testing"

	"github.com/cocofhu/grasp/internal/models"
)

func steerFrames(raw ...string) []json.RawMessage {
	out := make([]json.RawMessage, len(raw))
	for i, r := range raw {
		out[i] = json.RawMessage(r)
	}
	return out
}

func timelineParts(t *testing.T, events []models.AcpEvent) []models.AcpPart {
	t.Helper()
	for _, ev := range events {
		if ev.Kind == models.AcpKindTimeline {
			return ev.Parts
		}
	}
	t.Fatalf("no timeline: %+v", events)
	return nil
}

func TestUserSteerIsATimelineStep(t *testing.T) {
	chunk := func(text string) string {
		return `{"op":"event","data":{"type":"session_update","update":{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"` + text + `"}}}}`
	}
	steer := func(text string) string {
		return `{"op":"event","data":{"type":"user_steer","opId":"g-1","steerId":"s","text":"` + text + `"}}`
	}
	events := AggregateFrames(steerFrames(chunk("first"), steer("also this"), steer("and that"), steer(" "), chunk(" done")))
	got := timelineParts(t, events)
	want := []models.AcpPart{
		{Kind: "message", Text: "first"},
		{Kind: models.AcpPartSteer, Text: "also this"},
		{Kind: models.AcpPartSteer, Text: "and that"},
		{Kind: "message", Text: " done"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("parts = %+v", got)
	}
	if s := models.SteerTexts(got); !reflect.DeepEqual(s, []string{"also this", "and that"}) {
		t.Fatalf("steers = %v", s)
	}

	// A same-turn segment boundary keeps the steer on the open row.
	events = AggregateFrames(steerFrames(steer("keep me"), chunk("a"), `{"op":"event","data":{"type":"turn_segment"}}`, chunk("b")))
	if s := models.SteerTexts(timelineParts(t, events)); !reflect.DeepEqual(s, []string{"keep me"}) {
		t.Fatalf("steers after a segment = %v", s)
	}
}

func TestLiveTurnRecordsUserSteer(t *testing.T) {
	c := &ACPClient{}
	res := &ChatResult{}
	c.dispatchEventData(json.RawMessage(`{"op":"event","opId":"g-1","data":{"type":"user_steer","text":"live steer"}}`), res)
	if s := models.SteerTexts(timelineParts(t, res.AcpEvents())); !reflect.DeepEqual(s, []string{"live steer"}) {
		t.Fatalf("steers = %v", s)
	}
}

func TestAttachmentOnlySteerIsRecorded(t *testing.T) {
	data := `{"type":"user_steer","opId":"g-1","text":" ","fileNames":["diagram.pdf"],"imageURLs":["data:image/png;base64,AAAA"]}`
	want := []string{"[附件] diagram.pdf、图片 ×1"}

	replayed := AggregateFrames(steerFrames(`{"op":"event","data":` + data + `}`))
	if s := models.SteerTexts(timelineParts(t, replayed)); !reflect.DeepEqual(s, want) {
		t.Fatalf("replayed steers = %v", s)
	}
	if got := timelineParts(t, replayed)[0].ImageURLs; !reflect.DeepEqual(got, []string{"data:image/png;base64,AAAA"}) {
		t.Fatalf("replayed steer images = %v", got)
	}
	res := &ChatResult{}
	(&ACPClient{}).dispatchEventData(json.RawMessage(`{"op":"event","opId":"g-1","data":`+data+`}`), res)
	if s := models.SteerTexts(timelineParts(t, res.AcpEvents())); !reflect.DeepEqual(s, want) {
		t.Fatalf("live steers = %v", s)
	}
	if got := timelineParts(t, res.AcpEvents())[0].ImageURLs; !reflect.DeepEqual(got, []string{"data:image/png;base64,AAAA"}) {
		t.Fatalf("live steer images = %v", got)
	}
}

// steerTextFixture is shared with web/src/lib/run/acpUnpack.test.ts, so the
// live user_steer rendering and the persisted timeline cannot drift apart.
const steerTextFixture = "../../../web/src/lib/run/__fixtures__/steer_text.json"

func TestSteerTextNamesItsAttachments(t *testing.T) {
	raw, err := os.ReadFile(steerTextFixture)
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Event struct {
			Text      string   `json:"text"`
			FileNames []string `json:"fileNames"`
			ImageURLs []string `json:"imageURLs"`
		} `json:"event"`
		Want string `json:"want"`
	}
	if err := json.Unmarshal(raw, &cases); err != nil || len(cases) == 0 {
		t.Fatalf("fixture: %d cases, err %v", len(cases), err)
	}
	for _, c := range cases {
		e := c.Event
		if got := models.SteerText(e.Text, e.FileNames, len(e.ImageURLs)); got != c.Want {
			t.Errorf("SteerText(%q, %v, %d) = %q, want %q", e.Text, e.FileNames, len(e.ImageURLs), got, c.Want)
		}
	}
}
