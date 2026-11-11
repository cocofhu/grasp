package service

import "time"

func (b *Bridge) activeOpID() string {
	b.turnMu.Lock()
	defer b.turnMu.Unlock()
	if b.activeTurn == nil {
		return ""
	}
	return b.activeTurn.opID
}

func (b *Bridge) ChatWithDeadline(text, opID, action string, images []PromptImage, maxDuration time.Duration) error {
	return b.ChatWithLimits(text, opID, action, images, maxDuration, 0)
}
