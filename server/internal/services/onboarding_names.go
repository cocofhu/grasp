package services

import (
	"strings"

	"github.com/cocofhu/grasp/internal/models"
)

// OnboardingAgentNames are the Chinese canonical onboarding Agent names: the
// built-in template labels, which the default workflow's agent_profile refs use.
var OnboardingAgentNames = func() []string {
	out := make([]string, 0, len(TeamEngineerTemplates))
	for _, r := range TeamEngineerTemplates {
		out = append(out, r.RoleLabelZH)
	}
	return out
}()

// OnboardingAgentNamesEN are the English canonical names, in the same role order.
var OnboardingAgentNamesEN = []string{"Clarify", "Implement", "TestReview", "Deliver"}

func init() {
	if len(OnboardingAgentNames) != len(TeamEngineerTemplates) || len(OnboardingAgentNamesEN) != len(TeamEngineerTemplates) {
		panic("onboarding canonical names drifted from TeamEngineerTemplates")
	}
}

// IsProtectedOnboardingAgentName reports whether name is a Chinese or English
// canonical onboarding Agent. Identity is the name; there is no lock field.
func IsProtectedOnboardingAgentName(name string) bool {
	name = strings.TrimSpace(name)
	if name == "" {
		return false
	}
	for _, n := range OnboardingAgentNames {
		if n == name {
			return true
		}
	}
	for _, n := range OnboardingAgentNamesEN {
		if n == name {
			return true
		}
	}
	return false
}

// canonicalOnboardingNames returns the four names for a wizard language.
// Anything but English is Chinese.
func canonicalOnboardingNames(lang string) []string {
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(lang)), "en") {
		return OnboardingAgentNamesEN
	}
	return OnboardingAgentNames
}

// BaselineOnboardingSet reports which canonical quartet can be bound.
// Chinese wins when both sets exist. Empty means neither set is complete.
func BaselineOnboardingSet(names []string) string {
	have := map[string]struct{}{}
	for _, n := range names {
		n = strings.TrimSpace(n)
		if n != "" {
			have[n] = struct{}{}
		}
	}
	if onboardingSetComplete(have, OnboardingAgentNames) {
		return "zh"
	}
	if onboardingSetComplete(have, OnboardingAgentNamesEN) {
		return "en"
	}
	return ""
}

func onboardingSetComplete(have map[string]struct{}, set []string) bool {
	if len(set) == 0 {
		return false
	}
	for _, n := range set {
		if _, ok := have[n]; !ok {
			return false
		}
	}
	return true
}

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
