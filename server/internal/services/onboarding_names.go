package services

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/cocofhu/grasp/internal/models"

	"golang.org/x/text/unicode/norm"
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

// longestOnboardingRoleSuffixRunes is the longest role name the wizard may
// append in any UI language (TestReview = 10) so prefixed names stay within
// MaxAgentNameRunes.
const longestOnboardingRoleSuffixRunes = 10

// OnboardingNamePlan holds per-project agent / org naming for bootstrap.
type OnboardingNamePlan struct {
	Prefix     string
	GroupID    string
	GroupName  string
	AgentNames []string
	// NameMap maps canonical template names → actual save names.
	NameMap map[string]string
}

// SanitizeOnboardingPrefix washes a project name into a valid Agent-name prefix
// (Unicode letters/digits/_/- only, no whitespace). Truncates so the longest
// onboarding role still fits MaxAgentNameRunes.
func SanitizeOnboardingPrefix(projectName string) (string, error) {
	name := norm.NFC.String(strings.TrimSpace(projectName))
	if name == "" {
		return "", fmt.Errorf("%w: project name is required for onboarding prefix", ErrInvalidAgentName)
	}
	var b strings.Builder
	for _, r := range name {
		if unicode.IsSpace(r) {
			continue
		}
		switch r {
		case '.', '/', '\\':
			continue
		}
		if fullwidthPunctRe.MatchString(string(r)) {
			continue
		}
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' || r == '-' {
			b.WriteRune(r)
		}
	}
	out := b.String()
	if out == "" {
		return "", fmt.Errorf("%w: project name yields empty onboarding prefix", ErrInvalidAgentName)
	}
	maxPrefix := MaxAgentNameRunes - longestOnboardingRoleSuffixRunes
	if maxPrefix < 1 {
		maxPrefix = 1
	}
	if utf8.RuneCountInString(out) > maxPrefix {
		runes := []rune(out)
		out = string(runes[:maxPrefix])
	}
	if _, err := NormalizeAndValidateAgentName(out); err != nil {
		return "", err
	}
	return out, nil
}

// BuildOnboardingNamePlan derives Agent / group names for a project.
// The default project keeps the bare template names and FirstInstallGroupID;
// other projects prefix them with the project name.
func BuildOnboardingNamePlan(projectID, projectName, defaultProjectID string) (OnboardingNamePlan, error) {
	projectID = strings.TrimSpace(projectID)
	defaultProjectID = strings.TrimSpace(defaultProjectID)
	if defaultProjectID == "" {
		defaultProjectID = "proj-default"
	}
	if projectID == defaultProjectID {
		m := make(map[string]string, len(OnboardingAgentNames))
		names := make([]string, len(OnboardingAgentNames))
		for i, n := range OnboardingAgentNames {
			names[i] = n
			m[n] = n
		}
		return OnboardingNamePlan{
			Prefix:     "",
			GroupID:    FirstInstallGroupID,
			GroupName:  FirstInstallGroupName,
			AgentNames: names,
			NameMap:    m,
		}, nil
	}
	prefix, err := SanitizeOnboardingPrefix(projectName)
	if err != nil {
		return OnboardingNamePlan{}, err
	}
	displayName := strings.TrimSpace(projectName)
	if displayName == "" {
		displayName = prefix
	}
	m := make(map[string]string, len(OnboardingAgentNames))
	names := make([]string, 0, len(OnboardingAgentNames))
	for _, canonical := range OnboardingAgentNames {
		derived := prefix + canonical
		normalized, nerr := NormalizeAndValidateAgentName(derived)
		if nerr != nil {
			return OnboardingNamePlan{}, fmt.Errorf("derive %s: %w", canonical, nerr)
		}
		m[canonical] = normalized
		names = append(names, normalized)
	}
	return OnboardingNamePlan{
		Prefix:     prefix,
		GroupID:    "g_onb_" + projectID,
		GroupName:  displayName + "项目组",
		AgentNames: names,
		NameMap:    m,
	}, nil
}

// RemapOnboardingAgentProfiles rewrites the default workflow's agent_profile refs.
func RemapOnboardingAgentProfiles(g *models.Graph, nameMap map[string]string) {
	if g == nil || len(nameMap) == 0 {
		return
	}
	for oldName, newName := range nameMap {
		if oldName == "" || newName == "" || oldName == newName {
			continue
		}
		renameAgentProfileInGraph(g, oldName, newName)
	}
}
