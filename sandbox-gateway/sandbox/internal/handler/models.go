package handler

import (
	"net/http"
	"strings"

	"backend/internal/service"

	"github.com/gin-gonic/gin"
)

// ModelsGET 返回可用模型列表、默认模型与 ?chat= 会话的当前模型。
// current 为该会话实际使用的模型（会话选择 → 默认模型 → 空即 auto）；selected 为会话显式选择（空即跟随默认）。
func ModelsGET(chats *service.ChatManager) gin.HandlerFunc {
	return func(c *gin.Context) {
		bridge := chatFromQuery(c, chats)
		if bridge == nil {
			return
		}
		models, err := service.ListAgentModels()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{
			"models":   models,
			"current":  bridge.EffectiveModel(),
			"selected": bridge.Model(),
			"default":  chats.DefaultModel(),
			"fixed":    false,
		})
	}
}

// ModelPOST 设置 ?chat= 会话的模型并只重启该会话；model 为空串表示恢复跟随默认模型。
func ModelPOST(chats *service.ChatManager) gin.HandlerFunc {
	return func(c *gin.Context) {
		bridge := chatFromQuery(c, chats)
		if bridge == nil {
			return
		}
		var body struct {
			Model string `json:"model"`
		}
		if err := c.ShouldBindJSON(&body); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON"})
			return
		}
		model := strings.TrimSpace(body.Model)
		prev := bridge.Model()
		bridge.SetModel(model)
		sess, err := bridge.RestartAgent()
		if err != nil {
			bridge.SetModel(prev)
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		bridge.Broadcast(bridge.ConnectedPayload(sess))
		bridge.BroadcastQueueState()
		c.JSON(http.StatusOK, gin.H{
			"model":     bridge.EffectiveModel(),
			"selected":  bridge.Model(),
			"sessionId": sess.SessionID(),
		})
	}
}
