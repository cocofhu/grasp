package gateshare

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"time"

	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/nodereg"
)

const (
	ProductKindVisual     = "visual"
	ProductKindStructured = "structured"
	ProductKindAppPreview = "app_preview"

	// HeaderKnown* let silent pollers ask the server to omit unchanged large
	// fields (field-level sparse update).
	HeaderKnownVisualHTMLHash = "X-Gate-Known-Visual-Html-Hash"
	HeaderKnownUpstreamHash   = "X-Gate-Known-Upstream-Hash"
	HeaderKnownStructuredHash = "X-Gate-Known-Structured-Hash"
	HeaderKnownTurnsHash      = "X-Gate-Known-Turns-Hash"
	// HeaderSilentPoll marks a background poll. HeaderIssueNonce requests a
	// fresh one-time nonce (first load, foreground resume, or near TTL).
	HeaderSilentPoll = "X-Gate-Silent-Poll"
	HeaderIssueNonce = "X-Gate-Issue-Nonce"
)

// PreviewDTO is the leak-free public GET payload for the workbench-style page.
type PreviewDTO struct {
	Status            string             `json:"status"`
	Kind              string             `json:"kind,omitempty"`
	Title             string             `json:"title,omitempty"`
	Description       string             `json:"description,omitempty"`
	RemainingSec      *int64             `json:"remainingSec,omitempty"`
	ExpiresAt         *time.Time         `json:"expiresAt,omitempty"`
	PermissionPreset  string             `json:"permissionPreset,omitempty"`
	Actions           map[string]string  `json:"actions,omitempty"`
	VisualHTML        string             `json:"visualHtml,omitempty"`
	VisualHTMLHash    string             `json:"visualHtmlHash,omitempty"`
	Structured        map[string]any     `json:"structured,omitempty"`
	StructuredHash    string             `json:"structuredHash,omitempty"`
	Nonce             string             `json:"nonce,omitempty"`
	Turns             []PreviewTurn      `json:"turns,omitempty"`
	TurnsHash         string             `json:"turnsHash,omitempty"`
	Upstream          map[string]any     `json:"upstream,omitempty"`
	UpstreamHash      string             `json:"upstreamHash,omitempty"`
	ReactSessionAlive *bool              `json:"reactSessionAlive,omitempty"`
	SessionBusy       bool               `json:"sessionBusy,omitempty"`
	Waiting           int                `json:"waiting,omitempty"`
	QueueItems        []PreviewQueueItem `json:"queueItems,omitempty"`
	ActiveItem        *PreviewActiveItem `json:"activeItem,omitempty"`
	ProductKind       string             `json:"productKind,omitempty"`
	ProductName       string             `json:"productName,omitempty"`
	// Ports is the desensitized public app_preview port list (no runId/nodeId/paths).
	Ports []PublicPreviewPort `json:"ports,omitempty"`
	// NodeType is the graph node type (e.g. react / research / app_preview).
	// Kind stays "review" for ShareLinkKindReview; clients use NodeType to
	// distinguish Inbox 待澄清 from 待复审 without leaking Run#.
	NodeType string `json:"nodeType,omitempty"`
	// LiveEvents is a leak-free in-flight ACP snapshot (message/thought only)
	// while sessionBusy. Poll fallback when the public events WS is down.
	LiveEvents []PreviewLiveEvent `json:"liveEvents,omitempty"`
}

// PreviewLiveEvent is a leak-free ACP rail for public streaming / poll seed.
type PreviewLiveEvent struct {
	Kind string `json:"kind"`
	Text string `json:"text,omitempty"`
}

// PublicPreviewPort is the leak-free port entry for public app_preview remote / API iframe.
type PublicPreviewPort struct {
	Port  int    `json:"port"`
	Label string `json:"label,omitempty"`
	// Kind is "port" or "url".
	Kind string `json:"kind,omitempty"`
	// URL is the external iframe target when kind=url.
	URL string `json:"url,omitempty"`
	// Mode is "vnc" (remote + pick) or "api" (same-origin iframe, no pick).
	Mode string `json:"mode"`
	// DirectURL is set when the node uses IP-direct preview (browser iframe target).
	DirectURL string `json:"directUrl,omitempty"`
}

// PreviewExtras carries workbench fields that are optional on inactive links.
type PreviewExtras struct {
	Turns             []models.ReactMessage
	UpstreamName      string
	UpstreamContent   string
	ReactSessionAlive bool
	SessionBusy       bool
	Waiting           int
	QueueItems        []map[string]any
	ActiveItem        map[string]any
	ProductKind       string
	ProductName       string
	Ports             []PublicPreviewPort
	LiveEvents        []models.AcpEvent
}

// BuildPreviewDTO builds a whitelist human_gate preview from lookup + artifacts.
func BuildPreviewDTO(st string, lookup *LookupResult, visualHTML, structuredName, structuredContent, nonce string, extras PreviewExtras) PreviewDTO {
	dto := PreviewDTO{Status: st, Kind: models.ShareLinkKindHumanGate}
	if lookup == nil || (st != models.ShareLinkStateActive && st != "") {
		if st == "" {
			dto.Status = models.ShareLinkStateNone
		}
		return dto
	}
	if st != models.ShareLinkStateActive {
		return dto
	}
	dto.Title = strings.TrimSpace(lookup.Gate.Title)
	dto.Description = SanitizeDescription(lookup.Gate.BodyMd)
	exp := lookup.Link.ExpiresAt
	dto.ExpiresAt = &exp
	rem := int64(time.Until(lookup.Link.ExpiresAt).Seconds())
	if rem < 0 {
		rem = 0
	}
	dto.RemainingSec = &rem
	preset := NormalizePermissionPreset(lookup.Link.PermissionPreset)
	dto.PermissionPreset = preset
	dto.Actions = map[string]string{}
	if p := ResolvePassAction(lookup.Gate.Actions); p != "" {
		dto.Actions["approve"] = p
		dto.Actions["confirm"] = p
	}
	if f := ResolveFailAction(lookup.Gate.Actions); f != "" {
		dto.Actions["reject"] = f
	}
	if extras.ReactSessionAlive {
		dto.Actions["reply"] = "reply"
		dto.Actions["cancel"] = "cancel"
	}
	dto.Actions = FilterActionsByPreset(dto.Actions, preset)
	applyPreviewArtifacts(&dto, visualHTML, structuredName, structuredContent, extras)
	dto.Nonce = nonce
	return dto
}

// BuildReviewPreviewDTO builds a leak-free review preview (confirm + optional ReAct).
func BuildReviewPreviewDTO(st string, lookup *LookupResult, visualHTML, structuredName, structuredContent, nonce string, extras PreviewExtras) PreviewDTO {
	dto := PreviewDTO{Status: st, Kind: models.ShareLinkKindReview}
	if lookup == nil || st != models.ShareLinkStateActive {
		if st == "" {
			dto.Status = models.ShareLinkStateNone
		}
		return dto
	}
	if lookup.Node != nil {
		dto.NodeType = strings.TrimSpace(lookup.Node.Type)
	}
	title, desc := reviewPreviewCopy(lookup.Node)
	dto.Title = title
	dto.Description = SanitizeDescription(desc)
	exp := lookup.Link.ExpiresAt
	dto.ExpiresAt = &exp
	rem := int64(time.Until(lookup.Link.ExpiresAt).Seconds())
	if rem < 0 {
		rem = 0
	}
	dto.RemainingSec = &rem
	preset := NormalizePermissionPreset(lookup.Link.PermissionPreset)
	dto.PermissionPreset = preset
	dto.Actions = map[string]string{"confirm": "confirm"}
	if extras.ReactSessionAlive {
		dto.Actions["reply"] = "reply"
		dto.Actions["cancel"] = "cancel"
	}
	dto.Actions = FilterActionsByPreset(dto.Actions, preset)
	applyPreviewArtifacts(&dto, visualHTML, structuredName, structuredContent, extras)
	dto.Nonce = nonce
	return dto
}

func applyPreviewArtifacts(dto *PreviewDTO, visualHTML, structuredName, structuredContent string, extras PreviewExtras) {
	if html := SanitizeVisualHTML(visualHTML); html != "" {
		dto.VisualHTML = html
	}
	if s := SanitizeStructured(structuredName, structuredContent); s != nil {
		dto.Structured = s
	}
	kind, name := extras.ProductKind, strings.TrimSpace(extras.ProductName)
	if kind == "" {
		kind, name = inferProductKind(visualHTML, structuredName, extras.ProductKind)
	}
	if name == "" && dto.Structured != nil {
		if n, ok := dto.Structured["name"].(string); ok {
			name = n
		}
	}
	if name == "" && dto.VisualHTML != "" {
		name = "page.html"
	}
	dto.ProductKind = kind
	dto.ProductName = name
	if kind == ProductKindAppPreview && dto.ProductName == "" {
		dto.ProductName = "app_preview"
	}
	// Grasp nodes may register a live preview next to their structured product.
	if len(extras.Ports) > 0 {
		dto.Ports = append([]PublicPreviewPort(nil), extras.Ports...)
	}
	if turns := SanitizeTurns(extras.Turns); len(turns) > 0 {
		dto.Turns = turns
	}
	// Open/poll path: summary only (no doc). Full upstream is on-demand.
	if up := SanitizeUpstreamSummary(extras.UpstreamName, extras.UpstreamContent); up != nil {
		dto.Upstream = up
	}
	alive := extras.ReactSessionAlive
	dto.ReactSessionAlive = &alive
	if items := SanitizeQueueItems(extras.QueueItems); len(items) > 0 {
		dto.QueueItems = items
	}
	if ai := SanitizeActiveItem(extras.ActiveItem); ai != nil {
		dto.ActiveItem = ai
	}
	if extras.Waiting > 0 {
		dto.Waiting = extras.Waiting
	}
	dto.SessionBusy = alive && (extras.SessionBusy || extras.Waiting > 0 || dto.ActiveItem != nil)
	if dto.SessionBusy {
		if ev := SanitizeLiveEvents(extras.LiveEvents); len(ev) > 0 {
			dto.LiveEvents = ev
		}
	}
	dto.VisualHTMLHash = ContentHash(dto.VisualHTML)
	dto.UpstreamHash = HashUpstream(dto.Upstream)
	dto.StructuredHash = HashStructured(dto.Structured)
	dto.TurnsHash = HashTurns(dto.Turns)
}

// ContentHash returns a stable hex digest for sparse field comparison.
func ContentHash(s string) string {
	if s == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// HashUpstream digests the summary upstream map (no doc) for poll sparse updates.
func HashUpstream(up map[string]any) string {
	if up == nil {
		return ""
	}
	b, err := json.Marshal(up)
	if err != nil || len(b) == 0 {
		return ""
	}
	return ContentHash(string(b))
}

// HashStructured digests the sanitized structured map for sparse polls.
func HashStructured(s map[string]any) string {
	return HashUpstream(s)
}

// HashTurns digests sanitized turns for sparse polls.
func HashTurns(turns []PreviewTurn) string {
	if len(turns) == 0 {
		return ""
	}
	b, err := json.Marshal(turns)
	if err != nil || len(b) == 0 {
		return ""
	}
	return ContentHash(string(b))
}

// SparsePreviewKnown is the set of client-held hashes for ApplySparsePreview.
type SparsePreviewKnown struct {
	VisualHTML string
	Upstream   string
	Structured string
	Turns      string
}

// ApplySparsePreview omits unchanged large fields when the client already holds
// matching hashes. Hashes themselves are always returned so clients can detect
// empty↔non-empty transitions even when bodies omit.
func ApplySparsePreview(dto *PreviewDTO, known SparsePreviewKnown) {
	if dto == nil {
		return
	}
	if dto.VisualHTMLHash == "" {
		dto.VisualHTMLHash = ContentHash(dto.VisualHTML)
	}
	if dto.UpstreamHash == "" {
		dto.UpstreamHash = HashUpstream(dto.Upstream)
	}
	if dto.StructuredHash == "" {
		dto.StructuredHash = HashStructured(dto.Structured)
	}
	if dto.TurnsHash == "" {
		dto.TurnsHash = HashTurns(dto.Turns)
	}
	if known.VisualHTML != "" && known.VisualHTML == dto.VisualHTMLHash {
		dto.VisualHTML = ""
	}
	if known.Upstream != "" && known.Upstream == dto.UpstreamHash {
		dto.Upstream = nil
	}
	if known.Structured != "" && known.Structured == dto.StructuredHash {
		dto.Structured = nil
	}
	if known.Turns != "" && known.Turns == dto.TurnsHash {
		dto.Turns = nil
	}
}

// WantPreviewNonce reports whether this public preview request should Issue a nonce.
// Silent polls skip Issue unless the client explicitly asks (resume / near TTL).
func WantPreviewNonce(silentPoll, issueNonce bool) bool {
	if issueNonce {
		return true
	}
	return !silentPoll
}

func inferProductKind(visualHTML, structuredName, hinted string) (kind, name string) {
	if hinted == ProductKindAppPreview {
		return ProductKindAppPreview, "app_preview"
	}
	if strings.TrimSpace(visualHTML) != "" {
		return ProductKindVisual, "page.html"
	}
	if n := strings.TrimSpace(structuredName); n != "" {
		return ProductKindStructured, safeArtifactName(n)
	}
	return hinted, ""
}

func reviewPreviewCopy(node *models.Node) (title, description string) {
	if node != nil && nodereg.ClarifyInteractive(node.Type) {
		title = strings.TrimSpace(node.Label)
		if title == "" {
			if spec, ok := nodereg.Get(node.Type); ok {
				title = strings.TrimSpace(spec.Label)
			}
		}
		if title == "" {
			title = "待澄清"
		}
		return title, "外部澄清。请回答问题，信息足够后确认并流转。"
	}
	typeLabel := ""
	if node != nil {
		if spec, ok := nodereg.Get(node.Type); ok {
			typeLabel = strings.TrimSpace(spec.Label)
		}
		title = strings.TrimSpace(node.Label)
		if title == "" {
			title = typeLabel
		}
		if title == "" {
			title = "待复审"
		}
	} else {
		title = "待复审"
	}
	if typeLabel != "" {
		description = typeLabel + " · 待复审。请审阅脱敏产物后确认并流转。"
	} else {
		description = "待复审。请审阅脱敏产物后确认并流转。"
	}
	return title, description
}
