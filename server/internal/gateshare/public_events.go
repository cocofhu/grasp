package gateshare

import (
	"encoding/json"
	"strings"

	"github.com/cocofhu/grasp/internal/models"
)

// PublicDialogueNodeID is the leak-free node id rewritten onto public WS frames
// so the unauthenticated workbench can consume review/ACP without host ids.
const PublicDialogueNodeID = "public-gate"

// SanitizeLiveEvents keeps message/thought rails (text verbatim, so the drawer
// matches the approval page), tool_call rows reduced to a bare tool name and
// status, and timeline rows sanitized the same way (SanitizeParts). Plan,
// segment and every tool argument/output are dropped.
func SanitizeLiveEvents(events []models.AcpEvent) []PreviewLiveEvent {
	if len(events) == 0 {
		return nil
	}
	out := make([]PreviewLiveEvent, 0, 2)
	tools := 0
	for _, ev := range events {
		kind := strings.ToLower(strings.TrimSpace(ev.Kind))
		if kind == "tool_call" {
			if tools < models.MaxReactTools {
				tools++
				out = append(out, PreviewLiveEvent{Kind: kind, Title: SanitizeToolTitle(ev.Title), Status: SanitizeToolStatus(ev.Status)})
			}
			continue
		}
		if kind == models.AcpKindTimeline {
			if parts := SanitizeParts(ev.Parts); len(parts) > 0 {
				out = append(out, PreviewLiveEvent{Kind: kind, Parts: parts})
			}
			continue
		}
		if kind != "message" && kind != "thought" {
			continue
		}
		if strings.TrimSpace(ev.Text) == "" {
			continue
		}
		out = append(out, PreviewLiveEvent{Kind: kind, Text: ev.Text})
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

const maxPublicToolTitle = 40

// SanitizeToolTitle reduces a tool title to its bare name for public viewers:
// the part before the first space or "(", limited to [A-Za-z0-9_.:-] and 40
// characters. Titles that carry paths, commands or URLs collapse to the
// leading verb (e.g. "write_artifact(prd.md)" → "write_artifact"); anything
// else becomes "tool".
func SanitizeToolTitle(title string) string {
	s := strings.TrimSpace(title)
	if i := strings.IndexAny(s, " \t\n("); i >= 0 {
		s = s[:i]
	}
	if s == "" || len(s) > maxPublicToolTitle {
		return "tool"
	}
	for _, r := range s {
		ok := r == '_' || r == '.' || r == ':' || r == '-' ||
			(r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')
		if !ok {
			return "tool"
		}
	}
	return s
}

// SanitizeToolStatus keeps only the statuses the UI knows.
func SanitizeToolStatus(status string) string {
	switch s := strings.ToLower(strings.TrimSpace(status)); s {
	case "running", "completed", "failed", "pending", "in_progress":
		return s
	}
	return ""
}

// FilterPublicBrokerFrame rewrites a run-broker payload for the public
// workbench: only review/acp for producerID, strip runId, rewrite nodeId,
// drop tool_call/plan. Image bytes become opaque indexes starting at
// imageBase() (conversation turn image count) so poll and WS share one
// catalogue. imageBase reads the database, so it runs only for review frames
// of producerID: the WS loop sees every frame of the run while streaming, and
// a slow subscriber is dropped by the broker.
func FilterPublicBrokerFrame(raw []byte, producerID string, imageBase func() int) ([]byte, bool) {
	return FilterPublicLaneFrame(raw, producerID, "", imageBase)
}

// FilterPublicLaneFrame is FilterPublicBrokerFrame for one visitor lane. A
// visitor sees only its own type:"visitor" frames (unwrapped to review/acp),
// never the node's own dialogue; lane "" sees the reverse.
func FilterPublicLaneFrame(raw []byte, producerID, lane string, imageBase func() int) ([]byte, bool) {
	producerID = strings.TrimSpace(producerID)
	if producerID == "" || len(raw) == 0 {
		return nil, false
	}
	var m map[string]any
	if json.Unmarshal(raw, &m) != nil {
		return nil, false
	}
	typ, _ := m["type"].(string)
	nodeID, _ := m["nodeId"].(string)
	if strings.TrimSpace(nodeID) != producerID {
		return nil, false
	}
	typ = strings.ToLower(strings.TrimSpace(typ))
	if lane != "" {
		switch typ {
		case "visitor":
			if l, _ := m["lane"].(string); l != lane {
				return nil, false
			}
			typ, _ = m["kind"].(string)
		case "review", "acp":
			return nil, false
		}
	} else if typ == "visitor" {
		return nil, false
	}
	switch typ {
	case "review":
		return marshalPublicReviewFrame(m, imageBase())
	case "acp":
		return marshalPublicAcpFrame(m)
	case "live":
		return marshalPublicLiveFrame(m)
	default:
		return nil, false
	}
}

// publicLiveFields are the LiveSession fields a public/drawer viewer sees
// (never owner or run id).
var publicLiveFields = []string{
	"sid", "mode", "action", "prompt", "count", "selector", "summary", "url",
	"state", "file", "variants", "selected", "retryAccept", "error", "createdAt", "updatedAt",
}

func marshalPublicLiveFrame(m map[string]any) ([]byte, bool) {
	sess, ok := m["session"].(map[string]any)
	if !ok {
		return nil, false
	}
	b, err := json.Marshal(map[string]any{
		"type":    "live",
		"nodeId":  PublicDialogueNodeID,
		"session": PublicLiveSession(sess),
	})
	if err != nil {
		return nil, false
	}
	return b, true
}

// PublicLiveSession keeps only publicLiveFields of a marshalled LiveSession.
func PublicLiveSession(sess map[string]any) map[string]any {
	out := make(map[string]any, len(publicLiveFields))
	for _, k := range publicLiveFields {
		if v, ok := sess[k]; ok {
			out[k] = v
		}
	}
	return out
}

func marshalPublicReviewFrame(m map[string]any, imageBase int) ([]byte, bool) {
	event, _ := m["event"].(string)
	event = strings.TrimSpace(event)
	if event == "" {
		return nil, false
	}
	out := map[string]any{
		"type":   "review",
		"nodeId": PublicDialogueNodeID,
		"event":  event,
	}
	if waiting, ok := jsonNumber(m["waiting"]); ok {
		out["waiting"] = waiting
	}
	if busy, ok := m["busy"].(bool); ok {
		out["busy"] = busy
	}
	if interrupted, ok := m["interrupted"].(bool); ok {
		out["interrupted"] = interrupted
	}
	if msg, _ := m["message"].(string); strings.TrimSpace(msg) != "" {
		out["message"] = msg
	}
	idx := imageBase
	if ai, next := activeItemFromAny(m["activeItem"], idx); ai != nil {
		out["activeItem"] = ai
		idx = next
	}
	if item, next := activeItemFromAny(m["item"], idx); item != nil {
		out["item"] = item
		idx = next
	}
	if items, next := queueItemsFromAny(m["items"], idx); len(items) > 0 {
		out["items"] = items
		_ = next
	}
	b, err := json.Marshal(out)
	if err != nil {
		return nil, false
	}
	return b, true
}

func marshalPublicAcpFrame(m map[string]any) ([]byte, bool) {
	events := acpEventsFromAny(m["events"])
	rails := SanitizeLiveEvents(events)
	if len(rails) == 0 {
		return nil, false
	}
	out := map[string]any{
		"type":   "acp",
		"nodeId": PublicDialogueNodeID,
		"events": rails,
	}
	if busy, ok := m["busy"].(bool); ok {
		out["busy"] = busy
	}
	b, err := json.Marshal(out)
	if err != nil {
		return nil, false
	}
	return b, true
}

func jsonNumber(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	case json.Number:
		f, err := n.Float64()
		return f, err == nil
	default:
		return 0, false
	}
}

func queueItemsFromAny(v any, imageBase int) ([]PreviewQueueItem, int) {
	switch items := v.(type) {
	case []map[string]any:
		return SanitizeQueueItemsFrom(items, imageBase)
	case []any:
		parsed := make([]map[string]any, 0, len(items))
		for _, raw := range items {
			am, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			parsed = append(parsed, am)
		}
		return SanitizeQueueItemsFrom(parsed, imageBase)
	default:
		return nil, imageBase
	}
}

func activeItemFromAny(v any, imageBase int) (map[string]any, int) {
	switch item := v.(type) {
	case map[string]any:
		ai, next := SanitizeActiveItemFrom(item, imageBase)
		return previewActiveItemMap(ai), next
	default:
		return nil, imageBase
	}
}

func previewActiveItemMap(ai *PreviewActiveItem) map[string]any {
	if ai == nil {
		return nil
	}
	m := map[string]any{}
	if ai.ID != "" {
		m["id"] = ai.ID
	}
	if ai.Text != "" {
		m["text"] = ai.Text
	}
	if len(ai.Images) > 0 {
		m["images"] = ai.Images
	}
	if len(ai.Annotations) > 0 {
		m["annotations"] = ai.Annotations
	}
	if len(m) == 0 {
		return nil
	}
	return m
}

func acpEventsFromAny(v any) []models.AcpEvent {
	switch events := v.(type) {
	case []models.AcpEvent:
		return events
	case []any:
		out := make([]models.AcpEvent, 0, len(events))
		for _, raw := range events {
			am, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			kind, _ := am["kind"].(string)
			text, _ := am["text"].(string)
			title, _ := am["title"].(string)
			status, _ := am["status"].(string)
			out = append(out, models.AcpEvent{Kind: kind, Text: text, Title: title, Status: status, Parts: partsFromAny(am["parts"])})
		}
		return out
	default:
		return nil
	}
}

func partsFromAny(v any) []models.AcpPart {
	arr, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]models.AcpPart, 0, len(arr))
	for _, raw := range arr {
		m, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		kind, _ := m["kind"].(string)
		text, _ := m["text"].(string)
		title, _ := m["title"].(string)
		status, _ := m["status"].(string)
		out = append(out, models.AcpPart{Kind: kind, Text: text, Title: title, Status: status})
	}
	return out
}
