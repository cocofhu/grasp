package mcp

import (
	"strings"
	"testing"

	"github.com/cocofhu/grasp/internal/mcp/structured"
)

func TestValidateHumanArtifactContent_research(t *testing.T) {
	content := `{"summary":"s","findings":[{"title":"t","detail":"d"}]}`
	norm, err := ValidateHumanArtifactContent(ResearchArtifactName, content)
	if err != nil {
		t.Fatal(err)
	}
	if norm.Kind != "json" || norm.OutKey != "research" || norm.JSONKey != "research_json" {
		t.Fatalf("meta: %+v", norm)
	}
	if !strings.Contains(norm.Content, `"summary"`) {
		t.Fatalf("normalized: %s", norm.Content)
	}
}

func TestValidateHumanArtifactContent_schemaFail(t *testing.T) {
	_, err := ValidateHumanArtifactContent(structured.ClarifiedRequirementArtifactName, `{"title":"x"}`)
	if err == nil {
		t.Fatal("expected schema failure")
	}
}

func TestValidateHumanArtifactContent_pageAndMarkdown(t *testing.T) {
	if _, err := ValidateHumanArtifactContent("page.html", "  "); err == nil {
		t.Fatal("empty html should fail")
	}
	norm, err := ValidateHumanArtifactContent("page.html", "<html>hi</html>")
	if err != nil || norm.OutKey != "page" {
		t.Fatalf("page: %+v err=%v", norm, err)
	}
	norm, err = ValidateHumanArtifactContent("notes.md", "# hi")
	if err != nil || norm.Kind != "markdown" {
		t.Fatalf("md: %+v err=%v", norm, err)
	}
	if _, err := ValidateHumanArtifactContent("notes.md", "   "); err == nil {
		t.Fatal("empty md should fail")
	}
	if _, err := ValidateHumanArtifactContent("", "x"); err == nil {
		t.Fatal("empty name should fail")
	}
	if _, err := ValidateHumanArtifactContent("../evil.json", "{}"); err == nil {
		t.Fatal("path traversal name should fail")
	}
}

func TestValidateHumanArtifactContent_allStructured(t *testing.T) {
	cases := []struct {
		name    string
		content string
		outKey  string
	}{
		{
			ClarifiedRequirementArtifactName,
			MinimalValidClarifiedRequirementJSON,
			"clarified_requirement",
		},
		{
			ProposalsArtifactName,
			`{"context":"c","proposals":[{"title":"A","summary":"s"}]}`,
			"proposals",
		},
		{
			ProposalArtifactName,
			`{"title":"chosen","summary":"s","status":"accepted"}`,
			"proposal",
		},
		{
			TestResultArtifactName,
			`{"summary":"ok","cases":[{"name":"c1","status":"passed"}]}`,
			"test_result",
		},
		{
			ReviewArtifactName,
			`{"summary":"ok","verdict":"approve"}`,
			"review",
		},
		{
			ImplementationResultArtifactName,
			`{"summary":"done"}`,
			"implementation_result",
		},
		{
			PlanArtifactName,
			`{"title":"P","goals":[{"title":"G1","subgoals":[{"title":"S1"}]}]}`,
			"plan",
		},
		{
			PreflightArtifactName,
			`{"summary":"env ready","confirmed":true,"fields":[{"name":"db_host","value":"localhost"}]}`,
			"preflight",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			norm, err := ValidateHumanArtifactContent(tc.name, tc.content)
			if err != nil {
				t.Fatal(err)
			}
			if norm.OutKey != tc.outKey {
				t.Fatalf("outKey=%q want %q", norm.OutKey, tc.outKey)
			}
			if strings.TrimSpace(norm.Content) == "" || strings.TrimSpace(norm.Rendered) == "" {
				t.Fatalf("empty content/rendered: %+v", norm)
			}
		})
	}
}

func TestValidateHumanArtifactContent_freeformJSON(t *testing.T) {
	norm, err := ValidateHumanArtifactContent("custom.json", `{"a":1}`)
	if err != nil || !strings.Contains(norm.Content, `"a"`) {
		t.Fatalf("got %+v err=%v", norm, err)
	}
	if _, err := ValidateHumanArtifactContent("custom.json", `{`); err == nil {
		t.Fatal("invalid json should fail")
	}
	norm, err = ValidateHumanArtifactContent("plain.txt", "hello")
	if err != nil || norm.Kind != "text" {
		t.Fatalf("text: %+v err=%v", norm, err)
	}
}

func TestValidateHumanArtifactContent_proposalNeedsTitleOrSummary(t *testing.T) {
	if _, err := ValidateHumanArtifactContent(ProposalArtifactName, `{"id":"p1"}`); err == nil {
		t.Fatal("expected title/summary required")
	}
}

func TestValidateHumanArtifactContent_preflightMustBeConfirmed(t *testing.T) {
	if _, err := ValidateHumanArtifactContent(PreflightArtifactName, `{"summary":"s","confirmed":false}`); err == nil {
		t.Fatal("unconfirmed preflight should fail")
	}
}
