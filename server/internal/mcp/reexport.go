package mcp

import s "github.com/cocofhu/grasp/internal/mcp/structured"

// Re-export structured product symbols so existing importers keep working.

const (
	ClarifiedRequirementArtifactName = s.ClarifiedRequirementArtifactName
	ResearchArtifactName             = s.ResearchArtifactName
	RootCauseArtifactName            = s.RootCauseArtifactName
	TestResultArtifactName           = s.TestResultArtifactName
	ReviewArtifactName               = s.ReviewArtifactName
	ImplementationResultArtifactName = s.ImplementationResultArtifactName
	MergeRequestArtifactName         = s.MergeRequestArtifactName
	PreflightArtifactName            = s.PreflightArtifactName
)

var (
	ClarifiedOpenQuestions             = s.ClarifiedOpenQuestions
	ClarifiedWorkKind                  = s.ClarifiedWorkKind
	RenderClarifiedRequirementMarkdown = s.RenderClarifiedRequirementMarkdown
	RenderResearchMarkdown             = s.RenderResearchMarkdown
	RenderRootCauseMarkdown            = s.RenderRootCauseMarkdown
	RenderTestResultMarkdown           = s.RenderTestResultMarkdown
	RenderReviewMarkdown               = s.RenderReviewMarkdown
	RenderImplementationResultMarkdown = s.RenderImplementationResultMarkdown
	RenderMergeRequestMarkdown         = s.RenderMergeRequestMarkdown
	RenderPreflightMarkdown            = s.RenderPreflightMarkdown
	PreflightIncomplete                = s.PreflightIncomplete
	TestFailedCount                    = s.TestFailedCount
	TestSkippedCount                   = s.TestSkippedCount
	ReviewVerdict                      = s.ReviewVerdict
	ReviewVerdictOK                    = s.ReviewVerdictOK
)

const MinimalValidClarifiedRequirementJSON = s.MinimalValidClarifiedRequirementJSON
