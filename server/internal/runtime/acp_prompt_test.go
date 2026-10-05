package runtime

import (
	"strings"
	"testing"

	"github.com/cocofhu/grasp/internal/mcp"
	"github.com/cocofhu/grasp/internal/models"
)

func TestBuildAgentPromptAuto(t *testing.T) {
	p := newACPProvider(mcp.NewHost(newMemStore()), Options{}).(*acpProvider)
	got := p.buildAgentPrompt(NodeReq{
		NodeType: "agent", Caps: testImplementCaps,
		Config: map[string]any{"prompt": "USER_GOAL"},
		Vars:   map[string]any{"repos": `[{"name":"web","url":"https://h/web.git"},{"name":"api","url":"https://h/api.git"}]`},
	}, []string{"plan.json"})
	for _, want := range []string{
		"USER_GOAL",
		"`plan.json`",
		models.SchemaContract(models.SchemaImplementationResult),
		models.PlanProgressContract,
		models.PreviewContract,
		models.OutcomeContract,
		"/root/workspace/web/",
		"/root/workspace/api/",
	} {
		if !strings.Contains(got, strings.TrimSpace(want)) {
			t.Errorf("auto prompt missing %q", want)
		}
	}
	for _, unwanted := range []string{models.ClarifyContract, "以下是本次运行输入", models.PreviewPageControlContract} {
		if strings.Contains(got, strings.TrimSpace(unwanted)) {
			t.Errorf("auto prompt must not carry %q", unwanted)
		}
	}
}

func TestBuildAgentPromptClarify(t *testing.T) {
	p := newACPProvider(mcp.NewHost(newMemStore()), Options{}).(*acpProvider)
	got := p.buildAgentPrompt(NodeReq{
		NodeType: "agent", Caps: testClarifyCaps,
		Config: map[string]any{"prompt": "CLARIFY_GOAL"},
		Vars:   map[string]any{"feature": "邮箱验证码登录", "empty": ""},
	}, nil)
	for _, want := range []string{
		"CLARIFY_GOAL",
		"以下是本次运行输入",
		"邮箱验证码登录",
		models.SchemaContract(models.SchemaClarifiedRequirement),
		models.SchemaContract(models.SchemaPlan),
		models.ClarifyContract,
		models.PreviewContract,
		models.PreviewPageControlContract,
		models.PreviewLiveIndex,
	} {
		if !strings.Contains(got, strings.TrimSpace(want)) {
			t.Errorf("clarify prompt missing %q", want)
		}
	}
	if strings.Contains(got, strings.TrimSpace(models.OutcomeContract)) {
		t.Error("clarify opening prompt must not carry the outcome contract")
	}
}

func TestCapabilityContractsEmpty(t *testing.T) {
	if got := capabilityContracts(nil); got != "" {
		t.Errorf("nil caps contracts = %q", got)
	}
	if got := capabilityContracts(testPlainCaps); got != "" {
		t.Errorf("plain caps contracts = %q", got)
	}
}

func TestClarifyPreviewExtras(t *testing.T) {
	if got := clarifyPreviewExtras(NodeReq{Caps: testImplementCaps}); got != "" {
		t.Errorf("auto Agent must not get clarify preview extras: %q", got)
	}
	noPreview := &models.AgentCapabilities{Interaction: models.InteractionClarify}
	if got := clarifyPreviewExtras(NodeReq{Caps: noPreview}); got != "" {
		t.Errorf("clarify without set_preview must not get extras: %q", got)
	}
	got := clarifyPreviewExtras(NodeReq{Caps: testClarifyCaps})
	if !strings.Contains(got, "page_state") || !strings.Contains(got, "set_preview") {
		t.Errorf("clarify preview extras incomplete: %q", got)
	}
}

func TestReviewCapabilityExtras(t *testing.T) {
	if got := reviewCapabilityExtras(NodeReq{Caps: testPlainCaps}); got != "" {
		t.Errorf("review off must yield no extras: %q", got)
	}
	design := reviewCapabilityExtras(NodeReq{Caps: testTestReviewCaps})
	if !strings.HasPrefix(design, models.ReviewCapabilityDesignContract) {
		t.Errorf("non-committing Agent should get the design note: %q", design)
	}
	if !strings.Contains(design, "page_state") {
		t.Errorf("preview-capable Agent should get page control: %q", design)
	}
	commits := &models.AgentCapabilities{Interaction: models.InteractionAuto, Review: true,
		Writes: []models.ProductWrite{{Schema: models.SchemaImplementationResult, Required: true}}}
	dev := reviewCapabilityExtras(NodeReq{Caps: commits})
	if !strings.HasPrefix(dev, models.ReviewCapabilityDevContract) || strings.Contains(dev, "page_state") {
		t.Errorf("committing Agent without preview: %q", dev)
	}
}
