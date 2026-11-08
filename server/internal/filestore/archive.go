package filestore

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/cocofhu/grasp/internal/objectstore"
)

// ArchiveLocal copies immutable legacy payloads, preserving every fileref and
// retaining the source files. A partial run is safe to repeat after an outage.
func (s *Store) ArchiveLocal(ctx context.Context) (objectstore.ArchiveReport, error) {
	var report objectstore.ArchiveReport
	if s.objects == nil {
		return report, nil
	}
	root := filepath.Join(s.Root, "sha256")
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		digest := entry.Name()
		if _, _, err := Parse(Prefix + Bytes + "/" + digest); err != nil {
			return fmt.Errorf("invalid legacy payload name: %s", entry.Name())
		}
		if path != s.path(digest) || entry.Type()&os.ModeSymlink != 0 {
			return errors.New("invalid legacy payload layout")
		}
		file, err := os.Open(path)
		if err != nil {
			return err
		}
		defer func() { _ = file.Close() }()
		stat, err := file.Stat()
		if err != nil {
			return err
		}
		if !stat.Mode().IsRegular() {
			return errors.New("legacy payload is not regular")
		}
		hash := sha256.New()
		size, err := io.Copy(hash, file)
		if err != nil {
			return err
		}
		if size != stat.Size() || hex.EncodeToString(hash.Sum(nil)) != digest {
			return errors.New("legacy payload integrity check failed")
		}
		if _, err := file.Seek(0, io.SeekStart); err != nil {
			return err
		}
		if err := objectstore.ArchiveImmutable(ctx, s.objects, objectKey(digest), file, objectstore.Info{Size: size, ContentType: "application/octet-stream", Metadata: map[string]string{"grasp-sha256": digest}}, digest); err != nil {
			return err
		}
		report.Files++
		report.Bytes += size
		return nil
	})
	return report, err
}
