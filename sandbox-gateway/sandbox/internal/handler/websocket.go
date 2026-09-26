package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"backend/internal/acp"
	"backend/internal/correl"
	"backend/internal/service"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 4096,
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

func wsPayloadPreview(b []byte, maxLen int) string {
	if len(b) <= maxLen {
		return string(b)
	}
	return string(b[:maxLen]) + fmt.Sprintf("…(共%d字节)", len(b))
}

// WebSocket 处理与前端的 ACP 控制消息。
func WebSocket(bridge *service.Bridge) gin.HandlerFunc {
	return func(c *gin.Context) {
		conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
		if err != nil {
			log.Printf("ws: WebSocket 升级失败 remote=%s: %v", c.ClientIP(), err)
			return
		}
		cid := correl.ID()
		remote := c.ClientIP()
		log.Printf("ws cid=%s remote=%s: 已连接", cid, remote)

		defer func() {
			bridge.UnregisterClient(conn)
			log.Printf("ws cid=%s remote=%s: 已断开", cid, remote)
		}()
		defer func() {
			if err := conn.Close(); err != nil {
				log.Printf("ws cid=%s: 关闭连接失败: %v", cid, err)
			}
		}()

		bridge.RegisterClient(conn)

		if p := bridge.Session(); p != nil {
			if err := bridge.WriteJSONWS(conn, bridge.ConnectedPayload(p)); err != nil {
				log.Printf("ws cid=%s: 同步 connected 失败: %v", cid, err)
			} else {
				log.Printf("ws cid=%s: 已同步当前会话 sid=%s", cid, p.SessionID())
			}
		}
		bridge.BroadcastQueueState()

		for {
			_, payload, err := conn.ReadMessage()
			if err != nil {
				var ce *websocket.CloseError
				if errors.As(err, &ce) {
					log.Printf("ws cid=%s: 对端关闭 code=%d text=%q", cid, ce.Code, ce.Text)
				} else {
					log.Printf("ws cid=%s: ReadMessage 结束: %v", cid, err)
				}
				return
			}
			var msg struct {
				Op string `json:"op"`
			}
			if err := json.Unmarshal(payload, &msg); err != nil {
				log.Printf("ws cid=%s: 无法解析消息 JSON（忽略）: %v body=%q", cid, err, wsPayloadPreview(payload, 180))
				continue
			}
			switch msg.Op {
			case "connect":
				if err := handleConnect(cid, conn, bridge, payload); err != nil {
					log.Printf("ws cid=%s: connect 失败: %v", cid, err)
					if errors.Is(err, context.Canceled) {
						writeWSError(bridge, conn, "连接被取消：请勿多标签同时连同一服务；请只保留一个页面并刷新。")
					} else {
						writeWSError(bridge, conn, acp.UserFacingAny(err))
					}
				}
			case "chat":
				chatOpID, err := handleChat(bridge, payload)
				if err != nil {
					log.Printf("ws cid=%s oid=%s: chat 失败: %v", cid, chatOpID, err)
					writeWSError(bridge, conn, acp.UserFacingAny(err))
				}
			case "cancel":
				var body struct {
					OpID string `json:"opId"`
				}
				_ = json.Unmarshal(payload, &body)
				bridge.CancelPromptOp(body.OpID)
			case "restart_agent":
				panel, err := bridge.RestartAgent()
				if err != nil {
					log.Printf("ws cid=%s: restart_agent 失败: %v", cid, err)
					writeWSError(bridge, conn, acp.UserFacingAny(err))
				} else {
					bridge.Broadcast(bridge.ConnectedPayload(panel))
					bridge.BroadcastQueueState()
				}
			case "permission":
				var body struct {
					RpcID    string `json:"rpcId"`
					OptionID string `json:"optionId"`
				}
				if err := json.Unmarshal(payload, &body); err != nil {
					log.Printf("ws cid=%s: permission 消息无效: %v", cid, err)
					writeWSError(bridge, conn, "权限响应格式错误")
					continue
				}
				if err := bridge.ResolvePermission(body.RpcID, body.OptionID); err != nil {
					log.Printf("ws cid=%s: permission 处理失败: %v", cid, err)
					writeWSError(bridge, conn, acp.UserFacingAny(err))
				}
			default:
				log.Printf("ws cid=%s: 未知 op=%q", cid, msg.Op)
				writeWSError(bridge, conn, "未知操作: "+msg.Op)
			}
		}
	}
}

func handleConnect(cid string, conn *websocket.Conn, bridge *service.Bridge, payload []byte) error {
	cwd, fsRoot, mcp, auto, err := service.ParseConnect(payload)
	if err != nil {
		return err
	}
	// 全局单例：由 agents.Current() 决定 provider/argv，不使用浏览器 JSON 里的 argv。
	// 始终走 EnsureAgent：已有会话且 MCP 无需升级时复用；空 MCP→非空时重建。
	log.Printf("ws cid=%s: connect 将 EnsureAgent cwd=%q fsRoot=%q", cid, cwd, fsRoot)
	sess, err := bridge.EnsureAgent(cwd, fsRoot, mcp, auto)
	if err != nil {
		log.Printf("ws cid=%s: EnsureAgent/握手失败: %v", cid, err)
		return err
	}
	log.Printf("ws cid=%s sid=%s: 握手成功 agent=%s", cid, sess.SessionID(), sess.Info().Name)
	if err := bridge.WriteJSONWS(conn, bridge.ConnectedPayload(sess)); err != nil {
		return err
	}
	bridge.BroadcastQueueState()
	return nil
}

const maxChatClientOpIDLen = 128

// handleChat 返回最终入队使用的 opId（与客户端 opId/id 或服务端生成一致），便于与 prompt 日志 oid= 交叉检索。
func handleChat(bridge *service.Bridge, payload []byte) (opID string, err error) {
	var body struct {
		Text    string                `json:"text"`
		Action  string                `json:"action"`
		Type    string                `json:"type"`
		Content string                `json:"content"`
		OpID    string                `json:"opId"`
		ID      string                `json:"id"`
		Images  []service.PromptImage `json:"images"`
		// DeadlineSec overrides the bridge's total-duration limit for this turn.
		DeadlineSec int `json:"deadlineSec"`
	}
	if err := json.Unmarshal(payload, &body); err != nil {
		return "", err
	}
	text := strings.TrimSpace(body.Text)
	if text == "" {
		text = strings.TrimSpace(body.Content)
	}
	act := strings.TrimSpace(body.Action)
	if act == "" {
		act = strings.TrimSpace(body.Type)
	}
	opID = strings.TrimSpace(body.OpID)
	if opID == "" {
		opID = strings.TrimSpace(body.ID)
	}
	if len(opID) > maxChatClientOpIDLen {
		opID = opID[:maxChatClientOpIDLen]
	}
	if opID == "" {
		opID = correl.ID()
	}
	var maxDuration time.Duration
	if body.DeadlineSec > 0 {
		maxDuration = time.Duration(body.DeadlineSec) * time.Second
	}
	err = bridge.ChatWithDeadline(text, opID, act, body.Images, maxDuration)
	return opID, err
}

func writeWSError(bridge *service.Bridge, c *websocket.Conn, msg string) {
	b, err := json.Marshal(map[string]any{"op": "error", "message": msg})
	if err != nil {
		log.Printf("ws: 构造 error JSON 失败: %v", err)
		return
	}
	if err := bridge.WriteTextWS(c, b); err != nil {
		log.Printf("ws: 写入 error 帧失败: %v（原消息: %q）", err, msg)
	}
}
