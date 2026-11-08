package blob

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/cocofhu/grasp/internal/objectstore"
)

func (s *Objects) ArchiveLegacy(ctx context.Context) (objectstore.ArchiveReport, error) {
	var report objectstore.ArchiveReport
	if s.legacy == nil {
		return report, nil
	}
	err := filepath.WalkDir(s.legacy.Root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return errors.New("legacy attachment contains a symlink")
		}
		if entry.IsDir() || entry.Name() != "meta.json" {
			return nil
		}
		id := filepath.Base(filepath.Dir(path))
		ref, err := ParseRef("blob:" + id)
		if err != nil {
			return err
		}
		if filepath.Dir(path) != s.legacy.dirFor(id) {
			return errors.New("invalid legacy attachment layout")
		}
		body, meta, err := s.legacy.Open(ctx, ref)
		if err != nil {
			return err
		}
		file, err := objectstore.Spool(ctx, body, s.limit)
		_ = body.Close()
		if err != nil {
			return err
		}
		defer func() { _ = file.Close() }()
		if file.Size != meta.Size || file.SHA256 != meta.SHA256 {
			return errors.New("legacy attachment integrity check failed")
		}
		raw, err := json.Marshal(meta)
		if err != nil {
			return err
		}
		if err := objectstore.ArchiveImmutable(ctx, s.backend, attachmentKey(ref), file, objectstore.Info{Size: meta.Size, ContentType: meta.MimeType, Metadata: map[string]string{"grasp-meta": base64.StdEncoding.EncodeToString(raw), "grasp-sha256": meta.SHA256}}, meta.SHA256); err != nil {
			return err
		}
		report.Files++
		report.Bytes += meta.Size
		return nil
	})
	return report, err
}
