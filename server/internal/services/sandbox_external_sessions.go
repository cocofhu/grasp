package services

import (
	"sync"
	"time"
)

// AcquireExternalSession marks a user connection (for example a local IDE
// over the SSH gateway) that is using sandbox id. While any is open the
// sandbox is not idle-reclaimed or memory-trimmed. When the last one closes,
// the idle interval restarts from that moment. The returned release is safe
// to call more than once.
func (s *SandboxService) AcquireExternalSession(id uint) (release func()) {
	if s == nil || id == 0 {
		return func() {}
	}
	s.mu.Lock()
	if s.externalSessions == nil {
		s.externalSessions = map[uint]int{}
	}
	s.externalSessions[id]++
	s.mu.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() { s.releaseExternalSession(id) })
	}
}

func (s *SandboxService) releaseExternalSession(id uint) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.externalSessions[id] <= 1 {
		delete(s.externalSessions, id)
		if s.externalEnded == nil {
			s.externalEnded = map[uint]time.Time{}
		}
		s.externalEnded[id] = time.Now()
		return
	}
	s.externalSessions[id]--
}

// ExternalSessions reports how many external connections sandbox id has open.
func (s *SandboxService) ExternalSessions(id uint) int {
	if s == nil {
		return 0
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.externalSessions[id]
}

func (s *SandboxService) externalBusy(id uint) bool {
	return s.ExternalSessions(id) > 0
}
