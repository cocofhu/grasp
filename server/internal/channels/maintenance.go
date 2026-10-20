package channels

// BeginMaintenance refuses new inbound messages while keeping adapters available
// for queued replies and already-claimed notifications.
func (m *Manager) BeginMaintenance() {
	m.admissionMu.Lock()
	m.maintenance = true
	m.admissionMu.Unlock()
}
func (m *Manager) PendingMaintenance() int64 {
	n := m.dispatchActive.Load() + m.sending.Load() + m.flushing.Load()
	m.convMu.Lock()
	for _, q := range m.convQueues {
		q.mu.Lock()
		n += int64(len(q.pending))
		if q.busy {
			n++
		}
		q.mu.Unlock()
	}
	m.convMu.Unlock()
	m.pushMu.Lock()
	for _, q := range m.pushQueues {
		q.mu.Lock()
		n += int64(len(q.pending))
		q.mu.Unlock()
	}
	m.pushMu.Unlock()
	return n
}

// FlushMaintenance retries idle push queues without closing channel connections.
func (m *Manager) FlushMaintenance() {
	m.pushMu.Lock()
	keys := make([]string, 0, len(m.pushQueues))
	for k := range m.pushQueues {
		keys = append(keys, k)
	}
	m.pushMu.Unlock()
	for _, key := range keys {
		_ = m.flushPushQueue(key)
	}
}
