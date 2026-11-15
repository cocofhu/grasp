package mcp

import (
	"encoding/json"
	"fmt"
	"strings"
)

// toolUploadImageArtifact stores an image the agent uploaded as an artifact.
func (h *Host) toolUploadImageArtifact(runID, token string, args map[string]any) (string, bool) {
	aname := asString(args["name"])
	content := asString(args["content"])
	if aname == "" {
		return "error: 'name' is required", true
	}
	id, err := h.UploadImageArtifact(runID, token, h.ActiveNode(runID), aname, content)
	if err != nil {
		return "upload_image_artifact failed: " + err.Error(), true
	}
	return fmt.Sprintf("ok: uploaded image artifact %q (id=%s)", aname, id), false
}

// toolWriteArtifact writes a text artifact. A review Agent in review rewrites an existing artifact under its owner node.
func (h *Host) toolWriteArtifact(runID, token string, args map[string]any) (string, bool) {
	aname := asString(args["name"])
	content := asString(args["content"])
	kind := asString(args["kind"])
	if aname == "" {
		return "error: 'name' is required", true
	}
	if strings.TrimSpace(aname) == PreflightArtifactName {
		return "write_artifact failed: preflight.json 只能通过 set_preflight 写入,不能用 write_artifact 伪造", true
	}
	writer := h.ActiveNode(runID)
	if h.reviewAgentInReview(runID) {
		if owner := h.artifactWriterNode(runID, token, aname); owner != "" {
			writer = owner
		}
	}
	id, err := h.WriteArtifact(runID, token, writer, aname, content, kind)
	if err != nil {
		return "write_artifact failed: " + err.Error(), true
	}
	return fmt.Sprintf("ok: wrote artifact %q (id=%s)", aname, id), false
}

// toolReadArtifact returns an artifact the active Agent may read.
func (h *Host) toolReadArtifact(runID, token string, args map[string]any) (string, bool) {
	if aname := asString(args["name"]); h.authorize(runID, token) && !h.readAllowed(runID, token, aname) {
		return readDeniedMsg("read_artifact", aname), true
	}
	content, err := h.ReadArtifact(runID, token, asString(args["name"]))
	if err != nil {
		return "read_artifact failed: " + err.Error(), true
	}
	return content, false
}

// toolListArtifacts lists the run's artifacts the active Agent may read.
func (h *Host) toolListArtifacts(runID, token string, args map[string]any) (string, bool) {
	infos, err := h.ListArtifacts(runID, token)
	if err != nil {
		return "list_artifacts failed: " + err.Error(), true
	}
	readable := infos[:0]
	for _, info := range infos {
		if h.readAllowed(runID, token, info.Name) {
			readable = append(readable, info)
		}
	}
	b, _ := json.Marshal(readable)
	return string(b), false
}
