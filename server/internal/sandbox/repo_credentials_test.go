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
