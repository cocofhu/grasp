package payloadhold

import (
	"context"

	"gorm.io/gorm"
)

// publication never spans a PUT/GET. The fresh authority proof and current
// grant are checked before reservation/hold locks and again before commit.
func (m *Manager) publication(ctx context.Context, id Identity, body func(*gorm.DB) error) error {
	if err := m.outsideTransaction(); err != nil {
		return err
	}
	if err := m.verifyAuthority(ctx, id); err != nil {
		return err
	}
	if m.options.PrepareHoldAuthorization != nil {
		var err error
		ctx, err = m.options.PrepareHoldAuthorization(ctx, id)
		if err != nil {
			return err
		}
	}
	if m.options.PrepareAuthorization != nil {
		var err error
		ctx, err = m.options.PrepareAuthorization(ctx)
		if err != nil {
			return err
		}
	}
	return m.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := m.options.Authorize(ctx, tx, id); err != nil {
			return err
		}
		if err := body(tx); err != nil {
			return err
		}
		return m.options.Authorize(ctx, tx, id)
	})
}
