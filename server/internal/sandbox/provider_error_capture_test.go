package sandbox

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestDispatchEventDataCapturesErrorText(t *testing.T) {
	c := NewACPClient("127.0.0.1", 1)
	res := &ChatResult{}
	frame := json.RawMessage(`{"op":"event","data":{"type":"error_text","text":"The free trial quota for the service has been exhausted"}}`)
	if c.dispatchEventData(frame, res) {
		t.Fatal("error_text must not signal turn done")
	}
	if !strings.Contains(res.ErrorText, "free trial quota") {
		t.Fatalf("ErrorText not captured: %q", res.ErrorText)
	}
	if res.Failed {
		t.Fatal("error_text alone must not set Failed")
	}
}

func TestDispatchEventDataPromptDoneStopReasonFailed(t *testing.T) {
	c := NewACPClient("127.0.0.1", 1)
	for _, reason := range []string{"failed", "refusal", " REFUSAL "} {
		t.Run(reason, func(t *testing.T) {
			res := &ChatResult{}
			frame := json.RawMessage(`{"op":"event","data":{"type":"prompt_done","stopReason":"` + reason + `"}}`)
			if !c.dispatchEventData(frame, res) {
				t.Fatal("prompt_done must signal done")
			}
			if !res.Failed {
				t.Fatalf("stopReason=%q must set Failed", reason)
			}
			// Provider failures stay in ChatResult for React to render partial
			// output; finishTurn must not turn them into transport errors.
			if result, err := c.finishTurn(res); err != nil || result != res {
				t.Fatalf("finishTurn changed provider result: %+v %v", result, err)
			}
		})
	}
}

func TestDispatchEventDataAppendsMultipleErrorTexts(t *testing.T) {
	c := NewACPClient("127.0.0.1", 1)
	res := &ChatResult{}
	_ = c.dispatchEventData(json.RawMessage(`{"op":"event","data":{"type":"error_text","text":"first"}}`), res)
	_ = c.dispatchEventData(json.RawMessage(`{"op":"event","data":{"type":"error_text","text":"second"}}`), res)
	if res.ErrorText != "first\nsecond" {
		t.Fatalf("expected joined ErrorText, got %q", res.ErrorText)
	}
}

func TestAppendErrorTextOnNilIsNoop(t *testing.T) {
	var res *ChatResult
	res.appendErrorText("x") // must not panic
}
