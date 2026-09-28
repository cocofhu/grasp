package gateshare

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/models"
)

func mustFuture() time.Time {
	return time.Now().Add(24 * time.Hour)
}

func TestSanitizeQueueItemsKeepsAnnotationsDropsImages(t *testing.T) {
	items := SanitizeQueueItems([]map[string]any{
		{
			"id":   "q-ann",
			"text": "改标题 http://10.1.2.3/api/runs/x",
			"annotations": []any{
				map[string]any{
					"selector": "#title",
					"label":    "标题",
					"jsonPath": "goals[0]",
					"url":      "http://127.0.0.1:8080/preview/x",
				},
			},
			"images": []any{map[string]any{"url": "blob:http://127.0.0.1/abc", "mimeType": "image/png"}},
		},
		{
			"id": "q-ann-only",
			"annotations": []models.ReactAnnotation{
				{Selector: "#hero", Label: "仅标注"},
			},
		},
	})
	if len(items) != 2 {
		t.Fatalf("items=%d %+v", len(items), items)
	}
	if strings.Contains(items[0].Text, "10.1.2.3") {
		t.Fatalf("text leak: %s", items[0].Text)
	}
	if len(items[0].Annotations) != 1 || items[0].Annotations[0].Selector != "#title" {
		t.Fatalf("ann: %+v", items[0].Annotations)
	}
	if len(items[1].Annotations) != 1 || items[1].Annotations[0].Label != "仅标注" {
		t.Fatalf("ann-only: %+v", items[1])
	}
	raw, _ := json.Marshal(items)
	if strings.Contains(string(raw), "blob:") || strings.Contains(string(raw), "images") || strings.Contains(string(raw), "127.0.0.1") {
		t.Fatalf("queue sanitize leak: %s", raw)
	}
}

func TestSanitizeTurnsKeepsChoicesImagesAndForms(t *testing.T) {
	msgs := []models.ReactMessage{
		{
			Role: "human",
			Text: "看 http://127.0.0.1:8080/api/blobs/secret 和 blob:not-a-ref",
			At:   "2026-09-28T00:00:00Z",
			Images: []models.PromptImage{
				{Ref: "blob:abc123", MimeType: "image/png", Name: "shot.png", SizeBytes: 12, Data: "AAAA"},
				{Ref: "http://127.0.0.1/x.png", MimeType: "image/png"},
				{Ref: "blob:../etc", MimeType: "image/png"},
			},
		},
		{
			Role: "agent",
			Text: "选一处",
			Questions: []models.ReactQuestion{{
				ID:     "which",
				Prompt: "哪条 http://10.1.2.3/secret",
				Options: []models.ReactOption{{
					ID:          "drawer",
					Label:       "抽屉",
					Recommended: true,
					DemoHtml:    `<div>ok</div><a href="http://127.0.0.1:8080/api/runs/x">x</a>`,
				}},
			}},
			Forms: []models.ReactForm{{
				Title: "环境",
				Fields: []models.ReactFormField{{
					Name:        "host",
					Label:       "地址",
					Type:        "url",
					Placeholder: "https://example.com",
					Value:       "http://127.0.0.1:8080/api",
					Required:    true,
					Why:         "缺口",
				}},
			}},
		},
	}
	turns := SanitizeTurns(msgs)
	if len(turns) != 2 {
		t.Fatalf("turns=%d", len(turns))
	}
	raw, err := json.Marshal(turns)
	if err != nil {
		t.Fatal(err)
	}
	body := string(raw)
	if strings.Contains(body, "127.0.0.1") || strings.Contains(body, "/api/runs") || strings.Contains(body, "AAAA") || strings.Contains(body, "blob:../") {
		t.Fatalf("leak: %s", body)
	}
	if strings.Contains(turns[0].Text, "blob:") || !strings.Contains(turns[0].Text, "[redacted]") {
		t.Fatalf("text: %s", turns[0].Text)
	}
	if len(turns[0].Images) != 1 || turns[0].Images[0].Ref != "blob:abc123" || turns[0].Images[0].MimeType != "image/png" || turns[0].Images[0].Name != "shot.png" {
		t.Fatalf("images: %+v", turns[0].Images)
	}
	if len(turns[1].Questions) != 1 || turns[1].Questions[0].Options[0].Label != "抽屉" || !turns[1].Questions[0].Options[0].Recommended {
		t.Fatalf("questions: %+v", turns[1].Questions)
	}
	if !strings.Contains(turns[1].Questions[0].Prompt, "[redacted]") {
		t.Fatalf("prompt: %s", turns[1].Questions[0].Prompt)
	}
	if strings.Contains(turns[1].Questions[0].Options[0].DemoHtml, "127.0.0.1") || !strings.Contains(turns[1].Questions[0].Options[0].DemoHtml, "ok") {
		t.Fatalf("demo: %s", turns[1].Questions[0].Options[0].DemoHtml)
	}
	if len(turns[1].Forms) != 1 || turns[1].Forms[0].Fields[0].Type != "url" || !strings.Contains(turns[1].Forms[0].Fields[0].Value, "[redacted]") {
		t.Fatalf("forms: %+v", turns[1].Forms)
	}
}

func TestSanitizeTurnsDropsIdentityAndCaps(t *testing.T) {
	msgs := []models.ReactMessage{
		{Role: "agent", Text: "请审阅 page.html，勿访问 http://10.1.2.3/api/runs/abc", At: "2026-08-01T00:00:00Z"},
		{Role: "human", Text: "改标题", At: "2026-08-01T00:01:00Z", Annotations: []models.ReactAnnotation{
			{Selector: "#title", Note: "改成交付确认", URL: "http://127.0.0.1:8080/preview/run-1/n/1/"},
		}},
		{Role: "system", Text: "should skip"},
	}
	turns := SanitizeTurns(msgs)
	if len(turns) != 2 {
		t.Fatalf("turns=%d %+v", len(turns), turns)
	}
	if strings.Contains(turns[0].Text, "10.1.2.3") || strings.Contains(turns[0].Text, "/api/runs") {
		t.Fatalf("agent text leaked: %s", turns[0].Text)
	}
	if turns[1].Role != "human" || len(turns[1].Annotations) == 0 || turns[1].Annotations[0].Selector != "#title" {
		t.Fatalf("human ann: %+v", turns[1])
	}
}

func TestSanitizeUpstreamKeepsThesisWithoutRunID(t *testing.T) {
	raw := `{
		"title":"澄清需求",
		"summary":"把临时页做成审批工作台",
		"goals":["三区布局","ReAct 复审"],
		"runId":"run-secret",
		"projectId":"p1",
		"url":"http://127.0.0.1/api/blobs/x"
	}`
	up := SanitizeUpstream("clarified_requirement.json", raw)
	if up == nil {
		t.Fatal("expected upstream")
	}
	if up["title"] != "澄清需求" {
		t.Fatalf("title: %+v", up)
	}
	if up["doc"] == nil {
		t.Fatal("full SanitizeUpstream must include doc for on-demand path")
	}
	b, err := json.Marshal(up)
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	if strings.Contains(s, "run-secret") || strings.Contains(s, "127.0.0.1") || strings.Contains(s, "projectId") {
		t.Fatalf("upstream leak: %s", s)
	}
}

func TestSanitizeUpstreamSummaryOmitsDoc(t *testing.T) {
	raw := `{"title":"澄清需求","summary":"摘要","goals":["g1"],"background":"背景较长"}`
	sum := SanitizeUpstreamSummary("clarified_requirement.json", raw)
	if sum == nil {
		t.Fatal("expected summary")
	}
	if sum["doc"] != nil {
		t.Fatalf("summary must not include doc: %+v", sum)
	}
	if sum["title"] != "澄清需求" || sum["summary"] == "" {
		t.Fatalf("summary fields: %+v", sum)
	}
	full := SanitizeUpstream("clarified_requirement.json", raw)
	if full["doc"] == nil {
		t.Fatal("full upstream must keep doc")
	}
}

func TestApplySparsePreviewOmitsUnchangedLargeFields(t *testing.T) {
	html := "<p>visual</p>"
	up := map[string]any{"name": "clarified_requirement.json", "title": "澄清", "summary": "摘要"}
	dto := PreviewDTO{
		Status:         "active",
		VisualHTML:     html,
		VisualHTMLHash: ContentHash(html),
		Upstream:       up,
		UpstreamHash:   HashUpstream(up),
	}
	ApplySparsePreview(&dto, SparsePreviewKnown{VisualHTML: dto.VisualHTMLHash, Upstream: dto.UpstreamHash})
	if dto.VisualHTML != "" {
		t.Fatalf("expected visualHtml omitted, got %q", dto.VisualHTML)
	}
	if dto.Upstream != nil {
		t.Fatalf("expected upstream omitted, got %+v", dto.Upstream)
	}
	if dto.VisualHTMLHash == "" || dto.UpstreamHash == "" {
		t.Fatal("hashes must remain so client can keep merging")
	}
	// Changed visual → body returns.
	dto2 := PreviewDTO{
		Status:         "active",
		VisualHTML:     "<p>new</p>",
		VisualHTMLHash: ContentHash("<p>new</p>"),
		Upstream:       up,
		UpstreamHash:   HashUpstream(up),
	}
	ApplySparsePreview(&dto2, SparsePreviewKnown{VisualHTML: ContentHash(html), Upstream: dto.UpstreamHash})
	if dto2.VisualHTML != "<p>new</p>" {
		t.Fatalf("changed visual must be returned: %q", dto2.VisualHTML)
	}
	if dto2.Upstream != nil {
		t.Fatalf("unchanged upstream still omitted: %+v", dto2.Upstream)
	}
}

func TestApplySparsePreviewOmitsUnchangedStructuredAndTurns(t *testing.T) {
	st := map[string]any{"name": "research.json", "title": "调研"}
	turns := []PreviewTurn{{Role: "agent", Text: "请复审", At: "2026-08-01T00:00:00Z"}}
	dto := PreviewDTO{
		Status:         "active",
		Structured:     st,
		StructuredHash: HashStructured(st),
		Turns:          turns,
		TurnsHash:      HashTurns(turns),
	}
	ApplySparsePreview(&dto, SparsePreviewKnown{Structured: dto.StructuredHash, Turns: dto.TurnsHash})
	if dto.Structured != nil {
		t.Fatalf("expected structured omitted, got %+v", dto.Structured)
	}
	if dto.Turns != nil {
		t.Fatalf("expected turns omitted, got %+v", dto.Turns)
	}
	if dto.StructuredHash == "" || dto.TurnsHash == "" {
		t.Fatal("structured/turns hashes must remain")
	}
	dto2 := PreviewDTO{
		Status:         "active",
		Structured:     map[string]any{"name": "research.json", "title": "新调研"},
		StructuredHash: HashStructured(map[string]any{"name": "research.json", "title": "新调研"}),
		Turns:          append(turns, PreviewTurn{Role: "human", Text: "改摘要", At: "2026-08-01T00:01:00Z"}),
	}
	dto2.TurnsHash = HashTurns(dto2.Turns)
	ApplySparsePreview(&dto2, SparsePreviewKnown{Structured: HashStructured(st), Turns: HashTurns(turns)})
	if dto2.Structured == nil || dto2.Structured["title"] != "新调研" {
		t.Fatalf("changed structured must return: %+v", dto2.Structured)
	}
	if len(dto2.Turns) != 2 {
		t.Fatalf("changed turns must return: %+v", dto2.Turns)
	}
}

func TestWantPreviewNonce(t *testing.T) {
	if !WantPreviewNonce(false, false) {
		t.Fatal("first/non-silent load must issue")
	}
	if WantPreviewNonce(true, false) {
		t.Fatal("silent poll must not issue by default")
	}
	if !WantPreviewNonce(true, true) {
		t.Fatal("silent + issueNonce must issue (resume / near TTL)")
	}
}

func TestBuildReviewPreviewDTOIncludesWorkbenchFields(t *testing.T) {
	alive := true
	lookup := &LookupResult{
		Kind: models.ShareLinkKindReview,
		Link: models.GateShareLink{ExpiresAt: mustFuture()},
		Node: &models.Node{ID: "research1", Type: "research", Label: "调研"},
	}
	dto := BuildReviewPreviewDTO(models.ShareLinkStateActive, lookup, "", "research.json", `{"title":"调研摘要","goals":["g1"],"runId":"hide-me"}`, "nonce-1", PreviewExtras{
		Turns: []models.ReactMessage{
			{Role: "agent", Text: "请复审 research.json", At: "2026-08-01T00:00:00Z"},
		},
		UpstreamName:      "clarified_requirement.json",
		UpstreamContent:   `{"title":"澄清","summary":"对照审阅","runId":"nope"}`,
		ReactSessionAlive: alive,
		ProductKind:       ProductKindStructured,
		ProductName:       "research.json",
	})
	if dto.Kind != models.ShareLinkKindReview {
		t.Fatalf("kind=%s", dto.Kind)
	}
	if dto.NodeType != "research" {
		t.Fatalf("nodeType=%s want research", dto.NodeType)
	}
	if dto.Actions["confirm"] != "confirm" || dto.Actions["reply"] != "reply" || dto.Actions["cancel"] != "cancel" {
		t.Fatalf("actions=%+v", dto.Actions)
	}
	if dto.Actions["reject"] != "" {
		t.Fatalf("review must not expose reject: %+v", dto.Actions)
	}
	if dto.ReactSessionAlive == nil || !*dto.ReactSessionAlive {
		t.Fatal("expected alive")
	}
	if dto.ProductKind != ProductKindStructured || dto.ProductName != "research.json" {
		t.Fatalf("product=%s/%s", dto.ProductKind, dto.ProductName)
	}
	if len(dto.Turns) != 1 || dto.Upstream == nil || dto.Upstream["title"] != "澄清" {
		t.Fatalf("turns/upstream: turns=%+v upstream=%+v", dto.Turns, dto.Upstream)
	}
	if dto.Upstream["doc"] != nil {
		t.Fatalf("preview upstream must omit doc: %+v", dto.Upstream)
	}
	if dto.VisualHTMLHash == "" && dto.VisualHTML != "" {
		t.Fatal("expected visualHtmlHash when visual present")
	}
	if dto.UpstreamHash == "" {
		t.Fatal("expected upstreamHash")
	}
	raw, _ := json.Marshal(dto)
	s := string(raw)
	if strings.Contains(s, "hide-me") || strings.Contains(s, "nope") || strings.Contains(s, "runId") {
		t.Fatalf("preview leak: %s", s)
	}
}

func TestBuildReviewPreviewDTOIncludesQueueState(t *testing.T) {
	lookup := &LookupResult{
		Kind: models.ShareLinkKindReview,
		Link: models.GateShareLink{ExpiresAt: mustFuture()},
		Node: &models.Node{ID: "research1", Type: "research", Label: "调研"},
	}
	dto := BuildReviewPreviewDTO(models.ShareLinkStateActive, lookup, "", "research.json", `{"title":"调研摘要"}`, "nonce-q", PreviewExtras{
		ReactSessionAlive: true,
		SessionBusy:       true,
		Waiting:           1,
		QueueItems: []map[string]any{{
			"id":   "q1",
			"text": "请改标题，勿访问 http://10.1.2.3/api/runs/x",
			"annotations": []any{
				map[string]any{"selector": "#title", "label": "标题", "jsonPath": "goals[0]"},
			},
			"images": []any{map[string]any{"url": "blob:http://127.0.0.1/queue"}},
		}},
		ActiveItem: map[string]any{
			"id":     "q1",
			"text":   "请改标题，勿访问 http://10.1.2.3/api/runs/x",
			"images": []any{map[string]any{"url": "blob:http://127.0.0.1/abc"}},
			"annotations": []any{
				map[string]any{"selector": "#hero", "label": "进行中"},
			},
		},
		ProductKind: ProductKindStructured,
		ProductName: "research.json",
	})
	if !dto.SessionBusy || dto.Waiting != 1 || len(dto.QueueItems) != 1 || dto.ActiveItem == nil {
		t.Fatalf("queue dto: busy=%v waiting=%d items=%+v active=%+v", dto.SessionBusy, dto.Waiting, dto.QueueItems, dto.ActiveItem)
	}
	if strings.Contains(dto.QueueItems[0].Text, "10.1.2.3") || strings.Contains(dto.ActiveItem.Text, "/api/runs") {
		t.Fatalf("queue leak: %+v %+v", dto.QueueItems[0], dto.ActiveItem)
	}
	if len(dto.QueueItems[0].Annotations) != 1 || dto.QueueItems[0].Annotations[0].Selector != "#title" {
		t.Fatalf("queue items must keep sanitized annotations: %+v", dto.QueueItems[0])
	}
	if len(dto.ActiveItem.Annotations) != 1 || dto.ActiveItem.Annotations[0].Selector != "#hero" {
		t.Fatalf("activeItem annotations: %+v", dto.ActiveItem)
	}
	raw, _ := json.Marshal(dto)
	if strings.Contains(string(raw), "blob:") || strings.Contains(string(raw), "127.0.0.1") {
		t.Fatalf("activeItem leaked images/host: %s", raw)
	}
	if strings.Contains(string(raw), `"images"`) {
		t.Fatalf("public queue/active must not leak images key: %s", raw)
	}
}

func TestBuildReviewPreviewDTOClarifyCopyAndEmptyProduct(t *testing.T) {
	lookup := &LookupResult{
		Kind: models.ShareLinkKindReview,
		Link: models.GateShareLink{ExpiresAt: mustFuture()},
		Node: &models.Node{ID: "clarify", Type: "react", Label: "需求澄清"},
	}
	dto := BuildReviewPreviewDTO(models.ShareLinkStateActive, lookup, "", "", "", "nonce-c", PreviewExtras{
		Turns: []models.ReactMessage{
			{Role: "agent", Text: "请补充验收标准", At: "2026-08-01T00:00:00Z"},
		},
		ReactSessionAlive: true,
	})
	if dto.Kind != models.ShareLinkKindReview {
		t.Fatalf("kind must stay review: %s", dto.Kind)
	}
	if dto.NodeType != "react" {
		t.Fatalf("nodeType=%s", dto.NodeType)
	}
	if dto.Title != "需求澄清" {
		t.Fatalf("title=%q", dto.Title)
	}
	if !strings.Contains(dto.Description, "外部澄清") {
		t.Fatalf("description=%q", dto.Description)
	}
	if strings.Contains(dto.Description, "待复审") || strings.Contains(dto.Description, "不触发 Agent") {
		t.Fatalf("clarify copy leaked review wording: %q", dto.Description)
	}
	if dto.Structured != nil || dto.VisualHTML != "" {
		t.Fatalf("first-round empty product expected, got structured=%v visual=%q", dto.Structured, dto.VisualHTML)
	}
	raw, _ := json.Marshal(dto)
	if strings.Contains(string(raw), "run-") || strings.Contains(string(raw), "Run#") {
		t.Fatalf("preview leak: %s", raw)
	}

	reviewLookup := &LookupResult{
		Kind: models.ShareLinkKindReview,
		Link: models.GateShareLink{ExpiresAt: mustFuture()},
		Node: &models.Node{ID: "research1", Type: "research", Label: "调研"},
	}
	rev := BuildReviewPreviewDTO(models.ShareLinkStateActive, reviewLookup, "", "research.json", `{"title":"调研"}`, "n2", PreviewExtras{})
	if !strings.Contains(rev.Description, "待复审") {
		t.Fatalf("review copy must stay 待复审: %q", rev.Description)
	}
	if strings.Contains(rev.Description, "外部澄清") {
		t.Fatalf("review copy must not use clarify wording: %q", rev.Description)
	}
}
