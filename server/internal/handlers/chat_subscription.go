package handlers

import (
	"time"

	"github.com/cocofhu/grasp/internal/chatsession"

	"github.com/gorilla/websocket"
)

// chatWriteWait bounds a data frame so a peer that stops reading cannot pin the
// subscription pump. Tests may shorten it. Ping uses the same bound.
var chatWriteWait = soloWriteWait

func writeChatJSON(conn *websocket.Conn, v any) error {
	_ = conn.SetWriteDeadline(time.Now().Add(chatWriteWait))
	defer func() { _ = conn.SetWriteDeadline(time.Time{}) }()
	return conn.WriteJSON(v)
}

// pumpChatSubscription 在订阅结束或写失败时关闭 WebSocket，使主读循环退出。
// 数据写在独立 goroutine 中进行：订阅 channel 关闭时即使写调用尚未返回，pump
// 也会关闭连接并退出，避免慢订阅把重连一直卡在阻塞写上。
func pumpChatSubscription(conn *websocket.Conn, ch <-chan chatsession.Event, write func(any) error, ping <-chan time.Time) {
	defer func() { _ = conn.Close() }()
	var pending []chatsession.Event
	var writing chan error
	for {
		if writing == nil && len(pending) > 0 {
			ev := pending[0]
			pending = pending[1:]
			writing = make(chan error, 1)
			frame := ev.Frame()
			done := writing
			_ = conn.SetWriteDeadline(time.Now().Add(chatWriteWait))
			go func() {
				done <- write(frame)
			}()
			continue
		}
		if writing != nil {
			select {
			case err := <-writing:
				writing = nil
				_ = conn.SetWriteDeadline(time.Time{})
				if err != nil {
					return
				}
			case ev, ok := <-ch:
				if !ok {
					return
				}
				pending = append(pending, ev)
			}
			continue
		}
		select {
		case ev, ok := <-ch:
			if !ok {
				return
			}
			pending = append(pending, ev)
		case <-ping:
			if err := conn.WriteControl(websocket.PingMessage, nil, time.Now().Add(chatWriteWait)); err != nil {
				return
			}
		}
	}
}
