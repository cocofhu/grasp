package mcp

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/cocofhu/grasp/internal/mcp/structured"
	gatenode "github.com/cocofhu/grasp/internal/models/nodereg"
)

// HumanArtifactNormalized is the result of validating human-edited content
// with the same Parse* path as set_* (without ActiveNodeType gating).
type HumanArtifactNormalized struct {
	Name     string
	Kind     string
	Content  string // normalized JSON or original text/html
	Rendered string // markdown (or raw html/text) for node outputs snapshot
	OutKey   string // e.g. research / page
	JSONKey  string // e.g. research_json; empty for page.html / freeform
}

// ValidateHumanArtifactContent parses and normalizes content for a named
// artifact. Structured reserved names use Parse*; markdown/html/text get
// basic non-empty checks. Returns a readable error when validation fails.
func ValidateHumanArtifactContent(name, content string) (HumanArtifactNormalized, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return HumanArtifactNormalized{}, errors.New("产物名不能为空")
	}
	if strings.ContainsAny(name, "/\\") || strings.Contains(name, "..") {
		return HumanArtifactNormalized{}, errors.New("产物名非法")
	}
	// The feedback ledger is an append-only record of what humans actually said.
	// Letting it be edited through the human-artifact path would make it
	// unciteable evidence, so it is platform-written only.
	if IsFeedbackArtifactName(name) {
		return HumanArtifactNormalized{}, fmt.Errorf("产物 %q 为人工反馈台账，由平台自动记录，不可编辑", name)
	}
	kind := gatenode.InferArtifactKind(name)
	out := HumanArtifactNormalized{Name: name, Kind: kind, Content: content, OutKey: gatenode.ArtifactToOutputKey[name]}

	if gatenode.IsReadonlyArtifactKind(kind) {
		return out, fmt.Errorf("产物 %q 为非文本主产物，只读不可保存", name)
	}

	switch name {
	case structured.ClarifiedRequirementArtifactName:
		doc, err := structured.ParseClarifiedRequirement(jsonToArgs(content))
		if err != nil {
			return out, err
		}
		b, err := json.MarshalIndent(doc, "", "  ")
		if err != nil {
			return out, err
		}
		out.Content = string(b)
		out.Rendered = structured.RenderClarifiedRequirementMarkdown(out.Content)
		out.JSONKey = "clarified_requirement_json"
		return out, nil
	case ResearchArtifactName:
		doc, err := structured.ParseResearch(jsonToArgs(content))
		if err != nil {
			return out, err
		}
		b, err := json.MarshalIndent(doc, "", "  ")
		if err != nil {
			return out, err
		}
		out.Content = string(b)
		out.Rendered = structured.RenderResearchMarkdown(out.Content)
		out.JSONKey = "research_json"
		return out, nil
	case ProposalsArtifactName:
		doc, err := structured.ParseProposals(jsonToArgs(content))
		if err != nil {
			return out, err
		}
		b, err := json.MarshalIndent(doc, "", "  ")
		if err != nil {
			return out, err
		}
		out.Content = string(b)
		out.Rendered = structured.RenderProposalsMarkdown(out.Content)
		out.JSONKey = "proposals_json"
		return out, nil
	case ProposalArtifactName:
		// Selected proposal is a single proposal object (+ status fields).
		// Minimal check: valid JSON object with a title or summary.
		var m map[string]any
		if err := json.Unmarshal([]byte(content), &m); err != nil {
			return out, fmt.Errorf("解析方案失败: %w", err)
		}
		title, _ := m["title"].(string)
		summary, _ := m["summary"].(string)
		if strings.TrimSpace(title) == "" && strings.TrimSpace(summary) == "" {
			return out, errors.New("proposal 需要 title 或 summary")
		}
		b, err := json.MarshalIndent(m, "", "  ")
		if err != nil {
			return out, err
		}
		out.Content = string(b)
		out.Rendered = RenderProposalMarkdown(out.Content)
		out.JSONKey = "proposal_json"
		out.OutKey = "proposal"
		return out, nil
	case TestResultArtifactName:
		doc, err := structured.ParseTestResult(jsonToArgs(content))
		if err != nil {
			return out, err
		}
		b, err := json.MarshalIndent(doc, "", "  ")
		if err != nil {
			return out, err
		}
		out.Content = string(b)
		out.Rendered = structured.RenderTestResultMarkdown(out.Content)
		out.JSONKey = "test_result_json"
		return out, nil
	case ReviewArtifactName:
		doc, err := structured.ParseReview(jsonToArgs(content))
		if err != nil {
			return out, err
		}
		b, err := json.MarshalIndent(doc, "", "  ")
		if err != nil {
			return out, err
		}
		out.Content = string(b)
		out.Rendered = structured.RenderReviewMarkdown(out.Content)
		out.JSONKey = "review_json"
		return out, nil
	case ImplementationResultArtifactName:
		doc, err := structured.ParseImplementationResult(jsonToArgs(content))
		if err != nil {
			return out, err
		}
		b, err := json.MarshalIndent(doc, "", "  ")
		if err != nil {
			return out, err
		}
		out.Content = string(b)
		out.Rendered = structured.RenderImplementationResultMarkdown(out.Content)
		out.JSONKey = "implementation_result_json"
		return out, nil
	case PreflightArtifactName:
		doc, err := structured.ParsePreflight(jsonToArgs(content))
		if err != nil {
			return out, err
		}
		b, err := json.MarshalIndent(doc, "", "  ")
		if err != nil {
			return out, err
		}
		out.Content = string(b)
		out.Rendered = structured.RenderPreflightMarkdown(out.Content)
		out.JSONKey = "preflight_json"
		return out, nil
	case PlanArtifactName:
		doc, err := parsePlan(jsonToArgs(content))
		if err != nil {
			return out, err
		}
		b, err := json.MarshalIndent(doc, "", "  ")
		if err != nil {
			return out, err
		}
		out.Content = string(b)
		out.Rendered = RenderPlanMarkdown(out.Content)
		out.JSONKey = "plan_json"
		out.OutKey = "plan"
		return out, nil
	case "page.html":
		if strings.TrimSpace(content) == "" {
			return out, errors.New("page.html 内容不能为空")
		}
		out.Content = content
		out.Rendered = content
		out.OutKey = "page"
		out.Kind = "html"
		return out, nil
	default:
		switch kind {
		case "json":
			var m any
			if err := json.Unmarshal([]byte(content), &m); err != nil {
				return out, fmt.Errorf("JSON 无效: %w", err)
			}
			b, err := json.MarshalIndent(m, "", "  ")
			if err != nil {
				return out, err
			}
			out.Content = string(b)
			out.Rendered = out.Content
			return out, nil
		case "html", "markdown", "text":
			if strings.TrimSpace(content) == "" {
				return out, errors.New("内容不能为空")
			}
			out.Content = content
			out.Rendered = content
			return out, nil
		default:
			if strings.TrimSpace(content) == "" {
				return out, errors.New("内容不能为空")
			}
			out.Content = content
			out.Rendered = content
			return out, nil
		}
	}
}

func jsonToArgs(content string) map[string]any {
	var m map[string]any
	if err := json.Unmarshal([]byte(content), &m); err != nil {
		// Parse* will surface a clearer error via decodeArgs when empty.
		return map[string]any{}
	}
	return m
}
