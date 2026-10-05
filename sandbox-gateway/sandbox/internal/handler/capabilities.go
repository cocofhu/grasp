package handler

import (
	"net/http"
	"os"
	"strconv"

	"backend/internal/agents"
	"backend/internal/service"

	"github.com/gin-gonic/gin"
)

// Capabilities 实现协议「能力发现」握手：返回沙箱声明的能力描述符（WSP/1）。
// 工作流在通信前调用，据此决定走哪些路径、如何注入配置、能否上报用量等，并对
// 缺失能力安全降级。Cursor-agent / git / code-server 仅为参考实现。
func Capabilities(bridge *service.Bridge) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.JSON(http.StatusOK, BuildCapabilities(bridge))
	}
}

// BuildCapabilities 组装能力描述符。复用桥接已有的 agent 信息与端口，不另造数据。
func BuildCapabilities(bridge *service.Bridge) gin.H {
	b := agents.FromEnv()
	root := agents.ConfigRoot()
	agent := gin.H{"runtime": agents.RuntimeLabel(b)}
	tokenUsage := false
	if bridge != nil {
		if p := bridge.Session(); p != nil {
			info := p.Info()
			if info.Name != "" {
				agent["name"] = info.Name
			}
			if info.Version != "" {
				agent["version"] = info.Version
			}
			// 据实声明：仅当当前会话真的上报用量时才置 true，平台据此安全降级。
			tokenUsage = p.ReportsUsage()
		}
	}

	return gin.H{
		"protocol": "wsp/1",
		"agent":    agent,
		"session": gin.H{
			"ws":     "/ws",
			"events": "/api/events",
			// tokenUsage 反映当前会话是否透出用量；不报用量的后端为 false。
			"tokenUsage": tokenUsage,
		},
		"ide": gin.H{
			"codeServer": true,
			"port":       envInt("CODE_SERVER_PORT", 8744),
		},
		// 应用预览桌面(可选):沙箱内 Xvfb+Chromium+x11vnc+websockify。
		// 平台为开启应用预览的 Agent 注入 enableEnv=1 后 dial cdpPort / websockifyPort。
		"preview": gin.H{
			"vnc":            true,
			"cdpPort":        9222,
			"websockifyPort": 6080,
			"enableEnv":      "VNC_PREVIEW",
		},
		// 配置注入契约：工作流把 MCP/RULE/SKILL/ENV 写到这些声明位置；
		// 参考布局即 /root/.cursor，整树 bind-mount 进沙箱。
		"config": gin.H{
			"mcp":    gin.H{"path": root + "/mcp.json", "schema": "mcpServers"},
			"rules":  gin.H{"dir": root + "/rules"},
			"skills": gin.H{"dir": root + "/skills"},
			"env":    gin.H{"via": "container-env"},
		},
	}
}

func envInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}
