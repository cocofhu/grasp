package router

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"backend/internal/auth"
	"backend/internal/handler"
	"backend/internal/qqbot"
	"backend/internal/service"

	"github.com/gin-gonic/gin"
)

// Dependencies 注入路由依赖（前端从磁盘 webRoot 提供）。
type Dependencies struct {
	WebRoot       string // 含 index.html 与 static/ 的目录（绝对路径）
	Chats         *service.ChatManager
	QQBot         *qqbot.Service
	Auth          *auth.Guard // 非 nil 且 Enabled() 时启用口令与登录页
	LoginHTMLPath string      // login.html 绝对路径（启用 Auth 时必填）
}

// New 构建 Gin 引擎并注册路由。
func New(deps *Dependencies) *gin.Engine {
	if deps == nil {
		panic("router.New: nil Dependencies")
	}
	r := gin.New()
	r.Use(gin.Recovery(), gin.Logger())
	r.Use(corsMiddleware())

	// Registered before the password guard: loopback-only, see handler.RuntimeBusy.
	r.GET("/api/runtime/busy", handler.RuntimeBusy(deps.Chats))

	if deps.Auth != nil && deps.Auth.Enabled() {
		r.Use(deps.Auth.Middleware())
		r.GET("/login", deps.Auth.LoginGET(deps.LoginHTMLPath))
		r.POST("/api/login", deps.Auth.LoginPOST())
		r.POST("/api/logout", deps.Auth.LogoutPOST())
	}

	staticDir := filepath.Join(deps.WebRoot, "static")
	r.Static("/assets", staticDir)

	indexFile := handler.IndexPath(deps.WebRoot)
	r.GET("/", handler.IndexHTML(indexFile))
	r.GET("/ws", handler.WebSocket(deps.Chats))
	r.GET("/api/chats", handler.ChatsList(deps.Chats))
	r.POST("/api/chats", handler.ChatsCreate(deps.Chats))
	r.PATCH("/api/chats/:id", handler.ChatsRename(deps.Chats))
	r.DELETE("/api/chats/:id", handler.ChatsDelete(deps.Chats))
	r.GET("/api/prompt_queue", handler.PromptQueue(deps.Chats))
	r.GET("/api/events", handler.EventsBefore(deps.Chats))
	r.GET("/api/capabilities", handler.Capabilities(deps.Chats.Default()))
	r.GET("/api/models", handler.ModelsGET(deps.Chats))
	r.POST("/api/model", handler.ModelPOST(deps.Chats))
	r.GET("/api/qq/config", handler.QQConfig(deps.QQBot))
	r.POST("/api/qq/config", handler.QQConfigUpdate(deps.QQBot))

	// SPA 回退：未知 GET 仍回 index
	r.NoRoute(func(c *gin.Context) {
		if c.Request.Method != http.MethodGet {
			c.Status(http.StatusNotFound)
			return
		}
		p := c.Request.URL.Path
		if strings.HasPrefix(p, "/api/") {
			c.Status(http.StatusNotFound)
			return
		}
		c.File(indexFile)
	})

	return r
}

func corsMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Access-Control-Allow-Origin", "*")
		c.Header("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
		c.Header("Access-Control-Allow-Headers", "Content-Type, Authorization")
		if c.Request.Method == http.MethodOptions {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}
		c.Next()
	}
}

// Serve 启动 HTTP；收到 SIGINT/SIGTERM 时优雅关闭（关闭监听并等待已有请求，含 WebSocket 连接）。
func Serve(addr string, e *gin.Engine) error {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("监听 %s: %w", addr, err)
	}

	srv := &http.Server{
		Handler: e,
	}

	errCh := make(chan error, 1)
	go func() {
		err := srv.Serve(ln)
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
			return
		}
		errCh <- nil
	}()

	log.Printf("acp-bridge 已启动 http://%s（web: 磁盘 · Ctrl+C 优雅退出）", addr)

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	select {
	case err := <-errCh:
		return err
	case <-quit:
		log.Println("acp-bridge: 收到退出信号，正在关闭…")
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := srv.Shutdown(ctx); err != nil {
			return fmt.Errorf("关闭服务: %w", err)
		}
		if err := <-errCh; err != nil {
			return err
		}
		log.Println("acp-bridge: 已退出")
		return nil
	}
}
