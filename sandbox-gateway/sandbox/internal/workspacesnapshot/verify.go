package workspacesnapshot

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"path"
	"slices"
	"strconv"
	"strings"

	"backend/internal/workspacejob"
)

func (e *Engine) validateManifest(data []byte, receipt Receipt) (Manifest, error) {
	var manifest Manifest
	if int64(len(data)) != receipt.Manifest.Bytes || int64(len(data)) > e.config.Limits.MaxManifestBytes || receipt.Version != Version || receipt.Manifest.Name != ManifestName || receipt.Archive.Name != ArchiveName || !hashPattern.MatchString(receipt.Manifest.SHA256) || !hashPattern.MatchString(receipt.Archive.SHA256) || receipt.Archive.Bytes <= 0 || receipt.Archive.Bytes > e.config.Limits.MaxArchiveBytes {
		return manifest, ErrIntegrity
	}
	hash := sha256.Sum256(data)
	if hex.EncodeToString(hash[:]) != receipt.Manifest.SHA256 || strictDecode(data, &manifest) != nil || manifest.Version != Version || manifest.PolicyVersion != PolicyVersion || manifest.Consistency != "verified_copy" || !validLimits(manifest.Limits) || !hashPattern.MatchString(manifest.PolicySHA256) {
		return manifest, ErrIntegrity
	}
	if _, err := normalizeSpec(Spec{Binding: manifest.Binding, PolicyVersion: manifest.PolicyVersion, Paths: manifest.Paths, IncludeIgnored: manifest.IncludeIgnored}, e.config.PrivatePaths); err != nil {
		return manifest, err
	}
	if len(manifest.Files) > e.config.Limits.MaxEntries || len(manifest.Repositories) > e.config.Limits.MaxRepositories || len(manifest.Excluded) > e.config.Limits.MaxEntries {
		return manifest, ErrLimit
	}
	expected := []Entry{{Path: "workspace", Kind: "directory", Mode: 0700}}
	seen := map[string]string{}
	parents := map[string]bool{}
	var bytes int64
	for _, entry := range manifest.Files {
		if !safePath(entry.Path) || entry.Path == "." || mandatory(entry.Path, e.config.PrivatePaths) != "" || entry.Mode < 0 || entry.Mode&^0777 != 0 || seen[entry.Path] != "" || !slices.ContainsFunc(manifest.Paths, func(parent string) bool { return beneath(entry.Path, parent) }) {
			return manifest, ErrUnsafe
		}
		for parent := path.Dir(entry.Path); parent != "."; parent = path.Dir(parent) {
			if kind := seen[parent]; kind != "" && kind != "directory" {
				return manifest, ErrUnsafe
			}
			parents[parent] = true
		}
		if parents[entry.Path] && entry.Kind != "directory" {
			return manifest, ErrUnsafe
		}
		switch entry.Kind {
		case "file":
			if entry.Size < 0 || entry.Size > e.config.Limits.MaxFileBytes || entry.Size > e.config.Limits.MaxBytes-bytes || !hashPattern.MatchString(entry.SHA256) || entry.Link != "" {
				return manifest, ErrLimit
			}
			bytes += entry.Size
		case "symlink":
			if !safeLink(entry.Path, entry.Link) || entry.Mode != 0777 || entry.Size != 0 || entry.SHA256 != "" {
				return manifest, ErrUnsafe
			}
		case "directory":
			if entry.Size != 0 || entry.Link != "" || entry.SHA256 != "" {
				return manifest, ErrUnsafe
			}
		default:
			return manifest, ErrUnsafe
		}
		seen[entry.Path] = entry.Kind
		value := entry
		value.Path = path.Join("workspace", value.Path)
		expected = append(expected, value)
	}
	if receipt.Files != len(manifest.Files) || receipt.Bytes != bytes || receipt.Consistency != manifest.Consistency {
		return manifest, ErrIntegrity
	}
	if len(manifest.Repositories) > 0 {
		expected = append(expected, Entry{Path: "git", Kind: "directory", Mode: 0700})
	}
	paths := map[string]bool{}
	var gitBytes int64
	indexes := 0
	for i, repo := range manifest.Repositories {
		if !safePath(repo.Path) || !slices.ContainsFunc(manifest.Paths, func(parent string) bool { return beneath(repo.Path, parent) }) || (repo.Path != "." && seen[repo.Path] != "directory") || paths[repo.Path] || (repo.ObjectFormat != "sha1" && repo.ObjectFormat != "sha256") || (repo.HEAD != "" && !validObjectID(repo.HEAD, repo.ObjectFormat)) || (repo.SymbolicHEAD != "" && (!validRef(repo.SymbolicHEAD) || !strings.HasPrefix(repo.SymbolicHEAD, "refs/heads/"))) {
			return manifest, ErrUnsafe
		}
		paths[repo.Path] = true
		indexes += len(repo.Index) + len(repo.Refs)
		if indexes > e.config.Limits.MaxEntries {
			return manifest, ErrLimit
		}
		refs := map[string]string{}
		for _, ref := range repo.Refs {
			if !validRef(ref.Name) || !validObjectID(ref.OID, repo.ObjectFormat) || refs[ref.Name] != "" {
				return manifest, ErrUnsafe
			}
			refs[ref.Name] = ref.OID
		}
		if repo.SymbolicHEAD != "" && refs[repo.SymbolicHEAD] != repo.HEAD {
			return manifest, ErrIntegrity
		}
		operations := map[string]bool{}
		for _, value := range repo.Operation {
			if !validGitOperation(value) || operations[value.Name] {
				return manifest, ErrUnsafe
			}
			operations[value.Name] = true
		}
		stages := map[string]int{}
		for _, entry := range repo.Index {
			if !safePath(entry.Path) || entry.Path == "." || !validObjectID(entry.OID, repo.ObjectFormat) || entry.Stage < 0 || entry.Stage > 3 || (entry.Mode != "100644" && entry.Mode != "100755" && entry.Mode != "120000" && entry.Mode != "160000") || slices.Contains(strings.Split(entry.Path, "/"), ".git") {
				return manifest, ErrUnsafe
			}
			mask := 1 << entry.Stage
			if stages[entry.Path]&mask != 0 || (entry.Stage == 0 && stages[entry.Path] != 0) || (entry.Stage != 0 && stages[entry.Path]&1 != 0) {
				return manifest, ErrUnsafe
			}
			stages[entry.Path] |= mask
		}
		if repo.Pack != "" {
			if repo.Pack != "git/"+strconv.Itoa(i)+".pack" || repo.PackBytes <= 0 || repo.PackBytes > e.config.Limits.MaxGitBytes-gitBytes || !hashPattern.MatchString(repo.PackSHA256) {
				return manifest, ErrLimit
			}
			gitBytes += repo.PackBytes
			expected = append(expected, Entry{Path: repo.Pack, Kind: "file", Mode: 0600, Size: repo.PackBytes, SHA256: repo.PackSHA256})
		} else if repo.PackBytes != 0 || repo.PackSHA256 != "" || repo.HEAD != "" || len(repo.Refs) != 0 || len(repo.Index) != 0 {
			return manifest, ErrIntegrity
		}
	}
	if manifest.Archive.Version != Version || manifest.Archive.Bytes != bytes+gitBytes || !slices.Equal(manifest.Archive.Entries, expected) {
		return manifest, ErrIntegrity
	}
	return manifest, nil
}

func validObjectID(value, format string) bool {
	length := 40
	if format == "sha256" {
		length = 64
	}
	return len(value) == length && oidPattern.MatchString(value)
}

func inspectArchive(h *workspacejob.Handle, source io.Reader, receipt Receipt, expected ArchiveManifest, visit func(Entry, io.Reader) error) error {
	hash := sha256.New()
	compressed := &io.LimitedReader{R: io.TeeReader(&fencedReader{handle: h, reader: source}, hash), N: receipt.Archive.Bytes + 1}
	z, err := gzip.NewReader(compressed)
	if err != nil {
		return err
	}
	defer func() { _ = z.Close() }()
	tr := tar.NewReader(z)
	for _, entry := range expected.Entries {
		if err := h.Check(); err != nil {
			return err
		}
		header, err := tr.Next()
		if err != nil || header.Name != entry.Path || header.Mode != entry.Mode || header.Uid != 0 || header.Gid != 0 || header.Uname != "" || header.Gname != "" || header.Linkname != entry.Link || header.Mode&^0777 != 0 {
			return ErrIntegrity
		}
		for key := range header.PAXRecords {
			if key != "path" && key != "linkpath" {
				return ErrUnsafe
			}
		}
		kind := byte(tar.TypeReg)
		size := entry.Size
		if entry.Kind == "directory" {
			kind, size = tar.TypeDir, 0
		} else if entry.Kind == "symlink" {
			kind, size = tar.TypeSymlink, 0
		}
		if header.Typeflag != kind || header.Size != size {
			return ErrIntegrity
		}
		hash := sha256.New()
		content := io.TeeReader(&fencedReader{handle: h, reader: tr}, hash)
		if visit != nil {
			if err := visit(entry, content); err != nil {
				return err
			}
		}
		if _, err := io.Copy(io.Discard, content); err != nil {
			return err
		}
		if entry.Kind == "file" && hex.EncodeToString(hash.Sum(nil)) != entry.SHA256 {
			return ErrIntegrity
		}
	}
	if _, err := tr.Next(); !errors.Is(err, io.EOF) {
		return ErrIntegrity
	}
	padding := &io.LimitedReader{R: z, N: (1 << 20) + 1}
	buffer := make([]byte, 32<<10)
	for {
		n, err := padding.Read(buffer)
		for _, b := range buffer[:n] {
			if b != 0 {
				return ErrIntegrity
			}
		}
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return err
		}
		if padding.N <= 0 {
			return ErrLimit
		}
	}
	if compressed.N != 1 || hex.EncodeToString(hash.Sum(nil)) != receipt.Archive.SHA256 {
		return ErrIntegrity
	}
	return h.Check()
}
