package chatsession

import (
	"context"
	"errors"

	"github.com/cocofhu/grasp/internal/models"
)

func (s *DurableSession[T]) decodedMetadataSnapshot() (DurableSnapshot, []T, T, bool, error) {
	var zero T
	ctx := context.Background()
	snapshot, err := s.store.SnapshotMetadata(ctx, s.scope)
	if err != nil {
		return snapshot, nil, zero, false, err
	}
	pending := make([]T, 0, len(snapshot.Pending))
	for _, row := range snapshot.Pending {
		item, err := s.decodeMetadata(ctx, row)
		if err != nil {
			return snapshot, nil, zero, false, err
		}
		pending = append(pending, item)
	}
	if snapshot.Active == nil {
		return snapshot, pending, zero, false, nil
	}
	active, err := s.decodeMetadata(ctx, *snapshot.Active)
	return snapshot, pending, active, true, err
}

func (s *DurableSession[T]) decodeMetadata(ctx context.Context, row models.ChatQueueTurn) (T, error) {
	var zero T
	if row.Display == "" {
		// This path is only for receipts accepted before previews were stored.
		// SnapshotMetadata has already released its transaction and queue lock.
		full, err := s.store.ReadTurn(ctx, s.scope, row.ID)
		if err != nil {
			return zero, err
		}
		item, err := s.cfg.Decode(full)
		if err != nil {
			return zero, err
		}
		intent, err := s.cfg.Intent(item)
		if err != nil {
			return zero, err
		}
		if intent.ID != row.ID || len(intent.Display) == 0 {
			return zero, errors.New("accepted message display preview is unavailable")
		}
		if err := s.store.BackfillDisplay(ctx, s.scope, row.ID, full.Fingerprint, intent.Display); err != nil {
			return zero, err
		}
		row.Display = string(intent.Display)
	}
	return s.cfg.DecodeMetadata(row)
}
