package embed

import (
	"errors"
	"time"

	"gorm.io/gorm"
)

func (s *Store) CreateSession(c Claims) (string, time.Time, error) {
	if s == nil || s.db == nil {
		return "", time.Time{}, errors.New("embed store unavailable")
	}
	var (
		token string
		exp   time.Time
	)
	now := s.now()
	err := s.db.Transaction(func(tx *gorm.DB) error {
		var err error
		token, exp, err = createSessionTx(tx, c, now)
		return err
	})
	if err != nil {
		return "", time.Time{}, err
	}
	return token, exp, nil
}
