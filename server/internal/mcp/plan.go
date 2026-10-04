package mcp

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/cocofhu/grasp/internal/mcp/mermaidvalidate"
)

// mermaidSyntaxCheck validates a mermaid diagram source. Tests may override.
var mermaidSyntaxCheck = mermaidvalidate.Check

// --- structured plan (set_plan / get_plan / update_plan_status) ------------

// PlanArtifactName is the reserved artifact holding the run's global, two-level
// plan. It is written by set_plan (plan node), read by get_plan, and its item
// statuses are advanced by update_plan_status (implement node).
const PlanArtifactName = "plan.json"

const (
	planStatusPending    = "pending"
	planStatusInProgress = "in_progress"
	planStatusDone       = "done"
)

const planNAPlaceholder = "不涉及"

type planSub struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	Detail string `json:"detail,omitempty"`
	Status string `json:"status"`
}

type planGoal struct {
	ID       string    `json:"id"`
	Title    string    `json:"title"`
	Detail   string    `json:"detail,omitempty"`
	Status   string    `json:"status"`
	Subgoals []planSub `json:"subgoals,omitempty"`
}

// planDiagram is an optional Mermaid (or other) diagram attached to a design section.
// When the object is present, source must be non-empty after trim.
// kind/title/scope support multi-diagram tabs; diagrams[] is preferred, singular
// diagram remains for backward compatibility.
type planDiagram struct {
	Kind             string `json:"kind,omitempty"`
	Title            string `json:"title,omitempty"`
	Scope            string `json:"scope,omitempty"`
	Format           string `json:"format,omitempty"`
	Source           string `json:"source"`
	FallbackArtifact string `json:"fallback_artifact,omitempty"`
	Caption          string `json:"caption,omitempty"`
}

type planArchitecture struct {
	Summary  string        `json:"summary"`
	Diagrams []planDiagram `json:"diagrams,omitempty"`
	Diagram  *planDiagram  `json:"diagram,omitempty"`
}

type planField struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	PK          *bool  `json:"pk,omitempty"`
	Nullable    *bool  `json:"nullable,omitempty"`
	FK          string `json:"fk,omitempty"`
	Description string `json:"description,omitempty"`
}

type planEntity struct {
	Name          string      `json:"name"`
	Fields        []planField `json:"fields,omitempty"`
	Attributes    []string    `json:"attributes,omitempty"`
	Description   string      `json:"description,omitempty"`
	Relationships []string    `json:"relationships,omitempty"`
}

type planDataDesign struct {
	Summary       string        `json:"summary"`
	Entities      []planEntity  `json:"entities,omitempty"`
	Relationships []string      `json:"relationships,omitempty"`
	Diagrams      []planDiagram `json:"diagrams,omitempty"`
	Diagram       *planDiagram  `json:"diagram,omitempty"`
}

type planInterfaceItem struct {
	Name      string        `json:"name"`
	Kind      string        `json:"kind,omitempty"`
	Direction string        `json:"direction,omitempty"`
	Summary   string        `json:"summary,omitempty"`
	Detail    string        `json:"detail,omitempty"`
	Diagrams  []planDiagram `json:"diagrams,omitempty"`
	Diagram   *planDiagram  `json:"diagram,omitempty"`
}

type planComponentItem struct {
	Name           string        `json:"name"`
	Responsibility string        `json:"responsibility,omitempty"`
	Dependencies   []string      `json:"dependencies,omitempty"`
	Detail         string        `json:"detail,omitempty"`
	Diagrams       []planDiagram `json:"diagrams,omitempty"`
	Diagram        *planDiagram  `json:"diagram,omitempty"`
}

type planInteraction struct {
	Summary  string        `json:"summary"`
	Diagrams []planDiagram `json:"diagrams,omitempty"`
	Diagram  *planDiagram  `json:"diagram,omitempty"`
}

type planDoc struct {
	Title        string              `json:"title,omitempty"`
	Architecture *planArchitecture   `json:"architecture,omitempty"`
	DataDesign   *planDataDesign     `json:"data_design,omitempty"`
	Interfaces   []planInterfaceItem `json:"interfaces,omitempty"`
	Components   []planComponentItem `json:"components,omitempty"`
	Interaction  *planInteraction    `json:"interaction,omitempty"`
	TestDesign   string              `json:"test_design,omitempty"`
	Goals        []planGoal          `json:"goals"`
}

func validPlanStatus(s string) bool {
	switch s {
	case planStatusPending, planStatusInProgress, planStatusDone:
		return true
	}
	return false
}

func normPlanStatus(s string) string {
	if validPlanStatus(s) {
		return s
	}
	return planStatusPending
}

func parsePlanDiagram(path string, d *planDiagram) (*planDiagram, error) {
	if d == nil {
		return nil, nil
	}
	src := strings.TrimSpace(d.Source)
	if src == "" {
		return nil, fmt.Errorf("%s.source 不能为空", path)
	}
	format := strings.TrimSpace(d.Format)
	if format == "" {
		format = "mermaid"
	}
	// Mermaid (default) sources must be parseable by the same Mermaid 11.x engine
	// the PlanView frontend uses; non-mermaid formats stay non-empty-only.
	if strings.EqualFold(format, "mermaid") {
		if err := mermaidSyntaxCheck(src); err != nil {
			return nil, fmt.Errorf("%s.source mermaid 语法错误: %v", path, err)
		}
	}
	out := &planDiagram{
		Kind:             strings.TrimSpace(d.Kind),
		Title:            strings.TrimSpace(d.Title),
		Scope:            strings.TrimSpace(d.Scope),
		Format:           format,
		Source:           src,
		FallbackArtifact: strings.TrimSpace(d.FallbackArtifact),
		Caption:          strings.TrimSpace(d.Caption),
	}
	return out, nil
}

func defaultDiagramKind(section string) string {
	switch section {
	case "architecture":
		return "flowchart"
	case "data_design":
		return "er"
	case "interaction":
		return "sequence"
	default:
		return ""
	}
}

func diagramSourceKey(d planDiagram) string {
	return strings.TrimSpace(d.Source)
}

// mergeSectionDiagrams normalizes diagrams[] with optional singular diagram.
// Only-singular: promote to one entry and infer kind from section when empty.
// Both present with different sources: keep both. Same source: keep one.
// Returns the normalized list and a singular pointer (first entry) for legacy readers.
func mergeSectionDiagrams(section, singularPath string, singular *planDiagram, plural []planDiagram) ([]planDiagram, *planDiagram, error) {
	var out []planDiagram
	seen := map[string]struct{}{}

	for i, raw := range plural {
		d, err := parsePlanDiagram(fmt.Sprintf("%s.diagrams[%d]", section, i), &raw)
		if err != nil {
			return nil, nil, err
		}
		if d == nil {
			continue
		}
		if d.Kind == "" {
			d.Kind = defaultDiagramKind(section)
		}
		key := diagramSourceKey(*d)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, *d)
	}

	sing, err := parsePlanDiagram(singularPath, singular)
	if err != nil {
		return nil, nil, err
	}
	if sing != nil {
		if sing.Kind == "" {
			sing.Kind = defaultDiagramKind(section)
		}
		key := diagramSourceKey(*sing)
		if _, ok := seen[key]; !ok {
			seen[key] = struct{}{}
			// Prefer elevating singular first when it was the only legacy field,
			// but when diagrams[] already had items, append singular if source differs.
			if len(plural) == 0 {
				out = append([]planDiagram{*sing}, out...)
			} else {
				out = append(out, *sing)
			}
		}
	}

	if len(out) == 0 {
		return nil, nil, nil
	}
	first := out[0]
	return out, &first, nil
}

func isDataDesignSubstantive(summary string) bool {
	s := strings.TrimSpace(summary)
	return s != "" && s != planNAPlaceholder && s != "N/A"
}

func parsePlanFields(entityIdx int, in []planField) ([]planField, error) {
	if len(in) == 0 {
		return nil, nil
	}
	out := make([]planField, 0, len(in))
	for fi, f := range in {
		name := strings.TrimSpace(f.Name)
		if name == "" {
			return nil, fmt.Errorf("data_design.entities[%d].fields[%d].name 不能为空", entityIdx, fi)
		}
		typ := strings.TrimSpace(f.Type)
		if typ == "" {
			return nil, fmt.Errorf("data_design.entities[%d].fields[%d].type 不能为空", entityIdx, fi)
		}
		out = append(out, planField{
			Name:        name,
			Type:        typ,
			PK:          f.PK,
			Nullable:    f.Nullable,
			FK:          strings.TrimSpace(f.FK),
			Description: strings.TrimSpace(f.Description),
		})
	}
	return out, nil
}

func hasERDiagram(dd *planDataDesign) bool {
	if dd == nil {
		return false
	}
	for _, d := range dd.Diagrams {
		if strings.EqualFold(strings.TrimSpace(d.Kind), "er") && strings.TrimSpace(d.Source) != "" {
			return true
		}
	}
	if dd.Diagram != nil && strings.TrimSpace(dd.Diagram.Source) != "" {
		k := strings.TrimSpace(dd.Diagram.Kind)
		// Legacy singular diagram on data_design is treated as ER when kind empty.
		if k == "" || strings.EqualFold(k, "er") {
			return true
		}
	}
	return false
}

func validateDataDesignHardGate(dd *planDataDesign) error {
	if dd == nil || !isDataDesignSubstantive(dd.Summary) {
		return nil
	}
	if !hasERDiagram(dd) {
		return errors.New("data_design 实质内容须至少一张 ER 图(diagrams[] 中 kind=er，或兼容单数 diagram)")
	}
	if len(dd.Entities) == 0 {
		return errors.New("data_design.entities 不能为空")
	}
	for i, e := range dd.Entities {
		if len(e.Fields) == 0 {
			return fmt.Errorf("data_design.entities[%d].fields 不能为空", i)
		}
	}
	return nil
}

func parseArchitecture(in *planArchitecture) (*planArchitecture, error) {
	if in == nil {
		return nil, nil
	}
	summary := strings.TrimSpace(in.Summary)
	diagrams, diagram, err := mergeSectionDiagrams("architecture", "architecture.diagram", in.Diagram, in.Diagrams)
	if err != nil {
		return nil, err
	}
	if summary == "" && len(diagrams) == 0 {
		return nil, nil
	}
	return &planArchitecture{Summary: summary, Diagrams: diagrams, Diagram: diagram}, nil
}

func parseDataDesign(in *planDataDesign) (*planDataDesign, error) {
	if in == nil {
		return nil, nil
	}
	summary := strings.TrimSpace(in.Summary)
	diagrams, diagram, err := mergeSectionDiagrams("data_design", "data_design.diagram", in.Diagram, in.Diagrams)
	if err != nil {
		return nil, err
	}
	var entities []planEntity
	for i, e := range in.Entities {
		name := strings.TrimSpace(e.Name)
		if name == "" {
			return nil, fmt.Errorf("data_design.entities[%d] 缺少 name", i)
		}
		fields, err := parsePlanFields(i, e.Fields)
		if err != nil {
			return nil, err
		}
		ent := planEntity{
			Name:        name,
			Fields:      fields,
			Description: strings.TrimSpace(e.Description),
		}
		for _, a := range e.Attributes {
			if t := strings.TrimSpace(a); t != "" {
				ent.Attributes = append(ent.Attributes, t)
			}
		}
		for _, r := range e.Relationships {
			if t := strings.TrimSpace(r); t != "" {
				ent.Relationships = append(ent.Relationships, t)
			}
		}
		entities = append(entities, ent)
	}
	var rels []string
	for _, r := range in.Relationships {
		if t := strings.TrimSpace(r); t != "" {
			rels = append(rels, t)
		}
	}
	if summary == "" && len(diagrams) == 0 && len(entities) == 0 && len(rels) == 0 {
		return nil, nil
	}
	out := &planDataDesign{
		Summary:       summary,
		Entities:      entities,
		Relationships: rels,
		Diagrams:      diagrams,
		Diagram:       diagram,
	}
	if err := validateDataDesignHardGate(out); err != nil {
		return nil, err
	}
	return out, nil
}

func parseInterfaces(in []planInterfaceItem) ([]planInterfaceItem, error) {
	if in == nil {
		return nil, nil
	}
	out := make([]planInterfaceItem, 0, len(in))
	for i, item := range in {
		name := strings.TrimSpace(item.Name)
		if name == "" {
			return nil, fmt.Errorf("interfaces[%d] 缺少 name", i)
		}
		prefix := fmt.Sprintf("interfaces[%d]", i)
		diagrams, diagram, err := mergeSectionDiagrams(prefix, prefix+".diagram", item.Diagram, item.Diagrams)
		if err != nil {
			return nil, err
		}
		out = append(out, planInterfaceItem{
			Name:      name,
			Kind:      strings.TrimSpace(item.Kind),
			Direction: strings.TrimSpace(item.Direction),
			Summary:   strings.TrimSpace(item.Summary),
			Detail:    strings.TrimSpace(item.Detail),
			Diagrams:  diagrams,
			Diagram:   diagram,
		})
	}
	return out, nil
}

func parseComponents(in []planComponentItem) ([]planComponentItem, error) {
	if in == nil {
		return nil, nil
	}
	out := make([]planComponentItem, 0, len(in))
	for i, item := range in {
		name := strings.TrimSpace(item.Name)
		if name == "" {
			return nil, fmt.Errorf("components[%d] 缺少 name", i)
		}
		prefix := fmt.Sprintf("components[%d]", i)
		diagrams, diagram, err := mergeSectionDiagrams(prefix, prefix+".diagram", item.Diagram, item.Diagrams)
		if err != nil {
			return nil, err
		}
		c := planComponentItem{
			Name:           name,
			Responsibility: strings.TrimSpace(item.Responsibility),
			Detail:         strings.TrimSpace(item.Detail),
			Diagrams:       diagrams,
			Diagram:        diagram,
		}
		for _, d := range item.Dependencies {
			if t := strings.TrimSpace(d); t != "" {
				c.Dependencies = append(c.Dependencies, t)
			}
		}
		out = append(out, c)
	}
	return out, nil
}

func parseInteraction(in *planInteraction) (*planInteraction, error) {
	if in == nil {
		return nil, nil
	}
	summary := strings.TrimSpace(in.Summary)
	diagrams, diagram, err := mergeSectionDiagrams("interaction", "interaction.diagram", in.Diagram, in.Diagrams)
	if err != nil {
		return nil, err
	}
	if summary == "" && len(diagrams) == 0 {
		return nil, nil
	}
	return &planInteraction{Summary: summary, Diagrams: diagrams, Diagram: diagram}, nil
}

// parsePlan coerces the loosely-typed set_plan arguments into a normalized
// planDoc: it enforces the two-level limit (a subgoal may not carry its own
// subgoals), requires non-empty titles, and assigns stable ids (g1, g1.2) plus
// an initial pending status to every item. Optional SDD design sections are
// validated when present; missing design keys remain compatible with goals-only plans.
func parsePlan(args map[string]any) (planDoc, error) {
	raw, _ := json.Marshal(args)
	var in struct {
		Title        string              `json:"title"`
		Architecture *planArchitecture   `json:"architecture"`
		DataDesign   *planDataDesign     `json:"data_design"`
		Interfaces   []planInterfaceItem `json:"interfaces"`
		Components   []planComponentItem `json:"components"`
		Interaction  *planInteraction    `json:"interaction"`
		TestDesign   *string             `json:"test_design"`
		Goals        []struct {
			Title    string `json:"title"`
			Detail   string `json:"detail"`
			Status   string `json:"status"`
			Subgoals []struct {
				Title    string          `json:"title"`
				Detail   string          `json:"detail"`
				Status   string          `json:"status"`
				Subgoals json.RawMessage `json:"subgoals"`
			} `json:"subgoals"`
		} `json:"goals"`
	}
	if err := json.Unmarshal(raw, &in); err != nil {
		return planDoc{}, fmt.Errorf("解析计划失败: %w", err)
	}
	if len(in.Goals) == 0 {
		return planDoc{}, errors.New("goals 不能为空")
	}

	arch, err := parseArchitecture(in.Architecture)
	if err != nil {
		return planDoc{}, err
	}
	data, err := parseDataDesign(in.DataDesign)
	if err != nil {
		return planDoc{}, err
	}
	ifaces, err := parseInterfaces(in.Interfaces)
	if err != nil {
		return planDoc{}, err
	}
	comps, err := parseComponents(in.Components)
	if err != nil {
		return planDoc{}, err
	}
	ix, err := parseInteraction(in.Interaction)
	if err != nil {
		return planDoc{}, err
	}
	testDesign := ""
	if in.TestDesign != nil {
		testDesign = strings.TrimSpace(*in.TestDesign)
	}

	doc := planDoc{
		Title:        strings.TrimSpace(in.Title),
		Architecture: arch,
		DataDesign:   data,
		Interfaces:   ifaces,
		Components:   comps,
		Interaction:  ix,
		TestDesign:   testDesign,
	}
	for gi, g := range in.Goals {
		if strings.TrimSpace(g.Title) == "" {
			return planDoc{}, fmt.Errorf("第 %d 个大目标缺少 title", gi+1)
		}
		goal := planGoal{ID: fmt.Sprintf("g%d", gi+1), Title: strings.TrimSpace(g.Title),
			Detail: strings.TrimSpace(g.Detail), Status: normPlanStatus(g.Status)}
		for si, s := range g.Subgoals {
			if len(s.Subgoals) > 0 {
				return planDoc{}, errors.New("计划最多支持两级(大目标→小目标),小目标下不能再有子目标")
			}
			if strings.TrimSpace(s.Title) == "" {
				return planDoc{}, fmt.Errorf("大目标 g%d 的第 %d 个小目标缺少 title", gi+1, si+1)
			}
			goal.Subgoals = append(goal.Subgoals, planSub{ID: fmt.Sprintf("g%d.%d", gi+1, si+1),
				Title: strings.TrimSpace(s.Title), Detail: strings.TrimSpace(s.Detail), Status: normPlanStatus(s.Status)})
		}
		doc.Goals = append(doc.Goals, goal)
	}
	return doc, nil
}

// applyPlanStatus sets the status of the item identified by id and reports
// whether an item matched. id may be a leaf (a subgoal, or a big goal with no
// subgoals) or a big goal that has subgoals. For a parent goal:
//   - status "done" cascades to mark all its subgoals done (so the plan-
//     completion contract, which only inspects leaf items, is satisfied);
//   - status "in_progress"/"pending" sets the parent's own displayed status
//     without touching its subgoals (so a just-started goal shows progress and
//     already-done subgoals are never regressed).
//
// Every other parent goal's status is rolled up from its subgoals.
// Design sections are never modified.
func applyPlanStatus(doc *planDoc, id, status string) bool {
	found := false
	for gi := range doc.Goals {
		g := &doc.Goals[gi]
		if len(g.Subgoals) == 0 {
			if g.ID == id {
				g.Status = status
				found = true
			}
			continue
		}
		parentHit := g.ID == id
		if parentHit && status == planStatusDone {
			for si := range g.Subgoals {
				g.Subgoals[si].Status = planStatusDone
			}
		}
		for si := range g.Subgoals {
			if g.Subgoals[si].ID == id {
				g.Subgoals[si].Status = status
				found = true
			}
		}
		if parentHit && status != planStatusDone {
			// Keep the manually-set parent status; skip the rollup that would
			// otherwise derive it from (unchanged) subgoals.
			g.Status = status
			found = true
			continue
		}
		if parentHit {
			found = true
		}
		g.Status = rollupStatus(g.Subgoals)
	}
	return found
}

// rollupStatus derives a parent goal's status from its subgoals: done when all
// are done, pending when all are pending, otherwise in_progress.
func rollupStatus(subs []planSub) string {
	if len(subs) == 0 {
		return planStatusPending
	}
	allDone, allPending := true, true
	for _, s := range subs {
		if s.Status != planStatusDone {
			allDone = false
		}
		if s.Status != planStatusPending {
			allPending = false
		}
	}
	switch {
	case allDone:
		return planStatusDone
	case allPending:
		return planStatusPending
	default:
		return planStatusInProgress
	}
}

// forEachPlanLeaf visits every leaf item: a goal with no subgoals, or each
// subgoal under a parent. Leaf id rules must stay aligned with ensurePlanComplete
// / planIncomplete and the plan_coverage gate. Design sections are ignored.
func forEachPlanLeaf(doc planDoc, fn func(id, title, status string)) {
	for _, g := range doc.Goals {
		if len(g.Subgoals) == 0 {
			fn(g.ID, g.Title, g.Status)
			continue
		}
		for _, s := range g.Subgoals {
			fn(s.ID, s.Title, s.Status)
		}
	}
}

// planLeafIDs returns leaf item ids in stable plan order.
func planLeafIDs(doc planDoc) []string {
	var out []string
	forEachPlanLeaf(doc, func(id, _, _ string) {
		out = append(out, id)
	})
	return out
}

// PlanLeafIDs returns leaf ids from a plan.json payload. Empty, malformed, or
// leaf-less plans yield nil/empty so callers can fail-open (no coverage required).
func PlanLeafIDs(content string) []string {
	var doc planDoc
	if json.Unmarshal([]byte(content), &doc) != nil || len(doc.Goals) == 0 {
		return nil
	}
	return planLeafIDs(doc)
}

// planIncomplete returns human-readable descriptions of every leaf item not yet
// done (subgoals, plus big goals that have no subgoals). Empty ⇒ plan complete.
func planIncomplete(doc planDoc) []string {
	var out []string
	forEachPlanLeaf(doc, func(id, title, status string) {
		if status != planStatusDone {
			out = append(out, fmt.Sprintf("%s %s(%s)", id, title, status))
		}
	})
	return out
}

func hasDesignSections(doc planDoc) bool {
	return doc.Architecture != nil ||
		doc.DataDesign != nil ||
		len(doc.Interfaces) > 0 ||
		len(doc.Components) > 0 ||
		doc.Interaction != nil ||
		doc.TestDesign != ""
}

func renderDiagramMarkdown(b *strings.Builder, path string, d *planDiagram) {
	if d == nil {
		return
	}
	meta := d.Format
	if d.Kind != "" {
		meta = d.Kind + "/" + meta
	}
	label := path
	if d.Title != "" {
		label = path + " " + d.Title
	}
	b.WriteString(fmt.Sprintf("- `%s` (%s)", label, meta))
	if d.Scope != "" {
		b.WriteString(" scope=`" + d.Scope + "`")
	}
	b.WriteString("\n")
	src := d.Source
	if len(src) > 120 {
		src = src[:117] + "..."
	}
	b.WriteString("  ```\n  " + strings.ReplaceAll(src, "\n", "\n  ") + "\n  ```\n")
	if d.Caption != "" {
		b.WriteString("  _" + d.Caption + "_\n")
	}
	if d.FallbackArtifact != "" {
		b.WriteString("  fallback: `" + d.FallbackArtifact + "`\n")
	}
}

func renderDiagramsMarkdown(b *strings.Builder, section string, diagrams []planDiagram, legacy *planDiagram) {
	if len(diagrams) > 0 {
		for i, d := range diagrams {
			d := d
			renderDiagramMarkdown(b, fmt.Sprintf("%s.diagrams[%d]", section, i), &d)
		}
		return
	}
	renderDiagramMarkdown(b, section+".diagram", legacy)
}

// RenderPlanMarkdown turns a plan.json content string into a human-readable
// GitHub-flavored task list (checkbox per item, with status chips), so the plan
// can be surfaced verbatim in a human_gate body or any markdown consumer. When
// design sections are present they are rendered above the goals tree; goals-only
// plans keep the historical output. On any parse error it returns the raw content unchanged.
func RenderPlanMarkdown(content string) string {
	var doc planDoc
	if err := json.Unmarshal([]byte(content), &doc); err != nil || len(doc.Goals) == 0 {
		return content
	}
	var b strings.Builder
	if doc.Title != "" {
		b.WriteString("### " + doc.Title + "\n\n")
	}
	if hasDesignSections(doc) {
		b.WriteString("#### 设计区\n\n")
		if doc.Architecture != nil {
			sum := doc.Architecture.Summary
			if sum == "" {
				sum = planNAPlaceholder
			}
			b.WriteString("**Architecture** — " + sum + "\n")
			renderDiagramsMarkdown(&b, "architecture", doc.Architecture.Diagrams, doc.Architecture.Diagram)
			b.WriteString("\n")
		}
		if doc.DataDesign != nil {
			sum := doc.DataDesign.Summary
			if sum == "" {
				sum = planNAPlaceholder
			}
			b.WriteString("**Data design** — " + sum + "\n")
			for _, e := range doc.DataDesign.Entities {
				b.WriteString("- entity `" + e.Name + "`")
				if e.Description != "" {
					b.WriteString(": " + e.Description)
				}
				b.WriteString("\n")
				for _, f := range e.Fields {
					line := "  - field `" + f.Name + "` " + f.Type
					var tags []string
					if f.PK != nil && *f.PK {
						tags = append(tags, "pk")
					}
					if f.Nullable != nil && *f.Nullable {
						tags = append(tags, "nullable")
					}
					if f.FK != "" {
						tags = append(tags, "fk→"+f.FK)
					}
					if len(tags) > 0 {
						line += " (" + strings.Join(tags, ", ") + ")"
					}
					if f.Description != "" {
						line += " — " + f.Description
					}
					b.WriteString(line + "\n")
				}
				for _, a := range e.Attributes {
					b.WriteString("  - attr(legacy) `" + a + "`\n")
				}
				for _, r := range e.Relationships {
					b.WriteString("  - rel: " + r + "\n")
				}
			}
			for _, r := range doc.DataDesign.Relationships {
				b.WriteString("- relationship: " + r + "\n")
			}
			renderDiagramsMarkdown(&b, "data_design", doc.DataDesign.Diagrams, doc.DataDesign.Diagram)
			b.WriteString("\n")
		}
		if len(doc.Interfaces) > 0 {
			b.WriteString("**Interfaces**\n")
			for i, it := range doc.Interfaces {
				line := "- `" + it.Name + "`"
				if it.Summary != "" {
					line += " — " + it.Summary
				}
				b.WriteString(line + "\n")
				renderDiagramsMarkdown(&b, fmt.Sprintf("interfaces[%d]", i), it.Diagrams, it.Diagram)
			}
			b.WriteString("\n")
		}
		if len(doc.Components) > 0 {
			b.WriteString("**Components**\n")
			for i, c := range doc.Components {
				line := "- `" + c.Name + "`"
				if c.Responsibility != "" {
					line += " — " + c.Responsibility
				}
				b.WriteString(line + "\n")
				renderDiagramsMarkdown(&b, fmt.Sprintf("components[%d]", i), c.Diagrams, c.Diagram)
			}
			b.WriteString("\n")
		}
		if doc.Interaction != nil {
			sum := doc.Interaction.Summary
			if sum == "" {
				sum = planNAPlaceholder
			}
			b.WriteString("**Interaction** — " + sum + "\n")
			renderDiagramsMarkdown(&b, "interaction", doc.Interaction.Diagrams, doc.Interaction.Diagram)
			b.WriteString("\n")
		}
		if doc.TestDesign != "" {
			b.WriteString("**Test design** — " + doc.TestDesign + "\n\n")
		}
		b.WriteString("#### 任务\n\n")
	}
	for _, g := range doc.Goals {
		b.WriteString(planMarkdownLine("", g.ID, g.Title, g.Detail, g.Status))
		for _, s := range g.Subgoals {
			b.WriteString(planMarkdownLine("  ", s.ID, s.Title, s.Detail, s.Status))
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

func planMarkdownLine(indent, id, title, detail, status string) string {
	box := " "
	if status == planStatusDone {
		box = "x"
	}
	line := fmt.Sprintf("%s- [%s] `%s` %s", indent, box, id, title)
	switch status {
	case planStatusInProgress:
		line += " _(进行中)_"
	case planStatusPending:
		line += " _(待办)_"
	}
	if detail != "" {
		line += " — " + detail
	}
	return line + "\n"
}
