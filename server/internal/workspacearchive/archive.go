// Package workspacearchive validates and canonicalizes file snapshots.
// It only processes supplied bytes, without following source links or capturing
// process memory. Callers authorize and confine the selected source workspace.
package workspacearchive

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"path"
	"strings"
	"unicode/utf8"
)

const Version = 1

var (
	ErrUnsafe = errors.New("unsafe workspace archive")
	ErrLimit  = errors.New("workspace snapshot exceeds its configured limit")
)

// Limits apply to decompressed bytes as well as entries, preventing a small
// compressed object from exhausting a restoration disk. Callers set quotas.
type Limits struct {
	MaxEntries   int
	MaxBytes     int64
	MaxFileBytes int64
}

type Entry struct {
	Path   string `json:"path"`
	Kind   string `json:"kind"`
	Mode   int64  `json:"mode"`
	Size   int64  `json:"size,omitempty"`
	SHA256 string `json:"sha256,omitempty"`
	Link   string `json:"link,omitempty"`
}

type Manifest struct {
	Version int     `json:"version"`
	Bytes   int64   `json:"bytes"`
	Entries []Entry `json:"entries"`
}

// Canonicalize emits an ordinary tar.gz containing only validated paths and
// supported file kinds. It strips owners, xattrs, setuid/setgid and timestamps;
// regular permission and executable bits remain. Dirty, ignored and non-Git
// files receive exactly the same treatment as tracked files.
//
// Neither partial output nor its manifest may be published after an error.
func Canonicalize(ctx context.Context, source io.Reader, destination io.Writer, limits Limits) (Manifest, error) {
	z := gzip.NewWriter(destination)
	tw := tar.NewWriter(z)
	manifest, err := scan(ctx, source, limits, false, func(entry Entry, content io.Reader) error {
		h := &tar.Header{Name: entry.Path, Mode: entry.Mode, Format: tar.FormatPAX, Linkname: entry.Link}
		switch entry.Kind {
		case "file":
			h.Typeflag, h.Size = tar.TypeReg, entry.Size
		case "directory":
			h.Typeflag = tar.TypeDir
		case "symlink":
			h.Typeflag = tar.TypeSymlink
		case "hardlink":
			h.Typeflag = tar.TypeLink
		}
		if err := tw.WriteHeader(h); err != nil {
			return err
		}
		if entry.Kind == "file" {
			_, err := io.Copy(tw, content)
			return err
		}
		return nil
	})
	if err != nil {
		return Manifest{}, err
	}
	if err := tw.Close(); err != nil {
		return Manifest{}, err
	}
	if err := z.Close(); err != nil {
		return Manifest{}, err
	}
	return manifest, nil
}

// Inspect verifies a canonical archive's complete stream, safe metadata and
// file hashes before it can become a verified, restorable SQL reference.
func Inspect(ctx context.Context, source io.Reader, limits Limits) (Manifest, error) {
	return scan(ctx, source, limits, true, nil)
}

type contextReader struct {
	ctx context.Context
	r   io.Reader
}

func (r contextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.r.Read(p)
}

func entryPath(name string) (string, error) {
	if name == "" || len(name) > 4096 || !utf8.ValidString(name) || strings.ContainsAny(name, "\x00\\") || path.IsAbs(name) {
		return "", ErrUnsafe
	}
	for _, part := range strings.Split(name, "/") {
		if part == ".." {
			return "", ErrUnsafe
		}
	}
	return path.Clean(name), nil
}

func safeSymlink(name, target string) bool {
	if target == "" || len(target) > 4096 || !utf8.ValidString(target) || strings.ContainsAny(target, "\x00\\") || path.IsAbs(target) {
		return false
	}
	resolved := path.Clean(path.Join(path.Dir(name), target))
	return resolved != ".." && !strings.HasPrefix(resolved, "../")
}

func scan(ctx context.Context, source io.Reader, limits Limits, canonical bool, visit func(Entry, io.Reader) error) (Manifest, error) {
	if limits.MaxEntries < 1 || limits.MaxBytes < 1 || limits.MaxFileBytes < 1 {
		return Manifest{}, errors.New("positive workspace archive limits are required")
	}
	if err := ctx.Err(); err != nil {
		return Manifest{}, err
	}
	z, err := gzip.NewReader(contextReader{ctx, source})
	if err != nil {
		return Manifest{}, err
	}
	defer func() { _ = z.Close() }()
	tr := tar.NewReader(z)
	manifest := Manifest{Version: Version, Entries: []Entry{}}
	seen := map[string]Entry{}
	parents := map[string]bool{}
	for count := 0; ; count++ {
		if err := ctx.Err(); err != nil {
			return Manifest{}, err
		}
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return Manifest{}, err
		}
		if count >= limits.MaxEntries {
			return Manifest{}, ErrLimit
		}
		if canonical {
			if h.Uid != 0 || h.Gid != 0 || h.Uname != "" || h.Gname != "" || h.Mode&^0o777 != 0 {
				return Manifest{}, fmt.Errorf("%w: non-canonical ownership or permissions", ErrUnsafe)
			}
			for key := range h.PAXRecords {
				if key != "path" && key != "linkpath" {
					return Manifest{}, fmt.Errorf("%w: unexpected extended metadata", ErrUnsafe)
				}
			}
		}
		name, err := entryPath(h.Name)
		if err != nil {
			return Manifest{}, err
		}
		if name == "." {
			if h.Typeflag != tar.TypeDir || h.Size != 0 {
				return Manifest{}, ErrUnsafe
			}
			continue
		}
		if _, exists := seen[name]; exists {
			return Manifest{}, fmt.Errorf("%w: duplicate path", ErrUnsafe)
		}
		for parent := path.Dir(name); parent != "."; parent = path.Dir(parent) {
			if existing, ok := seen[parent]; ok && existing.Kind != "directory" {
				return Manifest{}, fmt.Errorf("%w: non-directory ancestor", ErrUnsafe)
			}
			parents[parent] = true
		}
		entry := Entry{Path: name, Mode: h.Mode & 0o777}
		switch h.Typeflag {
		case tar.TypeReg:
			if h.Size < 0 || h.Size > limits.MaxFileBytes || h.Size > limits.MaxBytes-manifest.Bytes {
				return Manifest{}, ErrLimit
			}
			entry.Kind, entry.Size = "file", h.Size
		case tar.TypeDir:
			entry.Kind = "directory"
		case tar.TypeSymlink:
			if !safeSymlink(name, h.Linkname) {
				return Manifest{}, fmt.Errorf("%w: symlink leaves workspace", ErrUnsafe)
			}
			entry.Kind, entry.Link = "symlink", h.Linkname
			if canonical && h.Mode != 0o777 {
				return Manifest{}, fmt.Errorf("%w: non-canonical symlink permissions", ErrUnsafe)
			}
			entry.Mode = 0o777
		case tar.TypeLink:
			target, err := entryPath(h.Linkname)
			if err != nil {
				return Manifest{}, err
			}
			file, exists := seen[target]
			if !exists || file.Kind != "file" {
				return Manifest{}, fmt.Errorf("%w: hardlink target is not an earlier regular file", ErrUnsafe)
			}
			entry.Kind, entry.Link, entry.Size, entry.SHA256, entry.Mode = "hardlink", target, file.Size, file.SHA256, file.Mode
		default:
			return Manifest{}, fmt.Errorf("%w: unsupported file kind", ErrUnsafe)
		}
		if entry.Kind != "directory" && parents[name] {
			return Manifest{}, fmt.Errorf("%w: path shadows existing descendants", ErrUnsafe)
		}
		if entry.Kind != "file" && h.Size != 0 {
			return Manifest{}, ErrUnsafe
		}
		hash := sha256.New()
		content := io.TeeReader(contextReader{ctx, tr}, hash)
		if visit != nil {
			if err := visit(entry, content); err != nil {
				return Manifest{}, err
			}
		}
		if entry.Kind == "file" {
			// Consume even without a visitor, including zero-length files.
			if _, err := io.Copy(io.Discard, content); err != nil {
				return Manifest{}, err
			}
			entry.SHA256 = hex.EncodeToString(hash.Sum(nil))
			manifest.Bytes += entry.Size
		}
		seen[name] = entry
		manifest.Entries = append(manifest.Entries, entry)
	}
	// tar EOF can precede gzip's checksum. Read the footer, permitting only
	// bounded zero padding, so truncated streams never become verified copies.
	padding := io.LimitReader(contextReader{ctx, z}, (1<<20)+1)
	buffer := make([]byte, 8192)
	var bytes int64
	for {
		n, err := padding.Read(buffer)
		bytes += int64(n)
		if bytes > 1<<20 {
			return Manifest{}, ErrLimit
		}
		for _, b := range buffer[:n] {
			if b != 0 {
				return Manifest{}, fmt.Errorf("%w: trailing archive data", ErrUnsafe)
			}
		}
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return Manifest{}, err
		}
	}
	return manifest, nil
}
