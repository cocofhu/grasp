package handler

import (
	"net/http"

	"backend/internal/service"

	"github.com/gin-gonic/gin"
)

// PromptQueue 只读 ?chat= 会话的队列快照（queue_* 等字段）。
func PromptQueue(chats *service.ChatManager) gin.HandlerFunc {
	return func(c *gin.Context) {
		bridge := chatFromQuery(c, chats)
		if bridge == nil {
			return
		}
		c.JSON(http.StatusOK, bridge.PromptQueueSnapshot())
	}
}
