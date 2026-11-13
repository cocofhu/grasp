package services

import (
	"errors"
	"strconv"
	"strings"

	"github.com/cocofhu/grasp/internal/models"
	"gorm.io/gorm"
)

func (c *SandboxChats) Active(id uint) bool {
	c.mu.Lock()
	sc := c.chats[id]
	c.mu.Unlock()
	return sc != nil && !sc.sess.Ready()
}

func (s *NotificationService) List(username string) ([]NotificationItemDTO, error) {
	res, err := s.ListPage(username, "all", 1, 1<<31-1)
	if err != nil {
		return nil, err
	}
	return res.Items, nil
}

func (s *SoloService) contextPreamble(sessionID, exceptMsgID, header string) string {
	text, _, _ := s.contextPreambleChecked(sessionID, exceptMsgID, header)
	return text
}

func (s *ChannelConfigService) GetByID(id string) (ChannelConfigDTO, error) {
	var row models.ChannelConfig
	if err := s.db.First(&row, "id = ?", strings.TrimSpace(id)).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ChannelConfigDTO{}, ErrChannelNotFound
		}
		return ChannelConfigDTO{}, err
	}
	return s.attachRuntime(s.toChannelDTO(row)), nil
}

func (s *SettingsService) setInt(key string, v int) error {
	// Key is the primary key, so Save upserts (update when present, else insert).
	return setSetting(s.db, key, strconv.Itoa(v))
}
