package mcp

import (
	"fmt"
	"strings"
)

// reservedArtifactExpectedKind maps platform contract / reserved artifact names
// to the only kind write_artifact may store for them.
var reservedArtifactExpectedKind = map[string]string{
	"page.html":                          "html",
	PlanArtifactName:                     "json",
	ClarifiedRequirementArtifactName:     "json",
	ResearchArtifactName:                 "json",
	ProposalsArtifactName:                "json",
	ProposalArtifactName:                 "json",
	TestResultArtifactName:               "json",
	ReviewArtifactName:                   "json",
	ImplementationResultArtifactName:     "json",
	PreflightArtifactName:                "json",
	NodeOutcomeArtifactName:              "json",
	FeedbackIndexArtifactName:            "json",
}

// ExpectedKindForReservedName returns the expected kind for a platform
// reserved/contract artifact name, or false when name is not reserved.
// Feedback ledger round files (feedback.*) are treated as json.
func ExpectedKindForReservedName(name string) (string, bool) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", false
	}
	if kind, ok := reservedArtifactExpectedKind[name]; ok {
		return kind, true
	}
	if IsFeedbackArtifactName(name) {
		return "json", true
	}
	return "", false
}

// InferWriteArtifactKind picks a text (or image) kind from the file extension.
// Unknown / missing extensions default to markdown to preserve the historical
// empty-kind behavior for freeform names.
func InferWriteArtifactKind(name string) string {
	n := strings.ToLower(strings.TrimSpace(name))
	switch {
	case strings.HasSuffix(n, ".json"):
		return "json"
	case strings.HasSuffix(n, ".yaml"), strings.HasSuffix(n, ".yml"):
		return "yaml"
	case strings.HasSuffix(n, ".html"), strings.HasSuffix(n, ".htm"):
		return "html"
	case strings.HasSuffix(n, ".md"), strings.HasSuffix(n, ".markdown"):
		return "markdown"
	case strings.HasSuffix(n, ".txt"):
		return "text"
	case strings.HasSuffix(n, ".png"),
		strings.HasSuffix(n, ".jpg"),
		strings.HasSuffix(n, ".jpeg"),
		strings.HasSuffix(n, ".webp"),
		strings.HasSuffix(n, ".gif"):
		return "image"
	default:
		return "markdown"
	}
}

// ResolveWriteArtifactKind normalizes kind for write_artifact before Save:
// empty kind is inferred (reserved name first, else extension); kind=image is
// always rejected; an explicit kind that disagrees with a reserved name fails.
func ResolveWriteArtifactKind(name, kind string) (string, error) {
	name = strings.TrimSpace(name)
	kind = strings.ToLower(strings.TrimSpace(kind))

	if kind == "" {
		if exp, ok := ExpectedKindForReservedName(name); ok {
			kind = exp
		} else {
			kind = InferWriteArtifactKind(name)
		}
	}

	if kind == "image" {
		if exp, ok := ExpectedKindForReservedName(name); ok {
			return "", fmt.Errorf(
				"产物 %q 期望 kind=%q，不能使用 image；图片请改用沙箱 `artifact-upload` 上传，不要经 write_artifact 写入",
				name, exp,
			)
		}
		return "", fmt.Errorf(
			"write_artifact 禁止 kind=image（name=%q）；图片请改用沙箱 `artifact-upload` 上传",
			name,
		)
	}

	if exp, ok := ExpectedKindForReservedName(name); ok && kind != exp {
		return "", fmt.Errorf("产物 %q 期望 kind=%q，收到 %q", name, exp, kind)
	}

	return kind, nil
}

// ValidateImageArtifactUpload rejects names that must never be stored as
// kind=image via the CLI-only upload channel (reserved contract names).
func ValidateImageArtifactUpload(name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("产物名不能为空")
	}
	if exp, ok := ExpectedKindForReservedName(name); ok {
		return fmt.Errorf(
			"产物 %q 为平台保留名(期望 kind=%q),不能作为截图上传;请使用自动生成的截图文件名",
			name, exp,
		)
	}
	return nil
}
