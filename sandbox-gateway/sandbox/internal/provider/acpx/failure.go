package acpx

import (
	"encoding/json"
	"sync/atomic"
)

// turnFailure records that the agent reported a failed turn in its reply text
// while still answering session/prompt with end_turn.
type turnFailure struct{ flag atomic.Bool }

func (f *turnFailure) reset()     { f.flag.Store(false) }
func (f *turnFailure) seen() bool { return f.flag.Load() }

// watchFailures wraps onEvent: agent text matching isFailure marks the turn,
// and that turn's prompt_done is relayed with stopReason "failed".
func watchFailures(onEvent func(json.RawMessage), isFailure func(string) bool) (func(json.RawMessage), *turnFailure) {
	failed := &turnFailure{}
	return func(ev json.RawMessage) {
		var frame struct {
			Type   string `json:"type"`
			Update struct {
				SessionUpdate string `json:"sessionUpdate"`
				Content       struct {
					Text string `json:"text"`
				} `json:"content"`
			} `json:"update"`
		}
		if json.Unmarshal(ev, &frame) == nil {
			switch {
			case frame.Type == "session_update" && frame.Update.SessionUpdate == "agent_message_chunk" && isFailure(frame.Update.Content.Text):
				failed.flag.Store(true)
			case frame.Type == "prompt_done" && failed.seen():
				var done map[string]any
				if json.Unmarshal(ev, &done) == nil {
					done["stopReason"] = "failed"
					if b, err := json.Marshal(done); err == nil {
						ev = b
					}
				}
			}
		}
		if onEvent != nil {
			onEvent(ev)
		}
	}, failed
}
