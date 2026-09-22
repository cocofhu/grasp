package structured

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/cocofhu/grasp/internal/mcp/mermaidvalidate"
)

// RootCauseArtifactName is the reserved JSON product for Grasp bug runs.
const RootCauseArtifactName = "root_cause.json"

// Work kind values on clarified_requirement (Grasp required; react optional).
const (
	WorkKindBug     = "bug"
	WorkKindFeature = "feature"
	WorkKindOther   = "other"
)

// mermaidSyntaxCheck validates mermaid sources (same engine as plan diagrams).
// Tests may override.
var mermaidSyntaxCheck = mermaidvalidate.Check

var (
	symbolOnlyRE = regexp.MustCompile(`^[A-Za-z0-9_./:\\$()-]+$`)
	// Forbidden top-level keys: root cause explains why, not how to fix or when.
	rootCauseForbiddenKeys = []string{
		"fix", "fix_steps", "fix_plan", "patch", "patches", "diff", "code_change",
		"schedule", "deadline", "due_date", "milestone", "milestones", "delivery_date", "dates",
	}
)

type rootCauseEvidence struct {
	Title  string `json:"title"`
	Detail string `json:"detail"`
}

type rootCauseDiagram struct {
	Kind    string `json:"kind,omitempty"`
	Title   string `json:"title,omitempty"`
	Format  string `json:"format,omitempty"`
	Source  string `json:"source"`
	Caption string `json:"caption,omitempty"`
}

type rootCauseDoc struct {
	Title                string             `json:"title"`
	Summary              string             `json:"summary"`
	Symptom              string             `json:"symptom"`
	Expected             string             `json:"expected"`
	Actual               string             `json:"actual"`
	Reproduction         flexStrings        `json:"reproduction"`
	Impact               string             `json:"impact"`
	RootCause            string             `json:"root_cause"`
	Evidence             []rootCauseEvidence `json:"evidence"`
	Diagrams             []rootCauseDiagram `json:"diagrams"`
	RuledOut             flexStrings        `json:"ruled_out,omitempty"`
	ContributingFactors  flexStrings        `json:"contributing_factors,omitempty"`
	AffectedScope        string             `json:"affected_scope,omitempty"`
	CausalChain          string             `json:"causal_chain,omitempty"`
}

// ValidWorkKind reports whether s is bug|feature|other (empty is not valid).
func ValidWorkKind(s string) bool {
	switch strings.TrimSpace(s) {
	case WorkKindBug, WorkKindFeature, WorkKindOther:
		return true
	default:
		return false
	}
}

// NormalizeWorkKind trims and lowercases; empty stays empty.
func NormalizeWorkKind(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	if ValidWorkKind(s) {
		return s
	}
	return strings.TrimSpace(s)
}

// ClarifiedWorkKind returns the work_kind from a clarified_requirement.json body.
// Empty when missing or unparseable.
func ClarifiedWorkKind(content string) string {
	var doc clarifiedRequirementDoc
	if json.Unmarshal([]byte(content), &doc) != nil {
		return ""
	}
	return NormalizeWorkKind(doc.WorkKind)
}

// ParseRootCause validates and normalizes a root_cause.json payload.
func ParseRootCause(args map[string]any) (rootCauseDoc, error) {
	if err := rejectForbiddenRootCauseKeys(args); err != nil {
		return rootCauseDoc{}, err
	}
	var doc rootCauseDoc
	if err := decodeArgs(args, &doc); err != nil {
		return doc, fmt.Errorf("解析根因报告失败: %w", err)
	}
	doc.Title = strings.TrimSpace(doc.Title)
	doc.Summary = strings.TrimSpace(doc.Summary)
	doc.Symptom = strings.TrimSpace(doc.Symptom)
	doc.Expected = strings.TrimSpace(doc.Expected)
	doc.Actual = strings.TrimSpace(doc.Actual)
	doc.Impact = strings.TrimSpace(doc.Impact)
	doc.RootCause = strings.TrimSpace(doc.RootCause)
	doc.AffectedScope = strings.TrimSpace(doc.AffectedScope)
	doc.CausalChain = strings.TrimSpace(doc.CausalChain)
	doc.RuledOut = trimStringList(doc.RuledOut)
	doc.ContributingFactors = trimStringList(doc.ContributingFactors)

	for _, field := range []struct {
		name, val string
	}{
		{"title", doc.Title},
		{"summary", doc.Summary},
		{"symptom", doc.Symptom},
		{"expected", doc.Expected},
		{"actual", doc.Actual},
		{"impact", doc.Impact},
		{"root_cause", doc.RootCause},
	} {
		if field.val == "" {
			return doc, fmt.Errorf("%s 不能为空", field.name)
		}
	}

	repro, err := requireNonEmptyList("reproduction", doc.Reproduction)
	if err != nil {
		return doc, err
	}
	doc.Reproduction = repro

	if looksLikeSymbolOnly(doc.RootCause) {
		return doc, errors.New("root_cause 必须是原因说明,不能只有符号名或文件名")
	}

	ev := make([]rootCauseEvidence, 0, len(doc.Evidence))
	for _, e := range doc.Evidence {
		e.Title = strings.TrimSpace(e.Title)
		e.Detail = strings.TrimSpace(e.Detail)
		if e.Title == "" || e.Detail == "" {
			continue
		}
		ev = append(ev, e)
	}
	if len(ev) == 0 {
		return doc, errors.New("evidence 至少需要 1 条(含 title 与 detail)")
	}
	doc.Evidence = ev

	diags, err := normalizeRootCauseDiagrams(doc.Diagrams)
	if err != nil {
		return doc, err
	}
	if len(diags) == 0 {
		return doc, errors.New("diagrams 至少需要 1 张图")
	}
	doc.Diagrams = diags
	return doc, nil
}

func rejectForbiddenRootCauseKeys(args map[string]any) error {
	if args == nil {
		return nil
	}
	for _, k := range rootCauseForbiddenKeys {
		if _, ok := args[k]; ok {
			return fmt.Errorf("根因报告不接受字段 %q(解释原因,不承载修复或排期)", k)
		}
	}
	return nil
}

func looksLikeSymbolOnly(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" {
		return true
	}
	// One token that looks like an identifier/path/file — no prose.
	if !strings.ContainsAny(s, " \t\n，。；、") && symbolOnlyRE.MatchString(s) {
		return true
	}
	return false
}

func normalizeRootCauseDiagrams(in []rootCauseDiagram) ([]rootCauseDiagram, error) {
	out := make([]rootCauseDiagram, 0, len(in))
	for i, d := range in {
		path := fmt.Sprintf("diagrams[%d]", i)
		src := strings.TrimSpace(d.Source)
		if src == "" {
			return nil, fmt.Errorf("%s.source 不能为空", path)
		}
		format := strings.TrimSpace(d.Format)
		if format == "" {
			format = "mermaid"
		}
		if strings.EqualFold(format, "mermaid") {
			if err := mermaidSyntaxCheck(src); err != nil {
				return nil, fmt.Errorf("%s.source mermaid 语法错误: %v", path, err)
			}
		}
		kind := strings.ToLower(strings.TrimSpace(d.Kind))
		switch kind {
		case "", "flowchart", "sequence", "activity", "chart", "other":
			if kind == "" {
				kind = "flowchart"
			}
		default:
			return nil, fmt.Errorf("%s.kind 须为 flowchart|sequence|activity|chart|other", path)
		}
		title := strings.TrimSpace(d.Title)
		caption := strings.TrimSpace(d.Caption)
		if title == "" && caption == "" {
			return nil, fmt.Errorf("%s 须有 title 或 caption", path)
		}
		out = append(out, rootCauseDiagram{
			Kind:    kind,
			Title:   title,
			Format:  format,
			Source:  src,
			Caption: caption,
		})
	}
	return out, nil
}

// RenderRootCauseMarkdown renders root_cause.json for gates / inbox.
// Raw content on parse error.
func RenderRootCauseMarkdown(content string) string {
	var doc rootCauseDoc
	if json.Unmarshal([]byte(content), &doc) != nil || strings.TrimSpace(doc.Summary) == "" {
		return content
	}
	var b strings.Builder
	if t := strings.TrimSpace(doc.Title); t != "" {
		b.WriteString("# ")
		b.WriteString(t)
		b.WriteByte('\n')
	}
	b.WriteString("\n## 概述\n\n")
	b.WriteString(strings.TrimSpace(doc.Summary))
	b.WriteByte('\n')
	writeRCSection(&b, "现象", doc.Symptom)
	writeRCSection(&b, "期望", doc.Expected)
	writeRCSection(&b, "实际", doc.Actual)
	if len(doc.Reproduction) > 0 {
		b.WriteString("\n## 复现步骤\n\n")
		for i, step := range doc.Reproduction {
			fmt.Fprintf(&b, "%d. %s\n", i+1, step)
		}
	}
	writeRCSection(&b, "影响", doc.Impact)
	writeRCSection(&b, "根因", doc.RootCause)
	if len(doc.Evidence) > 0 {
		b.WriteString("\n## 证据\n\n")
		for _, e := range doc.Evidence {
			fmt.Fprintf(&b, "- **%s**: %s\n", e.Title, e.Detail)
		}
	}
	if len(doc.RuledOut) > 0 {
		b.WriteString("\n## 已排除\n\n")
		for _, s := range doc.RuledOut {
			fmt.Fprintf(&b, "- %s\n", s)
		}
	}
	if len(doc.ContributingFactors) > 0 {
		b.WriteString("\n## 促成因素\n\n")
		for _, s := range doc.ContributingFactors {
			fmt.Fprintf(&b, "- %s\n", s)
		}
	}
	writeRCSection(&b, "影响范围", doc.AffectedScope)
	writeRCSection(&b, "因果链", doc.CausalChain)
	if len(doc.Diagrams) > 0 {
		b.WriteString("\n## 图示\n\n")
		for i, d := range doc.Diagrams {
			label := d.Title
			if label == "" {
				label = d.Caption
			}
			if label == "" {
				label = fmt.Sprintf("图 %d", i+1)
			}
			fmt.Fprintf(&b, "### %s\n\n", label)
			if d.Caption != "" && d.Title != "" {
				b.WriteString(d.Caption)
				b.WriteString("\n\n")
			}
			fmt.Fprintf(&b, "```%s\n%s\n```\n\n", d.Format, d.Source)
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

func writeRCSection(b *strings.Builder, title, body string) {
	body = strings.TrimSpace(body)
	if body == "" {
		return
	}
	b.WriteString("\n## ")
	b.WriteString(title)
	b.WriteString("\n\n")
	b.WriteString(body)
	b.WriteByte('\n')
}
