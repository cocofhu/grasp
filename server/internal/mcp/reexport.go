package mcp

import s "github.com/cocofhu/grasp/internal/mcp/structured"

// Re-export structured product symbols so existing importers keep working.

const (
	ClarifiedRequirementArtifactName = s.ClarifiedRequirementArtifactName
	ResearchArtifactName             = s.ResearchArtifactName
	RootCauseArtifactName            = s.RootCauseArtifactName
	ProposalsArtifactName            = s.ProposalsArtifactName
	ProposalArtifactName             = s.ProposalArtifactName
	TestResultArtifactName           = s.TestResultArtifactName
	ReviewArtifactName               = s.ReviewArtifactName
	ImplementationResultArtifactName = s.ImplementationResultArtifactName
	PreflightArtifactName            = s.PreflightArtifactName
)

type ProposalChoice = s.ProposalChoice

var (
	ClarifiedOpenQuestions             = s.ClarifiedOpenQuestions
	ClarifiedWorkKind                  = s.ClarifiedWorkKind
	RenderClarifiedRequirementMarkdown = s.RenderClarifiedRequirementMarkdown
	RenderResearchMarkdown             = s.RenderResearchMarkdown
	RenderRootCauseMarkdown            = s.RenderRootCauseMarkdown
	RenderProposalsMarkdown            = s.RenderProposalsMarkdown
	RenderProposalMarkdown             = s.RenderProposalMarkdown
	RenderTestResultMarkdown           = s.RenderTestResultMarkdown
	RenderReviewMarkdown               = s.RenderReviewMarkdown
	RenderImplementationResultMarkdown = s.RenderImplementationResultMarkdown
	RenderPreflightMarkdown            = s.RenderPreflightMarkdown
	PreflightIncomplete                = s.PreflightIncomplete
	ProposalChoices                    = s.ProposalChoices
	SelectProposal                     = s.SelectProposal
	TestFailedCount                    = s.TestFailedCount
	TestSkippedCount                   = s.TestSkippedCount
	ReviewVerdict                      = s.ReviewVerdict
	ReviewVerdictOK                    = s.ReviewVerdictOK
)

const MinimalValidClarifiedRequirementJSON = s.MinimalValidClarifiedRequirementJSON
