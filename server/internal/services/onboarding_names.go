package services

import (
	"strings"

	"github.com/cocofhu/grasp/internal/models"
)

// OnboardingAgentNames are the canonical onboarding Agent names: the built-in
// template labels, which the default workflow's agent_profile refs use.
var OnboardingAgentNames = func() []string {
	out := make([]string, 0, len(TeamEngineerTemplates))
	for _, r := range TeamEngineerTemplates {
		out = append(out, r.RoleLabelZH)
	}
	return out
}()

// onboardingLocale is the wizard-language copy of what onboarding names.
type onboardingLocale struct {
	WorkflowName string
	WorkflowDesc string
	InputLabel   string
	OutputLabel  string
}

var (
	onboardingLocaleZH = onboardingLocale{
		WorkflowName: OnboardingWorkflowName,
		WorkflowDesc: "需求澄清 → 实现 → 测试评审 → 提交 MR/PR;测试评审未通过时回到实现。仓库在运行时填写。",
		InputLabel:   "开始",
		OutputLabel:  "结束",
	}
	onboardingLocaleEN = onboardingLocale{
		WorkflowName: OnboardingWorkflowNameEN,
		WorkflowDesc: "Clarify → Implement → Test & review → Submit MR/PR; a failed review goes back to Implement. Pick the repository when you start a run.",
		InputLabel:   "Start",
		OutputLabel:  "End",
	}
	// onboardingLocales lists every language so re-running onboarding in another
	// language still finds the workflow it published before.
	onboardingLocales = []onboardingLocale{onboardingLocaleZH, onboardingLocaleEN}
)

// onboardingLocaleFor picks the copy for a UI language; anything but English is Chinese.
func onboardingLocaleFor(lang string) onboardingLocale {
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(lang)), "en") {
		return onboardingLocaleEN
	}
	return onboardingLocaleZH
}

// isOnboardingWorkflowName reports whether name is the default workflow in any language.
func isOnboardingWorkflowName(name string) bool {
	for _, l := range onboardingLocales {
		if name == l.WorkflowName {
			return true
		}
	}
	return false
}

// localizeOnboardingWorkflow names the embedded default workflow and its start/end nodes.
func localizeOnboardingWorkflow(env *models.ExportEnvelope, loc onboardingLocale) {
	env.Name = loc.WorkflowName
	env.Description = loc.WorkflowDesc
	for i := range env.Graph.Nodes {
		switch env.Graph.Nodes[i].Type {
		case "input":
			env.Graph.Nodes[i].Label = loc.InputLabel
		case "output":
			env.Graph.Nodes[i].Label = loc.OutputLabel
		}
	}
}
