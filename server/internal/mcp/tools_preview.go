package mcp

import (
	"fmt"
	"strings"

	"github.com/cocofhu/grasp/internal/models"
)

// toolSetPreview registers a port or external URL preview for the active node.
func (h *Host) toolSetPreview(runID, token string, args map[string]any) (string, bool) {
	if !h.authorize(runID, token) {
		return "set_preview failed: " + ErrUnauthorized.Error(), true
	}
	if !h.nodeToolAllowed(runID, models.ToolSetPreview) {
		return toolDeniedMsg("set_preview"), true
	}
	portRaw, hasPort := args["port"]
	urlRaw := strings.TrimSpace(asString(args["url"]))
	hasURL := urlRaw != ""
	if hasPort == hasURL {
		return "set_preview failed: 必须恰好提供 port 或 url 之一", true
	}
	label := asString(args["label"])
	nodeID := h.ActiveNode(runID)
	if hasURL {
		display, err := h.setPreviewURL(runID, nodeID, urlRaw, label)
		if err != nil {
			return "set_preview failed: " + err.Error(), true
		}
		return fmt.Sprintf("ok: 外部 URL 预览已注册: %s", display), false
	}
	port, err := parsePreviewPort(portRaw)
	if err != nil {
		return "set_preview failed: " + err.Error(), true
	}
	reg, err := h.setPreviewPort(runID, nodeID, port, label)
	if err != nil {
		return "set_preview failed: " + err.Error(), true
	}
	return previewPortResult(port, reg), false
}

// toolSetArtifactPreview pins an existing artifact as the preview.
func (h *Host) toolSetArtifactPreview(runID, token string, args map[string]any) (string, bool) {
	solo := strings.HasPrefix(runID, models.SoloArtifactRunPrefix)
	if !h.authorize(runID, token) {
		return "set_artifact_preview failed: " + ErrUnauthorized.Error(), true
	}
	if !h.nodeToolAllowed(runID, models.ToolSetArtifactPreview) {
		return toolDeniedMsg("set_artifact_preview"), true
	}
	aname := strings.TrimSpace(asString(args["name"]))
	if aname == "" {
		return "set_artifact_preview failed: 'name' is required", true
	}
	if _, ok := h.store.Get(runID, aname); !ok {
		return "set_artifact_preview failed: 产物不存在: " + aname, true
	}
	nodeID := h.ActiveNode(runID)
	h.mu.RLock()
	hook := h.artifactPreview
	if solo {
		hook = h.soloArtifactPreview
	}
	h.mu.RUnlock()
	if hook != nil {
		if err := hook(runID, nodeID, aname); err != nil {
			return "set_artifact_preview failed: " + err.Error(), true
		}
	} else if solo {
		return "set_artifact_preview failed: Solo 产物预览不可用", true
	}
	return fmt.Sprintf("ok: 已设置产物预览 %q", aname), false
}
