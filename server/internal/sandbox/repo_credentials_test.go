package sandbox

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"io"
	"testing"
)

func untar(t *testing.T, data []byte) (map[string]string, map[string]int64) {
	t.Helper()
	gr, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	defer gr.Close()
	tr := tar.NewReader(gr)
	body, mode := map[string]string{}, map[string]int64{}
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(tr)
		body[hdr.Name] = string(b)
		mode[hdr.Name] = hdr.Mode & 0o777
	}
	return body, mode
}

func TestPackSSHInjectBundleOmitsRepositoryGrants(t *testing.T) {
	data, err := PackSSHInjectBundle("PRIVATE", "github.com ssh-ed25519 AAAA")
	if err != nil {
		t.Fatal(err)
	}
	body, mode := untar(t, data)
	if body["id_rsa"] != "PRIVATE\n" || mode["id_rsa"] != 0o600 {
		t.Fatalf("host key = %q mode %o", body["id_rsa"], mode["id_rsa"])
	}
	if body["known_hosts"] != "github.com ssh-ed25519 AAAA\n" {
		t.Fatalf("known_hosts = %q", body["known_hosts"])
	}
	for name := range body {
		if name == "repos/manifest" || len(name) > 6 && name[:6] == "repos/" {
			t.Fatalf("repository grant staged: %s", name)
		}
	}
}

func TestPackSSHInjectBundleEmpty(t *testing.T) {
	data, err := PackSSHInjectBundle("  ", "")
	if err != nil || data != nil {
		t.Fatalf("want nil bundle, got %d bytes err=%v", len(data), err)
	}
}
