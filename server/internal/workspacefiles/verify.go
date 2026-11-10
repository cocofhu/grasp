package workspacefiles

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"path"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/cocofhu/grasp/internal/payloadhold"
	"github.com/cocofhu/grasp/internal/sandbox"
	"github.com/cocofhu/grasp/internal/workspacearchive"
)

var (
	ErrIntegrity  = errors.New("workspace capture manifest or archive does not match frozen intent")
	digestPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

func sameBinding(a, b *sandbox.WorkspaceBindingReference) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

func Decode(data []byte, value any) error {
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if err := d.Decode(value); err != nil {
		return ErrIntegrity
	}
	var extra any
	if err := d.Decode(&extra); !errors.Is(err, io.EOF) {
		return ErrIntegrity
	}
	return nil
}
func safePath(name string) bool {
	if name == "." {
		return true
	}
	if name == "" || len(name) > 4096 || !utf8.ValidString(name) || strings.ContainsAny(name, "\x00\\") || path.IsAbs(name) {
		return false
	}
	for _, part := range strings.Split(name, "/") {
		if part == "" || part == "." || part == ".." {
			return false
		}
	}
	return true
}
func beneath(name, parent string) bool {
	return parent == "." || name == parent || strings.HasPrefix(name, parent+"/")
}
func hasGit(name string) bool { return slices.Contains(strings.Split(name, "/"), ".git") }
func privatePath(name string) bool {
	for _, component := range strings.Split(name, "/") {
		switch strings.ToLower(component) {
		case ".git", ".ssh", ".gnupg", ".codex", ".claude", ".cursor", ".opencode", ".trae", ".qwen", ".gemini", ".codeium", ".grasp", ".grasp-workspace-control", ".grasp-runtime", ".grasp-credentials", ".cache":
			return true
		}
	}
	switch strings.ToLower(path.Base(name)) {
	case ".git-credentials", ".gitconfig", ".netrc", ".npmrc", ".pypirc":
		return true
	}
	return false
}
func validRef(name string) bool {
	if !strings.HasPrefix(name, "refs/") || !safePath(name) || strings.ContainsAny(name, " ~^:?*[\x00\t\n\r") || strings.Contains(name, "..") || strings.Contains(name, "@{") || strings.HasSuffix(name, ".") {
		return false
	}
	for _, part := range strings.Split(name, "/") {
		if strings.HasPrefix(part, ".") || strings.HasSuffix(part, ".lock") {
			return false
		}
		for _, character := range part {
			if character < 0x20 || character == 0x7f {
				return false
			}
		}
	}
	return true
}

func validRefs(values []GitRef, format string) (map[string]GitRef, bool) {
	refs := map[string]GitRef{}
	for _, ref := range values {
		direct := ref.Target == "" && validOID(ref.OID, format)
		symbolic := ref.OID == "" && validRef(ref.Target) && ref.Target != ref.Name
		if _, dup := refs[ref.Name]; !validRef(ref.Name) || !(direct || symbolic) || dup {
			return nil, false
		}
		refs[ref.Name] = ref
	}
	return refs, true
}

// resolveRef follows symbolic refs the way Git does (bounded depth) and
// returns "" for a dangling or cyclic chain.
func resolveRef(refs map[string]GitRef, name string) string {
	for range 5 {
		ref, ok := refs[name]
		if !ok {
			return ""
		}
		if ref.Target == "" {
			return ref.OID
		}
		name = ref.Target
	}
	return ""
}
func validOID(value, format string) bool {
	length := 40
	if format == "sha256" {
		length = 64
	}
	if len(value) != length {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil && strings.ToLower(value) == value
}

func ValidateSpec(data json.RawMessage) error {
	var spec Spec
	if Decode(data, &spec) != nil || spec.PolicyVersion != PolicyVersion || !digestPattern.MatchString(spec.PolicySHA256) || len(spec.Paths) < 1 || len(spec.Paths) > 64 || len(spec.IncludeIgnored) > 1024 {
		return ErrIntegrity
	}
	if spec.Binding != nil && !sandbox.ValidWorkspaceBindingReference(*spec.Binding) {
		return ErrIntegrity
	}
	l := spec.Limits
	for _, value := range []int64{l.MaxFileBytes, l.MaxBytes, l.MaxGitBytes, l.MaxArchiveBytes, l.MaxManifestBytes, l.MaxSpoolBytes} {
		if value < 1 || value > 1<<50 {
			return ErrIntegrity
		}
	}
	if l.MaxEntries < 1 || l.MaxEntries > 1000000 || l.MaxRepositories < 1 || l.MaxRepositories > 256 || l.MaxScanEntries < 1 || l.MaxScanEntries > 10000000 {
		return ErrIntegrity
	}
	if l.MaxFileBytes > l.MaxBytes || l.MaxArchiveBytes > l.MaxSpoolBytes || l.MaxEntries > l.MaxScanEntries || !slices.IsSorted(spec.Paths) || !slices.IsSorted(spec.IncludeIgnored) {
		return ErrIntegrity
	}
	for i, name := range spec.Paths {
		if !safePath(name) || privatePath(name) {
			return ErrIntegrity
		}
		for _, other := range spec.Paths[:i] {
			if beneath(name, other) || beneath(other, name) {
				return ErrIntegrity
			}
		}
	}
	for _, name := range spec.IncludeIgnored {
		if !safePath(name) || privatePath(name) || !slices.ContainsFunc(spec.Paths, func(p string) bool { return beneath(name, p) }) {
			return ErrIntegrity
		}
	}
	return nil
}

// Proof contains only privately verified immutable digests. It can be compared
// with a SQL pin without opening objects or doing filesystem I/O in the TX.
type Proof struct {
	receipt Receipt
	files   int
	bytes   int64
}

func (p Proof) Matches(pin payloadhold.Pinned) bool {
	return p.receipt.Version == 1 && p.receipt.Archive.SHA256 == pin.ArchiveSHA256 && p.receipt.Manifest.SHA256 == pin.ManifestSHA256 && p.receipt.Archive.Bytes == pin.ArchiveBytes && p.receipt.Manifest.Bytes == pin.ManifestBytes
}
func (p Proof) Counts() (int, int64) { return p.files, p.bytes }

// Verify reads only the existing immutable P6 stage. It does not create a third
// archive copy, upload objects, touch SQL, or trust Stage's JSON validity alone.
func Verify(ctx context.Context, plan payloadhold.Plan, expected Spec, receipt Receipt) (Proof, error) {
	summary, err := plan.Summary()
	if err != nil {
		return Proof{}, err
	}
	if receipt.Version != 1 || receipt.Consistency != "verified_copy" || receipt.Archive.Name != "workspace.tar.gz" || receipt.Manifest.Name != "manifest.json" || receipt.Archive.SHA256 != summary.ArchiveSHA256 || receipt.Manifest.SHA256 != summary.ManifestSHA256 || receipt.Archive.Bytes != summary.ArchiveBytes || receipt.Manifest.Bytes != summary.ManifestBytes {
		return Proof{}, ErrIntegrity
	}
	reader, err := plan.OpenManifest(ctx)
	if err != nil {
		return Proof{}, err
	}
	data, err := io.ReadAll(io.LimitReader(reader, summary.ManifestBytes+1))
	err = errors.Join(err, reader.Close())
	if err != nil {
		return Proof{}, err
	}
	var manifest Manifest
	if Decode(data, &manifest) != nil || manifest.Version != 1 || manifest.PolicyVersion != PolicyVersion || manifest.PolicyVersion != expected.PolicyVersion || manifest.PolicySHA256 != expected.PolicySHA256 || manifest.Limits != expected.Limits || !sameBinding(manifest.Binding, expected.Binding) || !slices.Equal(manifest.Paths, expected.Paths) || !slices.Equal(manifest.IncludeIgnored, expected.IncludeIgnored) || manifest.Consistency != receipt.Consistency {
		return Proof{}, ErrIntegrity
	}
	if err := validateEntries(manifest, receipt, summary.Limits); err != nil {
		return Proof{}, err
	}
	archive, err := plan.OpenArchive(ctx)
	if err != nil {
		return Proof{}, err
	}
	defer func() { _ = archive.Close() }()
	hash := sha256.New()
	actual, err := workspacearchive.Inspect(ctx, io.TeeReader(archive, hash), workspacearchive.Limits{MaxEntries: int(summary.Limits.MaxEntries), MaxFileBytes: summary.Limits.MaxFileBytes, MaxBytes: summary.Limits.MaxUncompressedBytes})
	if err != nil {
		return Proof{}, err
	}
	if actual.Version != manifest.Archive.Version || actual.Bytes != manifest.Archive.Bytes || !slices.Equal(actual.Entries, manifest.Archive.Entries) || hex.EncodeToString(hash.Sum(nil)) != receipt.Archive.SHA256 {
		return Proof{}, ErrIntegrity
	}
	return Proof{receipt: receipt, files: len(manifest.Files), bytes: receipt.Bytes}, nil
}

func validateEntries(m Manifest, r Receipt, limits payloadhold.Limits) error {
	if len(m.Files) > m.Limits.MaxEntries || len(m.Repositories) > m.Limits.MaxRepositories || len(m.Excluded) > m.Limits.MaxEntries || len(m.Archive.Entries) > int(limits.MaxEntries) {
		return ErrIntegrity
	}
	expected := []workspacearchive.Entry{{Path: "workspace", Kind: "directory", Mode: 0700}}
	seen := map[string]string{}
	var bytes int64
	for _, entry := range m.Files {
		if !safePath(entry.Path) || entry.Path == "." || privatePath(entry.Path) || seen[entry.Path] != "" || entry.Mode < 0 || entry.Mode&^0777 != 0 || !slices.ContainsFunc(m.Paths, func(p string) bool { return beneath(entry.Path, p) }) {
			return ErrIntegrity
		}
		switch entry.Kind {
		case "file":
			if entry.Size < 0 || entry.Size > m.Limits.MaxFileBytes || entry.Size > m.Limits.MaxBytes-bytes || !digestPattern.MatchString(entry.SHA256) {
				return ErrIntegrity
			}
			bytes += entry.Size
			if entry.Link != "" {
				return ErrIntegrity
			}
		case "directory":
			if entry.Size != 0 || entry.Link != "" || entry.SHA256 != "" {
				return ErrIntegrity
			}
		case "symlink":
			resolved := path.Clean(path.Join(path.Dir(entry.Path), entry.Link))
			if entry.Mode != 0777 || entry.Size != 0 || entry.SHA256 != "" || entry.Link == "" || len(entry.Link) > 4096 || !utf8.ValidString(entry.Link) || path.IsAbs(entry.Link) || strings.ContainsAny(entry.Link, "\x00\\") || resolved == ".." || strings.HasPrefix(resolved, "../") {
				return ErrIntegrity
			}
		default:
			return ErrIntegrity
		}
		seen[entry.Path] = entry.Kind
		entry.Path = "workspace/" + entry.Path
		expected = append(expected, entry)
	}
	if len(m.Files) != r.Files || bytes != r.Bytes {
		return ErrIntegrity
	}
	if len(m.Repositories) > 0 {
		expected = append(expected, workspacearchive.Entry{Path: "git", Kind: "directory", Mode: 0700})
	}
	repos := map[string]bool{}
	var gitBytes int64
	indexes := 0
	for i, repo := range m.Repositories {
		if !safePath(repo.Path) || hasGit(repo.Path) || repos[repo.Path] || !slices.ContainsFunc(m.Paths, func(p string) bool { return beneath(repo.Path, p) }) || (repo.Path != "." && seen[repo.Path] != "directory") || (repo.ObjectFormat != "sha1" && repo.ObjectFormat != "sha256") {
			return ErrIntegrity
		}
		repos[repo.Path] = true
		refs, ok := validRefs(repo.Refs, repo.ObjectFormat)
		if !ok {
			return ErrIntegrity
		}
		if repo.HEAD != "" && !validOID(repo.HEAD, repo.ObjectFormat) {
			return ErrIntegrity
		}
		if repo.SymbolicHEAD != "" && (!validRef(repo.SymbolicHEAD) || !strings.HasPrefix(repo.SymbolicHEAD, "refs/heads/") || resolveRef(refs, repo.SymbolicHEAD) != repo.HEAD) {
			return ErrIntegrity
		}
		stages := map[string]int{}
		for _, entry := range repo.Index {
			if !safePath(entry.Path) || entry.Path == "." || hasGit(entry.Path) || !validOID(entry.OID, repo.ObjectFormat) || entry.Stage < 0 || entry.Stage > 3 || !slices.Contains([]string{"100644", "100755", "120000", "160000"}, entry.Mode) {
				return ErrIntegrity
			}
			mask := 1 << entry.Stage
			if stages[entry.Path]&mask != 0 || (entry.Stage == 0 && stages[entry.Path] != 0) || (entry.Stage != 0 && stages[entry.Path]&1 != 0) {
				return ErrIntegrity
			}
			stages[entry.Path] |= mask
		}
		indexes += len(repo.Index) + len(repo.Refs)
		if indexes > m.Limits.MaxEntries {
			return ErrIntegrity
		}
		operations := map[string]bool{}
		for _, op := range repo.Operation {
			if operations[op.Name] || len(op.Data) > 1<<20 || !slices.Contains([]string{"MERGE_HEAD", "MERGE_MODE", "MERGE_MSG", "ORIG_HEAD", "CHERRY_PICK_HEAD", "REVERT_HEAD", "MERGE_AUTOSTASH"}, op.Name) {
				return ErrIntegrity
			}
			operations[op.Name] = true
			if op.Name != "MERGE_MODE" && op.Name != "MERGE_MSG" {
				values := strings.Fields(string(op.Data))
				if len(values) < 1 || len(values) > 64 {
					return ErrIntegrity
				}
				for _, oid := range values {
					if !validOID(oid, repo.ObjectFormat) {
						return ErrIntegrity
					}
				}
			}
		}
		if repo.Pack != "" {
			if repo.Pack != "git/"+strconv.Itoa(i)+".pack" || repo.PackBytes < 1 || repo.PackBytes > m.Limits.MaxGitBytes-gitBytes || !digestPattern.MatchString(repo.PackSHA256) {
				return ErrIntegrity
			}
			gitBytes += repo.PackBytes
			expected = append(expected, workspacearchive.Entry{Path: repo.Pack, Kind: "file", Mode: 0600, Size: repo.PackBytes, SHA256: repo.PackSHA256})
		} else if repo.PackBytes != 0 || repo.PackSHA256 != "" || repo.HEAD != "" || len(repo.Refs) != 0 || len(repo.Index) != 0 {
			return ErrIntegrity
		}
	}
	if m.Archive.Version != 1 || m.Archive.Bytes != bytes+gitBytes || !slices.Equal(m.Archive.Entries, expected) {
		return ErrIntegrity
	}
	return nil
}
