package main

import (
	"flag"
	"log"
	"os"
	"path/filepath"
	"strings"

	"backend/internal/auth"
	"backend/internal/config"
	"backend/internal/logging"
	"backend/internal/qqbot"
	"backend/internal/router"
	"backend/internal/service"

	"github.com/gin-gonic/gin"
	zlog "github.com/rs/zerolog/log"
)

func main() {
	logging.Init("universal-sandbox")
	log.SetFlags(0)
	log.SetPrefix("")
	log.SetOutput(logging.StdBridge())

	listen := flag.String("listen", "0.0.0.0:8765", "HTTP 监听地址（对外服务可用 0.0.0.0:端口，勿写 0.0.0.1）")
	ginMode := flag.String("gin-mode", "debug", "Gin 模式: debug | release | test")
	webDir := flag.String("web", "web", "前端静态目录：内含 index.html 与 static/（相对当前工作目录或绝对路径）")
	password := flag.String("password", "", "访问口令；非空时必须先登录。也可设环境变量 ACP_BRIDGE_PASSWORD（优先级低于本参数）")
	model := flag.String("model", "", "默认 Agent 模型（如 claude-sonnet），各会话可在页面另选。也可设环境变量 ACP_BRIDGE_MODEL（优先级低于本参数）")
	qqConfigPath := flag.String("qq-config", "qq_bot.json", "QQ 机器人配置文件路径（JSON，可通过页面左上角 QQ 图标配置）")
	flag.Parse()

	authPassword := strings.TrimSpace(*password)
	if authPassword == "" {
		authPassword = strings.TrimSpace(os.Getenv("ACP_BRIDGE_PASSWORD"))
	}

	agentModel := strings.TrimSpace(*model)
	if agentModel == "" {
		agentModel = strings.TrimSpace(os.Getenv("ACP_BRIDGE_MODEL"))
	}

	cfg := config.Config{
		ListenAddr: *listen,
		GinMode:    *ginMode,
		Model:      agentModel,
	}
	gin.SetMode(cfg.GinMode)

	webRoot, err := filepath.Abs(*webDir)
	if err != nil {
		zlog.Fatal().Err(err).Str("web", *webDir).Msg("resolve web path failed")
	}
	indexPath := filepath.Join(webRoot, "index.html")
	if st, err := os.Stat(indexPath); err != nil || st.IsDir() {
		zlog.Fatal().Err(err).Str("path", indexPath).Msg("frontend index missing")
	}
	staticDir := filepath.Join(webRoot, "static")
	if st, err := os.Stat(staticDir); err != nil || !st.IsDir() {
		zlog.Fatal().Err(err).Str("path", staticDir).Msg("frontend static dir missing")
	}

	loginPath := filepath.Join(webRoot, "login.html")
	if authPassword != "" {
		if st, err := os.Stat(loginPath); err != nil || st.IsDir() {
			zlog.Fatal().Err(err).Str("path", loginPath).Msg("login page missing with auth enabled")
		}
	}

	chats := service.NewChatManager()
	chats.SetDefaultModel(agentModel)
	if agentModel != "" {
		zlog.Info().Str("model", agentModel).Msg("agent default model")
	}
	chats.StartDefaultAgent()
	qqService, err := qqbot.New(chats.Default(), qqbot.NewStore(*qqConfigPath))
	if err != nil {
		zlog.Fatal().Err(err).Str("qq_config", *qqConfigPath).Msg("qq bot init failed")
	}
	qqService.Start()

	engine := router.New(&router.Dependencies{
		WebRoot:       webRoot,
		Chats:         chats,
		QQBot:         qqService,
		Auth:          auth.NewGuard(authPassword),
		LoginHTMLPath: loginPath,
	})

	zlog.Info().Str("listen", cfg.ListenAddr).Msg("acp-bridge listening")
	if err := router.Serve(cfg.ListenAddr, engine); err != nil {
		zlog.Fatal().Err(err).Str("listen", cfg.ListenAddr).Msg("http server failed")
	}
}
