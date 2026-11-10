package objectstore

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"math"
	"os"
	"strings"
	"testing"
)

func TestSpoolPreservesBytesAndRemovesTemporaryFile(t *testing.T) {
	body := []byte("二进制\x00\xff正文")
	hash := sha256.Sum256(body)
	file, err := Spool(context.Background(), bytes.NewReader(body), 64)
	if err != nil {
		t.Fatal(err)
	}
	name := file.Name()
	if file.Size != int64(len(body)) || file.SHA256 != hex.EncodeToString(hash[:]) {
		t.Fatalf("wrong immutable metadata: %d %s", file.Size, file.SHA256)
	}
	read, err := io.ReadAll(file)
	if err != nil || !bytes.Equal(read, body) {
		t.Fatalf("body=%q err=%v", read, err)
	}
	if err = file.Close(); err != nil {
		t.Fatal(err)
	}
	if err = file.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(name); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("staging file leaked: %v", err)
	}
}

func TestSpoolAndDownloadRejectUnacknowledgedOrCorruptBytes(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Spool(ctx, strings.NewReader("body"), 100); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled source=%v", err)
	}
	if _, err := Spool(context.Background(), strings.NewReader("oversized"), 3); err == nil {
		t.Fatal("oversized input accepted")
	}
	hash := sha256.Sum256([]byte("correct"))
	digest := hex.EncodeToString(hash[:])
	for _, size := range []int64{-1, 0, 7, math.MaxInt64} {
		if file, err := DownloadVerified(context.Background(), strings.NewReader("corrupt"), size, digest); err == nil {
			_ = file.Close()
			t.Fatalf("corrupt body accepted with size %d", size)
		}
	}
	file, err := DownloadVerified(context.Background(), strings.NewReader("correct"), 7, digest)
	if err != nil {
		t.Fatal(err)
	}
	_ = file.Close()
}
