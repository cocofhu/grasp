package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/cocofhu/grasp/internal/models"
)

// ArtifactFiles bridges the run-scoped MCP to the platform's sandbox and
// durable delivery service. The agent never chooses a sandbox or conversation.
type ArtifactFiles interface {
	Import(context.Context, string, string, string, string) (models.ArtifactAttachment, error)
	SendFile(context.Context, string, string, string, string, string) (models.ArtifactAttachment, error)
	SendExisting(context.Context, string, string, string, int, string) (models.ArtifactAttachment, error)
}

func (h *Host) SetArtifactFiles(files ArtifactFiles) {
	h.mu.Lock()
	h.artifactFiles = files
	h.mu.Unlock()
}

func isFileTool(name string) bool {
	return name == "send_file" || name == "upload_file_artifact" || name == "send_artifact"
}

func (h *Host) runFileTool(runID, token, name string, args map[string]any) (string, bool) {
	if !h.authorize(runID, token) {
		return ErrUnauthorized.Error(), true
	}
	h.mu.RLock()
	files := h.artifactFiles
	h.mu.RUnlock()
	if files == nil {
		return "file delivery is unavailable", true
	}
	if name == "send_artifact" {
		if revision, supplied := args["revision"]; supplied && asInt(revision) < 1 {
			return "revision must be positive", true
		}
		metadata, ok := h.store.(interface {
			GetByID(string) (models.Artifact, bool)
		})
		if !ok {
			return "artifact metadata is unavailable", true
		}
		a, ok := metadata.GetByID(asString(args["artifact_id"]))
		if !ok || a.RunID != runID || !h.readAllowed(runID, token, a.Name) {
			return "artifact not found or not readable in current scope", true
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	nodeID := h.ActiveNode(runID)
	var attachment models.ArtifactAttachment
	var err error
	switch name {
	case "upload_file_artifact":
		attachment, err = files.Import(ctx, runID, nodeID, asString(args["path"]), asString(args["name"]))
	case "send_file":
		attachment, err = files.SendFile(ctx, runID, nodeID, asString(args["path"]), asString(args["name"]), asString(args["caption"]))
	case "send_artifact":
		attachment, err = files.SendExisting(ctx, runID, nodeID, asString(args["artifact_id"]), asInt(args["revision"]), asString(args["caption"]))
	default:
		err = errors.New("unknown file tool")
	}
	if err != nil {
		return name + " failed: " + err.Error(), true
	}
	encoded, _ := json.Marshal(attachment)
	return string(encoded), false
}

func fileTools() []map[string]any {
	str := func(desc string) map[string]any { return map[string]any{"type": "string", "description": desc} }
	tool := func(name, desc string, props map[string]any, required ...string) map[string]any {
		return map[string]any{"name": name, "description": desc, "inputSchema": map[string]any{"type": "object", "properties": props, "required": required, "additionalProperties": false}}
	}
	return []map[string]any{
		tool("send_file", "把当前沙箱工作区中的文件发送给当前对话的用户。平台导入文件并登记消息附件，支持图片、音视频、PDF及其他文件。只传文件路径，禁止内联base64；重复发送同一版本会去重。", map[string]any{"path": str("当前沙箱内文件路径，建议相对工作区"), "name": str("可选显示文件名"), "caption": str("可选文件说明")}, "path"),
		tool("upload_file_artifact", "把沙箱文件导入当前运行产物存储，返回产物ID与版本；需要直接交付用户时优先使用send_file。", map[string]any{"path": str("当前沙箱内文件路径"), "name": str("可选产物文件名")}, "path"),
		tool("send_artifact", "把本次运行内已有且有权读取的产物发送到当前对话。消息固定引用该版本，重复调用会去重。", map[string]any{"artifact_id": str("产物ID"), "revision": map[string]any{"type": "integer", "minimum": 1, "description": "可选版本，缺省当前版本"}, "caption": str("可选说明")}, "artifact_id"),
	}
}
