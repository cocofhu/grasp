package handler

import (
	"log"
	"net/http"
	"strconv"

	"backend/internal/service"

	"github.com/gin-gonic/gin"
)

// EventsBefore 返回指定轮次之前的历史事件（用于向上滚动加载）。
// 参数: ?before=<turnIndex>&limit=<count>&chat=<id>
func EventsBefore(chats *service.ChatManager) gin.HandlerFunc {
	return func(c *gin.Context) {
		bridge := chatFromQuery(c, chats)
		if bridge == nil {
			return
		}
		beforeStr := c.DefaultQuery("before", "0")
		limitStr := c.DefaultQuery("limit", "10")
		before, err := strconv.Atoi(beforeStr)
		if err != nil {
			log.Printf("api/events: invalid before=%q: %v", beforeStr, err)
			before = 0
		}
		limit, err := strconv.Atoi(limitStr)
		if err != nil {
			log.Printf("api/events: invalid limit=%q: %v", limitStr, err)
			limit = 10
		}
		if limit <= 0 || limit > 50 {
			limit = 10
		}
		events, hasMore := bridge.EventLogTurnsBefore(before, limit)
		c.JSON(http.StatusOK, gin.H{
			"events":  events,
			"hasMore": hasMore,
		})
	}
}
