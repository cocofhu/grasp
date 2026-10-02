package engine

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
)

// pageSession binds one running turn to the page of whoever sent it. The id
// travels only in that turn's prompt; page_* calls must present it.
type pageSession struct {
	runID, nodeID, lane, owner string
	done                       <-chan struct{}
}

func newPageSessionID() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic(fmt.Sprintf("page session id: %v", err))
	}
	return "ps_" + base64.RawURLEncoding.EncodeToString(b)
}

// mintPageSession issues the page session for a turn that has a sender.
// Returns "" for turns nobody sent (they may not drive any page).
func (e *Engine) mintPageSession(s *reviewSession, owner string, done <-chan struct{}) string {
	if owner == "" {
		return ""
	}
	id := newPageSessionID()
	e.pageMu.Lock()
	if e.pageSessions == nil {
		e.pageSessions = map[string]*pageSession{}
	}
	e.pageSessions[id] = &pageSession{runID: s.runID, nodeID: s.producerID, lane: s.lane, owner: owner, done: done}
	e.pageMu.Unlock()
	return id
}

func (e *Engine) revokePageSession(id string) {
	if id == "" {
		return
	}
	e.pageMu.Lock()
	delete(e.pageSessions, id)
	e.pageMu.Unlock()
}

// revokeLanePageSessions drops every page session a lane still holds.
func (e *Engine) revokeLanePageSessions(runID, nodeID, lane string) {
	e.pageMu.Lock()
	for id, ps := range e.pageSessions {
		if ps.runID == runID && ps.nodeID == nodeID && ps.lane == lane {
			delete(e.pageSessions, id)
		}
	}
	e.pageMu.Unlock()
}

// PageTurn resolves a page_* call's session id to the sender of the turn
// still running on that node, plus a channel closed when the turn ends.
func (e *Engine) PageTurn(runID, nodeID, sessionID string) (owner string, done <-chan struct{}, ok bool) {
	if sessionID == "" {
		return "", nil, false
	}
	e.pageMu.Lock()
	ps := e.pageSessions[sessionID]
	e.pageMu.Unlock()
	if ps == nil || ps.runID != runID || ps.nodeID != nodeID {
		return "", nil, false
	}
	select {
	case <-ps.done:
		return "", nil, false
	default:
	}
	return ps.owner, ps.done, true
}

// withPageSession prefixes the agent prompt with the turn's page session id.
func withPageSession(id, prompt string) string {
	if id == "" {
		return prompt
	}
	return "本轮页面操作 session_id: " + id + "(调用 page_* 时必须原样传入,不要写进文件或回复)\n\n" + prompt
}
