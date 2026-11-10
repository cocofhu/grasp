package payloadstore

import (
	"context"

	"github.com/cocofhu/grasp/internal/blob"
	"github.com/cocofhu/grasp/internal/objectstore"
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
