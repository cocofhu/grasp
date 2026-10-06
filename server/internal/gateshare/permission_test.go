package gateshare

import (
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/models"
)

func TestParsePermissionPreset(t *testing.T) {
	if _, ok := ParsePermissionPreset(""); ok {
		t.Fatal("empty preset must be rejected")
	}
	p, ok := ParsePermissionPreset("react_only")
	if !ok || p != models.SharePermissionReactOnly {
		t.Fatalf("react_only: %q ok=%v", p, ok)
	}
	if _, ok := ParsePermissionPreset("comment_only"); ok {
		t.Fatal("unknown create preset should fail")
	}
}

func TestAllowAndFilterActions(t *testing.T) {
	if !Allow(models.SharePermissionFull, ActionDecide) {
		t.Fatal("full should allow decide")
	}
	if Allow(models.SharePermissionReactOnly, ActionDecide) {
		t.Fatal("react_only must deny decide")
	}
	if !Allow(models.SharePermissionReactOnly, ActionReply) || !Allow(models.SharePermissionReactOnly, ActionCancel) {
		t.Fatal("react_only should allow reply/cancel")
	}
	if Allow(models.SharePermissionReactOnly, ActionLive) {
		t.Fatal("react_only must deny live writes")
	}
	if !Allow(models.SharePermissionFull, ActionLive) {
		t.Fatal("full should allow live")
	}
	actions := map[string]string{
		"approve": "approve",
		"confirm": "approve",
		"reject":  "revise",
		"reply":   "reply",
		"cancel":  "cancel",
	}
	filtered := FilterActionsByPreset(actions, models.SharePermissionReactOnly)
	if filtered["approve"] != "" || filtered["confirm"] != "" || filtered["reject"] != "" {
		t.Fatalf("decide keys remain: %+v", filtered)
	}
	if filtered["reply"] != "reply" || filtered["cancel"] != "cancel" {
		t.Fatalf("react keys missing: %+v", filtered)
	}
	full := FilterActionsByPreset(actions, models.SharePermissionFull)
	if full["confirm"] != "approve" {
		t.Fatalf("full preset should keep decide: %+v", full)
	}
}

func TestBuildPreviewDTOFiltersReactOnly(t *testing.T) {
	gateID := uint(9)
	lookup := &LookupResult{
		Link: models.GateShareLink{
			ID:               "gsl-1",
			PermissionPreset: models.SharePermissionReactOnly,
			ExpiresAt:        time.Now().Add(time.Hour),
			GateID:           &gateID,
		},
		Gate: models.Gate{
			Title: "审",
			Actions: []models.GateAction{
				{ID: "approve", Label: "批准"},
				{ID: "revise", Label: "驳回", RequireForm: true},
			},
		},
	}
	hot := BuildPreviewDTO(models.ShareLinkStateActive, lookup, "", "", "", "n1", PreviewExtras{ReactSessionAlive: true})
	if hot.PermissionPreset != models.SharePermissionReactOnly {
		t.Fatalf("preset=%q", hot.PermissionPreset)
	}
	if hot.Actions["reply"] != "reply" || hot.Actions["cancel"] != "cancel" {
		t.Fatalf("hot react_only actions: %+v", hot.Actions)
	}
	if hot.Actions["confirm"] != "" || hot.Actions["approve"] != "" || hot.Actions["reject"] != "" {
		t.Fatalf("hot react_only still has decide: %+v", hot.Actions)
	}
	cold := BuildPreviewDTO(models.ShareLinkStateActive, lookup, "", "", "", "n2", PreviewExtras{ReactSessionAlive: false})
	if len(cold.Actions) != 0 {
		t.Fatalf("cold react_only must have no actions: %+v", cold.Actions)
	}

}

func TestBuildReviewPreviewDTOFiltersReactOnly(t *testing.T) {
	lookup := &LookupResult{
		Link: models.GateShareLink{
			ID:               "gsl-r1",
			PermissionPreset: models.SharePermissionReactOnly,
			ExpiresAt:        time.Now().Add(time.Hour),
			Kind:             models.ShareLinkKindReview,
		},
		Kind: models.ShareLinkKindReview,
		Node: &models.Node{ID: "research1", Type: "agent", Label: "调研", Caps: &models.AgentCapabilities{Interaction: models.InteractionAuto, Review: true}},
	}
	dto := BuildReviewPreviewDTO(models.ShareLinkStateActive, lookup, "", "", "", "n", PreviewExtras{ReactSessionAlive: true})
	if dto.Actions["confirm"] != "" {
		t.Fatalf("review react_only still has confirm: %+v", dto.Actions)
	}
	if dto.Actions["reply"] != "reply" {
		t.Fatalf("review react_only missing reply: %+v", dto.Actions)
	}
}

func TestBuildReviewPreviewDTOKeepsGraspPreviewPorts(t *testing.T) {
	lookup := &LookupResult{
		Link: models.GateShareLink{ID: "gsl-g1", PermissionPreset: models.SharePermissionFull, ExpiresAt: time.Now().Add(time.Hour), Kind: models.ShareLinkKindReview},
		Kind: models.ShareLinkKindReview,
		Node: &models.Node{ID: "grasp1", Type: "agent", Caps: &models.AgentCapabilities{Interaction: models.InteractionClarify, Tools: []string{models.ToolAskQuestion}}},
	}
	ports := []PublicPreviewPort{{Port: 18080, Kind: "port", Mode: "direct", DirectURL: "http://10.0.0.5:18080/"}}
	dto := BuildReviewPreviewDTO(models.ShareLinkStateActive, lookup, "", "research.json", `{"title":"t"}`, "n", PreviewExtras{Ports: ports})
	if dto.ProductKind != ProductKindStructured || len(dto.Ports) != 1 || dto.Ports[0].DirectURL != "http://10.0.0.5:18080/" {
		t.Fatalf("kind=%q ports=%+v", dto.ProductKind, dto.Ports)
	}
}
