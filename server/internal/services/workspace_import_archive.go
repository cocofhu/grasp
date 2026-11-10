package services

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/cocofhu/grasp/internal/sandbox"
	"github.com/cocofhu/grasp/internal/workspacearchive"
)

var workspaceCommit = regexp.MustCompile(`^[0-9a-f]{40}$`)

func workspaceRepoPath(value string) bool {
	return value == "." || (value != "" && value != ".." && !strings.HasPrefix(value, ".") && !strings.ContainsAny(value, "/\\\x00\r\n") && utf8.ValidString(value) && len(value) <= 255)
}

func workspaceArchiveLimits() workspacearchive.Limits {
	return workspacearchive.Limits{MaxEntries: sandbox.WorkspaceCodeMaxRepositories + 1, MaxBytes: sandbox.WorkspaceCodeMaxTotalBytes + 64<<10, MaxFileBytes: sandbox.WorkspaceCodeMaxBundleBytes}
}

// A generic safe tar is insufficient: code imports accept exactly a small
// manifest and indexed Git bundles, never arbitrary workspace entries.
func inspectCodeArchive(ctx context.Context, file io.ReadSeeker) (sandbox.WorkspaceCodeManifest, string, error) {
	var manifest sandbox.WorkspaceCodeManifest
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return manifest, "", err
	}
	entries, err := workspacearchive.Inspect(ctx, file, workspaceArchiveLimits())
	if err != nil {
		return manifest, "", err
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return manifest, "", err
	}
	z, err := gzip.NewReader(file)
	if err != nil {
		return manifest, "", err
	}
	defer func() { _ = z.Close() }()
	tr := tar.NewReader(z)
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return manifest, "", err
		}
		if h.Name == "manifest.json" {
			if h.Size > 64<<10 {
				return manifest, "", workspacearchive.ErrLimit
			}
			decoder := json.NewDecoder(io.LimitReader(tr, 64<<10))
			decoder.DisallowUnknownFields()
			if err := decoder.Decode(&manifest); err != nil {
				return manifest, "", err
			}
			var extra any
			if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
				return manifest, "", workspacearchive.ErrUnsafe
			}
		}
	}
	if manifest.Version != sandbox.WorkspaceCodeVersion || len(manifest.Repositories) < 1 || len(manifest.Repositories) > sandbox.WorkspaceCodeMaxRepositories || len(entries.Entries) != len(manifest.Repositories)+1 {
		return manifest, "", workspacearchive.ErrUnsafe
	}
	expected := map[string]bool{"manifest.json": true}
	seen := map[string]bool{}
	var total int64
	var files int
	for i, repo := range manifest.Repositories {
		if !workspaceRepoPath(repo.Path) || seen[repo.Path] || !workspaceCommit.MatchString(repo.Commit) || !workspaceCommit.MatchString(repo.Head) || !repo.Supported || repo.Files < 0 || repo.Files > 50000 || repo.Bytes < 0 || repo.Bytes > sandbox.WorkspaceCodeMaxTotalBytes || repo.Reason != "" {
			return manifest, "", workspacearchive.ErrUnsafe
		}
		seen[repo.Path] = true
		total += repo.Bytes
		files += repo.Files
		expected[fmt.Sprintf("repos/%d.bundle", i)] = true
	}
	if total > sandbox.WorkspaceCodeMaxTotalBytes || files > 50000 {
		return manifest, "", workspacearchive.ErrLimit
	}
	for _, entry := range entries.Entries {
		if !expected[entry.Path] || entry.Kind != "file" || entry.Size <= 0 {
			return manifest, "", workspacearchive.ErrUnsafe
		}
		delete(expected, entry.Path)
	}
	if len(expected) != 0 {
		return manifest, "", workspacearchive.ErrUnsafe
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return manifest, "", err
	}
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return manifest, "", err
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return manifest, "", err
	}
	return manifest, hex.EncodeToString(hash.Sum(nil)), nil
}

func canonicalCodeArchive(ctx context.Context, source io.Reader) (*os.File, sandbox.WorkspaceCodeManifest, string, error) {
	var manifest sandbox.WorkspaceCodeManifest
	file, err := os.CreateTemp("", "grasp-code-import-*.tgz")
	if err != nil {
		return nil, manifest, "", err
	}
	cleanup := func() { _ = file.Close(); _ = os.Remove(file.Name()) }
	bounded := &io.LimitedReader{R: source, N: sandbox.WorkspaceCodeMaxTotalBytes + 1<<20}
	if _, err := workspacearchive.Canonicalize(ctx, bounded, file, workspaceArchiveLimits()); err != nil {
		cleanup()
		return nil, manifest, "", err
	}
	if bounded.N <= 0 {
		cleanup()
		return nil, manifest, "", workspacearchive.ErrLimit
	}
	manifest, digest, err := inspectCodeArchive(ctx, file)
	if err != nil {
		cleanup()
		return nil, manifest, "", err
	}
	return file, manifest, digest, nil
}
