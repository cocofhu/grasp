package structured

// This file defines the structured framework-node products. Each is written by
// a dedicated set_* MCP tool (gated to its node type in httpmcp.go), read back
// by its get_* tool, enforced by the engine, and rendered to human-readable
// markdown for human_gate bodies / the UI. Field designs are grounded in
// software-engineering standards (see the plan): ISO/IEC/IEEE 29148 SRS
// (clarified_requirement), ADR/MADR + design-doc/RFC (proposals), technical
// spike (research), IEEE 829 Test Summary Report (test_result), code-review
// verdict practice (review), and PR-description conventions (implementation).

import (
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"sort"
	"strings"
)

// Reserved artifact names for the structured products.
const (
	ClarifiedRequirementArtifactName = "clarified_requirement.json"
	ResearchArtifactName             = "research.json"
	ProposalsArtifactName            = "proposals.json"
	ProposalArtifactName             = "proposal.json"
	TestResultArtifactName           = "test_result.json"
	ReviewArtifactName               = "review.json"
	ImplementationResultArtifactName = "implementation_result.json"
)

// flexStrings tolerates the loose shapes an LLM may emit for a string list:
// a bare string, an array of strings, or an array of objects carrying a
// text-like field. It always decodes to a []string.
type flexStrings []string

func (fs *flexStrings) UnmarshalJSON(b []byte) error {
	b = []byte(strings.TrimSpace(string(b)))
	if len(b) == 0 || string(b) == "null" {
		return nil
	}
	switch b[0] {
	case '"':
		var s string
		if err := json.Unmarshal(b, &s); err != nil {
			return err
		}
		if strings.TrimSpace(s) != "" {
			*fs = flexStrings{strings.TrimSpace(s)}
		}
		return nil
	case '[':
		var raw []json.RawMessage
		if err := json.Unmarshal(b, &raw); err != nil {
			return err
		}
		out := make([]string, 0, len(raw))
		for _, r := range raw {
			s := coerceString(r)
			if strings.TrimSpace(s) != "" {
				out = append(out, strings.TrimSpace(s))
			}
		}
		*fs = out
		return nil
	default:
		return fmt.Errorf("expected string or array")
	}
}

// coerceString turns a raw JSON element into a display string: a bare string is
// used verbatim; an object is reduced to its first text-like field.
func coerceString(r json.RawMessage) string {
	var s string
	if json.Unmarshal(r, &s) == nil {
		return s
	}
	var m map[string]any
	if json.Unmarshal(r, &m) == nil {
		for _, k := range []string{"text", "title", "label", "detail", "value", "name"} {
			if v, ok := m[k]; ok {
				return asString(v)
			}
		}
	}
	return strings.TrimSpace(string(r))
}

func asString(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprint(v)
}

func decodeArgs(args map[string]any, v any) error {
	raw, err := json.Marshal(args)
	if err != nil {
		return fmt.Errorf("encode args: %w", err)
	}
	return json.Unmarshal(raw, v)
}

// --- clarified_requirement (react) ----------------------------------------
//
// Clarified-requirement fields follow a tailored ISO/IEC/IEEE 29148 SRS +
// product-PRD subset for the clarify gate (no timeline, no tech design).

var (
	frPriorities  = map[string]bool{"must": true, "should": true, "could": true}
	nfrCategories = map[string]bool{
		"performance": true, "security": true, "usability": true,
		"reliability": true, "compatibility": true, "other": true,
	}
	ifaceKinds = map[string]bool{
		"user": true, "system": true, "hardware": true, "software": true, "communication": true,
	}
	ifaceDirections = map[string]bool{"in": true, "out": true, "both": true}
)

type funcReq struct {
	ID                 string      `json:"id"`
	Title              string      `json:"title"`
	Detail             string      `json:"detail,omitempty"`
	Priority           string      `json:"priority,omitempty"`
	AcceptanceCriteria flexStrings `json:"acceptance_criteria,omitempty"`
	ScenarioIDs        flexStrings `json:"scenario_ids,omitempty"`
}

type nonFuncReq struct {
	ID       string `json:"id"`
	Category string `json:"category,omitempty"`
	Detail   string `json:"detail"`
	Metric   string `json:"metric,omitempty"`
}

type persona struct {
	ID          string      `json:"id"`
	Name        string      `json:"name"`
	Description string      `json:"description,omitempty"`
	Goals       flexStrings `json:"goals,omitempty"`
}

type userScenario struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Actor   string `json:"actor,omitempty"`
	Trigger string `json:"trigger,omitempty"`
	Flow    string `json:"flow,omitempty"`
	Outcome string `json:"outcome,omitempty"`
}

type extInterface struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Kind        string `json:"kind,omitempty"`
	Direction   string `json:"direction,omitempty"`
	Description string `json:"description,omitempty"`
}

type dataEntity struct {
	ID          string      `json:"id"`
	Name        string      `json:"name"`
	Description string      `json:"description,omitempty"`
	Attributes  flexStrings `json:"attributes,omitempty"`
}

type reqRisk struct {
	ID          string `json:"id"`
	Description string `json:"description"`
	Mitigation  string `json:"mitigation,omitempty"`
}

type glossaryEntry struct {
	Term       string `json:"term"`
	Definition string `json:"definition"`
}

type clarifiedRequirementDoc struct {
	Title                     string          `json:"title,omitempty"`
	Summary                   string          `json:"summary"`
	Background                string          `json:"background,omitempty"`
	// WorkKind is bug|feature|other. Required for Grasp; optional for independent react.
	WorkKind                  string          `json:"work_kind,omitempty"`
	Goals                     flexStrings     `json:"goals,omitempty"`
	SuccessMetrics            flexStrings     `json:"success_metrics,omitempty"`
	InScope                   flexStrings     `json:"in_scope,omitempty"`
	OutOfScope                flexStrings     `json:"out_of_scope,omitempty"`
	Personas                  []persona       `json:"personas,omitempty"`
	UserScenarios             []userScenario  `json:"user_scenarios,omitempty"`
	FunctionalRequirements    []funcReq       `json:"functional_requirements"`
	NonFunctionalRequirements []nonFuncReq    `json:"non_functional_requirements,omitempty"`
	ExternalInterfaces        []extInterface  `json:"external_interfaces,omitempty"`
	DataEntities              []dataEntity    `json:"data_entities,omitempty"`
	BusinessRules             flexStrings     `json:"business_rules,omitempty"`
	EdgeCases                 flexStrings     `json:"edge_cases,omitempty"`
	Assumptions               flexStrings     `json:"assumptions,omitempty"`
	Dependencies              flexStrings     `json:"dependencies,omitempty"`
	Constraints               flexStrings     `json:"constraints,omitempty"`
	Limitations               flexStrings     `json:"limitations,omitempty"`
	Risks                     []reqRisk       `json:"risks,omitempty"`
	Glossary                  []glossaryEntry `json:"glossary,omitempty"`
	OpenQuestions             flexStrings     `json:"open_questions,omitempty"`
}

func requireNonEmptyList(name string, items flexStrings) (flexStrings, error) {
	out := make(flexStrings, 0, len(items))
	for _, s := range items {
		if t := strings.TrimSpace(s); t != "" {
			out = append(out, t)
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("%s 至少需要 1 条", name)
	}
	return out, nil
}

func trimStringList(items flexStrings) flexStrings {
	out := make(flexStrings, 0, len(items))
	for _, s := range items {
		if t := strings.TrimSpace(s); t != "" {
			out = append(out, t)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func ParseClarifiedRequirement(args map[string]any) (clarifiedRequirementDoc, error) {
	var doc clarifiedRequirementDoc
	if err := decodeArgs(args, &doc); err != nil {
		return doc, fmt.Errorf("解析需求失败: %w", err)
	}
	doc.Title = strings.TrimSpace(doc.Title)
	doc.Summary = strings.TrimSpace(doc.Summary)
	doc.Background = strings.TrimSpace(doc.Background)
	doc.WorkKind = NormalizeWorkKind(doc.WorkKind)
	if doc.Title == "" {
		return doc, errors.New("title 不能为空")
	}
	if doc.Summary == "" {
		return doc, errors.New("summary 不能为空")
	}
	if doc.Background == "" {
		return doc, errors.New("background 不能为空")
	}
	// work_kind is optional for independent react; when present must be valid.
	if doc.WorkKind != "" && !ValidWorkKind(doc.WorkKind) {
		return doc, errors.New("work_kind 须为 bug、feature 或 other")
	}

	var err error
	if doc.Goals, err = requireNonEmptyList("goals", doc.Goals); err != nil {
		return doc, err
	}
	if doc.InScope, err = requireNonEmptyList("in_scope", doc.InScope); err != nil {
		return doc, err
	}
	if doc.OutOfScope, err = requireNonEmptyList("out_of_scope", doc.OutOfScope); err != nil {
		return doc, err
	}
	if doc.Assumptions, err = requireNonEmptyList("assumptions", doc.Assumptions); err != nil {
		return doc, err
	}
	if doc.Dependencies, err = requireNonEmptyList("dependencies", doc.Dependencies); err != nil {
		return doc, err
	}
	if doc.Constraints, err = requireNonEmptyList("constraints", doc.Constraints); err != nil {
		return doc, err
	}
	doc.SuccessMetrics = trimStringList(doc.SuccessMetrics)
	doc.BusinessRules = trimStringList(doc.BusinessRules)
	doc.EdgeCases = trimStringList(doc.EdgeCases)
	doc.Limitations = trimStringList(doc.Limitations)
	doc.OpenQuestions = trimStringList(doc.OpenQuestions)

	personas := make([]persona, 0, len(doc.Personas))
	for _, p := range doc.Personas {
		p.Name = strings.TrimSpace(p.Name)
		if p.Name == "" {
			continue
		}
		p.ID = fmt.Sprintf("u%d", len(personas)+1)
		p.Description = strings.TrimSpace(p.Description)
		p.Goals = trimStringList(p.Goals)
		personas = append(personas, p)
	}
	doc.Personas = personas

	scenarios := make([]userScenario, 0, len(doc.UserScenarios))
	scenarioIDs := map[string]bool{}
	for _, s := range doc.UserScenarios {
		s.Name = strings.TrimSpace(s.Name)
		if s.Name == "" {
			continue
		}
		s.ID = fmt.Sprintf("s%d", len(scenarios)+1)
		s.Actor = strings.TrimSpace(s.Actor)
		s.Trigger = strings.TrimSpace(s.Trigger)
		s.Flow = strings.TrimSpace(s.Flow)
		s.Outcome = strings.TrimSpace(s.Outcome)
		scenarios = append(scenarios, s)
		scenarioIDs[s.ID] = true
	}
	doc.UserScenarios = scenarios

	fr := make([]funcReq, 0, len(doc.FunctionalRequirements))
	for _, f := range doc.FunctionalRequirements {
		f.Title = strings.TrimSpace(f.Title)
		f.Detail = strings.TrimSpace(f.Detail)
		if f.Title == "" {
			continue
		}
		if f.Detail == "" {
			return doc, fmt.Errorf("functional_requirements[%q] detail 不能为空", f.Title)
		}
		acs := trimStringList(f.AcceptanceCriteria)
		if len(acs) == 0 {
			return doc, fmt.Errorf("functional_requirements[%q] acceptance_criteria 至少需要 1 条", f.Title)
		}
		f.AcceptanceCriteria = acs
		pri := strings.ToLower(strings.TrimSpace(f.Priority))
		if pri == "" {
			pri = "must"
		}
		if !frPriorities[pri] {
			return doc, fmt.Errorf("functional_requirements[%q] priority 无效(须为 must|should|could)", f.Title)
		}
		f.Priority = pri
		refs := make(flexStrings, 0)
		for _, id := range f.ScenarioIDs {
			id = strings.TrimSpace(id)
			if scenarioIDs[id] {
				refs = append(refs, id)
			}
		}
		f.ScenarioIDs = refs
		f.ID = fmt.Sprintf("f%d", len(fr)+1)
		fr = append(fr, f)
	}
	if len(fr) == 0 {
		return doc, errors.New("functional_requirements 至少需要 1 条")
	}
	doc.FunctionalRequirements = fr

	nf := make([]nonFuncReq, 0, len(doc.NonFunctionalRequirements))
	for _, n := range doc.NonFunctionalRequirements {
		n.Detail = strings.TrimSpace(n.Detail)
		if n.Detail == "" {
			continue
		}
		cat := strings.ToLower(strings.TrimSpace(n.Category))
		if cat == "" {
			cat = "other"
		}
		if !nfrCategories[cat] {
			return doc, fmt.Errorf("non_functional_requirements category 无效(须为 performance|security|usability|reliability|compatibility|other)")
		}
		n.ID = fmt.Sprintf("n%d", len(nf)+1)
		n.Category = cat
		n.Metric = strings.TrimSpace(n.Metric)
		nf = append(nf, n)
	}
	doc.NonFunctionalRequirements = nf

	ifaces := make([]extInterface, 0, len(doc.ExternalInterfaces))
	for _, i := range doc.ExternalInterfaces {
		i.Name = strings.TrimSpace(i.Name)
		if i.Name == "" {
			continue
		}
		kind := strings.ToLower(strings.TrimSpace(i.Kind))
		if kind == "" {
			kind = "system"
		}
		if !ifaceKinds[kind] {
			return doc, fmt.Errorf("external_interfaces[%q] kind 无效(须为 user|system|hardware|software|communication)", i.Name)
		}
		dir := strings.ToLower(strings.TrimSpace(i.Direction))
		if dir == "" {
			dir = "both"
		}
		if !ifaceDirections[dir] {
			return doc, fmt.Errorf("external_interfaces[%q] direction 无效(须为 in|out|both)", i.Name)
		}
		i.ID = fmt.Sprintf("i%d", len(ifaces)+1)
		i.Kind = kind
		i.Direction = dir
		i.Description = strings.TrimSpace(i.Description)
		ifaces = append(ifaces, i)
	}
	doc.ExternalInterfaces = ifaces

	entities := make([]dataEntity, 0, len(doc.DataEntities))
	for _, d := range doc.DataEntities {
		d.Name = strings.TrimSpace(d.Name)
		if d.Name == "" {
			continue
		}
		d.ID = fmt.Sprintf("d%d", len(entities)+1)
		d.Description = strings.TrimSpace(d.Description)
		d.Attributes = trimStringList(d.Attributes)
		entities = append(entities, d)
	}
	doc.DataEntities = entities

	risks := make([]reqRisk, 0, len(doc.Risks))
	for _, r := range doc.Risks {
		r.Description = strings.TrimSpace(r.Description)
		if r.Description == "" {
			continue
		}
		r.ID = fmt.Sprintf("r%d", len(risks)+1)
		r.Mitigation = strings.TrimSpace(r.Mitigation)
		risks = append(risks, r)
	}
	doc.Risks = risks

	gloss := make([]glossaryEntry, 0, len(doc.Glossary))
	for _, g := range doc.Glossary {
		g.Term = strings.TrimSpace(g.Term)
		g.Definition = strings.TrimSpace(g.Definition)
		if g.Term == "" || g.Definition == "" {
			continue
		}
		gloss = append(gloss, g)
	}
	doc.Glossary = gloss

	return doc, nil
}

// ClarifiedOpenQuestions returns the trimmed, non-empty open_questions recorded
// in a stored clarified_requirement.json. It returns nil on parse error or when
// none remain. The react gate uses it to keep clarifying until the agent has
// resolved every question with the user (open_questions empty).
func ClarifiedOpenQuestions(content string) []string {
	var doc clarifiedRequirementDoc
	if json.Unmarshal([]byte(content), &doc) != nil {
		return nil
	}
	out := make([]string, 0, len(doc.OpenQuestions))
	for _, q := range doc.OpenQuestions {
		if s := strings.TrimSpace(q); s != "" {
			out = append(out, s)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// RenderClarifiedRequirementMarkdown renders clarified_requirement.json as a
// readable spec. Returns the raw content unchanged on parse error.
// Missing optional sections are skipped so older thin artifacts still render.
func RenderClarifiedRequirementMarkdown(content string) string {
	var doc clarifiedRequirementDoc
	if json.Unmarshal([]byte(content), &doc) != nil || doc.Summary == "" {
		return content
	}
	var b strings.Builder
	if doc.Title != "" {
		b.WriteString("### " + doc.Title + "\n\n")
	}
	b.WriteString("#### 概述\n")
	b.WriteString(doc.Summary + "\n")
	if wk := NormalizeWorkKind(doc.WorkKind); wk != "" {
		b.WriteString("\n#### 工作类型\n")
		b.WriteString(wk + "\n")
	}
	if doc.Background != "" {
		b.WriteString("\n#### 背景\n")
		b.WriteString(doc.Background + "\n")
	}
	writeBulletSection(&b, "目标", doc.Goals)
	writeBulletSection(&b, "成功指标", doc.SuccessMetrics)
	writeBulletSection(&b, "范围内", doc.InScope)
	writeBulletSection(&b, "不在范围内", doc.OutOfScope)

	if len(doc.Personas) > 0 {
		b.WriteString("\n#### 用户画像\n")
		for _, p := range doc.Personas {
			b.WriteString(fmt.Sprintf("- **`%s` %s**", p.ID, p.Name))
			if p.Description != "" {
				b.WriteString(" — " + p.Description)
			}
			b.WriteString("\n")
			for _, g := range p.Goals {
				b.WriteString("  - [目标] " + g + "\n")
			}
		}
	}
	if len(doc.UserScenarios) > 0 {
		b.WriteString("\n#### 用户场景\n")
		for _, s := range doc.UserScenarios {
			b.WriteString(fmt.Sprintf("- **`%s` %s**", s.ID, s.Name))
			parts := make([]string, 0, 4)
			if s.Actor != "" {
				parts = append(parts, "角色: "+s.Actor)
			}
			if s.Trigger != "" {
				parts = append(parts, "触发: "+s.Trigger)
			}
			if s.Flow != "" {
				parts = append(parts, "流程: "+s.Flow)
			}
			if s.Outcome != "" {
				parts = append(parts, "结果: "+s.Outcome)
			}
			if len(parts) > 0 {
				b.WriteString(" — " + strings.Join(parts, "; "))
			}
			b.WriteString("\n")
		}
	}

	if len(doc.FunctionalRequirements) > 0 {
		b.WriteString("\n#### 功能需求\n")
		for _, f := range doc.FunctionalRequirements {
			b.WriteString(fmt.Sprintf("- **`%s` %s**", f.ID, f.Title))
			if f.Priority != "" {
				b.WriteString(fmt.Sprintf(" _(%s)_", f.Priority))
			}
			if f.Detail != "" {
				b.WriteString(" — " + f.Detail)
			}
			b.WriteString("\n")
			if len(f.ScenarioIDs) > 0 {
				b.WriteString("  - [场景] " + strings.Join(f.ScenarioIDs, ", ") + "\n")
			}
			for _, ac := range f.AcceptanceCriteria {
				b.WriteString("  - [验收] " + ac + "\n")
			}
		}
	}
	if len(doc.NonFunctionalRequirements) > 0 {
		b.WriteString("\n#### 非功能需求\n")
		for _, n := range doc.NonFunctionalRequirements {
			if n.Category != "" {
				b.WriteString(fmt.Sprintf("- _(%s)_ %s", n.Category, n.Detail))
			} else {
				b.WriteString("- " + n.Detail)
			}
			if n.Metric != "" {
				b.WriteString(" 〔指标: " + n.Metric + "〕")
			}
			b.WriteString("\n")
		}
	}
	if len(doc.ExternalInterfaces) > 0 {
		b.WriteString("\n#### 外部接口\n")
		for _, i := range doc.ExternalInterfaces {
			b.WriteString(fmt.Sprintf("- **`%s` %s**", i.ID, i.Name))
			meta := make([]string, 0, 2)
			if i.Kind != "" {
				meta = append(meta, i.Kind)
			}
			if i.Direction != "" {
				meta = append(meta, i.Direction)
			}
			if len(meta) > 0 {
				b.WriteString(" _(" + strings.Join(meta, "/") + ")_")
			}
			if i.Description != "" {
				b.WriteString(" — " + i.Description)
			}
			b.WriteString("\n")
		}
	}
	if len(doc.DataEntities) > 0 {
		b.WriteString("\n#### 数据实体\n")
		for _, d := range doc.DataEntities {
			b.WriteString(fmt.Sprintf("- **`%s` %s**", d.ID, d.Name))
			if d.Description != "" {
				b.WriteString(" — " + d.Description)
			}
			b.WriteString("\n")
			for _, a := range d.Attributes {
				b.WriteString("  - " + a + "\n")
			}
		}
	}
	writeBulletSection(&b, "业务规则", doc.BusinessRules)
	writeBulletSection(&b, "边界与异常", doc.EdgeCases)
	writeBulletSection(&b, "假设", doc.Assumptions)
	writeBulletSection(&b, "依赖", doc.Dependencies)
	writeBulletSection(&b, "约束", doc.Constraints)
	writeBulletSection(&b, "限制", doc.Limitations)
	if len(doc.Risks) > 0 {
		b.WriteString("\n#### 风险\n")
		for _, r := range doc.Risks {
			b.WriteString(fmt.Sprintf("- **`%s`** %s", r.ID, r.Description))
			if r.Mitigation != "" {
				b.WriteString(" — 缓解: " + r.Mitigation)
			}
			b.WriteString("\n")
		}
	}
	if len(doc.Glossary) > 0 {
		b.WriteString("\n#### 术语表\n")
		for _, g := range doc.Glossary {
			b.WriteString(fmt.Sprintf("- **%s**: %s\n", g.Term, g.Definition))
		}
	}
	writeBulletSection(&b, "待确认问题", doc.OpenQuestions)
	return strings.TrimRight(b.String(), "\n")
}

// --- research (research) ---------------------------------------------------

type researchQA struct {
	ID       string `json:"id"`
	Question string `json:"question"`
	Answer   string `json:"answer,omitempty"`
}

type researchFinding struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	Detail string `json:"detail,omitempty"`
}

type researchDoc struct {
	Title          string            `json:"title,omitempty"`
	Summary        string            `json:"summary"`
	Questions      []researchQA      `json:"questions,omitempty"`
	Findings       []researchFinding `json:"findings,omitempty"`
	Recommendation string            `json:"recommendation,omitempty"`
	References     flexStrings       `json:"references,omitempty"`
	FollowUps      flexStrings       `json:"follow_ups,omitempty"`
}

func ParseResearch(args map[string]any) (researchDoc, error) {
	var doc researchDoc
	if err := decodeArgs(args, &doc); err != nil {
		return doc, fmt.Errorf("解析调研失败: %w", err)
	}
	doc.Title = strings.TrimSpace(doc.Title)
	doc.Summary = strings.TrimSpace(doc.Summary)
	doc.Recommendation = strings.TrimSpace(doc.Recommendation)
	if doc.Summary == "" {
		return doc, errors.New("summary 不能为空")
	}
	qs := make([]researchQA, 0, len(doc.Questions))
	for _, q := range doc.Questions {
		if strings.TrimSpace(q.Question) == "" {
			continue
		}
		q.ID = fmt.Sprintf("q%d", len(qs)+1)
		q.Question = strings.TrimSpace(q.Question)
		q.Answer = strings.TrimSpace(q.Answer)
		qs = append(qs, q)
	}
	doc.Questions = qs
	fs := make([]researchFinding, 0, len(doc.Findings))
	for _, f := range doc.Findings {
		if strings.TrimSpace(f.Title) == "" {
			continue
		}
		f.ID = fmt.Sprintf("r%d", len(fs)+1)
		f.Title = strings.TrimSpace(f.Title)
		f.Detail = strings.TrimSpace(f.Detail)
		fs = append(fs, f)
	}
	doc.Findings = fs
	if len(doc.Questions) == 0 && len(doc.Findings) == 0 {
		return doc, errors.New("questions 与 findings 至少一类非空")
	}
	return doc, nil
}

// RenderResearchMarkdown renders research.json. Raw content on parse error.
func RenderResearchMarkdown(content string) string {
	var doc researchDoc
	if json.Unmarshal([]byte(content), &doc) != nil || doc.Summary == "" {
		return content
	}
	var b strings.Builder
	if doc.Title != "" {
		b.WriteString("### " + doc.Title + "\n\n")
	}
	b.WriteString(doc.Summary + "\n")
	if len(doc.Questions) > 0 {
		b.WriteString("\n#### 调研问题\n")
		for _, q := range doc.Questions {
			b.WriteString(fmt.Sprintf("- **%s** %s\n", q.ID, q.Question))
			if q.Answer != "" {
				b.WriteString("  - 答:" + q.Answer + "\n")
			}
		}
	}
	if len(doc.Findings) > 0 {
		b.WriteString("\n#### 发现\n")
		for _, f := range doc.Findings {
			b.WriteString(fmt.Sprintf("- **`%s` %s**", f.ID, f.Title))
			if f.Detail != "" {
				b.WriteString(" — " + f.Detail)
			}
			b.WriteString("\n")
		}
	}
	if doc.Recommendation != "" {
		b.WriteString("\n#### 建议\n" + doc.Recommendation + "\n")
	}
	writeBulletSection(&b, "后续任务", doc.FollowUps)
	writeBulletSection(&b, "参考", doc.References)
	return strings.TrimRight(b.String(), "\n")
}

// --- proposals (proposal) --------------------------------------------------

type proposalItem struct {
	ID          string      `json:"id"`
	Title       string      `json:"title"`
	Summary     string      `json:"summary,omitempty"`
	Pros        flexStrings `json:"pros,omitempty"`
	Cons        flexStrings `json:"cons,omitempty"`
	Tradeoffs   string      `json:"tradeoffs,omitempty"`
	Effort      string      `json:"effort,omitempty"`
	Risk        string      `json:"risk,omitempty"`
	Recommended bool        `json:"recommended,omitempty"`
}

type proposalsDoc struct {
	Context         string         `json:"context"`
	DecisionDrivers flexStrings    `json:"decision_drivers,omitempty"`
	Proposals       []proposalItem `json:"proposals"`
}

func normLowMedHigh(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "low", "medium", "high":
		return strings.ToLower(strings.TrimSpace(s))
	default:
		return ""
	}
}

func ParseProposals(args map[string]any) (proposalsDoc, error) {
	var doc proposalsDoc
	if err := decodeArgs(args, &doc); err != nil {
		return doc, fmt.Errorf("解析方案失败: %w", err)
	}
	doc.Context = strings.TrimSpace(doc.Context)
	if doc.Context == "" {
		return doc, errors.New("context 不能为空")
	}
	ps := make([]proposalItem, 0, len(doc.Proposals))
	recommendedSeen := false
	for _, p := range doc.Proposals {
		if strings.TrimSpace(p.Title) == "" {
			continue
		}
		p.ID = fmt.Sprintf("p%d", len(ps)+1)
		p.Title = strings.TrimSpace(p.Title)
		p.Summary = strings.TrimSpace(p.Summary)
		p.Tradeoffs = strings.TrimSpace(p.Tradeoffs)
		p.Effort = normLowMedHigh(p.Effort)
		p.Risk = normLowMedHigh(p.Risk)
		if p.Recommended {
			if recommendedSeen {
				p.Recommended = false // at most one recommended
			} else {
				recommendedSeen = true
			}
		}
		ps = append(ps, p)
	}
	if len(ps) == 0 {
		return doc, errors.New("proposals 至少需要 1 个方案")
	}
	doc.Proposals = ps
	return doc, nil
}

// fillProposalIDs backfills positional ids (p1, p2, …) for any proposal whose
// id is empty. Older artifacts were written before ParseProposals assigned ids,
// so reads must self-heal or the ids never match on select (empty id → the gate
// action / picker card can't be resolved).
func fillProposalIDs(doc *proposalsDoc) {
	for i := range doc.Proposals {
		if strings.TrimSpace(doc.Proposals[i].ID) == "" {
			doc.Proposals[i].ID = fmt.Sprintf("p%d", i+1)
		}
	}
}

// RenderProposalsMarkdown renders proposals.json (all options). Raw on error.
func RenderProposalsMarkdown(content string) string {
	var doc proposalsDoc
	if json.Unmarshal([]byte(content), &doc) != nil || len(doc.Proposals) == 0 {
		return content
	}
	fillProposalIDs(&doc)
	var b strings.Builder
	if doc.Context != "" {
		b.WriteString("### 背景\n" + doc.Context + "\n")
	}
	writeBulletSection(&b, "决策驱动", doc.DecisionDrivers)
	b.WriteString("\n#### 备选方案\n")
	for _, p := range doc.Proposals {
		b.WriteString(renderProposalItem(p))
	}
	return strings.TrimRight(b.String(), "\n")
}

func renderProposalItem(p proposalItem) string {
	var b strings.Builder
	star := ""
	if p.Recommended {
		star = " ⭐推荐"
	}
	b.WriteString(fmt.Sprintf("- **`%s` %s**%s\n", p.ID, p.Title, star))
	if p.Summary != "" {
		b.WriteString("  - " + p.Summary + "\n")
	}
	var meta []string
	if p.Effort != "" {
		meta = append(meta, "工作量:"+p.Effort)
	}
	if p.Risk != "" {
		meta = append(meta, "风险:"+p.Risk)
	}
	if len(meta) > 0 {
		b.WriteString("  - _(" + strings.Join(meta, " · ") + ")_\n")
	}
	for _, pro := range p.Pros {
		b.WriteString("  - ✅ " + pro + "\n")
	}
	for _, con := range p.Cons {
		b.WriteString("  - ⚠️ " + con + "\n")
	}
	if p.Tradeoffs != "" {
		b.WriteString("  - 权衡:" + p.Tradeoffs + "\n")
	}
	return b.String()
}

// ProposalChoice is a selectable option surfaced to the human confirmation gate.
type ProposalChoice struct {
	ID    string
	Title string
}

// ProposalChoices lists the proposals in proposals.json as gate actions.
func ProposalChoices(content string) []ProposalChoice {
	var doc proposalsDoc
	if json.Unmarshal([]byte(content), &doc) != nil {
		return nil
	}
	fillProposalIDs(&doc)
	out := make([]ProposalChoice, 0, len(doc.Proposals))
	for _, p := range doc.Proposals {
		out = append(out, ProposalChoice{ID: p.ID, Title: p.Title})
	}
	return out
}

// SelectProposal resolves the final single proposal from proposals.json. When
// id is empty it auto-selects the recommended one (or the first). It returns
// the final proposal.json content (the chosen proposal plus an accepted status)
// and the chosen id.
func SelectProposal(content, id string) (finalJSON, chosenID string, ok bool) {
	var doc proposalsDoc
	if json.Unmarshal([]byte(content), &doc) != nil || len(doc.Proposals) == 0 {
		return "", "", false
	}
	fillProposalIDs(&doc)
	var chosen *proposalItem
	if id != "" {
		for i := range doc.Proposals {
			if doc.Proposals[i].ID == id {
				chosen = &doc.Proposals[i]
				break
			}
		}
	}
	if chosen == nil {
		for i := range doc.Proposals {
			if doc.Proposals[i].Recommended {
				chosen = &doc.Proposals[i]
				break
			}
		}
	}
	if chosen == nil {
		chosen = &doc.Proposals[0]
	}
	final := struct {
		proposalItem
		Status       string `json:"status"`
		SelectedFrom string `json:"selected_from"`
		Context      string `json:"context,omitempty"`
	}{proposalItem: *chosen, Status: "accepted", SelectedFrom: ProposalsArtifactName, Context: doc.Context}
	b, err := json.MarshalIndent(final, "", "  ")
	if err != nil {
		return "", "", false
	}
	return string(b), chosen.ID, true
}

// RenderProposalMarkdown renders the final proposal.json (single chosen option).
func RenderProposalMarkdown(content string) string {
	var p struct {
		proposalItem
		Status  string `json:"status"`
		Context string `json:"context"`
	}
	if json.Unmarshal([]byte(content), &p) != nil || p.Title == "" {
		return content
	}
	var b strings.Builder
	b.WriteString(fmt.Sprintf("### 已选方案:%s\n", p.Title))
	if p.Context != "" {
		b.WriteString("\n**背景**:" + p.Context + "\n")
	}
	if p.Summary != "" {
		b.WriteString("\n" + p.Summary + "\n")
	}
	for _, pro := range p.Pros {
		b.WriteString("- ✅ " + pro + "\n")
	}
	for _, con := range p.Cons {
		b.WriteString("- ⚠️ " + con + "\n")
	}
	if p.Tradeoffs != "" {
		b.WriteString("\n权衡:" + p.Tradeoffs + "\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

// --- test_result (test) ----------------------------------------------------

type testCase struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Status string `json:"status"`
	Detail string `json:"detail,omitempty"`
}

type testDefect struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Severity string `json:"severity,omitempty"`
	Detail   string `json:"detail,omitempty"`
	Status   string `json:"status,omitempty"`
}

// testScreenshot is one browser/UI test screenshot attached to a test result.
// New writes store Artifact (+ optional MimeType/Caption) only; Data is never
// written by set_test_result. Legacy rows may still carry inline Data from
// older hydrate-on-write behavior; readers prefer Data when present, else lazy-
// load via Artifact.
type testScreenshot struct {
	// Data is legacy inline base64 (no data: prefix). Not accepted as tool
	// input (stripped by normTestScreenshots). New writes leave this empty;
	// historical test_result.json may still contain it for display compatibility.
	Data string `json:"data,omitempty"`
	// Artifact references a screenshot uploaded via the artifact-upload CLI
	// (an artifact name in this run). This is the only supported way to attach
	// a screenshot on write; the stored test_result.json keeps the reference
	// (and caption/mimeType) without inlining binary content.
	Artifact string `json:"artifact,omitempty"`
	MimeType string `json:"mimeType,omitempty"`
	Caption  string `json:"caption,omitempty"`
}

// maxTestScreenshots caps how many screenshots a single test result may carry;
// extras are dropped in ParseTestResult to keep the artifact payload bounded.
const maxTestScreenshots = 10

// planCoverageItem is one plan-leaf fit check recorded in test_result.json.
// When the run has plan leaves, the test gate requires full coverage with
// passed=true and non-empty evidence (Agent self-attestation; no code semantics).
type planCoverageItem struct {
	PlanID   string `json:"plan_id"`
	Title    string `json:"title,omitempty"`
	Passed   bool   `json:"passed"`
	Evidence string `json:"evidence,omitempty"`
}

type testResultDoc struct {
	Summary      string             `json:"summary"`
	Cases        []testCase         `json:"cases,omitempty"`
	Defects      []testDefect       `json:"defects,omitempty"`
	Variances    string             `json:"variances,omitempty"`
	Assessment   string             `json:"assessment,omitempty"`
	Screenshots  []testScreenshot   `json:"screenshots,omitempty"`
	PlanCoverage []planCoverageItem `json:"plan_coverage,omitempty"`
	Passed       int                `json:"passed"`
	Failed       int                `json:"failed"`
	Skipped      int                `json:"skipped"`
}

func normTestStatus(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "passed", "pass", "ok", "success":
		return "passed"
	case "skipped", "skip":
		return "skipped"
	default:
		return "failed"
	}
}

func ParseTestResult(args map[string]any) (testResultDoc, error) {
	var doc testResultDoc
	if err := decodeArgs(args, &doc); err != nil {
		return doc, fmt.Errorf("解析测试结果失败: %w", err)
	}
	doc.Summary = strings.TrimSpace(doc.Summary)
	doc.Variances = strings.TrimSpace(doc.Variances)
	doc.Assessment = strings.TrimSpace(doc.Assessment)
	if doc.Summary == "" {
		return doc, errors.New("summary 不能为空")
	}
	cs := make([]testCase, 0, len(doc.Cases))
	doc.Passed, doc.Failed, doc.Skipped = 0, 0, 0
	for _, c := range doc.Cases {
		if strings.TrimSpace(c.Name) == "" {
			continue
		}
		c.ID = fmt.Sprintf("t%d", len(cs)+1)
		c.Name = strings.TrimSpace(c.Name)
		c.Status = normTestStatus(c.Status)
		c.Detail = strings.TrimSpace(c.Detail)
		switch c.Status {
		case "passed":
			doc.Passed++
		case "skipped":
			doc.Skipped++
		default:
			doc.Failed++
		}
		cs = append(cs, c)
	}
	doc.Cases = cs
	ds := make([]testDefect, 0, len(doc.Defects))
	for _, d := range doc.Defects {
		if strings.TrimSpace(d.Title) == "" {
			continue
		}
		d.ID = fmt.Sprintf("d%d", len(ds)+1)
		d.Title = strings.TrimSpace(d.Title)
		d.Detail = strings.TrimSpace(d.Detail)
		ds = append(ds, d)
	}
	doc.Defects = ds
	doc.Screenshots = normTestScreenshots(doc.Screenshots)
	doc.PlanCoverage = normPlanCoverage(doc.PlanCoverage)
	return doc, nil
}

// normPlanCoverage trims plan_coverage fields. Entries with empty plan_id are
// dropped. Missing plan_coverage is allowed at parse time (no leaves / fail-open);
// relative coverage against plan leaves is enforced by the test gate.
func normPlanCoverage(in []planCoverageItem) []planCoverageItem {
	if len(in) == 0 {
		return nil
	}
	out := make([]planCoverageItem, 0, len(in))
	for _, item := range in {
		id := strings.TrimSpace(item.PlanID)
		if id == "" {
			continue
		}
		out = append(out, planCoverageItem{
			PlanID:   id,
			Title:    strings.TrimSpace(item.Title),
			Passed:   item.Passed,
			Evidence: strings.TrimSpace(item.Evidence),
		})
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// normTestScreenshots sanitizes attached screenshots. Only artifact references
// (uploaded via the artifact-upload CLI) are accepted: any inline `data` in the
// input is ignored, entries without an `artifact` are dropped, captions/mime are
// trimmed, and the count is capped at maxTestScreenshots. Artifact-only entries
// are stored as-is; the MCP host validates each reference exists before write.
func normTestScreenshots(in []testScreenshot) []testScreenshot {
	if len(in) == 0 {
		return nil
	}
	out := make([]testScreenshot, 0, len(in))
	for _, s := range in {
		artifact := strings.TrimSpace(s.Artifact)
		if artifact == "" {
			continue
		}
		out = append(out, testScreenshot{
			Artifact: artifact,
			MimeType: strings.TrimSpace(s.MimeType),
			Caption:  strings.TrimSpace(s.Caption),
		})
		if len(out) >= maxTestScreenshots {
			break
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// ValidateScreenshotArtifacts checks that every screenshot artifact reference
// exists in the run store. Returns an error naming the first missing artifact.
func (d *testResultDoc) ValidateScreenshotArtifacts(exists func(name string) bool) error {
	if len(d.Screenshots) == 0 || exists == nil {
		return nil
	}
	for _, s := range d.Screenshots {
		if s.Artifact != "" && !exists(s.Artifact) {
			return fmt.Errorf("screenshot artifact not found: %s", s.Artifact)
		}
	}
	return nil
}

// GuessImageMIME infers an image MIME type from an artifact filename suffix.
func GuessImageMIME(name string) string {
	return guessImageMIME(name)
}

// guessImageMIME infers an image MIME type from an artifact filename suffix.
func guessImageMIME(name string) string {
	switch strings.ToLower(path.Ext(name)) {
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".webp":
		return "image/webp"
	case ".gif":
		return "image/gif"
	default:
		return "image/png"
	}
}

// normalizeScreenshotData trims whitespace and strips an optional data: URL prefix
// so stored screenshots carry raw base64 only.
func normalizeScreenshotData(content string) string {
	content = strings.TrimSpace(content)
	if strings.HasPrefix(content, "data:") {
		if idx := strings.Index(content, ";base64,"); idx >= 0 {
			content = content[idx+len(";base64,"):]
		}
	}
	return strings.TrimSpace(content)
}

// HydrateScreenshotArtifacts resolves each screenshot artifact reference into
// inline base64 data. Write path (set_test_result) no longer calls this — new
// storage keeps artifact refs only. Kept for tests and any caller that still
// needs an explicit in-memory hydrate; prefer HydrateTestResultContent for
// read-time buffering of API responses.
func (d *testResultDoc) HydrateScreenshotArtifacts(read func(name string) (string, error)) error {
	if len(d.Screenshots) == 0 || read == nil {
		return nil
	}
	out := make([]testScreenshot, 0, len(d.Screenshots))
	for _, s := range d.Screenshots {
		if s.Artifact == "" {
			continue
		}
		raw, err := read(s.Artifact)
		if err != nil {
			return fmt.Errorf("screenshot artifact read failed: %s: %w", s.Artifact, err)
		}
		mime := strings.TrimSpace(s.MimeType)
		if mime == "" {
			mime = guessImageMIME(s.Artifact)
		}
		out = append(out, testScreenshot{
			Data:     normalizeScreenshotData(raw),
			MimeType: mime,
			Caption:  s.Caption,
		})
	}
	d.Screenshots = out
	return nil
}

// HydrateTestResultContent injects inline data for artifact-only screenshot entries
// in a test_result.json payload (response buffering only; does not rewrite storage).
// Used by get_test_result / node outputs as a short-term buffer; the frontend
// ArtifactContent path bypasses this so TestResultView can lazy-load by artifact.
// Read failures leave those entries unchanged. Non-test_result or malformed JSON
// is returned as-is.
func HydrateTestResultContent(raw string, read func(name string) (string, error)) (string, error) {
	var doc testResultDoc
	if err := json.Unmarshal([]byte(raw), &doc); err != nil {
		return raw, nil
	}
	if len(doc.Screenshots) == 0 || read == nil {
		return raw, nil
	}
	changed := false
	for i, s := range doc.Screenshots {
		if s.Artifact == "" || strings.TrimSpace(s.Data) != "" {
			continue
		}
		content, err := read(s.Artifact)
		if err != nil {
			continue
		}
		mime := strings.TrimSpace(s.MimeType)
		if mime == "" {
			mime = guessImageMIME(s.Artifact)
		}
		doc.Screenshots[i].Data = normalizeScreenshotData(content)
		doc.Screenshots[i].MimeType = mime
		doc.Screenshots[i].Artifact = ""
		changed = true
	}
	if !changed {
		return raw, nil
	}
	b, err := json.Marshal(doc)
	if err != nil {
		return raw, err
	}
	return string(b), nil
}

// TestFailedCount returns how many test cases failed in a stored
// test_result.json. It prefers the normalized `failed` counter and falls back
// to recounting cases. Returns -1 when the JSON is malformed so callers can
// fail closed instead of treating a bad artifact as passing.
func TestFailedCount(content string) int {
	var doc testResultDoc
	if json.Unmarshal([]byte(content), &doc) != nil {
		return -1
	}
	if doc.Failed > 0 {
		return doc.Failed
	}
	n := 0
	for _, c := range doc.Cases {
		if strings.TrimSpace(c.Name) != "" && normTestStatus(c.Status) == "failed" {
			n++
		}
	}
	return n
}

// TestSkippedCount returns how many test cases were skipped in a stored
// test_result.json. It prefers the normalized `skipped` counter and falls back
// to recounting cases. Returns -1 when the JSON is malformed.
func TestSkippedCount(content string) int {
	var doc testResultDoc
	if json.Unmarshal([]byte(content), &doc) != nil {
		return -1
	}
	if doc.Skipped > 0 {
		return doc.Skipped
	}
	n := 0
	for _, c := range doc.Cases {
		if strings.TrimSpace(c.Name) != "" && normTestStatus(c.Status) == "skipped" {
			n++
		}
	}
	return n
}

// RenderTestResultMarkdown renders test_result.json. Raw on parse error.
func RenderTestResultMarkdown(content string) string {
	var doc testResultDoc
	if json.Unmarshal([]byte(content), &doc) != nil || doc.Summary == "" {
		return content
	}
	var b strings.Builder
	b.WriteString(doc.Summary + "\n")
	if len(doc.Cases) > 0 {
		b.WriteString(fmt.Sprintf("\n**结果**:✅ %d 通过 · ❌ %d 失败 · ⏭️ %d 跳过\n\n", doc.Passed, doc.Failed, doc.Skipped))
		for _, c := range doc.Cases {
			icon := "❌"
			switch c.Status {
			case "passed":
				icon = "✅"
			case "skipped":
				icon = "⏭️"
			}
			b.WriteString(fmt.Sprintf("- %s %s", icon, c.Name))
			if c.Detail != "" {
				b.WriteString(" — " + c.Detail)
			}
			b.WriteString("\n")
		}
	}
	if len(doc.Defects) > 0 {
		b.WriteString("\n#### 缺陷\n")
		for _, d := range doc.Defects {
			line := "- " + d.Title
			if d.Severity != "" {
				line = fmt.Sprintf("- [%s] %s", d.Severity, d.Title)
			}
			if d.Detail != "" {
				line += " — " + d.Detail
			}
			b.WriteString(line + "\n")
		}
	}
	if len(doc.PlanCoverage) > 0 {
		b.WriteString("\n#### 计划贴合度\n")
		for _, item := range doc.PlanCoverage {
			icon := "❌"
			if item.Passed {
				icon = "✅"
			}
			label := item.PlanID
			if item.Title != "" {
				label += " " + item.Title
			}
			b.WriteString(fmt.Sprintf("- %s %s", icon, label))
			if item.Evidence != "" {
				b.WriteString(" — " + item.Evidence)
			}
			b.WriteString("\n")
		}
	}
	if doc.Variances != "" {
		b.WriteString("\n#### 与计划偏差\n" + doc.Variances + "\n")
	}
	if doc.Assessment != "" {
		b.WriteString("\n#### 评估\n" + doc.Assessment + "\n")
	}
	if n := len(doc.Screenshots); n > 0 {
		b.WriteString(fmt.Sprintf("\n#### 测试截图(%d 张)\n", n))
		for i, s := range doc.Screenshots {
			cap := strings.TrimSpace(s.Caption)
			if cap == "" {
				cap = fmt.Sprintf("截图 %d", i+1)
			}
			b.WriteString("- " + cap + "\n")
		}
		b.WriteString("\n在「产物」标签查看截图。\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

// --- review (review) -------------------------------------------------------

type reviewFinding struct {
	ID         string `json:"id"`
	Severity   string `json:"severity"`
	File       string `json:"file,omitempty"`
	Line       int    `json:"line,omitempty"`
	Title      string `json:"title"`
	Detail     string `json:"detail,omitempty"`
	Suggestion string `json:"suggestion,omitempty"`
}

type reviewDoc struct {
	Summary     string          `json:"summary"`
	Verdict     string          `json:"verdict"`
	Findings    []reviewFinding `json:"findings,omitempty"`
	ActionItems flexStrings     `json:"action_items,omitempty"`
}

func validVerdict(s string) bool {
	switch s {
	case "approve", "approve_with_comments", "request_changes", "reject":
		return true
	}
	return false
}

func severityRank(s string) int {
	switch strings.ToLower(s) {
	case "critical":
		return 0
	case "high":
		return 1
	case "medium":
		return 2
	case "low":
		return 3
	default:
		return 4
	}
}

func normSeverity(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "critical", "high", "medium", "low":
		return strings.ToLower(strings.TrimSpace(s))
	default:
		return "medium"
	}
}

func ParseReview(args map[string]any) (reviewDoc, error) {
	var doc reviewDoc
	if err := decodeArgs(args, &doc); err != nil {
		return doc, fmt.Errorf("解析评审失败: %w", err)
	}
	doc.Summary = strings.TrimSpace(doc.Summary)
	doc.Verdict = strings.ToLower(strings.TrimSpace(doc.Verdict))
	if doc.Summary == "" {
		return doc, errors.New("summary 不能为空")
	}
	if !validVerdict(doc.Verdict) {
		return doc, errors.New("verdict 须为 approve|approve_with_comments|request_changes|reject")
	}
	fs := make([]reviewFinding, 0, len(doc.Findings))
	for _, f := range doc.Findings {
		if strings.TrimSpace(f.Title) == "" {
			continue
		}
		f.Severity = normSeverity(f.Severity)
		f.Title = strings.TrimSpace(f.Title)
		f.Detail = strings.TrimSpace(f.Detail)
		f.Suggestion = strings.TrimSpace(f.Suggestion)
		f.File = strings.TrimSpace(f.File)
		fs = append(fs, f)
	}
	sort.SliceStable(fs, func(i, j int) bool { return severityRank(fs[i].Severity) < severityRank(fs[j].Severity) })
	for i := range fs {
		fs[i].ID = fmt.Sprintf("v%d", i+1)
	}
	doc.Findings = fs
	return doc, nil
}

// RenderReviewMarkdown renders review.json. Raw on parse error.
func RenderReviewMarkdown(content string) string {
	var doc reviewDoc
	if json.Unmarshal([]byte(content), &doc) != nil || doc.Summary == "" {
		return content
	}
	var b strings.Builder
	b.WriteString(fmt.Sprintf("**结论**:`%s`\n\n", doc.Verdict))
	b.WriteString(doc.Summary + "\n")
	if len(doc.Findings) > 0 {
		b.WriteString("\n#### 评审意见\n")
		for _, f := range doc.Findings {
			loc := ""
			if f.File != "" {
				loc = " (" + f.File
				if f.Line > 0 {
					loc += fmt.Sprintf(":%d", f.Line)
				}
				loc += ")"
			}
			b.WriteString(fmt.Sprintf("- **[%s]** %s%s\n", strings.ToUpper(f.Severity), f.Title, loc))
			if f.Detail != "" {
				b.WriteString("  - " + f.Detail + "\n")
			}
			if f.Suggestion != "" {
				b.WriteString("  - 建议:" + f.Suggestion + "\n")
			}
		}
	}
	writeBulletSection(&b, "待处理项", doc.ActionItems)
	return strings.TrimRight(b.String(), "\n")
}

// ReviewVerdict extracts the verdict from review.json ("" when absent/invalid).
func ReviewVerdict(content string) string {
	verdict, ok := ReviewVerdictOK(content)
	if !ok {
		return ""
	}
	return verdict
}

// ReviewVerdictOK extracts a valid verdict from review.json. The second return
// is false when the JSON is malformed, summary is empty, or verdict is absent
// or invalid.
func ReviewVerdictOK(content string) (string, bool) {
	var doc reviewDoc
	if json.Unmarshal([]byte(content), &doc) != nil {
		return "", false
	}
	if strings.TrimSpace(doc.Summary) == "" {
		return "", false
	}
	verdict := strings.ToLower(strings.TrimSpace(doc.Verdict))
	if !validVerdict(verdict) {
		return "", false
	}
	return verdict, true
}

// --- implementation_result (implement) -------------------------------------

type implChangedArea struct {
	Title  string `json:"title"`
	Detail string `json:"detail,omitempty"`
}

type implementationResultDoc struct {
	Summary         string            `json:"summary"`
	ChangeType      string            `json:"change_type,omitempty"`
	ChangedAreas    []implChangedArea `json:"changed_areas,omitempty"`
	Tests           flexStrings       `json:"tests,omitempty"`
	BreakingChanges flexStrings       `json:"breaking_changes,omitempty"`
	FollowUps       flexStrings       `json:"follow_ups,omitempty"`
}

func ParseImplementationResult(args map[string]any) (implementationResultDoc, error) {
	var doc implementationResultDoc
	if err := decodeArgs(args, &doc); err != nil {
		return doc, fmt.Errorf("解析实现结果失败: %w", err)
	}
	doc.Summary = strings.TrimSpace(doc.Summary)
	doc.ChangeType = strings.TrimSpace(doc.ChangeType)
	if doc.Summary == "" {
		return doc, errors.New("summary 不能为空")
	}
	cas := make([]implChangedArea, 0, len(doc.ChangedAreas))
	for _, ca := range doc.ChangedAreas {
		if strings.TrimSpace(ca.Title) == "" {
			continue
		}
		ca.Title = strings.TrimSpace(ca.Title)
		ca.Detail = strings.TrimSpace(ca.Detail)
		cas = append(cas, ca)
	}
	doc.ChangedAreas = cas
	return doc, nil
}

// RenderImplementationResultMarkdown renders implementation_result.json.
func RenderImplementationResultMarkdown(content string) string {
	var doc implementationResultDoc
	if json.Unmarshal([]byte(content), &doc) != nil || doc.Summary == "" {
		return content
	}
	var b strings.Builder
	if doc.ChangeType != "" {
		b.WriteString(fmt.Sprintf("_(%s)_\n\n", doc.ChangeType))
	}
	b.WriteString(doc.Summary + "\n")
	if len(doc.ChangedAreas) > 0 {
		b.WriteString("\n#### 改动\n")
		for _, ca := range doc.ChangedAreas {
			b.WriteString("- **" + ca.Title + "**")
			if ca.Detail != "" {
				b.WriteString(" — " + ca.Detail)
			}
			b.WriteString("\n")
		}
	}
	writeBulletSection(&b, "测试", doc.Tests)
	writeBulletSection(&b, "破坏性变更", doc.BreakingChanges)
	writeBulletSection(&b, "后续", doc.FollowUps)
	return strings.TrimRight(b.String(), "\n")
}

// --- shared render helper --------------------------------------------------

func writeBulletSection(b *strings.Builder, title string, items []string) {
	if len(items) == 0 {
		return
	}
	b.WriteString("\n#### " + title + "\n")
	for _, it := range items {
		b.WriteString("- " + it + "\n")
	}
}
