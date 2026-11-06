package models

import (
	"time"

	"gorm.io/gorm"
)

// ArtifactAttachment pins the exact delivered revision. It contains no storage
// path or bytes and is safe to keep on a message and return to an authorized UI.
type ArtifactAttachment struct {
	ArtifactID string `json:"artifactId"`
	Revision   int    `json:"revision"`
	Name       string `json:"name"`
	Kind       string `json:"kind"`
	MimeType   string `json:"mimeType,omitempty"`
	SizeBytes  int    `json:"sizeBytes"`
	Caption    string `json:"caption,omitempty"`
}

type ArtifactDelivery struct {
	ID                 string `gorm:"primaryKey;size:80" json:"id"`
	RunID              string `gorm:"index:idx_delivery_turn,priority:1;size:64" json:"-"`
	NodeID             string `gorm:"index:idx_delivery_turn,priority:2;size:128" json:"-"`
	TurnID             string `gorm:"index:idx_delivery_turn,priority:3;size:191" json:"-"`
	SessionID          string `gorm:"index;size:191" json:"-"`
	StateRunID         uint   `gorm:"index" json:"-"`
	Iteration          int    `json:"-"`
	ArtifactAttachment `gorm:"embedded"`
	CreatedAt          time.Time `json:"createdAt"`
}

func (s *StateRun) AfterFind(tx *gorm.DB) error {
	s.Files = nil
	if !s.HasFileDeliveries {
		return nil
	}
	var files []ArtifactDelivery
	if err := tx.Session(&gorm.Session{NewDB: true}).Where("state_run_id = ?", s.ID).Order("created_at,id").Find(&files).Error; err != nil {
		return err
	}
	for _, f := range files {
		s.Files = append(s.Files, f.ArtifactAttachment)
	}
	return nil
}

func (c *ReactConversation) AfterFind(tx *gorm.DB) error {
	if !c.HasFileDeliveries {
		return nil
	}
	var files []ArtifactDelivery
	if err := tx.Session(&gorm.Session{NewDB: true}).Where("run_id = ? AND node_id = ? AND iteration = ?", c.RunID, c.NodeID, c.Iteration).Order("created_at,id").Find(&files).Error; err != nil {
		return err
	}
	last := map[string]int{}
	for i, m := range c.Messages {
		if m.Role == "agent" && m.TurnID != "" {
			last[m.TurnID] = i
		}
	}
	for _, f := range files {
		if i, ok := last[f.TurnID]; ok {
			duplicate := false
			for _, old := range c.Messages[i].Files {
				if old.ArtifactID == f.ArtifactID && old.Revision == f.Revision {
					duplicate = true
					break
				}
			}
			if !duplicate {
				c.Messages[i].Files = append(c.Messages[i].Files, f.ArtifactAttachment)
			}
		}
	}
	return nil
}
