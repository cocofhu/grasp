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
		models, configured, err := service.ChatModelList()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		_, reasoningEfforts := bridge.ReasoningOptions(bridge.EffectiveModel())
		c.JSON(http.StatusOK, gin.H{
			"models":           models,
			"configured":       configured,
			"current":          bridge.EffectiveModel(),
			"selected":         bridge.Model(),
			"default":          chats.DefaultModel(),
			"fixed":            false,
			"reasoningEffort":  bridge.ReasoningEffort(),
			"reasoningEfforts": reasoningEfforts,
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
			Model           *string `json:"model"`
			ReasoningEffort *string `json:"reasoningEffort"`
		}
		if err := c.ShouldBindJSON(&body); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON"})
			return
		}
		model := bridge.Model()
		if body.Model != nil {
			model = strings.TrimSpace(*body.Model)
		}
		if !service.ModelSelectable(model) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "该模型不在当前配置中"})
			return
		}
		effectiveModel := model
		if effectiveModel == "" {
			effectiveModel = chats.DefaultModel()
		}
		effort := bridge.ReasoningEffort()
		if body.ReasoningEffort != nil {
			effort = strings.TrimSpace(*body.ReasoningEffort)
			if !bridge.ReasoningEffortSelectable(effectiveModel, effort) {
				c.JSON(http.StatusBadRequest, gin.H{"error": "该模型不支持所选思考强度"})
				return
			}
		} else if !bridge.ReasoningEffortSelectable(effectiveModel, effort) {
			effort = ""
		}
		sess, err := bridge.SwitchModelSettings(model, effort)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		bridge.Broadcast(bridge.ConnectedPayload(sess))
		bridge.BroadcastQueueState()
		c.JSON(http.StatusOK, gin.H{
			"model":           bridge.EffectiveModel(),
			"selected":        bridge.Model(),
			"sessionId":       sess.SessionID(),
			"reasoningEffort": bridge.ReasoningEffort(),
		})
	}
}
