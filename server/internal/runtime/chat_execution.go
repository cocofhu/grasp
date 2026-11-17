package runtime

import (
	"strings"
	"time"

	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/sandbox"
)

// executionForEnvironment reads only the already resolved launch environment.
// Re-reading an Agent here could label a parked session with a newly saved
// default even while its original CLI and credentials are still in use.
func (c *acpProvider) executionForEnvironment(req NodeReq, env map[string]string) *models.ChatExecution {
	backend := string(c.backend)
	if envBackend := strings.TrimSpace(env["AGENT_PROVIDER"]); envBackend != "" {
		backend = envBackend
	}
	return &models.ChatExecution{
		AgentName: models.AgentProfile(req.Config), Backend: backend,
		ConfigurationID: strings.TrimSpace(env[EnvModelConfigurationID]),
		ModelID:         strings.TrimSpace(env[EnvACPBridgeModel]),
		ReasoningEffort: strings.TrimSpace(env[EnvACPBridgeReasoningEffort]),
		FastMode:        strings.EqualFold(strings.TrimSpace(env[EnvACPBridgeFastMode]), "true"),
	}
}

func finishChatTurn(out *ReactTurn, acp *sandbox.ACPClient, started time.Time) {
	// Successful/failed finish paths historically carry the detailed buckets
	// on Result. Preserve them on the reply too without adding them twice.
	if out.Usage == nil {
		out.Usage = models.CloneTokenUsage(out.Result.Usage)
	}
	if out.UsageByModel == nil {
		out.UsageByModel = models.CloneTokenUsageByModel(out.Result.UsageByModel)
	}
	out.DurationMs = time.Since(started).Milliseconds()
	out.Execution = stampedExecution(acp, started)
}

// ReplyMetadata is this provider call as one reply.
func (t ReactTurn) ReplyMetadata() models.ReplyMetadata {
	return models.NewReplyMetadata(t.Execution, t.Usage, t.UsageByModel, t.DurationMs)
}

// ReplyMetadata is the automatic run as one reply. Its duration comes from
// the reply's own start/end times.
func (r NodeResult) ReplyMetadata() models.ReplyMetadata {
	return models.NewReplyMetadata(r.Execution, r.Usage, r.UsageByModel, 0)
}

func stampedExecution(acp *sandbox.ACPClient, started time.Time) *models.ChatExecution {
	if acp == nil {
		return nil
	}
	execution := acp.Execution()
	if execution != nil {
		execution.StartedAt = started.UTC()
	}
	return execution
}
