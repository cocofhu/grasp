package objectstore

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
)

type ArchiveReport struct {
	Files int64 `json:"files"`
	Bytes int64 `json:"bytes"`
}

// ArchiveImmutable never overwrites a different object at the same logical
// key. Completion requires a GET and SHA-256 verification, not just PUT/HEAD.
func ArchiveImmutable(ctx context.Context, backend Store, key string, source io.ReadSeeker, info Info, digest string) error {
	head, err := backend.Head(ctx, key)
	if err == nil {
		if head.Size != info.Size || head.Metadata["grasp-sha256"] != digest {
			return fmt.Errorf("archive target conflicts with immutable source: %s", key)
		}
	} else if errors.Is(err, os.ErrNotExist) {
		if err := backend.Put(ctx, key, source, info); err != nil {
			return err
		}
	} else {
		return err
	}
	body, stored, err := backend.Open(ctx, key)
	if err != nil {
		return err
	}
	defer func() { _ = body.Close() }()
	if stored.Size != info.Size {
		return errors.New("archived object size differs")
	}
	verified, err := DownloadVerified(ctx, body, info.Size, digest)
	if err != nil {
		return err
	}
	return verified.Close()
}
