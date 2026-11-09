package workspacesnapshot

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"

	"backend/internal/workspacejob"
)

func part(value Receipt, name string) (Artifact, error) {
	switch name {
	case "archive":
		return value.Archive, nil
	case "manifest":
		return value.Manifest, nil
	default:
		return Artifact{}, ErrUnsafe
	}
}

type Chunk struct {
	Offset int64
	Data   []byte
	SHA256 string
	EOF    bool
}

// ReadChunk is read-only and retryable. The source is a completed immutable
// capture; the separate read job must still own its acknowledged live lease.
func (c *Controller) ReadChunk(id string, fence workspacejob.RenewRequest, name string, offset int64, length int) (Chunk, error) {
	var result Chunk
	if offset < 0 || length < 1 || length > MaxChunkBytes {
		return result, ErrLimit
	}
	item, err := c.transfer(id, fence)
	if err != nil {
		return result, err
	}
	item.mu.Lock()
	defer item.mu.Unlock()
	if item.kind != "read_snapshot" || item.finished {
		return result, workspacejob.ErrFence
	}
	artifact, err := part(item.read.Receipt, name)
	if err != nil || offset > artifact.Bytes {
		return result, ErrUnsafe
	}
	file, err := c.Jobs.OpenCompletedArtifact(item.read.SourceJobID, item.read.SourceSHA256, artifact.Name)
	if err != nil {
		return result, err
	}
	defer func() { _ = file.Close() }()
	result.Offset = offset
	result.Data = make([]byte, min(int64(length), artifact.Bytes-offset))
	err = item.handle.Guard(func() error {
		info, err := file.Stat()
		if err != nil || info.Size() != artifact.Bytes {
			return ErrIntegrity
		}
		n, err := file.ReadAt(result.Data, offset)
		if n != len(result.Data) || (err != nil && !errors.Is(err, io.EOF)) {
			return errors.Join(ErrIntegrity, err)
		}
		return nil
	})
	if err != nil {
		return Chunk{}, err
	}
	hash := sha256.Sum256(result.Data)
	result.SHA256, result.EOF = hex.EncodeToString(hash[:]), offset+int64(len(result.Data)) == artifact.Bytes
	return result, nil
}

// WriteChunk only appends to private receiving files. A repeated acknowledged
// range is accepted iff its bytes are identical; gaps/overlap/overwrite fail.
// Every chunk is fsynced before acknowledgement and no target directory exists
// until Finish has validated complete immutable archive+manifest descriptors.
func (c *Controller) WriteChunk(id string, fence workspacejob.RenewRequest, name string, offset int64, data []byte, digest string) (int64, error) {
	if offset < 0 || len(data) < 1 || len(data) > MaxChunkBytes || !hashPattern.MatchString(digest) {
		return 0, ErrLimit
	}
	hash := sha256.Sum256(data)
	if hex.EncodeToString(hash[:]) != digest {
		return 0, ErrIntegrity
	}
	item, err := c.transfer(id, fence)
	if err != nil {
		return 0, err
	}
	item.mu.Lock()
	defer item.mu.Unlock()
	if item.kind != "restore_files" || item.finished {
		return 0, workspacejob.ErrFence
	}
	artifact, err := part(item.restore.Receipt, name)
	if err != nil || offset > artifact.Bytes || int64(len(data)) > artifact.Bytes-offset {
		return 0, ErrLimit
	}
	var acknowledged int64
	err = item.handle.Guard(func() error {
		file, err := item.stage.OpenFile(artifact.Name, os.O_CREATE|os.O_RDWR|nofollow, 0600)
		if err != nil {
			return err
		}
		defer func() { _ = file.Close() }()
		info, err := file.Stat()
		if err != nil || !info.Mode().IsRegular() || info.Size() > artifact.Bytes || offset > info.Size() {
			return ErrIntegrity
		}
		end := offset + int64(len(data))
		if offset < info.Size() {
			if end > info.Size() {
				return workspacejob.ErrConflict
			}
			existing := make([]byte, len(data))
			if _, err := file.ReadAt(existing, offset); err != nil || !bytes.Equal(existing, data) {
				return workspacejob.ErrConflict
			}
		} else {
			if n, err := file.WriteAt(data, offset); err != nil || n != len(data) {
				return errors.Join(io.ErrShortWrite, err)
			}
		}
		if err := file.Sync(); err != nil {
			return err
		}
		acknowledged = end
		return nil
	})
	return acknowledged, err
}

// Finish never renews a capability. Repeating it for the same live receiver is
// harmless; after a lost terminal response callers must GET the durable receipt.
func (c *Controller) Finish(id string, fence workspacejob.RenewRequest) error {
	item, err := c.transfer(id, fence)
	if err != nil {
		return err
	}
	item.mu.Lock()
	defer item.mu.Unlock()
	if item.finished {
		return item.handle.Check()
	}
	if item.kind == "restore_files" {
		for _, artifact := range []Artifact{item.restore.Receipt.Archive, item.restore.Receipt.Manifest} {
			if err := item.handle.Guard(func() error {
				info, err := item.stage.Lstat(artifact.Name)
				if err != nil || !info.Mode().IsRegular() || info.Size() != artifact.Bytes {
					return ErrIntegrity
				}
				return nil
			}); err != nil {
				return err
			}
		}
	}
	// Full hashes/tar/manifest/Git validation run in the owned executor after
	// this bounded control acknowledgement, before creating the new target.
	if err := item.handle.Check(); err != nil {
		return err
	}
	item.finished = true
	close(item.done)
	return nil
}
