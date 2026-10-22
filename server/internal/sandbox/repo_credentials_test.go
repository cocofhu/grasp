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

func TestPackSSHInjectBundleRepoCredentials(t *testing.T) {
	data, err := PackSSHInjectBundle("", "", []RepoCredential{
		{URL: "https://github.com/acme/app.git", Kind: RepoCredentialGitHubHTTPS, Secret: " tok\n"},
		{URL: "bad url", Kind: RepoCredentialGitHubHTTPS, Secret: "x"},
		{URL: "https://x/y", Kind: "weird", Secret: "x"},
		{URL: "https://x/empty", Kind: RepoCredentialGitLabHTTPS, Secret: "  "},
		{URL: "git@github.com:acme/infra.git", Kind: RepoCredentialSSH, Secret: "KEY", KnownHosts: "github.com ssh-ed25519 AAAA"},
	})
	if err != nil {
		t.Fatal(err)
	}
	body, mode := untar(t, data)
	wantManifest := "0\tgithub_https\thttps://github.com/acme/app.git\n1\tssh\tgit@github.com:acme/infra.git\n"
	if body["repos/manifest"] != wantManifest {
		t.Fatalf("manifest=%q", body["repos/manifest"])
	}
	if body["repos/0.secret"] != "tok\n" || body["repos/1.secret"] != "KEY\n" {
		t.Fatalf("secrets=%q %q", body["repos/0.secret"], body["repos/1.secret"])
	}
	if mode["repos/0.secret"] != 0o600 || mode["repos/1.secret"] != 0o600 {
		t.Fatalf("secret modes=%o %o", mode["repos/0.secret"], mode["repos/1.secret"])
	}
	if _, ok := body["repos/0.known_hosts"]; ok {
		t.Fatal("https entry must not get known_hosts")
	}
	if body["repos/1.known_hosts"] != "github.com ssh-ed25519 AAAA\n" {
		t.Fatalf("known_hosts=%q", body["repos/1.known_hosts"])
	}
	if _, ok := body["id_rsa"]; ok {
		t.Fatal("no host key was given")
	}
}

func TestPackSSHInjectBundleOnlyInvalidRepos(t *testing.T) {
	data, err := PackSSHInjectBundle("", "", []RepoCredential{{URL: "https://x/y", Kind: "weird", Secret: "x"}})
	if err != nil || data != nil {
		t.Fatalf("want nil bundle, got %d bytes err=%v", len(data), err)
	}
}
