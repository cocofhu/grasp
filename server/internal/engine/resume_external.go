package engine

import (
	"errors"
	"strings"

	"github.com/cocofhu/grasp/internal/gateshare"
	"github.com/cocofhu/grasp/internal/models"
)

// ExternalResumeResult is returned after an external share-link decision.
type ExternalResumeResult struct {
	Status           string // approved | rejected | already_processed
	Action           string
	AlreadyProcessed bool
	Conflict         bool
	Link             *models.GateShareLink
	Gate             models.Gate
}

// ResumeGateExternal consumes a share link (CAS) then resumes the bound human_gate.
// External confirm and reject both require non-empty name + comment (audit).
func (e *Engine) ResumeGateExternal(share *gateshare.Service, token, action, comment, externalName string) (*ExternalResumeResult, error) {
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
	switch st {
	case models.ShareLinkStateExpired:
		return &ExternalResumeResult{Status: "expired", Link: &lookup.Link, Gate: lookup.Gate}, gateshare.ErrNotActive
	case models.ShareLinkStateRevoked:
		return &ExternalResumeResult{Status: "revoked", Link: &lookup.Link, Gate: lookup.Gate}, gateshare.ErrNotActive
	case models.ShareLinkStateUsed:
		if strings.TrimSpace(lookup.Link.UsedAction) != "" && lookup.Link.UsedAction == action {
			kind := "approved"
			if gateshare.IsFailAction(action) {
				kind = "rejected"
			}
			return &ExternalResumeResult{
				Status: kind, Action: action, AlreadyProcessed: true,
				Link: &lookup.Link, Gate: lookup.Gate,
			}, nil
		}
		return &ExternalResumeResult{
			Status: "used", Action: lookup.Link.UsedAction, Conflict: lookup.Link.UsedAction != "" && lookup.Link.UsedAction != action,
			Link: &lookup.Link, Gate: lookup.Gate,
		}, gateshare.ErrActionConflict
	case models.ShareLinkStateNone:
		return nil, gateshare.ErrTokenInvalid
	}
	if lookup.Node == nil || lookup.Node.Type != "human_gate" {
		return nil, gateshare.ErrNotHumanGate
	}
	if !gateshare.IsWhitelistedExternalAction(action, lookup.Gate.Actions) {
		return nil, gateshare.ErrNoStandardAction
	}
	if strings.TrimSpace(externalName) == "" || strings.TrimSpace(comment) == "" {
		return nil, gateshare.ErrAuditRequired
	}

	unlock := e.lockResume(lookup.Link.RunID + ":" + lookup.Link.NodeID)
	defer unlock()

	lookup2, st2, err := share.LookupByToken(token)
	if err != nil || lookup2 == nil {
		return nil, gateshare.ErrTokenInvalid
	}
	lookup = lookup2
	if st2 == models.ShareLinkStateUsed {
		if lookup.Link.UsedAction == action {
			kind := "approved"
			if gateshare.IsFailAction(action) {
				kind = "rejected"
			}
			return &ExternalResumeResult{
				Status: kind, Action: action, AlreadyProcessed: true,
				Link: &lookup.Link, Gate: lookup.Gate,
			}, nil
		}
		return &ExternalResumeResult{
			Status: "used", Action: lookup.Link.UsedAction, Conflict: true,
			Link: &lookup.Link, Gate: lookup.Gate,
		}, gateshare.ErrActionConflict
	}
	if st2 != models.ShareLinkStateActive {
		return &ExternalResumeResult{Status: st2, Link: &lookup.Link, Gate: lookup.Gate}, gateshare.ErrNotActive
	}
	if lookup.Gate.Resolved {
		return &ExternalResumeResult{Status: "used", Link: &lookup.Link, Gate: lookup.Gate}, gateshare.ErrActionConflict
	}

	consumed, usedLink, err := share.ConsumeCAS(lookup.Link.ID, action)
	if err != nil {
		return nil, err
	}
	if !consumed {
		if usedLink != nil && usedLink.UsedAction == action {
			kind := "approved"
			if gateshare.IsFailAction(action) {
				kind = "rejected"
			}
			return &ExternalResumeResult{
				Status: kind, Action: action, AlreadyProcessed: true,
				Link: usedLink, Gate: lookup.Gate,
			}, nil
		}
		conflict := usedLink != nil && usedLink.UsedAction != "" && usedLink.UsedAction != action
		return &ExternalResumeResult{
			Status: "used", Action: usedLinkAction(usedLink), Conflict: conflict,
			Link: usedLink, Gate: lookup.Gate,
		}, gateshare.ErrActionConflict
	}

	form := map[string]any{}
	if strings.TrimSpace(comment) != "" {
		form["comment"] = comment
		hasCommentKey := false
		for _, f := range lookup.Gate.Form {
			if f.Key == "comment" {
				hasCommentKey = true
				break
			}
		}
		if !hasCommentKey {
			// Still persist comment as vars.comment for downstream; FR10 mapping.
			form["comment"] = comment
		}
	}

	err = e.resumeGateLocked(lookup.Link.RunID, lookup.Link.NodeID, action, form, "", resumeGateOpts{
		skipFormValidate: true,
		callerKind:       models.CallerKindExternal,
		externalName:     strings.TrimSpace(externalName),
	})
	if err != nil {
		if strings.Contains(err.Error(), "already resolved") || strings.Contains(err.Error(), "run already ended") {
			if usedLink != nil && usedLink.UsedAction == action {
				kind := "approved"
				if gateshare.IsFailAction(action) {
					kind = "rejected"
				}
				return &ExternalResumeResult{
					Status: kind, Action: action, AlreadyProcessed: true,
					Link: usedLink, Gate: lookup.Gate,
				}, nil
			}
			return &ExternalResumeResult{Status: "used", Link: usedLink, Gate: lookup.Gate, Conflict: true}, gateshare.ErrActionConflict
		}
		// Resume failed while the gate is still pending: roll back CAS so the
		// one-shot link remains usable. Do not burn the token on loadCtx / node /
		// persist errors.
		if usedLink != nil {
			_ = share.RollbackConsume(usedLink.ID)
		}
		return nil, err
	}
	kind := "approved"
	if gateshare.IsFailAction(action) {
		kind = "rejected"
	}
	return &ExternalResumeResult{
		Status: kind, Action: action, Link: usedLink, Gate: lookup.Gate,
	}, nil
}

func usedLinkAction(link *models.GateShareLink) string {
	if link == nil {
		return ""
	}
	return link.UsedAction
}
