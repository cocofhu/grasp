package pmmcp

import (
	"errors"
	"strings"

	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/platformmcp"
	"github.com/cocofhu/grasp/internal/services"

	"github.com/rs/zerolog/log"
)

// callPrdManager dispatches pm-prd-manager tools. First cut: list / get / create.
// Later update / status / delete / progress-summary tools should be added here
// (and in toolSchemas) without changing RequirementDraft semantics.
func (h *Host) callPrdManager(token, name string, args map[string]any) (any, bool) {
	if _, ok := h.SessionFor(token); !ok {
		return map[string]any{"error": "unauthorized"}, true
	}
	h.mu.RLock()
	drafts := h.drafts
	h.mu.RUnlock()
	if drafts == nil {
		return map[string]any{"error": "requirement draft service unavailable"}, true
	}
	switch name {
	case "pm_list_requirement_drafts":
		return h.pmListRequirementDrafts(drafts, args)
	case "pm_get_requirement_draft":
		return h.pmGetRequirementDraft(drafts, args)
	case "pm_create_requirement_draft":
		return h.pmCreateRequirementDraft(drafts, args)
	default:
		return map[string]any{"error": "unknown tool: " + name}, true
	}
}

func (h *Host) pmListRequirementDrafts(drafts *services.RequirementDraftService, args map[string]any) (any, bool) {
	rows, err := drafts.List(services.RequirementDraftListFilter{
		Status: platformmcp.StrArg(args, "status"),
		Query:  platformmcp.StrArg(args, "query"),
	})
	if err != nil {
		return requirementDraftToolError(err)
	}
	items := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		items = append(items, requirementDraftSummary(row))
	}
	return map[string]any{"items": items}, false
}

func (h *Host) pmGetRequirementDraft(drafts *services.RequirementDraftService, args map[string]any) (any, bool) {
	draftID := strings.TrimSpace(platformmcp.StrArg(args, "draftId"))
	if draftID == "" {
		return map[string]any{"error": "draftId is required"}, true
	}
	row, err := drafts.Get(draftID)
	if err != nil {
		return requirementDraftToolError(err)
	}
	return requirementDraftFull(row), false
}

func (h *Host) pmCreateRequirementDraft(drafts *services.RequirementDraftService, args map[string]any) (any, bool) {
	title := strings.TrimSpace(platformmcp.StrArg(args, "title"))
	body := ""
	if _, ok := args["bodyMarkdown"]; ok {
		body = platformmcp.StrArg(args, "bodyMarkdown")
	}
	needUpdate := title != "" || body != ""

	row, err := drafts.Create(services.RequirementDraftCreateInput{})
	if err != nil {
		return requirementDraftToolError(err)
	}
	if !needUpdate {
		return requirementDraftCreated(row), false
	}
	appliedTitle := title
	if appliedTitle == "" {
		appliedTitle = services.DefaultRequirementDraftTitle
	}
	updated, err := drafts.UpdateContent(row.ID, appliedTitle, body)
	if err != nil {
		if delErr := drafts.Delete(row.ID); delErr != nil {
			log.Error().Err(delErr).Str("draft_id", row.ID).
				Msg("failed to roll back requirement draft after UpdateContent error")
		}
		return requirementDraftToolError(err)
	}
	return requirementDraftCreated(updated), false
}

func requirementDraftSummary(row models.RequirementDraft) map[string]any {
	return map[string]any{
		"id":        row.ID,
		"title":     row.Title,
		"status":    row.Status,
		"updatedAt": row.UpdatedAt,
		"createdAt": row.CreatedAt,
	}
}

func requirementDraftFull(row models.RequirementDraft) map[string]any {
	out := requirementDraftSummary(row)
	out["bodyMarkdown"] = row.BodyMarkdown
	return out
}

func requirementDraftCreated(row models.RequirementDraft) map[string]any {
	return map[string]any{
		"id":           row.ID,
		"title":        row.Title,
		"status":       row.Status,
		"bodyMarkdown": row.BodyMarkdown,
	}
}

func requirementDraftToolError(err error) (any, bool) {
	switch {
	case errors.Is(err, services.ErrRequirementDraftNotFound):
		return map[string]any{"error": "requirement draft not found"}, true
	case errors.Is(err, services.ErrRequirementDraftInvalidStatus):
		return map[string]any{"error": err.Error()}, true
	case errors.Is(err, services.ErrRequirementDraftEmptyTitle):
		return map[string]any{"error": err.Error()}, true
	case errors.Is(err, services.ErrRequirementDraftTitleTooLong):
		return map[string]any{"error": err.Error()}, true
	case errors.Is(err, services.ErrRequirementDraftBodyTooLong):
		return map[string]any{"error": err.Error()}, true
	default:
		return map[string]any{"error": err.Error()}, true
	}
}
