package engine

import (
	"errors"
	"strings"

	"github.com/cocofhu/grasp/internal/gateshare"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/nodereg"
)

// ResumeReviewExternal consumes a review share link (CAS) then force-confirms
// the bound inbox review via ReactReply(force=true). Busy / validation failures
// roll back CAS so the token is not burned.
func (e *Engine) ResumeReviewExternal(share *gateshare.Service, token, action string) (*ExternalResumeResult, error) {
	return e.ResumeReviewExternalOpts(share, token, action, false)
}

// ResumeReviewExternalOpts is ResumeReviewExternal with abortRunning: cancel an
// orphan sandbox turn before confirming (public 「中止当前回合并确认」).
func (e *Engine) ResumeReviewExternalOpts(share *gateshare.Service, token, action string, abortRunning bool) (*ExternalResumeResult, error) {
	if e.IsHalted() {
		return nil, errors.New("server is shutting down")
	}
	if share == nil {
		return nil, errors.New("share service unavailable")
	}
	lookup, st, err := share.LookupByToken(token)
	if err != nil || lookup == nil {
		return nil, gateshare.ErrTokenInvalid
	}
	if normalizeLookupKind(lookup) != models.ShareLinkKindReview {
		return nil, gateshare.ErrNotReviewSession
	}
	switch st {
	case models.ShareLinkStateExpired:
		return &ExternalResumeResult{Status: "expired", Link: &lookup.Link}, gateshare.ErrNotActive
	case models.ShareLinkStateRevoked:
		return &ExternalResumeResult{Status: "revoked", Link: &lookup.Link}, gateshare.ErrNotActive
	case models.ShareLinkStateUsed:
		if strings.TrimSpace(lookup.Link.UsedAction) != "" && isReviewConfirmAction(lookup.Link.UsedAction) && isReviewConfirmAction(action) {
			if !shareConfirmSettled(e, lookup.Link.RunID, lookup.Link.NodeID, lookup.Link.Iteration) {
				return &ExternalResumeResult{Status: "busy", Link: &lookup.Link}, gateshare.ErrReviewBusy
			}
			return &ExternalResumeResult{
				Status: "confirmed", Action: lookup.Link.UsedAction, AlreadyProcessed: true,
				Link: &lookup.Link,
			}, nil
		}
		return &ExternalResumeResult{
			Status: "used", Action: lookup.Link.UsedAction,
			Conflict: lookup.Link.UsedAction != "" && !isReviewConfirmAction(action),
			Link:     &lookup.Link,
		}, gateshare.ErrActionConflict
	case models.ShareLinkStateNone:
		return nil, gateshare.ErrTokenInvalid
	}
	if !isReviewConfirmAction(action) {
		return nil, gateshare.ErrNoStandardAction
	}
	if lookup.Node == nil || !isShareableReviewConfirmNode(lookup.Node) {
		return nil, gateshare.ErrNotReviewSession
	}

	// Check busy before CAS so a mid-stream confirm does not flicker consume/rollback.
	if !e.ReviewSessionReady(lookup.Link.RunID, lookup.Link.NodeID) {
		return &ExternalResumeResult{Status: "busy", Link: &lookup.Link}, gateshare.ErrReviewBusy
	}
	if err := e.ensureSandboxIdleForConfirm(lookup.Link.RunID, lookup.Link.NodeID, abortRunning); err != nil {
		var busy *SandboxBusyError
		if errors.As(err, &busy) {
			return &ExternalResumeResult{Status: "sandbox_busy", Link: &lookup.Link}, err
		}
		return &ExternalResumeResult{Status: "busy", Link: &lookup.Link}, err
	}

	consumed, usedLink, err := share.ConsumeCAS(lookup.Link.ID, "confirm")
	if err != nil {
		return nil, err
	}
	if !consumed {
		if usedLink != nil && isReviewConfirmAction(usedLink.UsedAction) {
			if !shareConfirmSettled(e, lookup.Link.RunID, lookup.Link.NodeID, lookup.Link.Iteration) {
				return &ExternalResumeResult{Status: "busy", Link: usedLink}, gateshare.ErrReviewBusy
			}
			return &ExternalResumeResult{
				Status: "confirmed", Action: usedLink.UsedAction, AlreadyProcessed: true,
				Link: usedLink,
			}, nil
		}
		return &ExternalResumeResult{
			Status: "used", Action: usedLinkAction(usedLink), Conflict: true,
			Link: usedLink,
		}, gateshare.ErrActionConflict
	}

	if !e.ReviewSessionReady(lookup.Link.RunID, lookup.Link.NodeID) {
		_ = share.RollbackConsume(usedLink.ID)
		return &ExternalResumeResult{Status: "busy", Link: usedLink}, gateshare.ErrReviewBusy
	}

	err = e.ReactConfirmAs("", lookup.Link.RunID, lookup.Link.NodeID, "确认并流转", nil, nil, abortRunning)
	if err != nil {
		_ = share.RollbackConsume(usedLink.ID)
		if errors.Is(err, ErrSandboxBusy) {
			return &ExternalResumeResult{Status: "sandbox_busy", Link: usedLink}, err
		}
		if isReviewBusyError(err) || !e.ReviewSessionReady(lookup.Link.RunID, lookup.Link.NodeID) {
			return &ExternalResumeResult{Status: "busy", Link: usedLink}, gateshare.ErrReviewBusy
		}
		if stillWaitingHuman(e, lookup.Link.RunID, lookup.Link.NodeID, lookup.Link.Iteration) {
			return &ExternalResumeResult{Status: "validation_failed", Link: usedLink}, gateshare.ErrReviewValidation
		}
		if strings.Contains(err.Error(), "already done") || strings.Contains(err.Error(), "react already done") {
			if !shareConfirmSettled(e, lookup.Link.RunID, lookup.Link.NodeID, lookup.Link.Iteration) {
				return &ExternalResumeResult{Status: "busy", Link: usedLink}, gateshare.ErrReviewBusy
			}
			return &ExternalResumeResult{
				Status: "confirmed", Action: "confirm", AlreadyProcessed: true, Link: usedLink,
			}, nil
		}
		return nil, err
	}
	if !shareConfirmSettled(e, lookup.Link.RunID, lookup.Link.NodeID, lookup.Link.Iteration) {
		_ = share.RollbackConsume(usedLink.ID)
		return &ExternalResumeResult{Status: "validation_failed", Link: usedLink}, gateshare.ErrReviewValidation
	}
	return &ExternalResumeResult{Status: "confirmed", Action: "confirm", Link: usedLink}, nil
}

// isShareableReviewConfirmNode allows Inbox review / app_preview producers and
// classic react clarify. Must not flip isReviewNode (that would skip Agent wrap-up).
func isShareableReviewConfirmNode(node *models.Node) bool {
	if node == nil {
		return false
	}
	return nodereg.ClarifyInteractive(node.Type) || isReviewNode(node.Type)
}

// shareConfirmSettled is the only success condition for burning a review share
// link: the bound conversation must be Done and the node must have left waiting_human.
func shareConfirmSettled(e *Engine, runID, nodeID string, iteration int) bool {
	if stillWaitingHuman(e, runID, nodeID, iteration) {
		return false
	}
	var conv models.ReactConversation
	if err := e.db.Where("run_id = ? AND node_id = ?", runID, nodeID).
		Order("iteration desc, id desc").First(&conv).Error; err != nil {
		return false
	}
	return conv.Done
}

func normalizeLookupKind(lookup *gateshare.LookupResult) string {
	if lookup == nil {
		return models.ShareLinkKindHumanGate
	}
	if strings.TrimSpace(lookup.Kind) != "" {
		return lookup.Kind
	}
	if strings.TrimSpace(lookup.Link.Kind) == models.ShareLinkKindReview {
		return models.ShareLinkKindReview
	}
	return models.ShareLinkKindHumanGate
}

func isReviewConfirmAction(action string) bool {
	a := strings.ToLower(strings.TrimSpace(action))
	return a == "confirm" || a == "pass"
}

func isReviewBusyError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "复审进行中") || strings.Contains(msg, "待发送队列") || strings.Contains(msg, "澄清进行中")
}

func stillWaitingHuman(e *Engine, runID, nodeID string, iteration int) bool {
	var sr models.StateRun
	if err := e.db.Where("run_id = ? AND node_id = ? AND iteration = ?", runID, nodeID, iteration).
		Order("id desc").First(&sr).Error; err != nil {
		return false
	}
	return sr.Status == "waiting_human"
}
