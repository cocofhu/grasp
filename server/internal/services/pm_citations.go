package services

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/cocofhu/grasp/internal/models"

	"github.com/rs/zerolog/log"
)

// Citation ID shape rules (mirrored on the frontend CitationCard).
var (
	pmRunIDRe        = regexp.MustCompile(`(?i)^run-[0-9a-f]{8}$`)
	pmWorkflowIDRe   = regexp.MustCompile(`(?i)^wf-[0-9a-f]{8}$`)
	pmArtifactIDRe   = regexp.MustCompile(`(?i)^art-[0-9a-f]{8}$`)
	pmArtifactNameRe = regexp.MustCompile(`(?i)^[a-z0-9][a-z0-9._-]*\.[a-z0-9]{1,16}$`)
	pmGateTargetRe   = regexp.MustCompile(`(?i)^run-[0-9a-f]{8}(?::[a-z0-9][a-z0-9_.-]*)?$`)
	pmPlanIDRe       = regexp.MustCompile(`(?i)^g\d+(?:\.\d+)?$`)
	pmPlanScopedRe   = regexp.MustCompile(`(?i)^run-[0-9a-f]{8}:g\d+(?:\.\d+)?$`)

	// Broad discoverer; shape is enforced per type after match.
	pmCitationDiscoverRe = regexp.MustCompile(`(?i)\b(run|gate|artifact|workflow|plan)[:\s]+([a-zA-Z0-9_./:-]+)`)
)

const pmCitationFilterTimeout = 2 * time.Second

// validPmCitationShape reports whether targetId matches the type's ID morphology.
func validPmCitationShape(typ, targetID string) bool {
	typ = strings.ToLower(strings.TrimSpace(typ))
	targetID = strings.TrimSpace(targetID)
	if targetID == "" {
		return false
	}
	switch typ {
	case "run":
		return pmRunIDRe.MatchString(targetID)
	case "workflow":
		return pmWorkflowIDRe.MatchString(targetID)
	case "artifact":
		return pmArtifactIDRe.MatchString(targetID) || pmArtifactNameRe.MatchString(targetID)
	case "gate":
		return pmGateTargetRe.MatchString(targetID)
	case "plan":
		return pmPlanIDRe.MatchString(targetID) || pmPlanScopedRe.MatchString(targetID)
	default:
		return false
	}
}

func extractPmCitations(text string) []models.ProgressCitation {
	seen := map[string]struct{}{}
	out := make([]models.ProgressCitation, 0, 8)
	for _, m := range pmCitationDiscoverRe.FindAllStringSubmatch(text, -1) {
		if len(m) < 3 {
			continue
		}
		typ := strings.ToLower(m[1])
		target := strings.TrimSpace(m[2])
		// Strip trailing punctuation commonly glued to prose tokens.
		target = strings.TrimRight(target, ".,;:!?)]}\"'")
		if !validPmCitationShape(typ, target) {
			continue
		}
		key := typ + ":" + target
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, models.ProgressCitation{
			Type:     typ,
			TargetID: target,
			// SummarySnippet filled later by enrich; leave empty so frontend
			// falls back to readable defaults (e.g. "Run #<short>").
		})
		if len(out) >= 8 {
			break
		}
	}
	return out
}

// SetCitationDeps wires project-scoped existence checks used before AppendMessage.
// Nil deps are fail-closed: shape-valid candidates are dropped rather than written.
func (r *PmTurnRunner) SetCitationDeps(runs *RunService, arts *ArtifactService, wf *WorkflowService) {
	r.mu.Lock()
	r.runs = runs
	r.arts = arts
	r.wf = wf
	r.mu.Unlock()
}

func (r *PmTurnRunner) citationDeps() (*RunService, *ArtifactService, *WorkflowService) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.runs, r.arts, r.wf
}

// filterAndEnrichCitations drops citations that fail project-scoped existence
// checks (fail-closed on missing deps / lookup errors) and fills SummarySnippet
// for survivors. Message body writing is unaffected by failures here.
func (r *PmTurnRunner) filterAndEnrichCitations(threadID string, citations []models.ProgressCitation) []models.ProgressCitation {
	if len(citations) == 0 {
		return nil
	}
	projectID := ""
	if r.pm != nil {
		if thr, err := r.pm.GetThreadByID(threadID); err == nil {
			projectID = thr.ProjectID
		} else {
			log.Warn().Err(err).Str("thread", threadID).
				Msg("pm citation filter: thread lookup failed; dropping citations")
			return nil
		}
	}
	if projectID == "" {
		log.Warn().Str("thread", threadID).
			Msg("pm citation filter: empty project; dropping citations")
		return nil
	}

	runs, arts, wf := r.citationDeps()
	if runs == nil {
		log.Warn().Str("thread", threadID).Str("project", projectID).
			Msg("pm citation filter: run service unavailable; dropping citations")
		return nil
	}

	deadline := time.Now().Add(pmCitationFilterTimeout)
	out := make([]models.ProgressCitation, 0, len(citations))
	for _, c := range citations {
		if time.Now().After(deadline) {
			log.Warn().Str("thread", threadID).Str("project", projectID).
				Str("type", c.Type).Str("target", c.TargetID).
				Msg("pm citation filter: timeout budget exceeded; dropping remaining")
			break
		}
		if !validPmCitationShape(c.Type, c.TargetID) {
			log.Info().Str("thread", threadID).Str("type", c.Type).Str("target", c.TargetID).
				Msg("pm citation filter: invalid shape dropped")
			continue
		}
		ok, snippet := r.existsCitationInProject(projectID, c, runs, arts, wf)
		if !ok {
			log.Info().Str("thread", threadID).Str("project", projectID).
				Str("type", c.Type).Str("target", c.TargetID).
				Msg("pm citation filter: target missing or unverifiable; dropped")
			continue
		}
		if snippet != "" {
			c.SummarySnippet = snippet
		}
		out = append(out, c)
	}
	return out
}

func (r *PmTurnRunner) existsCitationInProject(
	projectID string,
	c models.ProgressCitation,
	runs *RunService,
	arts *ArtifactService,
	wf *WorkflowService,
) (ok bool, snippet string) {
	switch strings.ToLower(c.Type) {
	case "run":
		return existRunCitation(projectID, c.TargetID, runs)
	case "workflow":
		return existWorkflowCitation(projectID, c.TargetID, wf)
	case "artifact":
		return existArtifactCitation(projectID, c.TargetID, arts, runs)
	case "gate":
		return existGateCitation(projectID, c.TargetID, runs)
	case "plan":
		return existPlanCitation(projectID, c.TargetID, runs, arts)
	default:
		return false, ""
	}
}

func existRunCitation(projectID, runID string, runs *RunService) (bool, string) {
	if runs == nil {
		return false, ""
	}
	run, found := runs.Get(runID)
	if !found {
		return false, ""
	}
	if !runBelongsToProject(runs, projectID, run) {
		return false, ""
	}
	return true, runCitationSnippet(run)
}

func existWorkflowCitation(projectID, wfID string, wf *WorkflowService) (bool, string) {
	if wf == nil {
		return false, ""
	}
	def, found := wf.Get(wfID)
	if !found {
		return false, ""
	}
	if projectID != "" && def.ProjectID != projectID {
		return false, ""
	}
	snippet := strings.TrimSpace(def.Name)
	if snippet == "" {
		snippet = "Workflow " + shortID(wfID, "wf-")
	}
	return true, snippet
}

func existArtifactCitation(projectID, target string, arts *ArtifactService, runs *RunService) (bool, string) {
	if arts == nil {
		return false, ""
	}
	if pmArtifactIDRe.MatchString(target) {
		a, found := arts.GetByID(target)
		if !found {
			return false, ""
		}
		if runs != nil {
			if run, ok := runs.Get(a.RunID); ok && !runBelongsToProject(runs, projectID, run) {
				return false, ""
			}
		}
		name := strings.TrimSpace(a.Name)
		if name == "" {
			name = target
		}
		return true, name
	}
	// Name lookup within project scope.
	items, _ := arts.AllPage("", projectID, 1, 50, target)
	for _, a := range items {
		if strings.EqualFold(a.Name, target) {
			return true, a.Name
		}
	}
	return false, ""
}

func existGateCitation(projectID, target string, runs *RunService) (bool, string) {
	if runs == nil {
		return false, ""
	}
	runID, nodeID, _ := strings.Cut(target, ":")
	run, found := runs.Get(runID)
	if !found || !runBelongsToProject(runs, projectID, run) {
		return false, ""
	}
	if nodeID == "" {
		if g, ok := runs.PendingGate(runID); ok {
			title := strings.TrimSpace(g.Title)
			if title == "" {
				title = "Gate · " + runCitationSnippet(run)
			}
			return true, title
		}
		// Run-scoped gate reference without node: accept if run exists (may be resolved).
		return true, "Gate · " + runCitationSnippet(run)
	}
	if g, ok := runs.PendingGate(runID); ok && g.NodeID == nodeID {
		title := strings.TrimSpace(g.Title)
		if title == "" {
			title = fmt.Sprintf("Gate %s · %s", nodeID, runCitationSnippet(run))
		}
		return true, title
	}
	if _, ok := runs.StateRun(runID, nodeID); ok {
		return true, fmt.Sprintf("Gate %s · %s", nodeID, runCitationSnippet(run))
	}
	return false, ""
}

func existPlanCitation(projectID, target string, runs *RunService, arts *ArtifactService) (bool, string) {
	if runs == nil || arts == nil {
		return false, ""
	}
	runID := ""
	planID := target
	if pmPlanScopedRe.MatchString(target) {
		runID, planID, _ = strings.Cut(target, ":")
	}
	if runID != "" {
		run, found := runs.Get(runID)
		if !found || !runBelongsToProject(runs, projectID, run) {
			return false, ""
		}
		if planHasGoalID(arts, runID, planID) {
			return true, planID + " · " + runCitationSnippet(run)
		}
		return false, ""
	}
	// Bare plan id: scan recent project runs for a matching plan.json goal.
	recent := runs.List(nil, "", projectID)
	limit := 30
	if len(recent) < limit {
		limit = len(recent)
	}
	for i := 0; i < limit; i++ {
		run := recent[i]
		if planHasGoalID(arts, run.ID, planID) {
			return true, planID + " · " + runCitationSnippet(run)
		}
	}
	return false, ""
}

func planHasGoalID(arts *ArtifactService, runID, planID string) bool {
	content, ok := arts.Get(runID, "plan.json")
	if !ok || strings.TrimSpace(content) == "" {
		return false
	}
	var plan map[string]any
	if err := json.Unmarshal([]byte(content), &plan); err != nil {
		return false
	}
	return planContainsID(plan, planID)
}

func planContainsID(plan map[string]any, want string) bool {
	want = strings.ToLower(strings.TrimSpace(want))
	goals, _ := plan["goals"].([]any)
	for i, g := range goals {
		gm, ok := g.(map[string]any)
		if !ok {
			continue
		}
		id := strings.ToLower(strAny(gm["id"]))
		if id == "" {
			id = fmt.Sprintf("g%d", i+1)
		}
		if id == want {
			return true
		}
		subs, _ := gm["subgoals"].([]any)
		for j, sg := range subs {
			sm, ok := sg.(map[string]any)
			if !ok {
				continue
			}
			sid := strings.ToLower(strAny(sm["id"]))
			if sid == "" {
				sid = fmt.Sprintf("%s.%d", id, j+1)
			}
			if sid == want {
				return true
			}
		}
	}
	return false
}

func runBelongsToProject(runs *RunService, projectID string, run models.Run) bool {
	if projectID == "" || runs == nil {
		return false
	}
	scoped := runs.List(nil, run.WorkflowID, projectID)
	for _, x := range scoped {
		if x.ID == run.ID {
			return true
		}
	}
	return false
}

func runCitationSnippet(run models.Run) string {
	if t := strings.TrimSpace(run.Title); t != "" {
		return t
	}
	if n := strings.TrimSpace(run.WorkflowName); n != "" {
		status := strings.TrimSpace(run.Status)
		if status != "" {
			return n + " · " + status
		}
		return n
	}
	if s := strings.TrimSpace(run.Status); s != "" {
		return s
	}
	return ""
}

func shortID(id, prefix string) string {
	if strings.HasPrefix(strings.ToLower(id), strings.ToLower(prefix)) {
		return id[len(prefix):]
	}
	return id
}
