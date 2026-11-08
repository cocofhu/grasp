package objectstore

import (
	"context"
	"errors"
	"fmt"
	"maps"
)

// ReplicaObject is the credential-free inventory of one verified immutable
// object. ETags are intentionally not used as hashes (multipart ETags differ).
type ReplicaObject struct {
	Key         string            `json:"key"`
	Size        int64             `json:"size"`
	SHA256      string            `json:"sha256"`
	ContentType string            `json:"contentType"`
	Metadata    map[string]string `json:"metadata"`
}

// Replicate copies the entire logical prefix, including conservative orphans,
// logs and future snapshot/upload objects. Sources are never deleted. Repeating
// an interrupted copy verifies existing objects rather than trusting HEAD.
// The caller must fence application writers for a complete, stable inventory.
func Replicate(ctx context.Context, source, destination Store, record func(ReplicaObject) error) (ArchiveReport, error) {
	var result ArchiveReport
	if source == nil || destination == nil || record == nil {
		return result, errors.New("replica source, destination and inventory writer required")
	}
	cursor, previous := "", ""
	for {
		page, err := source.List(ctx, "", cursor)
		if err != nil {
			return result, err
		}
		for _, listed := range page.Objects {
			if !validKey(listed.Key, false) || listed.Key <= previous {
				return result, errors.New("replica source listing is invalid or changed during copy")
			}
			previous = listed.Key
			entry, err := replicateObject(ctx, source, destination, listed)
			if err != nil {
				return result, fmt.Errorf("replica object %s: %w", listed.Key, err)
			}
			if err = record(entry); err != nil {
				return result, err
			}
			result.Files++
			result.Bytes += entry.Size
		}
		if page.Next == "" {
			return result, nil
		}
		if page.Next == cursor || len(page.Objects) == 0 {
			return result, errors.New("replica source pagination did not advance")
		}
		cursor = page.Next
	}
}

func replicateObject(ctx context.Context, source, destination Store, listed Info) (ReplicaObject, error) {
	var entry ReplicaObject
	body, info, err := source.Open(ctx, listed.Key)
	if err != nil {
		return entry, err
	}
	defer func() { _ = body.Close() }()
	if info.Size != listed.Size {
		return entry, errors.New("source object changed since listing")
	}
	file, err := DownloadVerified(ctx, body, info.Size, info.Metadata["grasp-sha256"])
	if err != nil {
		return entry, err
	}
	defer func() { _ = file.Close() }()
	if err = ArchiveImmutable(ctx, destination, listed.Key, file, info, file.SHA256); err != nil {
		return entry, err
	}
	entry = ReplicaObject{Key: listed.Key, Size: info.Size, SHA256: file.SHA256, ContentType: info.ContentType, Metadata: maps.Clone(info.Metadata)}
	return entry, nil
}

// VerifyReplicaObject checks both bytes and presentation metadata. A matching
// byte hash cannot excuse a conflicting attachment name/MIME or native state.
func VerifyReplicaObject(ctx context.Context, store Store, entry ReplicaObject) error {
	if !validKey(entry.Key, false) || entry.SHA256 != entry.Metadata["grasp-sha256"] {
		return errors.New("invalid replica inventory entry")
	}
	body, info, err := store.Open(ctx, entry.Key)
	if err != nil {
		return err
	}
	defer func() { _ = body.Close() }()
	if info.Size != entry.Size || info.ContentType != entry.ContentType || !maps.Equal(info.Metadata, entry.Metadata) {
		return errors.New("replica object metadata differs from inventory")
	}
	file, err := DownloadVerified(ctx, body, entry.Size, entry.SHA256)
	if err != nil {
		return err
	}
	return file.Close()
}
