package workspacesnapshot

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path"
	"slices"
	"time"

	"backend/internal/workspacejob"
)

func (e *Engine) capture(ctx context.Context, h *workspacejob.Handle, body json.RawMessage) (json.RawMessage, error) {
	spec, err := e.decode(body)
	if err != nil {
		return nil, err
	}
	if err := h.SeparateSource(e.config.Root); err != nil {
		return nil, err
	}
	stage, err := h.OpenStage()
	if err != nil {
		return nil, err
	}
	defer func() { _ = stage.Close() }()
	walk := &captureWalk{engine: e, handle: h, ctx: ctx, stage: stage, spec: spec, files: []Entry{}, excluded: []Exclusion{}, repoPaths: map[string]*liveRepository{}, ignores: map[string]*liveRepository{}}
	defer walk.close()
	if err := walk.scan(); err != nil {
		return nil, err
	}
	var packs int64
	for index, repo := range walk.repos {
		if err := e.pack(ctx, h, stage, repo, index, e.config.Limits.MaxGitBytes-packs); err != nil {
			return nil, err
		}
		packs += repo.value.PackBytes
	}
	budget := min(e.config.Limits.MaxArchiveBytes, e.config.Limits.MaxSpoolBytes-walk.objects-packs-walk.metadata-e.config.Limits.MaxManifestBytes)
	if budget <= 0 {
		return nil, ErrLimit
	}
	archive, archiveManifest, err := e.archive(ctx, h, stage, walk.files, walk.repos, budget)
	if err != nil {
		return nil, err
	}
	verified := *walk
	verified.files, verified.excluded = []Entry{}, []Exclusion{}
	verified.bytes, verified.scanned, verified.verify, verified.manifestBytes = 0, 0, true, 0
	if err := verified.scan(); err != nil {
		return nil, err
	}
	if !slices.Equal(walk.files, verified.files) || !slices.Equal(walk.excluded, verified.excluded) {
		return nil, ErrChanged
	}
	repositories := make([]Repository, 0, len(walk.repos))
	for _, repo := range walk.repos {
		if err := e.recheckRepository(repo); err != nil {
			return nil, err
		}
		repositories = append(repositories, repo.value)
	}
	manifest := Manifest{Binding: spec.Binding, Version: Version, PolicyVersion: PolicyVersion, PolicySHA256: e.policy, Consistency: "verified_copy", Paths: spec.Paths, IncludeIgnored: spec.IncludeIgnored, Limits: e.config.Limits, Files: walk.files, Repositories: repositories, Excluded: walk.excluded, Archive: archiveManifest, CreatedAt: time.Now().UTC()}
	data, err := json.Marshal(manifest)
	if err != nil || int64(len(data)) > e.config.Limits.MaxManifestBytes {
		return nil, ErrLimit
	}
	if err := h.WriteFile(ManifestName, &byteReader{data: data}, max(int64(len(data)), 1)); err != nil {
		return nil, err
	}
	if err := syncDirectory(h, stage, ".", 0700); err != nil {
		return nil, err
	}
	manifestHash := sha256.Sum256(data)
	receipt := Receipt{Version: Version, Archive: archive, Manifest: Artifact{Name: ManifestName, SHA256: hex.EncodeToString(manifestHash[:]), Bytes: int64(len(data))}, Files: len(walk.files), Bytes: walk.bytes, Consistency: manifest.Consistency}
	return json.Marshal(receipt)
}

type byteReader struct{ data []byte }

func (r *byteReader) Read(p []byte) (int, error) {
	if len(r.data) == 0 {
		return 0, io.EOF
	}
	n := copy(p, r.data)
	r.data = r.data[n:]
	return n, nil
}

func (e *Engine) archive(ctx context.Context, h *workspacejob.Handle, stage *os.Root, files []Entry, repositories []*liveRepository, limit int64) (Artifact, ArchiveManifest, error) {
	read, write := io.Pipe()
	hash := sha256.New()
	manifest := ArchiveManifest{Version: Version, Entries: []Entry{}}
	done := make(chan error, 1)
	go func() {
		err := e.writeArchive(ctx, h, stage, io.MultiWriter(write, hash), files, repositories, &manifest)
		_ = write.CloseWithError(err)
		done <- err
	}()
	err := h.WriteFile(ArchiveName, read, limit)
	_ = read.CloseWithError(err)
	err = errors.Join(err, <-done)
	if err != nil {
		return Artifact{}, ArchiveManifest{}, err
	}
	info, err := stage.Lstat(ArchiveName)
	if err != nil {
		return Artifact{}, ArchiveManifest{}, err
	}
	return Artifact{Name: ArchiveName, SHA256: hex.EncodeToString(hash.Sum(nil)), Bytes: info.Size()}, manifest, nil
}

func (e *Engine) writeArchive(ctx context.Context, h *workspacejob.Handle, stage *os.Root, destination io.Writer, files []Entry, repositories []*liveRepository, manifest *ArchiveManifest) error {
	z, err := gzip.NewWriterLevel(destination, gzip.BestSpeed)
	if err != nil {
		return err
	}
	tw := tar.NewWriter(z)
	write := func(entry Entry, root *os.Root, name string) error {
		if err := h.Check(); err != nil {
			return err
		}
		header := &tar.Header{Name: entry.Path, Mode: entry.Mode, Format: tar.FormatPAX, Linkname: entry.Link}
		switch entry.Kind {
		case "directory":
			header.Typeflag = tar.TypeDir
		case "symlink":
			header.Typeflag = tar.TypeSymlink
		case "file":
			header.Typeflag, header.Size = tar.TypeReg, entry.Size
		default:
			return ErrUnsafe
		}
		if err := tw.WriteHeader(header); err != nil {
			return err
		}
		if entry.Kind == "file" {
			file, err := root.OpenFile(name, os.O_RDONLY|nofollow, 0)
			if err != nil {
				return err
			}
			before, err := file.Stat()
			if err == nil && (!before.Mode().IsRegular() || before.Size() != entry.Size) {
				err = ErrChanged
			}
			hash := sha256.New()
			if err == nil {
				_, err = io.CopyN(io.MultiWriter(tw, hash), &fencedReader{handle: h, reader: file}, entry.Size)
			}
			after, statErr := file.Stat()
			err = errors.Join(err, statErr, file.Close())
			if err != nil {
				return err
			}
			if !sameFileState(before, after) || hex.EncodeToString(hash.Sum(nil)) != entry.SHA256 {
				return ErrChanged
			}
			manifest.Bytes += entry.Size
		}
		manifest.Entries = append(manifest.Entries, entry)
		return nil
	}
	if err := write(Entry{Path: "workspace", Kind: "directory", Mode: 0700}, nil, ""); err != nil {
		return err
	}
	for _, entry := range files {
		name := entry.Path
		entry.Path = path.Join("workspace", name)
		if err := write(entry, e.root, name); err != nil {
			return err
		}
	}
	if len(repositories) > 0 {
		if err := write(Entry{Path: "git", Kind: "directory", Mode: 0700}, nil, ""); err != nil {
			return err
		}
	}
	for _, repo := range repositories {
		if repo.value.Pack == "" {
			continue
		}
		entry := Entry{Path: repo.value.Pack, Kind: "file", Mode: 0600, SHA256: repo.value.PackSHA256, Size: repo.value.PackBytes}
		if err := write(entry, stage, entry.Path); err != nil {
			return err
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := tw.Close(); err != nil {
		return err
	}
	return z.Close()
}
