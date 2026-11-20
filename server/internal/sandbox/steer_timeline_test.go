package sandbox

import (
	"encoding/json"
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
