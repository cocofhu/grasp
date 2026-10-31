package engine

import (
	"context"
	"strings"

	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/runtime"

	"github.com/rs/zerolog/log"
)

// bindChatRoute copies the visit choice onto the provider request. A visit
// that never switched asks the provider to clear a shared sandbox back to the
// node Agent's primary model. The Agent binding and workflow stay unchanged.
func (e *Engine) bindChatRoute(req *runtime.NodeReq, conv *models.ReactConversation) {
	if req == nil {
		return
	}
	req.ChatRoute = nil
	req.AppliedRoute = nil
	req.ResetToDefaultModel = false
	if conv == nil || !conv.CurrentModelSet {
		req.ResetToDefaultModel = true
		return
	}
	route := e.routeFrom(conv.CurrentConfigurationID, conv.CurrentModelID, conv.CurrentReasoningEffort)
	req.ChatRoute = &route
	if conv.AppliedModelSet {
		applied := e.routeFrom(conv.AppliedConfigurationID, conv.AppliedModelID, conv.AppliedReasoningEffort)
		req.AppliedRoute = &applied
	}
}

func (e *Engine) routeFrom(configurationID, modelID, effort string) runtime.ChatRoute {
	route := runtime.ChatRoute{
		ConfigurationID: strings.TrimSpace(configurationID),
		ModelID:         strings.TrimSpace(modelID),
		ReasoningEffort: strings.TrimSpace(effort),
		Set:             true,
	}
	if route.ConfigurationID != "" && e.db != nil {
		var cfg models.ModelConfiguration
		if err := e.db.First(&cfg, "id = ?", route.ConfigurationID).Error; err == nil {
			route.Backend = cfg.Backend
		}
	}
	return route
}

// noteChatRoute records a successful align, or rolls the stored choice back to
// the route the sandbox still has. Completed messages are left untouched.
func (e *Engine) noteChatRoute(conv *models.ReactConversation, req *runtime.NodeReq, notice string) {
	if conv == nil || req == nil {
		return
	}
	if notice != "" {
		conv.ModelNotice = notice
		if conv.AppliedModelSet {
			conv.CurrentConfigurationID = conv.AppliedConfigurationID
			conv.CurrentModelID = conv.AppliedModelID
			conv.CurrentReasoningEffort = conv.AppliedReasoningEffort
			conv.CurrentModelSet = true
		} else {
			conv.CurrentConfigurationID = ""
			conv.CurrentModelID = ""
			conv.CurrentReasoningEffort = ""
			conv.CurrentModelSet = false
		}
		return
	}
	conv.ModelNotice = ""
	if req.ChatRoute != nil && req.ChatRoute.Set {
		conv.AppliedConfigurationID = req.ChatRoute.ConfigurationID
		conv.AppliedModelID = req.ChatRoute.ModelID
		conv.AppliedReasoningEffort = req.ChatRoute.ReasoningEffort
		conv.AppliedModelSet = true
		return
	}
	if req.ResetToDefaultModel {
		conv.AppliedConfigurationID = ""
		conv.AppliedModelID = ""
		conv.AppliedReasoningEffort = ""
		conv.AppliedModelSet = true
	}
}

// prepareVisitModel aligns a parked session before the prompt so a failed
// switch is visible without waiting for the whole turn. A provider that does
// not align is left alone. The turn itself aligns again once a session exists.
func (e *Engine) prepareVisitModel(ctx context.Context, req *runtime.NodeReq, conv *models.ReactConversation, runID, nodeID string) {
	if req == nil || conv == nil {
		return
	}
	e.bindChatRoute(req, conv)
	aligner, ok := e.provider.(runtime.ChatRouteAligner)
	if !ok {
		return
	}
	notice, aligned, err := aligner.AlignChatRoute(ctx, *req)
	if err != nil {
		log.Warn().Err(err).Str("run", runID).Str("node", nodeID).Msg("visit model align failed")
		return
	}
	if !aligned && notice == "" {
		return
	}
	e.noteChatRoute(conv, req, notice)
	logDB(e.db.Save(conv), runID, "save visit model align")
	if e.broker != nil {
		e.publishReact(runID, nodeID)
	}
	e.bindChatRoute(req, conv)
}

func (e *Engine) rememberTurnRoute(conv *models.ReactConversation, req *runtime.NodeReq, t runtime.ReactTurn) {
	if conv == nil {
		return
	}
	// A rollback recorded before the prompt must survive the restored turn,
	// which aligns cleanly and therefore has no notice of its own.
	kept := ""
	if t.ModelNotice == "" && conv.ModelNotice != "" {
		kept = conv.ModelNotice
	}
	if t.ModelNotice == "" && !t.RouteApplied && kept == "" {
		return
	}
	e.noteChatRoute(conv, req, t.ModelNotice)
	if kept != "" {
		conv.ModelNotice = kept
	}
}
