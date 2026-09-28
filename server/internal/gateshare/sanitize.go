package gateshare

import (
	"encoding/json"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/cocofhu/grasp/internal/blob"
	"github.com/cocofhu/grasp/internal/models"
)

const (
	maxVisualHTMLBytes   = 512 * 1024
	maxDescriptionRunes  = 8000
	MaxCommentRunes      = 4000
	MaxExternalNameRunes = 80
	maxTurns             = 80
	maxTurnRunes         = 4000
	maxAnnotationRunes   = 400
	maxUpstreamBytes     = 64 * 1024
	maxQuestionOptions   = 12
	maxFormFields        = 20
)

// PreviewTurn is a leak-free conversation turn for the public workbench.
// Images are blob refs only (no bytes, no URLs). Questions and forms keep the
// clarify cards the main transcript shows.
type PreviewTurn struct {
	Role        string              `json:"role"`
	Text        string              `json:"text,omitempty"`
	At          string              `json:"at,omitempty"`
	Interrupted bool                `json:"interrupted,omitempty"`
	Annotations []PreviewAnnotation `json:"annotations,omitempty"`
	Images      []PreviewImage      `json:"images,omitempty"`
	Questions   []PreviewQuestion   `json:"questions,omitempty"`
	Forms       []PreviewForm       `json:"forms,omitempty"`
}

// PreviewImage is an attachment the logged-in preview can load via /api/blobs.
type PreviewImage struct {
	Ref       string `json:"ref"`
	MimeType  string `json:"mimeType"`
	Name      string `json:"name,omitempty"`
	SizeBytes int64  `json:"sizeBytes,omitempty"`
}

// PreviewQuestion is one ask_question card.
type PreviewQuestion struct {
	ID            string          `json:"id"`
	Prompt        string          `json:"prompt"`
	Options       []PreviewOption `json:"options"`
	AllowMultiple bool            `json:"allowMultiple,omitempty"`
}

// PreviewOption is one choice. DemoHtml is sanitized visual HTML for the option preview.
type PreviewOption struct {
	ID          string `json:"id"`
	Label       string `json:"label"`
	Recommended bool   `json:"recommended,omitempty"`
	DemoHtml    string `json:"demoHtml,omitempty"`
}

// PreviewForm is one ask_form card.
type PreviewForm struct {
	Title  string             `json:"title,omitempty"`
	Fields []PreviewFormField `json:"fields"`
}

// PreviewFormField is one plaintext field. Type is text (empty) or url.
type PreviewFormField struct {
	Name        string `json:"name"`
	Label       string `json:"label"`
	Type        string `json:"type,omitempty"`
	Placeholder string `json:"placeholder,omitempty"`
	Value       string `json:"value,omitempty"`
	Required    bool   `json:"required,omitempty"`
	Why         string `json:"why,omitempty"`
}

// PreviewAnnotation is a leak-free annotation chip (no blob URLs).
type PreviewAnnotation struct {
	Selector string `json:"selector,omitempty"`
	JSONPath string `json:"jsonPath,omitempty"`
	Label    string `json:"label,omitempty"`
	Note     string `json:"note,omitempty"`
	Quote    string `json:"quote,omitempty"`
}

// PreviewQueueItem is a leak-free pending-send row for polling resume.
// Annotations are kept (sanitized) so public refresh / WS reconcile can show
// the 批注 badge and refill chips on re-edit; images stay dropped (same as ActiveItem).
type PreviewQueueItem struct {
	ID          string              `json:"id,omitempty"`
	Text        string              `json:"text,omitempty"`
	Annotations []PreviewAnnotation `json:"annotations,omitempty"`
}

// PreviewActiveItem is a leak-free in-flight turn hint (no images / blob URLs).
type PreviewActiveItem struct {
	ID          string              `json:"id,omitempty"`
	Text        string              `json:"text,omitempty"`
	Annotations []PreviewAnnotation `json:"annotations,omitempty"`
}

var (
	leakyURLRe     = regexp.MustCompile(`(?i)(?:blob:[^\s"'<>]*|/api/[^\s"'<>]*|/preview/[^\s"'<>]*|/sandbox[^\s"'<>]*|/v1/[^\s"'<>]*|https?://(?:localhost|127\.0\.0\.1|0\.0\.0\.0|10\.\d{1,3}\.\d{1,3}\.\d{1,3}|192\.168\.\d{1,3}\.\d{1,3}|172\.(?:1[6-9]|2\d|3[0-1])\.\d{1,3}\.\d{1,3})(?::\d+)?[^\s"'<>]*)`)
	internalHostRe = regexp.MustCompile(`(?i)\b(?:localhost|127\.0\.0\.1|0\.0\.0\.0)\b`)
	mimeTypeRe     = regexp.MustCompile(`^[a-z0-9][a-z0-9.+-]{0,63}/[a-z0-9][a-z0-9.+-]{0,63}$`)
)

// SanitizeDescription redacts internal URLs / blob addresses from gate body text.
func SanitizeDescription(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	s = leakyURLRe.ReplaceAllString(s, "[redacted]")
	if utf8.RuneCountInString(s) > maxDescriptionRunes {
		r := []rune(s)
		s = string(r[:maxDescriptionRunes]) + "…"
	}
	return s
}

// SanitizeVisualHTML strips leaky addresses from visual page.html. Scripts stay
// (HtmlPreview sandbox); size is capped.
func SanitizeVisualHTML(html string) string {
	html = strings.TrimSpace(html)
	if html == "" {
		return ""
	}
	if len(html) > maxVisualHTMLBytes {
		html = html[:maxVisualHTMLBytes]
	}
	html = leakyURLRe.ReplaceAllString(html, "#")
	return html
}

// SanitizeStructured extracts a leak-free preview object from artifact JSON/text.
func SanitizeStructured(name, content string) map[string]any {
	name = strings.TrimSpace(name)
	content = strings.TrimSpace(content)
	if content == "" {
		return nil
	}
	out := map[string]any{}
	if name != "" && !looksInternalName(name) {
		out["name"] = safeArtifactName(name)
	}
	var parsed any
	if err := json.Unmarshal([]byte(content), &parsed); err != nil {
		text := SanitizeDescription(content)
		if text != "" {
			out["text"] = text
		}
		if len(out) == 0 {
			return nil
		}
		return out
	}
	walk := sanitizeJSONValue(parsed, 0)
	if m, ok := walk.(map[string]any); ok {
		if t := stringField(m, "title", "summary"); t != "" {
			out["title"] = t
		}
		if g, ok := m["goals"]; ok {
			if cleaned := sanitizeJSONValue(g, 0); cleaned != nil {
				out["goals"] = cleaned
			}
		}
		if d := stringField(m, "description", "detail", "body"); d != "" {
			out["description"] = d
		}
		if doc := capStructuredDoc(m); doc != nil {
			out["doc"] = doc
		}
	}
	if len(out) == 1 && out["name"] != nil {
		if s, ok := walk.(string); ok && strings.TrimSpace(s) != "" {
			out["text"] = SanitizeDescription(s)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func looksInternalName(name string) bool {
	n := strings.ToLower(name)
	return strings.Contains(n, "env") || strings.Contains(n, "secret") || strings.Contains(n, "token")
}

func safeArtifactName(name string) string {
	base := name
	if i := strings.LastIndex(name, "/"); i >= 0 {
		base = name[i+1:]
	}
	return strings.TrimSpace(base)
}

func stringField(m map[string]any, keys ...string) string {
	for _, k := range keys {
		if v, ok := m[k]; ok {
			if s, ok := v.(string); ok {
				s = SanitizeDescription(s)
				if s != "" && !internalHostRe.MatchString(s) {
					return s
				}
			}
		}
	}
	return ""
}

func sanitizeJSONValue(v any, depth int) any {
	if depth > 6 {
		return nil
	}
	switch t := v.(type) {
	case map[string]any:
		out := map[string]any{}
		for k, val := range t {
			lk := strings.ToLower(strings.ReplaceAll(k, "-", "_"))
			if strings.Contains(lk, "token") || strings.Contains(lk, "secret") ||
				strings.Contains(lk, "password") || strings.Contains(lk, "apikey") ||
				strings.Contains(lk, "env") || lk == "runid" || lk == "run_id" ||
				lk == "projectid" || lk == "project_id" || lk == "workflowid" ||
				lk == "workflow_id" || lk == "workflow_name" || lk == "workflowname" ||
				strings.Contains(lk, "member") || lk == "url" || lk == "href" ||
				strings.Contains(lk, "blob") {
				continue
			}
			if cleaned := sanitizeJSONValue(val, depth+1); cleaned != nil {
				out[k] = cleaned
			}
		}
		if len(out) == 0 {
			return nil
		}
		return out
	case []any:
		out := make([]any, 0, len(t))
		for _, item := range t {
			if cleaned := sanitizeJSONValue(item, depth+1); cleaned != nil {
				out = append(out, cleaned)
			}
		}
		if len(out) == 0 {
			return nil
		}
		return out
	case string:
		s := SanitizeDescription(t)
		if s == "" || leakyURLRe.MatchString(t) {
			return nil
		}
		return s
	case float64, bool, nil:
		return t
	default:
		return nil
	}
}

// SanitizeTurns redacts conversation history for the public ReAct sidebar.
// Inline bytes and raw URLs are dropped. Blob refs, choice cards and forms stay
// so the preview drawer matches the run transcript. Text is size-capped.
func SanitizeTurns(msgs []models.ReactMessage) []PreviewTurn {
	if len(msgs) == 0 {
		return nil
	}
	if len(msgs) > maxTurns {
		msgs = msgs[len(msgs)-maxTurns:]
	}
	out := make([]PreviewTurn, 0, len(msgs))
	for _, m := range msgs {
		role := strings.ToLower(strings.TrimSpace(m.Role))
		if role != "agent" && role != "human" {
			continue
		}
		text := capTurnText(SanitizeDescription(m.Text))
		turn := PreviewTurn{Role: role, Text: text, At: strings.TrimSpace(m.At), Interrupted: m.Interrupted}
		if anns := sanitizeAnnotations(m.Annotations); len(anns) > 0 {
			turn.Annotations = anns
		}
		if images := sanitizeImages(m.Images); len(images) > 0 {
			turn.Images = images
		}
		if questions := sanitizeQuestions(m.Questions); len(questions) > 0 {
			turn.Questions = questions
		}
		if forms := sanitizeForms(m.Forms); len(forms) > 0 {
			turn.Forms = forms
		}
		if turn.Text == "" && len(turn.Annotations) == 0 && len(turn.Images) == 0 && len(turn.Questions) == 0 && len(turn.Forms) == 0 && !turn.Interrupted {
			continue
		}
		out = append(out, turn)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// SanitizeQueueItems redacts pending FIFO rows for the public ReAct sidebar.
// Carries sanitized annotations (aligned with ActiveItem) so poll/WS waiting
// rows keep 批注 badges and edit refill; drops images to avoid blob/URL leaks.
func SanitizeQueueItems(items []map[string]any) []PreviewQueueItem {
	if len(items) == 0 {
		return nil
	}
	out := make([]PreviewQueueItem, 0, len(items))
	for _, it := range items {
		if it == nil {
			continue
		}
		id, _ := it["id"].(string)
		text, _ := it["text"].(string)
		text = capTurnText(SanitizeDescription(text))
		id = strings.TrimSpace(id)
		anns := annotationsFromAny(it["annotations"])
		if id == "" && text == "" && len(anns) == 0 {
			continue
		}
		item := PreviewQueueItem{ID: id, Text: text}
		if len(anns) > 0 {
			item.Annotations = anns
		}
		out = append(out, item)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// SanitizeActiveItem redacts the in-flight turn for polling resume. Images are dropped.
func SanitizeActiveItem(m map[string]any) *PreviewActiveItem {
	if m == nil {
		return nil
	}
	id, _ := m["id"].(string)
	text, _ := m["text"].(string)
	item := &PreviewActiveItem{
		ID:          strings.TrimSpace(id),
		Text:        capTurnText(SanitizeDescription(text)),
		Annotations: annotationsFromAny(m["annotations"]),
	}
	if item.ID == "" && item.Text == "" && len(item.Annotations) == 0 {
		return nil
	}
	return item
}

// annotationsFromAny parses ReactAnnotation slices from JSON-decoded maps or typed slices.
func annotationsFromAny(v any) []PreviewAnnotation {
	switch anns := v.(type) {
	case []models.ReactAnnotation:
		return sanitizeAnnotations(anns)
	case []PreviewAnnotation:
		if len(anns) == 0 {
			return nil
		}
		parsed := make([]models.ReactAnnotation, 0, len(anns))
		for _, a := range anns {
			parsed = append(parsed, models.ReactAnnotation{
				Selector: a.Selector,
				JSONPath: a.JSONPath,
				Label:    a.Label,
				Note:     a.Note,
				Quote:    a.Quote,
			})
		}
		return sanitizeAnnotations(parsed)
	case []any:
		parsed := make([]models.ReactAnnotation, 0, len(anns))
		for _, raw := range anns {
			am, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			parsed = append(parsed, models.ReactAnnotation{
				Selector: stringMapField(am, "selector"),
				JSONPath: stringMapField(am, "jsonPath"),
				Label:    stringMapField(am, "label"),
				Note:     stringMapField(am, "note"),
				Quote:    stringMapField(am, "quote"),
			})
		}
		return sanitizeAnnotations(parsed)
	default:
		return nil
	}
}

func capTurnText(text string) string {
	if utf8.RuneCountInString(text) <= maxTurnRunes {
		return text
	}
	r := []rune(text)
	return string(r[:maxTurnRunes]) + "…"
}

func stringMapField(m map[string]any, key string) string {
	s, _ := m[key].(string)
	return strings.TrimSpace(s)
}

func sanitizeAnnotations(anns []models.ReactAnnotation) []PreviewAnnotation {
	if len(anns) == 0 {
		return nil
	}
	out := make([]PreviewAnnotation, 0, len(anns))
	for _, a := range anns {
		pa := PreviewAnnotation{
			Selector: clampAnnotationField(a.Selector),
			JSONPath: clampAnnotationField(a.JSONPath),
			Label:    clampAnnotationField(a.Label),
			Note:     clampAnnotationField(a.Note),
			Quote:    clampAnnotationField(a.Quote),
		}
		if pa.Selector == "" && pa.JSONPath == "" && pa.Label == "" && pa.Note == "" && pa.Quote == "" {
			continue
		}
		out = append(out, pa)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func sanitizeImages(images []models.PromptImage) []PreviewImage {
	if len(images) == 0 {
		return nil
	}
	out := make([]PreviewImage, 0, len(images))
	for _, im := range images {
		ref, err := blob.ParseRef(im.Ref)
		if err != nil {
			continue
		}
		item := PreviewImage{Ref: ref.String(), MimeType: sanitizeMime(im.MimeType)}
		if name := capTurnText(SanitizeDescription(im.Name)); name != "" {
			item.Name = name
		}
		if im.SizeBytes > 0 {
			item.SizeBytes = im.SizeBytes
		}
		out = append(out, item)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func sanitizeMime(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	if mimeTypeRe.MatchString(s) {
		return s
	}
	return "application/octet-stream"
}

func sanitizeQuestions(qs []models.ReactQuestion) []PreviewQuestion {
	if len(qs) == 0 {
		return nil
	}
	out := make([]PreviewQuestion, 0, len(qs))
	for _, q := range qs {
		id := strings.TrimSpace(q.ID)
		prompt := capTurnText(SanitizeDescription(q.Prompt))
		opts := sanitizeOptions(q.Options)
		if id == "" || prompt == "" || len(opts) == 0 {
			continue
		}
		out = append(out, PreviewQuestion{
			ID:            id,
			Prompt:        prompt,
			Options:       opts,
			AllowMultiple: q.AllowMultiple,
		})
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func sanitizeOptions(opts []models.ReactOption) []PreviewOption {
	if len(opts) == 0 {
		return nil
	}
	if len(opts) > maxQuestionOptions {
		opts = opts[:maxQuestionOptions]
	}
	out := make([]PreviewOption, 0, len(opts))
	for _, o := range opts {
		id := strings.TrimSpace(o.ID)
		label := capTurnText(SanitizeDescription(o.Label))
		if id == "" || label == "" {
			continue
		}
		item := PreviewOption{ID: id, Label: label, Recommended: o.Recommended}
		if html := SanitizeVisualHTML(o.DemoHtml); html != "" {
			item.DemoHtml = html
		}
		out = append(out, item)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func sanitizeForms(forms []models.ReactForm) []PreviewForm {
	if len(forms) == 0 {
		return nil
	}
	out := make([]PreviewForm, 0, len(forms))
	for _, f := range forms {
		fields := sanitizeFormFields(f.Fields)
		if len(fields) == 0 {
			continue
		}
		out = append(out, PreviewForm{
			Title:  capTurnText(SanitizeDescription(f.Title)),
			Fields: fields,
		})
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func sanitizeFormFields(fields []models.ReactFormField) []PreviewFormField {
	if len(fields) == 0 {
		return nil
	}
	if len(fields) > maxFormFields {
		fields = fields[:maxFormFields]
	}
	out := make([]PreviewFormField, 0, len(fields))
	for _, f := range fields {
		name := strings.TrimSpace(f.Name)
		label := capTurnText(SanitizeDescription(f.Label))
		if name == "" || label == "" {
			continue
		}
		item := PreviewFormField{
			Name:        name,
			Label:       label,
			Placeholder: capTurnText(SanitizeDescription(f.Placeholder)),
			Value:       capTurnText(SanitizeDescription(f.Value)),
			Required:    f.Required,
			Why:         capTurnText(SanitizeDescription(f.Why)),
		}
		if strings.EqualFold(strings.TrimSpace(f.Type), "url") {
			item.Type = "url"
		}
		out = append(out, item)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func clampAnnotationField(s string) string {
	s = SanitizeDescription(s)
	if utf8.RuneCountInString(s) > maxAnnotationRunes {
		r := []rune(s)
		s = string(r[:maxAnnotationRunes]) + "…"
	}
	return s
}

// SanitizeUpstream extracts a leak-free clarified_requirement (or similar) payload,
// including a size-capped doc for on-demand full upstream views.
func SanitizeUpstream(name, content string) map[string]any {
	return sanitizeUpstream(name, content, true)
}

// SanitizeUpstreamSummary is the open-path / poll payload: name/title/summary/description
// only — never doc. Full body is fetched via the dedicated public upstream route.
func SanitizeUpstreamSummary(name, content string) map[string]any {
	return sanitizeUpstream(name, content, false)
}

func sanitizeUpstream(name, content string, includeDoc bool) map[string]any {
	name = strings.TrimSpace(name)
	content = strings.TrimSpace(content)
	if content == "" {
		return nil
	}
	out := map[string]any{}
	if name != "" && !looksInternalName(name) {
		out["name"] = safeArtifactName(name)
	} else {
		out["name"] = "clarified_requirement.json"
	}
	var parsed any
	if err := json.Unmarshal([]byte(content), &parsed); err != nil {
		text := SanitizeDescription(content)
		if text == "" {
			return nil
		}
		out["text"] = text
		return out
	}
	walk := sanitizeJSONValue(parsed, 0)
	m, ok := walk.(map[string]any)
	if !ok {
		if s, ok := walk.(string); ok && strings.TrimSpace(s) != "" {
			out["text"] = SanitizeDescription(s)
			return out
		}
		return nil
	}
	if t := stringField(m, "title", "summary"); t != "" {
		out["title"] = t
	}
	if s := stringField(m, "summary", "background"); s != "" {
		out["summary"] = s
	}
	if d := stringField(m, "description", "detail", "background"); d != "" {
		out["description"] = d
	}
	if includeDoc {
		if doc := capStructuredDoc(m); doc != nil {
			out["doc"] = doc
		}
	}
	if len(out) <= 1 {
		return nil
	}
	return out
}

func capStructuredDoc(m map[string]any) map[string]any {
	if m == nil {
		return nil
	}
	b, err := json.Marshal(m)
	if err != nil || len(b) == 0 {
		return nil
	}
	if len(b) > maxUpstreamBytes {
		// Keep title/summary/goals so the main thesis remains reviewable.
		slim := map[string]any{}
		for _, k := range []string{"title", "summary", "background", "goals", "description", "in_scope", "out_of_scope"} {
			if v, ok := m[k]; ok {
				slim[k] = v
			}
		}
		if len(slim) == 0 {
			return nil
		}
		return slim
	}
	return m
}

// ClampComment bounds external comment length.
func ClampComment(s string) (string, bool) {
	s = strings.TrimSpace(s)
	if utf8.RuneCountInString(s) > MaxCommentRunes {
		return "", false
	}
	return s, true
}

// ClampExternalName bounds optional external display name.
func ClampExternalName(s string) (string, bool) {
	s = strings.TrimSpace(s)
	if utf8.RuneCountInString(s) > MaxExternalNameRunes {
		return "", false
	}
	return s, true
}
