package workspacearchive

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"io"
	"reflect"
	"testing"
	"time"
)

var testLimits = Limits{MaxEntries: 100, MaxBytes: 1 << 20, MaxFileBytes: 1 << 19}

type member struct {
	name, body, link string
	kind             byte
	mode             int64
	owner            int
}

func archive(t *testing.T, members ...member) []byte {
	t.Helper()
	var out bytes.Buffer
	z := gzip.NewWriter(&out)
	tw := tar.NewWriter(z)
	for _, m := range members {
		kind := m.kind
		if kind == 0 {
			kind = tar.TypeReg
		}
		h := &tar.Header{Name: m.name, Mode: m.mode, Typeflag: kind, Linkname: m.link, Uid: m.owner, ModTime: time.Now()}
		if m.owner != 0 {
			h.Uname = "source-owner"
		}
		if kind == tar.TypeReg {
			h.Size = int64(len(m.body))
		}
		if err := tw.WriteHeader(h); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(m.body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := z.Close(); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

func TestCanonicalRoundTripPreservesFilesAndLinks(t *testing.T) {
	files := []member{
		{name: "./", kind: tar.TypeDir, mode: 0o755},
		{name: "./repo-a/.git/HEAD", body: "ref: refs/heads/work\n", mode: 0o644, owner: 123},
		{name: "./repo-a/dirty.go", body: "uncommitted edit\n", mode: 0o644},
		{name: "./repo-b/untracked.bin", body: "\x00\xff\x01\x00", mode: 0o600},
		{name: "./非Git目录/脚本.sh", body: "#!/bin/sh\n", mode: 0o6755},
		{name: "./repo-b/relative", kind: tar.TypeSymlink, link: "../repo-a/dirty.go", mode: 0o777},
		{name: "./repo-b/hard", kind: tar.TypeLink, link: "./repo-b/untracked.bin", mode: 0o600},
	}
	var out bytes.Buffer
	got, err := Canonicalize(context.Background(), bytes.NewReader(archive(t, files...)), &out, testLimits)
	if err != nil {
		t.Fatal(err)
	}
	verified, err := Inspect(context.Background(), bytes.NewReader(out.Bytes()), testLimits)
	if err != nil || !reflect.DeepEqual(got, verified) {
		t.Fatalf("manifest changed after publication: %+v %+v %v", got, verified, err)
	}
	z, err := gzip.NewReader(bytes.NewReader(out.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = z.Close() }()
	tr := tar.NewReader(z)
	for _, expected := range files[1:] {
		h, err := tr.Next()
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(tr)
		if err != nil || string(data) != expected.body || h.Mode&0o7000 != 0 || h.Uid != 0 || h.Gid != 0 || h.Uname != "" {
			t.Fatalf("bytes or safe permissions changed: %+v %q %v", h, data, err)
		}
	}
	if got.Entries[3].Mode != 0o755 || got.Entries[5].SHA256 != got.Entries[2].SHA256 {
		t.Fatal("executable or hardlink identity lost")
	}
}

func TestRejectsTraversalShadowingAndDevices(t *testing.T) {
	cases := map[string][]member{
		"absolute":        {{name: "/etc/passwd", body: "x"}},
		"parent":          {{name: "a/../b", body: "x"}},
		"outside-link":    {{name: "link", kind: tar.TypeSymlink, link: "../private"}},
		"absolute-link":   {{name: "link", kind: tar.TypeSymlink, link: "/root/.ssh"}},
		"through-link":    {{name: "link", kind: tar.TypeSymlink, link: "safe"}, {name: "link/data", body: "x"}},
		"late-shadow":     {{name: "parent/data", body: "x"}, {name: "parent", kind: tar.TypeSymlink, link: "safe"}},
		"duplicate":       {{name: "./data", body: "one"}, {name: "data", body: "two"}},
		"unknown-hard":    {{name: "hard", kind: tar.TypeLink, link: "missing"}},
		"outside-hard":    {{name: "hard", kind: tar.TypeLink, link: "../private"}},
		"device":          {{name: "device", kind: tar.TypeChar}},
		"fifo":            {{name: "pipe", kind: tar.TypeFifo}},
		"setuid":          {{name: "privileged", mode: 0o4755}},
		"ownership":       {{name: "other-owner", owner: 123}},
		"hard-to-symlink": {{name: "sym", kind: tar.TypeSymlink, link: "inside"}, {name: "hard", kind: tar.TypeLink, link: "sym"}},
	}
	for name, files := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := Inspect(context.Background(), bytes.NewReader(archive(t, files...)), testLimits)
			if !errors.Is(err, ErrUnsafe) {
				t.Fatalf("unsafe archive accepted: %v", err)
			}
		})
	}
}

func TestDecompressedQuotasAndTruncation(t *testing.T) {
	data := archive(t, member{name: "compressible", body: string(bytes.Repeat([]byte{0}, 10000))})
	for _, limits := range []Limits{{MaxEntries: 10, MaxBytes: 100, MaxFileBytes: 20000}, {MaxEntries: 10, MaxBytes: 20000, MaxFileBytes: 100}} {
		if _, err := Inspect(context.Background(), bytes.NewReader(data), limits); !errors.Is(err, ErrLimit) {
			t.Fatalf("decompression quota ignored: %v", err)
		}
	}
	if _, err := Inspect(context.Background(), bytes.NewReader(archive(t, member{name: "a"}, member{name: "b"})), Limits{MaxEntries: 1, MaxBytes: 10, MaxFileBytes: 10}); !errors.Is(err, ErrLimit) {
		t.Fatalf("entry quota ignored: %v", err)
	}
	for _, incomplete := range [][]byte{data[:len(data)-4], data[:len(data)/2], []byte("not gzip")} {
		if _, err := Inspect(context.Background(), bytes.NewReader(incomplete), testLimits); err == nil {
			t.Fatal("truncated snapshot was declared verified")
		}
	}
	corrupt := bytes.Clone(data)
	corrupt[len(corrupt)-8] ^= 1
	if _, err := Inspect(context.Background(), bytes.NewReader(corrupt), testLimits); err == nil {
		t.Fatal("gzip checksum corruption accepted")
	}
}

func TestRejectsTrailingDataCancellationAndFailedPublication(t *testing.T) {
	var raw bytes.Buffer
	tw := tar.NewWriter(&raw)
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	_, _ = raw.WriteString("uninspected content")
	var compressed bytes.Buffer
	z := gzip.NewWriter(&compressed)
	if _, err := z.Write(raw.Bytes()); err != nil {
		t.Fatal(err)
	}
	if err := z.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := Inspect(context.Background(), &compressed, testLimits); !errors.Is(err, ErrUnsafe) {
		t.Fatalf("trailing content accepted: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Inspect(ctx, bytes.NewReader(nil), testLimits); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel ignored: %v", err)
	}
	if _, err := Canonicalize(context.Background(), bytes.NewReader(archive(t, member{name: "file", body: "content"})), failedWriter{}, testLimits); err == nil {
		t.Fatal("failed destination published a manifest")
	}
}

type failedWriter struct{}

func (failedWriter) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }
