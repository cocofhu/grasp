package qqbot

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/gorilla/websocket"
)

func (s *Service) runGateway() {
	s.wsMu.Lock()
	if s.wsRunning {
		s.wsMu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.wsCancel = cancel
	s.wsRunning = true
	s.wsState = "connecting"
	s.wsMu.Unlock()

	defer func() {
		s.wsMu.Lock()
		s.wsRunning = false
		s.wsState = "disconnected"
		s.wsConn = nil
		s.wsMu.Unlock()
	}()

	reconnectAttempts := 0
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		err := s.connectAndServe(ctx)
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			log.Printf("qqbot: WebSocket 断开: %v", err)
		}

		s.wsMu.Lock()
		s.wsState = "connecting"
		s.wsMu.Unlock()

		reconnectAttempts++
		delay := reconnectDelay(reconnectAttempts)
		log.Printf("qqbot: %v 后重连 (第 %d 次)", delay, reconnectAttempts)

		select {
		case <-ctx.Done():
			return
		case <-time.After(delay):
		}
	}
}

func reconnectDelay(attempt int) time.Duration {
	delays := []time.Duration{
		1 * time.Second,
		2 * time.Second,
		5 * time.Second,
		10 * time.Second,
		30 * time.Second,
		60 * time.Second,
	}
	idx := attempt - 1
	if idx < 0 {
		idx = 0
	}
	if idx >= len(delays) {
		idx = len(delays) - 1
	}
	return delays[idx]
}

func (s *Service) connectAndServe(ctx context.Context) error {
	cfg := s.Config()
	token, err := s.accessToken(ctx, cfg)
	if err != nil {
		return fmt.Errorf("获取 token: %w", err)
	}

	gwURL, err := s.getGatewayURL(ctx, token)
	if err != nil {
		return fmt.Errorf("获取 gateway URL: %w", err)
	}

	log.Printf("qqbot: 连接 WebSocket %s", gwURL)

	dialer := websocket.Dialer{
		HandshakeTimeout: 10 * time.Second,
	}
	conn, _, err := dialer.DialContext(ctx, gwURL, nil)
	if err != nil {
		return fmt.Errorf("WebSocket 连接失败: %w", err)
	}

	s.wsMu.Lock()
	s.wsConn = conn
	s.wsMu.Unlock()

	defer func() {
		_ = conn.Close()
		s.wsMu.Lock()
		if s.wsConn == conn {
			s.wsConn = nil
		}
		s.wsMu.Unlock()
	}()

	return s.serveWS(ctx, conn, token)
}

func (s *Service) serveWS(ctx context.Context, conn *websocket.Conn, token string) error {
	msgCh := make(chan wsPayload, 64)
	errCh := make(chan error, 1)

	go func() {
		for {
			_, raw, err := conn.ReadMessage()
			if err != nil {
				errCh <- err
				return
			}
			var p wsPayload
			if err := json.Unmarshal(raw, &p); err != nil {
				log.Printf("qqbot: 解析 WebSocket 消息失败: %v", err)
				continue
			}
			msgCh <- p
		}
	}()

	var heartbeatTicker *time.Ticker
	defer func() {
		if heartbeatTicker != nil {
			heartbeatTicker.Stop()
		}
	}()

	for {
		select {
		case <-ctx.Done():
			_ = conn.WriteMessage(websocket.CloseMessage,
				websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
			return ctx.Err()

		case err := <-errCh:
			return err

		case p := <-msgCh:
			if p.S > 0 {
				s.wsMu.Lock()
				s.lastSeq = p.S
				s.wsMu.Unlock()
			}

			switch p.Op {
			case opHello:
				var hello helloData
				if err := json.Unmarshal(p.D, &hello); err != nil {
					log.Printf("qqbot: Hello 解析失败: %v", err)
				}
				interval := hello.HeartbeatInterval
				if interval <= 0 {
					interval = 41250
				}
				log.Printf("qqbot: Hello 收到，心跳间隔 %dms", interval)

				if heartbeatTicker != nil {
					heartbeatTicker.Stop()
				}
				heartbeatTicker = time.NewTicker(time.Duration(interval) * time.Millisecond)

				// 尝试 Resume 或 Identify
				s.wsMu.Lock()
				sid := s.sessionID
				seq := s.lastSeq
				s.wsMu.Unlock()

				if sid != "" && seq > 0 {
					log.Printf("qqbot: 尝试 Resume session=%s seq=%d", sid, seq)
					resume := map[string]any{
						"op": opResume,
						"d": map[string]any{
							"token":      "QQBot " + token,
							"session_id": sid,
							"seq":        seq,
						},
					}
					if err := conn.WriteJSON(resume); err != nil {
						return err
					}
				} else {
					if err := s.sendIdentify(conn, token); err != nil {
						return err
					}
				}

			case opHeartbAck:
				// 心跳 ACK，无需处理

			case opInvalidSe:
				log.Printf("qqbot: Session 无效，重新 Identify")
				s.wsMu.Lock()
				s.sessionID = ""
				s.lastSeq = 0
				s.wsMu.Unlock()
				if err := s.sendIdentify(conn, token); err != nil {
					return err
				}

			case opDispatch:
				s.handleDispatch(ctx, p)
			}

		case <-func() <-chan time.Time {
			if heartbeatTicker != nil {
				return heartbeatTicker.C
			}
			return nil
		}():
			s.wsMu.Lock()
			seq := s.lastSeq
			s.wsMu.Unlock()
			hb := map[string]any{"op": opHeartbeat, "d": seq}
			if err := conn.WriteJSON(hb); err != nil {
				return fmt.Errorf("发送心跳失败: %w", err)
			}
		}
	}
}

func (s *Service) sendIdentify(conn *websocket.Conn, token string) error {
	intents := intentGroupAndC2C | intentInteraction
	log.Printf("qqbot: 发送 Identify intents=%d", intents)
	identify := map[string]any{
		"op": opIdentify,
		"d": map[string]any{
			"token":   "QQBot " + token,
			"intents": intents,
			"shard":   []int{0, 1},
		},
	}
	return conn.WriteJSON(identify)
}
