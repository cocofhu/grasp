package services_test

import (
	"strings"
	"testing"

	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/services"
)

func TestSanitizeOnboardingPrefix(t *testing.T) {
	got, err := services.SanitizeOnboardingPrefix(" 中国 象棋 ")
	if err != nil {
		t.Fatal(err)
	}
	if got != "中国象棋" {
		t.Fatalf("got %q", got)
	}
	_, err = services.SanitizeOnboardingPrefix("   ")
	if err == nil {
		t.Fatal("expected empty name error")
	}
	_, err = services.SanitizeOnboardingPrefix("!!!")
	if err == nil {
		t.Fatal("expected invalid name error")
	}
}

func TestBuildOnboardingNamePlan_defaultAndDerived(t *testing.T) {
	def, err := services.BuildOnboardingNamePlan(models.DefaultProjectID, "默认项目", models.DefaultProjectID)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(def.AgentNames, ",") != "需求澄清,实现,测试评审,交付" {
		t.Fatalf("default agents = %v", def.AgentNames)
	}

	plan, err := services.BuildOnboardingNamePlan("proj-abc", "Acme Corp", models.DefaultProjectID)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Prefix != "AcmeCorp" {
		t.Fatalf("prefix = %q", plan.Prefix)
	}
	if plan.NameMap["实现"] != "AcmeCorp实现" || plan.NameMap["交付"] != "AcmeCorp交付" {
		t.Fatalf("map = %#v", plan.NameMap)
	}
}

func TestRemapOnboardingAgentProfiles(t *testing.T) {
	g := &models.Graph{
		Nodes: []models.Node{
			{ID: "n1", Type: "agent", Config: map[string]any{"agent_profile": "实现"}},
			{ID: "n2", Type: "agent", Config: map[string]any{"agent_profile": "测试评审"}},
		},
	}
	services.RemapOnboardingAgentProfiles(g, map[string]string{
		"实现":   "中国象棋实现",
		"测试评审": "中国象棋测试评审",
	})
	if got, _ := g.Nodes[0].Config["agent_profile"].(string); got != "中国象棋实现" {
		t.Fatalf("n1 = %q", got)
	}
	if got, _ := g.Nodes[1].Config["agent_profile"].(string); got != "中国象棋测试评审" {
		t.Fatalf("n2 = %q", got)
	}
}
