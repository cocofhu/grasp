package handler

import (
	"errors"
	"io"
	"net/http"

	"backend/internal/service"

	"github.com/gin-gonic/gin"
)

// chatFromQuery 按 ?chat= 取会话（缺省 default）；不存在时写 404 并返回 nil。
func chatFromQuery(c *gin.Context, chats *service.ChatManager) *service.Bridge {
	b, ok := chats.Get(c.Query("chat"))
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "chat not found"})
		return nil
	}
	return b
}

// ChatsList 返回所有会话（按创建顺序，default 在首位）。
func ChatsList(chats *service.ChatManager) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"chats":        chats.List(),
			"max":          chats.MaxChats(),
			"defaultModel": chats.DefaultModel(),
		})
	}
}

// ChatsCreate 新建会话；body {title?, model?}，model 为空表示跟随默认模型。
func ChatsCreate(chats *service.ChatManager) gin.HandlerFunc {
	return func(c *gin.Context) {
		var body struct {
			Title string `json:"title"`
			Model string `json:"model"`
		}
		if err := c.ShouldBindJSON(&body); err != nil && !errors.Is(err, io.EOF) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON"})
			return
		}
		b, err := chats.Create(body.Title, body.Model)
		if errors.Is(err, service.ErrTooManyChats) {
			c.JSON(http.StatusConflict, gin.H{"error": "会话数已达上限", "max": chats.MaxChats()})
			return
		}
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusCreated, chats.Info(b))
	}
}

// ChatsRename 修改会话标题；body {title}。
func ChatsRename(chats *service.ChatManager) gin.HandlerFunc {
	return func(c *gin.Context) {
		var body struct {
			Title string `json:"title"`
		}
		if err := c.ShouldBindJSON(&body); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON"})
			return
		}
		b, err := chats.Rename(c.Param("id"), body.Title)
		if errors.Is(err, service.ErrChatNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "chat not found"})
			return
		}
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, chats.Info(b))
	}
}

// ChatsDelete 结束会话的 Agent 并移除；default 不可删。
func ChatsDelete(chats *service.ChatManager) gin.HandlerFunc {
	return func(c *gin.Context) {
		err := chats.Delete(c.Param("id"))
		switch {
		case errors.Is(err, service.ErrDefaultChatDel):
			c.JSON(http.StatusBadRequest, gin.H{"error": "默认会话不可删除"})
		case errors.Is(err, service.ErrChatNotFound):
			c.JSON(http.StatusNotFound, gin.H{"error": "chat not found"})
		case err != nil:
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		default:
			c.JSON(http.StatusOK, gin.H{"ok": true})
		}
	}
}
