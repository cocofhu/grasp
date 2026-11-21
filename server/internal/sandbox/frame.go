package sandbox

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/cocofhu/grasp/internal/models"
)

// frame is one bridge frame, decoded once for every reader. Live event frames
// arrive wrapped ({op:"event", opId, data:{type,...}}); the bridge event log
// keeps only the data, so a frame with a type and no data is that bare event
// (a diagnostic still carries its own op:"raw").
type frame struct {
	Op     string          `json:"op"`
	Type   string          `json:"type"`
	OpID   string          `json:"opId"`
	Status string          `json:"status"`
	Busy   *bool           `json:"busy"`
	Data   json.RawMessage `json:"data"`
	ev     frameEvent
}

// frameEvent is the payload of an event frame.
type frameEvent struct {
	Type         string          `json:"type"`
	OpID         string          `json:"opId"`
	ReportedAt   time.Time       `json:"reportedAt"`
	Update       json.RawMessage `json:"update"`
	Usage        json.RawMessage `json:"usage"`
	UsagePartial bool            `json:"usagePartial"`
	Text         string          `json:"text"`
	StopReason   string          `json:"stopReason"`
	SessionReset bool            `json:"sessionReset"`
}

// decodeFrame decodes raw; malformed input is the zero frame, which no reader
// acts on.
func decodeFrame(raw json.RawMessage) frame {
	var f frame
	if json.Unmarshal(raw, &f) != nil {
		return frame{}
	}
	if f.Op == "" || (f.Type != "" && len(f.Data) == 0) {
		f.Op, f.Data = "event", raw
	}
	if f.Op == "event" {
		_ = json.Unmarshal(f.Data, &f.ev)
	}
	return f
}

// apply folds an event frame into the turn and reports whether it ended the
// turn (prompt_done). usageModel attributes usage reported without a model.
func (r *ChatResult) apply(f frame, usageModel string) bool {
	if f.Op != "event" {
		return false
	}
	ev := f.ev
	if r == nil {
		return ev.Type == "prompt_done"
	}
	switch ev.Type {
	case "turn_segment":
		r.sealSegment()
	case "prompt_done":
		if r.doneSeen {
			return true
		}
		// Per-turn usage only — never session CumulativeUsage (cross-node reuse
		// would otherwise bleed prior nodes into this turn).
		r.doneSeen = true
		r.SessionReset = ev.SessionReset
		r.UsagePartial = ev.UsagePartial
		r.EndedAt = ev.ReportedAt
		if u, byModel := parsePromptDoneUsage(ev.Usage, usageModel); u != nil {
			r.Usage = models.AddTokenUsage(r.Usage, u)
			r.UsageByModel = models.AddTokenUsageByModel(r.UsageByModel, byModel)
		}
		r.StopReason = strings.ToLower(strings.TrimSpace(ev.StopReason))
		switch r.StopReason {
		case "failed", "refusal":
			r.Failed = true
		case stopReasonTimeout, "cancelled":
			r.Interrupted = true
		}
		return true
	case "error_text":
		// oneshot provider surfaces model/CLI failures as raw error_text frames
		// before prompt_done{stopReason:failed}. Capture the body so React can
		// show it instead of an empty agent bubble.
		r.appendErrorText(ev.Text)
	case "user_steer":
		r.addSteer(f.Data)
	case "session_update":
		if len(ev.Update) > 0 {
			kind, flat := normalizeSessionUpdate(ev.Update)
			dispatchSessionUpdate(kind, flat, r)
		}
	}
	return false
}
