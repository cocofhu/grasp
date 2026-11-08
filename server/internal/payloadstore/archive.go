package payloadstore

import (
	"context"
	"time"

	"github.com/cocofhu/grasp/internal/blob"
	"github.com/cocofhu/grasp/internal/objectstore"
	"github.com/rs/zerolog/log"
)

type ArchiveReport struct {
	Payloads    objectstore.ArchiveReport `json:"payloads"`
	Attachments objectstore.ArchiveReport `json:"attachments"`
}

func (s *Storage) ArchiveLegacy(ctx context.Context) (ArchiveReport, error) {
	var report ArchiveReport
	var err error
	if s.Objects == nil {
		return report, nil
	}
	report.Payloads, err = s.Files.ArchiveLocal(ctx)
	if err != nil {
		return report, err
	}
	if objects, ok := s.Blobs.(*blob.Objects); ok {
		report.Attachments, err = objects.ArchiveLegacy(ctx)
	}
	return report, err
}

// RunLegacyArchive migrates immutable bytes while the server remains usable.
// It never deletes local files; removing a volume needs a separate verified
// reference-closure backup, not a successful background-copy message.
func (s *Storage) RunLegacyArchive(ctx context.Context) {
	if s.Objects == nil {
		return
	}
	for {
		report, err := s.ArchiveLegacy(ctx)
		if err == nil {
			log.Info().Interface("report", report).Msg("legacy payload archive verified; local files retained")
			return
		}
		if ctx.Err() != nil {
			return
		}
		log.Warn().Err(err).Msg("legacy payload archive incomplete; source files retained; retrying")
		timer := time.NewTimer(30 * time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}
