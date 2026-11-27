package handler

import (
	"context"
	"crypto/rand"
	"errors"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jezek/xgb"
	"github.com/jezek/xgb/xfixes"
	"github.com/jezek/xgb/xproto"
)

const clipboardChangeWait = 180 * time.Millisecond

type clipboardState struct {
	Generation string `json:"generation"`
	Revision   uint64 `json:"revision"`
	Owned      bool   `json:"owned"`
}

func (s clipboardState) token() string {
	return s.Generation + ":" + strconv.FormatUint(s.Revision, 10)
}

// DesktopClipboardState reports X11 CLIPBOARD ownership changes without
// requesting any selection data. The watcher starts when a viewer needs it.
func DesktopClipboardState() gin.HandlerFunc {
	var monitor clipboardMonitor
	return func(c *gin.Context) {
		state, err := monitor.snapshot(c.Request.Context(), c.Query("after"))
		if err != nil {
			c.Status(http.StatusServiceUnavailable)
			return
		}
		c.JSON(http.StatusOK, state)
	}
}

type clipboardMonitor struct {
	mu      sync.Mutex
	conn    *xgb.Conn
	state   clipboardState
	changed chan struct{}
}

func (m *clipboardMonitor) snapshot(ctx context.Context, after string) (clipboardState, error) {
	m.mu.Lock()
	if m.conn == nil {
		conn, owned, err := openClipboardMonitor()
		if err != nil {
			m.mu.Unlock()
			return clipboardState{}, err
		}
		m.conn = conn
		m.state = clipboardState{Generation: rand.Text(), Owned: owned}
		m.changed = make(chan struct{})
		go m.watch(conn)
	}
	state, changed := m.state, m.changed
	m.mu.Unlock()
	if after == "" || after != state.token() {
		return state, nil
	}

	// The RFB copy key may have only just reached the application.
	timer := time.NewTimer(clipboardChangeWait)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return clipboardState{}, ctx.Err()
	case <-changed:
	case <-timer.C:
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.state, nil
}

func openClipboardMonitor() (*xgb.Conn, bool, error) {
	conn, err := xgb.NewConn()
	if err != nil {
		return nil, false, err
	}
	owned, err := subscribeClipboard(conn)
	if err != nil {
		conn.Close()
		return nil, false, err
	}
	return conn, owned, nil
}

func subscribeClipboard(conn *xgb.Conn) (bool, error) {
	if err := xfixes.Init(conn); err != nil {
		return false, err
	}
	if _, err := xfixes.QueryVersion(conn, 2, 0).Reply(); err != nil {
		return false, err
	}
	const selection = "CLIPBOARD"
	atom, err := xproto.InternAtom(conn, false, uint16(len(selection)), selection).Reply()
	if err != nil {
		return false, err
	}
	if atom == nil {
		return false, errors.New("X11 returned no clipboard atom")
	}
	root := xproto.Setup(conn).DefaultScreen(conn).Root
	const mask = xfixes.SelectionEventMaskSetSelectionOwner | xfixes.SelectionEventMaskSelectionWindowDestroy | xfixes.SelectionEventMaskSelectionClientClose
	if err := xfixes.SelectSelectionInputChecked(conn, root, atom.Atom, mask).Check(); err != nil {
		return false, err
	}
	owner, err := xproto.GetSelectionOwner(conn, atom.Atom).Reply()
	if err != nil {
		return false, err
	}
	if owner == nil {
		return false, errors.New("X11 returned no clipboard ownership reply")
	}
	return owner.Owner != xproto.WindowNone, nil
}

func (m *clipboardMonitor) watch(conn *xgb.Conn) {
	for {
		event, err := conn.WaitForEvent()
		if event == nil && err == nil {
			break
		}
		if change, ok := event.(xfixes.SelectionNotifyEvent); ok {
			m.mu.Lock()
			m.state.Revision++
			m.state.Owned = change.Owner != xproto.WindowNone
			close(m.changed)
			m.changed = make(chan struct{})
			m.mu.Unlock()
		}
	}
	m.mu.Lock()
	m.conn = nil
	m.state = clipboardState{}
	close(m.changed)
	m.mu.Unlock()
}
