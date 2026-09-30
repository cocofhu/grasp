package gateshare

import (
	"encoding/json"
	"regexp"
	"strings"
	"unicode/utf8"

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
)

// PreviewTurn is a leak-free conversation turn for the public workbench.
// Questions and Forms are omitted when empty so turns without structured
// prompts keep the same JSON (and sparse-poll hash) as before they existed.
type PreviewTurn struct {
	Role        string              `json:"role"`
	Text        string              `json:"text,omitempty"`
	At          string              `json:"at,omitempty"`
	Interrupted bool                `json:"interrupted,omitempty"`
	Images      []PreviewImage      `json:"images,omitempty"`
	Annotations []PreviewAnnotation `json:"annotations,omitempty"`
	Questions   []PreviewQuestion   `json:"questions,omitempty"`
	Forms       []PreviewForm       `json:"forms,omitempty"`
	// Live points a human turn at its Live variant session (sid/op only).
	Live *models.LiveRef `json:"live,omitempty"`
}

// PreviewQuestion is a leak-free ask_question card (id, prompt, options).
type PreviewQuestion struct {
	ID            string          `json:"id"`
	Prompt        string          `json:"prompt"`
	Options       []PreviewOption `json:"options"`
	AllowMultiple bool            `json:"allowMultiple,omitempty"`
}

// PreviewOption is one choice. DemoHtml follows the public visual-page rules.
type PreviewOption struct {
	ID          string `json:"id"`
	Label       string `json:"label"`
	Recommended bool   `json:"recommended,omitempty"`
	DemoHtml    string `json:"demoHtml,omitempty"`
}

// PreviewForm is a leak-free ask_form card.
type PreviewForm struct {
	Title  string             `json:"title,omitempty"`
	Fields []PreviewFormField `json:"fields"`
}

// PreviewFormField is one plaintext input. Type stays text|url.
type PreviewFormField struct {
	Name        string `json:"name"`
	Label       string `json:"label"`
	Type        string `json:"type,omitempty"`
	Placeholder string `json:"placeholder,omitempty"`
	Value       string `json:"value,omitempty"`
	Required    bool   `json:"required,omitempty"`
	Why         string `json:"why,omitempty"`
}

// PreviewImage is a leak-free image hint: mime, optional name, and an opaque
// dialogue-scoped index used by the public token image route. No blob refs,
// /api/blobs paths, internal hosts, or raw base64.
type PreviewImage struct {
	MimeType string `json:"mimeType,omitempty"`
	Name     string `json:"name,omitempty"`
	Index    int    `json:"index"`
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
// Annotations and opaque image indexes are kept so public refresh / WS
// reconcile can show 批注 badges, refill chips, and redraw attachments.
type PreviewQueueItem struct {
	ID          string              `json:"id,omitempty"`
	Text        string              `json:"text,omitempty"`
	Images      []PreviewImage      `json:"images,omitempty"`
	Annotations []PreviewAnnotation `json:"annotations,omitempty"`
}

// PreviewActiveItem is a leak-free in-flight turn hint (opaque image indexes only).
type PreviewActiveItem struct {
	ID          string              `json:"id,omitempty"`
	Text        string              `json:"text,omitempty"`
	Images      []PreviewImage      `json:"images,omitempty"`
	Annotations []PreviewAnnotation `json:"annotations,omitempty"`
}

var (
	leakyURLRe     = regexp.MustCompile(`(?i)(?:blob:[^\s"'<>]*|/api/[^\s"'<>]*|/preview/[^\s"'<>]*|/sandbox[^\s"'<>]*|/v1/[^\s"'<>]*|https?://(?:localhost|127\.0\.0\.1|0\.0\.0\.0|10\.\d{1,3}\.\d{1,3}\.\d{1,3}|192\.168\.\d{1,3}\.\d{1,3}|172\.(?:1[6-9]|2\d|3[0-1])\.\d{1,3}\.\d{1,3})(?::\d+)?[^\s"'<>]*)`)
	internalHostRe = regexp.MustCompile(`(?i)\b(?:localhost|127\.0\.0\.1|0\.0\.0\.0)\b`)
	// run IDs are "run-" plus the first 8 hex chars of a UUID (engine.StartRun).
	runIDRe = regexp.MustCompile(`(?i)\brun-[0-9a-f]{8}\b`)
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

// sanitizeQuestionText redacts intranet addresses, blobs, and run IDs from
// choice/form copy. It does not change SanitizeDescription, so ordinary turn
// text keeps the previous rules.
func sanitizeQuestionText(s string) string {
	s = SanitizeDescription(s)
	if s == "" {
		return ""
	}
	return runIDRe.ReplaceAllString(s, "[redacted]")
}

// sanitizeDemoHTML applies the public visual-page size cap and address rules,
// then strips run IDs. External stylesheet links stay.
func sanitizeDemoHTML(html string) string {
	html = SanitizeVisualHTML(html)
	if html == "" {
		return ""
	}
	return runIDRe.ReplaceAllString(html, "[redacted]")
}

func sanitizeQuestions(qs []models.ReactQuestion) []PreviewQuestion {
	if len(qs) == 0 {
		return nil
	}
	out := make([]PreviewQuestion, 0, len(qs))
	for _, q := range qs {
		opts := make([]PreviewOption, 0, len(q.Options))
		for _, o := range q.Options {
			label := sanitizeQuestionText(o.Label)
			demo := sanitizeDemoHTML(o.DemoHtml)
			id := strings.TrimSpace(o.ID)
			if id == "" && label == "" && demo == "" {
				continue
			}
			opt := PreviewOption{ID: id, Label: label, Recommended: o.Recommended}
			if demo != "" {
				opt.DemoHtml = demo
			}
			opts = append(opts, opt)
		}
		prompt := sanitizeQuestionText(q.Prompt)
		id := strings.TrimSpace(q.ID)
		if id == "" && prompt == "" && len(opts) == 0 {
			continue
		}
		if len(opts) == 0 {
			opts = []PreviewOption{}
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

func sanitizeFormFieldType(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "url":
		return "url"
	case "text":
		return "text"
	default:
		return ""
	}
}

func sanitizeForms(forms []models.ReactForm) []PreviewForm {
	if len(forms) == 0 {
		return nil
	}
	out := make([]PreviewForm, 0, len(forms))
	for _, f := range forms {
		fields := make([]PreviewFormField, 0, len(f.Fields))
		for _, field := range f.Fields {
			name := sanitizeQuestionText(field.Name)
			label := sanitizeQuestionText(field.Label)
			placeholder := sanitizeQuestionText(field.Placeholder)
			value := sanitizeQuestionText(field.Value)
			why := sanitizeQuestionText(field.Why)
			if name == "" && label == "" && placeholder == "" && value == "" && why == "" {
				continue
			}
			pf := PreviewFormField{
				Name:        name,
				Label:       label,
				Type:        sanitizeFormFieldType(field.Type),
				Placeholder: placeholder,
				Value:       value,
				Required:    field.Required,
				Why:         why,
			}
			fields = append(fields, pf)
		}
		title := sanitizeQuestionText(f.Title)
		if title == "" && len(fields) == 0 {
			continue
		}
		if len(fields) == 0 {
			fields = []PreviewFormField{}
		}
		out = append(out, PreviewForm{Title: title, Fields: fields})
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// SanitizeTurns redacts conversation history for the public ReAct sidebar.
// Image bytes / blob URLs are replaced with opaque dialogue-scoped indexes;
// text and annotation chips are size-capped. Image-only human turns are kept.
func SanitizeTurns(msgs []models.ReactMessage) []PreviewTurn {
	turns, _ := SanitizeTurnsFrom(msgs, 0)
	return turns
}

// SanitizeTurnsFrom is SanitizeTurns with a starting opaque image index.
func SanitizeTurnsFrom(msgs []models.ReactMessage, imageBase int) ([]PreviewTurn, int) {
	if len(msgs) == 0 {
		return nil, imageBase
	}
	if len(msgs) > maxTurns {
		msgs = msgs[len(msgs)-maxTurns:]
	}
	idx := imageBase
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
		if imgs := sanitizePromptImages(m.Images, &idx); len(imgs) > 0 {
			turn.Images = imgs
		}
		if qs := sanitizeQuestions(m.Questions); len(qs) > 0 {
			turn.Questions = qs
		}
		if forms := sanitizeForms(m.Forms); len(forms) > 0 {
			turn.Forms = forms
		}
		if m.Live != nil && models.ValidLiveSID(m.Live.SID) {
			turn.Live = &models.LiveRef{SID: m.Live.SID, Op: m.Live.Op, Variant: m.Live.Variant}
		}
		// Keep a text-less turn when it still carries a choice card or form.
		if turn.Text == "" && len(turn.Annotations) == 0 && len(turn.Images) == 0 && !turn.Interrupted && len(turn.Questions) == 0 && len(turn.Forms) == 0 {
			continue
		}
		out = append(out, turn)
	}
	if len(out) == 0 {
		return nil, idx
	}
	return out, idx
}

// SanitizeQueueItems redacts pending FIFO rows for the public ReAct sidebar.
// Carries sanitized annotations and opaque image indexes (aligned with ActiveItem).
func SanitizeQueueItems(items []map[string]any) []PreviewQueueItem {
	out, _ := SanitizeQueueItemsFrom(items, 0)
	return out
}

// SanitizeQueueItemsFrom is SanitizeQueueItems with a starting opaque image index.
func SanitizeQueueItemsFrom(items []map[string]any, imageBase int) ([]PreviewQueueItem, int) {
	if len(items) == 0 {
		return nil, imageBase
	}
	idx := imageBase
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
		imgs := sanitizePromptImages(imagesFromAny(it["images"]), &idx)
		if id == "" && text == "" && len(anns) == 0 && len(imgs) == 0 {
			continue
		}
		item := PreviewQueueItem{ID: id, Text: text}
		if len(anns) > 0 {
			item.Annotations = anns
		}
		if len(imgs) > 0 {
			item.Images = imgs
		}
		out = append(out, item)
	}
	if len(out) == 0 {
		return nil, idx
	}
	return out, idx
}

// SanitizeActiveItem redacts the in-flight turn for polling resume.
func SanitizeActiveItem(m map[string]any) *PreviewActiveItem {
	item, _ := SanitizeActiveItemFrom(m, 0)
	return item
}

// SanitizeActiveItemFrom is SanitizeActiveItem with a starting opaque image index.
func SanitizeActiveItemFrom(m map[string]any, imageBase int) (*PreviewActiveItem, int) {
	if m == nil {
		return nil, imageBase
	}
	idx := imageBase
	id, _ := m["id"].(string)
	text, _ := m["text"].(string)
	item := &PreviewActiveItem{
		ID:          strings.TrimSpace(id),
		Text:        capTurnText(SanitizeDescription(text)),
		Annotations: annotationsFromAny(m["annotations"]),
		Images:      sanitizePromptImages(imagesFromAny(m["images"]), &idx),
	}
	if item.ID == "" && item.Text == "" && len(item.Annotations) == 0 && len(item.Images) == 0 {
		return nil, idx
	}
	return item, idx
}

// DialogueImageCatalog flattens conversation + in-flight session images in the
// same order used when assigning opaque preview indexes (turns → active → queue).
func DialogueImageCatalog(turns []models.ReactMessage, active map[string]any, queue []map[string]any) []models.PromptImage {
	if len(turns) > maxTurns {
		turns = turns[len(turns)-maxTurns:]
	}
	var out []models.PromptImage
	for _, m := range turns {
		role := strings.ToLower(strings.TrimSpace(m.Role))
		if role != "agent" && role != "human" {
			continue
		}
		if len(m.Images) > 0 {
			out = append(out, m.Images...)
		}
	}
	if active != nil {
		out = append(out, imagesFromAny(active["images"])...)
	}
	for _, it := range queue {
		if it == nil {
			continue
		}
		out = append(out, imagesFromAny(it["images"])...)
	}
	return out
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

// imagesFromAny parses PromptImage slices from JSON-decoded maps or typed slices.
func imagesFromAny(v any) []models.PromptImage {
	switch imgs := v.(type) {
	case []models.PromptImage:
		if len(imgs) == 0 {
			return nil
		}
		out := make([]models.PromptImage, len(imgs))
		copy(out, imgs)
		return out
	case []any:
		out := make([]models.PromptImage, 0, len(imgs))
		for _, raw := range imgs {
			am, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			pi := models.PromptImage{
				MimeType: stringMapField(am, "mimeType"),
				Name:     stringMapField(am, "name"),
				Ref:      stringMapField(am, "ref"),
				Data:     stringMapField(am, "data"),
			}
			if n, ok := am["sizeBytes"].(float64); ok && n > 0 {
				pi.SizeBytes = int64(n)
			}
			// Keep a slot for any image-shaped map (engine uses ref; fixtures may
			// only set url) so opaque indexes stay aligned with the catalog.
			if pi.MimeType == "" && pi.Ref == "" && pi.Data == "" &&
				stringMapField(am, "url") == "" && pi.Name == "" {
				continue
			}
			if pi.MimeType == "" {
				pi.MimeType = "image/png"
			}
			out = append(out, pi)
		}
		if len(out) == 0 {
			return nil
		}
		return out
	default:
		return nil
	}
}

func sanitizePromptImages(imgs []models.PromptImage, idx *int) []PreviewImage {
	if len(imgs) == 0 || idx == nil {
		return nil
	}
	out := make([]PreviewImage, 0, len(imgs))
	for _, im := range imgs {
		mime := strings.TrimSpace(im.MimeType)
		if mime == "" {
			mime = "image/png"
		}
		// Skip clearly non-image attachments from the public ReAct strip.
		if !strings.HasPrefix(strings.ToLower(mime), "image/") {
			*idx++
			continue
		}
		name := safeImageName(im.Name)
		out = append(out, PreviewImage{MimeType: mime, Name: name, Index: *idx})
		*idx++
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func safeImageName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	name = safeArtifactName(name)
	name = SanitizeDescription(name)
	if utf8.RuneCountInString(name) > maxAnnotationRunes {
		r := []rune(name)
		name = string(r[:maxAnnotationRunes]) + "…"
	}
	if name == "" || leakyURLRe.MatchString(name) || internalHostRe.MatchString(name) {
		return ""
	}
	return name
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
