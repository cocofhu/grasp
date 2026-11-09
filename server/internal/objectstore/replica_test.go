package objectstore

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"strings"
	"testing"
)

func TestReplicaVerifiesSourceAndTargetBytesAndMetadata(t *testing.T) {
	ctx := context.Background()
	source, sourceWire := fixture(t)
	target, targetWire := fixture(t)
	for _, key := range []string{"attachments/one", "payloads/two", "snapshots/held-upload"} {
		body := []byte("原始数据\x00\xff" + key)
		hash := sha256.Sum256(body)
		info := Info{Size: int64(len(body)), ContentType: "application/octet-stream", Metadata: map[string]string{
			"grasp-sha256": hex.EncodeToString(hash[:]), "grasp-meta": "immutable-presentation"}}
		if err := source.Put(ctx, key, bytes.NewReader(body), info); err != nil {
			t.Fatal(err)
		}
	}
	var entries []ReplicaObject
	record := func(e ReplicaObject) error { entries = append(entries, e); return nil }
	report, err := Replicate(ctx, source, target, record)
	if err != nil || report.Files != 3 || len(entries) != 3 {
		t.Fatalf("replica: %+v %v", report, err)
	}
	if _, err := Replicate(ctx, source, target, func(ReplicaObject) error { return nil }); err != nil {
		t.Fatalf("idempotent verified copy: %v", err)
	}
	if len(sourceWire.objects) != 3 {
		t.Fatal("source data deleted")
	}
	key := "/bucket/root/attachments/one"
	targetWire.mu.Lock()
	object := targetWire.objects[key]
	object.body[0] ^= 1
	targetWire.objects[key] = object
	targetWire.mu.Unlock()
	if _, err := Replicate(ctx, source, target, record); err == nil {
		t.Fatal("corrupt target accepted based on HEAD")
	}
	targetWire.mu.Lock()
	object.body[0] ^= 1
	object.info.Metadata["grasp-meta"] = "different-name"
	targetWire.objects[key] = object
	targetWire.mu.Unlock()
	if _, err := Replicate(ctx, source, target, record); err == nil {
		t.Fatal("conflicting target metadata overwritten or accepted")
	}
	if err := target.Delete(ctx, "attachments/one"); err != nil {
		t.Fatal(err)
	}
	sourceWire.mu.Lock()
	broken := sourceWire.objects[key]
	broken.body[0] ^= 1
	sourceWire.objects[key] = broken
	sourceWire.mu.Unlock()
	if _, err := Replicate(ctx, source, target, record); err == nil {
		t.Fatal("corrupt source copied")
	}
	if _, err := target.Head(ctx, "attachments/one"); err == nil {
		t.Fatal("corrupt source published a target object")
	}
}

type stalledListing struct{ Store }

func (s stalledListing) List(context.Context, string, string) (Page, error) {
	return Page{Next: "same-cursor"}, nil
}

func TestReplicaCannotClaimIncompleteInventory(t *testing.T) {
	ctx := context.Background()
	source, _ := fixture(t)
	target, _ := fixture(t)
	if _, err := Replicate(ctx, stalledListing{source}, target, func(ReplicaObject) error { return nil }); err == nil {
		t.Fatal("non-advancing pagination accepted")
	}
	body := "retained"
	h := sha256.Sum256([]byte(body))
	if err := source.Put(ctx, "one", strings.NewReader(body), Info{Size: int64(len(body)), ContentType: "text/plain", Metadata: map[string]string{"grasp-sha256": hex.EncodeToString(h[:])}}); err != nil {
		t.Fatal(err)
	}
	failed := errors.New("inventory disk full")
	if _, err := Replicate(ctx, source, target, func(ReplicaObject) error { return failed }); !errors.Is(err, failed) {
		t.Fatalf("inventory failure was ignored: %v", err)
	}
	reader, _, err := source.Open(ctx, "one")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = reader.Close() }()
	if read, _ := io.ReadAll(reader); string(read) != body {
		t.Fatal("partial copy lost source")
	}
}

func TestSeparationProbeRejectsEndpointAliasesWithoutLeavingMarkers(t *testing.T) {
	ctx := context.Background()
	source, wire := fixture(t)
	if err := ProbeSeparation(ctx, source, source, "root", "root"); err == nil {
		t.Fatal("same storage behind different endpoint aliases accepted")
	}
	if len(wire.objects) != 0 {
		t.Fatal("separation marker retained")
	}
	target, targetWire := fixture(t)
	if err := ProbeSeparation(ctx, source, target, "root", "root"); err != nil {
		t.Fatalf("independent stores rejected: %v", err)
	}
	if len(wire.objects) != 0 || len(targetWire.objects) != 0 {
		t.Fatal("probe changed existing storage inventory")
	}
}
