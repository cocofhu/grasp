package handlers

import (
	"time"

	"github.com/cocofhu/grasp/internal/chatsession"

	"github.com/gorilla/websocket"
)

// pumpChatSubscription 在订阅结束或写失败时关闭 WebSocket，使主读循环退出。
func pumpChatSubscription(conn *websocket.Conn, ch <-chan chatsession.Event, write func(any) error, ping <-chan time.Time) {
	defer func() { _ = conn.Close() }()
	for {
		select {
		case ev, ok := <-ch:
			if !ok {
				return
			}
			if err := write(ev.Frame()); err != nil {
				return
			}
		case <-ping:
			if err := conn.WriteControl(websocket.PingMessage, nil, time.Now().Add(soloWriteWait)); err != nil {
				return
			}
		}
	}
}
