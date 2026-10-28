package sandbox

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
)

// DefaultInjectBundleTTL is how long a published ConfigHome .tgz stays downloadable.
const DefaultInjectBundleTTL = 30 * time.Minute

// BundleStore holds short-lived ConfigHome archives for gateway config.bundleUrl
// inject. Sandbox startup.sh fetches SANDBOX_INJECT before services start.
type BundleStore struct {
	mu    sync.Mutex
	items map[string]bundleItem
}

type bundleItem struct {
	token string
	data  []byte
	exp   time.Time
}

// NewBundleStore creates an empty inject bundle registry.
func NewBundleStore() *BundleStore {
	return &BundleStore{items: map[string]bundleItem{}}
}

// Put registers gzipped-tar bytes and returns an id + bearer token.
func (s *BundleStore) Put(data []byte, ttl time.Duration) (id, token string) {
	if s == nil {
		return "", ""
	}
	token = randomHex(24)
	id = s.PutWithToken(data, ttl, token)
	if id == "" {
		return "", ""
	}
	return id, token
}

// PutWithToken registers gzipped-tar bytes under a caller-supplied bearer token
// so multiple inject bundles (e.g. SSH staging + ConfigHome) can share one
// SANDBOX_INJECT_HEADERS value.
func (s *BundleStore) PutWithToken(data []byte, ttl time.Duration, token string) (id string) {
	if s == nil || len(data) == 0 || strings.TrimSpace(token) == "" {
		return ""
	}
	if ttl <= 0 {
		ttl = DefaultInjectBundleTTL
	}
	id = randomHex(16)
	if id == "" {
		return ""
	}
	s.mu.Lock()
	s.sweepLocked(time.Now())
	s.items[id] = bundleItem{token: token, data: data, exp: time.Now().Add(ttl)}
	s.mu.Unlock()
	return id
}

// Get returns bundle bytes when id+token match and not expired.
func (s *BundleStore) Get(id, token string) ([]byte, bool) {
	if s == nil {
		return nil, false
	}
	id = strings.TrimSuffix(strings.TrimSpace(id), ".tgz")
	id = strings.TrimSuffix(id, ".tar.gz")
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sweepLocked(time.Now())
	it, ok := s.items[id]
	if !ok || it.token == "" || it.token != token {
		return nil, false
	}
	return it.data, true
}

// Delete removes a bundle (best-effort after sandbox is running).
func (s *BundleStore) Delete(id string) {
	if s == nil {
		return
	}
	id = strings.TrimSuffix(strings.TrimSpace(id), ".tgz")
	s.mu.Lock()
	delete(s.items, id)
	s.mu.Unlock()
}

func (s *BundleStore) sweepLocked(now time.Time) {
	for id, it := range s.items {
		if now.After(it.exp) {
			delete(s.items, id)
		}
	}
}

// ServeHTTP serves GET /sandbox-inject/:id(.tgz) with Authorization: Bearer.
func (s *BundleStore) ServeHTTP(w http.ResponseWriter, r *http.Request, idParam string) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	token := bearerToken(r.Header.Get("Authorization"))
	data, ok := s.Get(idParam, token)
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	w.Header().Set("Content-Type", "application/gzip")
	w.Header().Set("Content-Disposition", `attachment; filename="config-home.tgz"`)
	w.Header().Set("Content-Length", fmt.Sprintf("%d", len(data)))
	if r.Method == http.MethodHead {
		w.WriteHeader(http.StatusOK)
		return
	}
	_, _ = w.Write(data)
}

func bearerToken(h string) string {
	h = strings.TrimSpace(h)
	if len(h) >= 7 && strings.EqualFold(h[:7], "Bearer ") {
		return strings.TrimSpace(h[7:])
	}
	return ""
}

func randomHex(nBytes int) string {
	b := make([]byte, nBytes)
	if _, err := rand.Read(b); err != nil {
		// fallback: time-based (tests / constrained entropy)
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}

// PackConfigHomeTarGz builds a .tar.gz of hostDir for SANDBOX_INJECT / bundleUrl.
func PackConfigHomeTarGz(hostDir string) ([]byte, error) {
	info, err := os.Stat(hostDir)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("config home %q is not a directory", hostDir)
	}
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	err = filepath.WalkDir(hostDir, func(p string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(hostDir, p)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		fi, err := d.Info()
		if err != nil {
			return err
		}
		if fi.Mode()&os.ModeSymlink != 0 {
			return nil
		}
		hdr, err := tar.FileInfoHeader(fi, "")
		if err != nil {
			return err
		}
		hdr.Name = filepath.ToSlash(rel)
		if d.IsDir() {
			if !strings.HasSuffix(hdr.Name, "/") {
				hdr.Name += "/"
			}
			return tw.WriteHeader(hdr)
		}
		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}
		f, err := os.Open(p)
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(tw, f)
		_ = f.Close()
		return copyErr
	})
	if err != nil {
		_ = tw.Close()
		_ = gz.Close()
		return nil, err
	}
	if err := tw.Close(); err != nil {
		_ = gz.Close()
		return nil, err
	}
	if err := gz.Close(); err != nil {
		return nil, err
	}
	log.Debug().Int("bytes", buf.Len()).Str("dir", hostDir).Msg("packed config-home inject bundle")
	return buf.Bytes(), nil
}

// PackSSHInjectTarGz builds a .tar.gz containing optional id_rsa and/or
// known_hosts entries for extraction into SSHInjectStagingDir. Returns
// (nil, nil) when both inputs are empty after trim.
func PackSSHInjectTarGz(privateKey, knownHosts string) ([]byte, error) {
	return PackSSHInjectBundle(privateKey, knownHosts)
}

// PackSSHInjectBundle packs the legacy host SSH files. Per-repository
// credential manifests are no longer produced; Agent Git authorization is the
// git/ catalog from packGitInjectBundle.
func PackSSHInjectBundle(privateKey, knownHosts string) ([]byte, error) {
	return packGitInjectBundle(privateKey, knownHosts, false, nil)
}

// packGitInjectBundle preserves legacy host SSH files while carrying an
// explicit Agent catalog (including an empty selection) as root-only files.
func packGitInjectBundle(privateKey, knownHosts string, configured bool, credentials []GitCredential) ([]byte, error) {
	if configured {
		privateKey, knownHosts = "", ""
	}
	key := strings.TrimSpace(privateKey)
	hosts := strings.TrimSpace(knownHosts)
	if key == "" && hosts == "" && !configured {
		return nil, nil
	}
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	writeFile := func(name, body string, mode int64) error {
		if !strings.HasSuffix(body, "\n") {
			body += "\n"
		}
		hdr := &tar.Header{
			Name: name,
			Mode: mode,
			Size: int64(len(body)),
		}
		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}
		_, err := io.WriteString(tw, body)
		return err
	}
	type entry struct {
		name, body string
		mode       int64
	}
	var files []entry
	if configured {
		catalog, err := gitCatalogFiles(credentials)
		if err != nil {
			return nil, err
		}
		for _, file := range catalog {
			files = append(files, entry{"git/" + file.name, file.body, 0o600})
		}
	}
	// Preserve the caller's PEM / known_hosts body (not the trimmed copy).
	if key != "" {
		files = append(files, entry{"id_rsa", privateKey, 0o600})
	}
	if hosts != "" {
		files = append(files, entry{"known_hosts", knownHosts, 0o644})
	}
	for _, f := range files {
		if err := writeFile(f.name, f.body, f.mode); err != nil {
			_ = tw.Close()
			_ = gz.Close()
			return nil, err
		}
	}
	if err := tw.Close(); err != nil {
		_ = gz.Close()
		return nil, err
	}
	if err := gz.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// gitCatalogFile has a generated basename and a newline-terminated body.
// Catalog metadata and secrets never determine file paths: names are either
// manifest.json or a numeric index with a fixed suffix.
type gitCatalogFile struct {
	name string
	body string
}

// gitCatalogFiles is shared by the sandbox transport and local worktree
// installer so validation, metadata and secret normalization stay identical.
func gitCatalogFiles(credentials []GitCredential) ([]gitCatalogFile, error) {
	type catalogEntry struct {
		Index     int    `json:"index"`
		ID        string `json:"id"`
		Name      string `json:"name,omitempty"`
		Kind      string `json:"kind"`
		Scope     string `json:"scope"`
		Preferred bool   `json:"preferred"`
	}
	catalog := make([]catalogEntry, 0, len(credentials))
	var files []gitCatalogFile
	appendFile := func(name, body string) {
		if !strings.HasSuffix(body, "\n") {
			body += "\n"
		}
		files = append(files, gitCatalogFile{name: name, body: body})
	}
	for i, credential := range credentials {
		switch credential.Kind {
		case RepoCredentialGitHubHTTPS, RepoCredentialGitLabHTTPS, RepoCredentialSSH:
		default:
			return nil, fmt.Errorf("invalid git credential kind at index %d", i)
		}
		if strings.TrimSpace(credential.Secret) == "" {
			return nil, fmt.Errorf("empty git credential at index %d", i)
		}
		catalog = append(catalog, catalogEntry{i, credential.ID, credential.Name, credential.Kind, credential.Scope, credential.Preferred})
		secret := credential.Secret
		if credential.Kind != RepoCredentialSSH {
			secret = strings.TrimSpace(secret)
		}
		appendFile(fmt.Sprintf("%d.secret", i), secret)
		if strings.TrimSpace(credential.KnownHosts) != "" {
			appendFile(fmt.Sprintf("%d.known_hosts", i), credential.KnownHosts)
		}
	}
	body, err := json.Marshal(catalog)
	if err != nil {
		return nil, err
	}
	appendFile("manifest.json", string(body))
	return files, nil
}
