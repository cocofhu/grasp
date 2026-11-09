package chatsession

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/cocofhu/grasp/internal/filestore"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/objectstore"
)

type countedQueueObject struct {
	body []byte
	info objectstore.Info
}

type countedQueueObjects struct {
	objectstore.Store
	mu      sync.Mutex
	objects map[string]countedQueueObject
	opens   atomic.Int32
	refuse  atomic.Bool
}

func (s *countedQueueObjects) Put(_ context.Context, key string, body io.ReadSeeker, info objectstore.Info) error {
	raw, err := io.ReadAll(body)
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.objects[key] = countedQueueObject{body: raw, info: info}
	s.mu.Unlock()
	return nil
}

func (s *countedQueueObjects) Open(_ context.Context, key string) (io.ReadCloser, objectstore.Info, error) {
	s.opens.Add(1)
	if s.refuse.Load() {
		return nil, objectstore.Info{}, errors.New("object backend unavailable")
	}
	s.mu.Lock()
	value, ok := s.objects[key]
	s.mu.Unlock()
	if !ok {
		return nil, objectstore.Info{}, errors.New("accepted object missing")
	}
	return io.NopCloser(bytes.NewReader(value.body)), value.info, nil
}

func metadataTestConfig() DurableConfig[durableTestTurn] {
	cfg := durableTestConfig(nil)
	encode := cfg.Intent
	cfg.Intent = func(turn durableTestTurn) (QueueIntent, error) {
		intent, err := encode(turn)
		if err != nil {
			return intent, err
		}
		if len(turn.Text) > 16 {
			turn.Text = turn.Text[:16]
		}
		intent.Display, err = json.Marshal(turn)
		return intent, err
	}
	cfg.DecodeMetadata = func(row models.ChatQueueTurn) (durableTestTurn, error) {
		var turn durableTestTurn
		err := json.Unmarshal([]byte(row.Display), &turn)
		if err == nil && turn.ID != row.ID {
			err = errors.New("preview identity differs from receipt")
		}
		return turn, err
	}
	return cfg
}

func metadataObjects(t *testing.T, stores ...*DurableStore) *countedQueueObjects {
	t.Helper()
	objects := &countedQueueObjects{objects: map[string]countedQueueObject{}}
	for _, store := range stores {
		files, err := filestore.FromDB(store.db)
		if err != nil {
			t.Fatal(err)
		}
		if err := files.SetObjectStore(objects); err != nil {
			t.Fatal(err)
		}
	}
	return objects
}

func TestDurableQueueStatusAndViewsDoNotReadExecutionObjects(t *testing.T) {
	_, store, remote, scope := durableFixture(t)
	objects := metadataObjects(t, store, remote)
	ctx := context.Background()
	cfg := metadataTestConfig()
	original := strings.Repeat("full original accepted body ", 10000)
	for _, id := range []string{"first", "second", "third"} {
		intent, err := cfg.Intent(durableTestTurn{ID: id, Text: original})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := store.Admit(ctx, scope, intent, 0, nil); err != nil {
			t.Fatal(err)
		}
	}
	lease := acquireQueue(t, store, scope)
	claimed, err := store.Claim(ctx, lease)
	if err != nil || claimed == nil {
		t.Fatalf("native claim failed: %+v %v", claimed, err)
	}
	full, err := cfg.Decode(*claimed)
	if err != nil || full.Text != original || objects.opens.Load() != 1 {
		t.Fatalf("claim did not load the exact complete body once: len=%d gets=%d %v", len(full.Text), objects.opens.Load(), err)
	}
	objects.opens.Store(0)
	objects.refuse.Store(true)
	viewer := NewDurableSession(remote, scope, cfg)
	for range 20 {
		snapshot, err := viewer.SnapshotChecked()
		if err != nil || !snapshot.Busy || snapshot.Waiting != 2 || snapshot.ActiveItem["id"] != "first" || snapshot.Items[0]["id"] != "second" {
			t.Fatalf("queue status depends on object availability: %+v %v", snapshot, err)
		}
		if view := viewer.View(); !view.HasActive || len(view.Pending) != 2 || len(view.Active.Text) != 16 {
			t.Fatalf("typed view loaded the full native body: %+v", view)
		}
		if active, ok := viewer.Active(); !ok || active.ID != "first" || len(active.Text) != 16 {
			t.Fatalf("active preview changed identity: %+v %v", active, ok)
		}
		viewer.PublishQueueState()
	}
	if got := objects.opens.Load(); got != 0 {
		t.Fatalf("status reads fetched immutable execution bodies %d times", got)
	}
	if err := store.Dispatch(ctx, lease, claimed.ID); err != nil {
		t.Fatal(err)
	}
}

func TestDurableQueueLegacyDisplayBackfillsOnceAndNeverRewritesAcceptedBody(t *testing.T) {
	_, store, remote, scope := durableFixture(t)
	objects := metadataObjects(t, store, remote)
	ctx := context.Background()
	cfg := metadataTestConfig()
	intent, err := cfg.Intent(durableTestTurn{ID: "legacy", Text: strings.Repeat("original ", 10000)})
	if err != nil {
		t.Fatal(err)
	}
	intent.Display = nil
	if _, err := store.Admit(ctx, scope, intent, 0, nil); err != nil {
		t.Fatal(err)
	}
	viewer := NewDurableSession(remote, scope, cfg)
	first, err := viewer.SnapshotChecked()
	if err != nil || first.Waiting != 1 || first.Items[0]["id"] != intent.ID || objects.opens.Load() != 1 {
		t.Fatalf("legacy preview was not safely derived once: %+v gets=%d %v", first, objects.opens.Load(), err)
	}
	metadata, err := store.SnapshotMetadata(ctx, scope)
	if err != nil || len(metadata.Pending) != 1 || metadata.Pending[0].Display == "" || len(metadata.Pending[0].Payload) != 0 {
		t.Fatalf("preview was not inline metadata: %+v %v", metadata, err)
	}
	objects.opens.Store(0)
	objects.refuse.Store(true)
	for range 5 {
		if _, err := viewer.SnapshotChecked(); err != nil {
			t.Fatalf("backfilled preview reread object storage: %v", err)
		}
	}
	if objects.opens.Load() != 0 {
		t.Fatal("backfilled status fetched the original body again")
	}
	objects.refuse.Store(false)
	if full, err := store.ReadTurn(ctx, scope, intent.ID); err != nil || !bytes.Equal(full.Payload, intent.Payload) {
		t.Fatalf("backfill rewrote original accepted bytes: %v", err)
	}
}

func TestDurableQueueDisplayBackfillIsImmutableAndScoped(t *testing.T) {
	_, store, remote, scope := durableFixture(t)
	objects := metadataObjects(t, store, remote)
	ctx := context.Background()
	row := admitQueue(t, store, scope, "original body", "one")
	other := QueueScope{Surface: scope.Surface, Key: "different-conversation"}
	display := []byte(`{"ID":"preview"}`)
	if err := store.BackfillDisplay(ctx, other, row.Turn.ID, row.Turn.Fingerprint, display); err != nil {
		t.Fatal(err)
	}
	if err := store.BackfillDisplay(ctx, scope, row.Turn.ID, strings.Repeat("0", 64), display); err != nil {
		t.Fatal(err)
	}
	if _, err := remote.ReadTurn(ctx, other, row.Turn.ID); err == nil || objects.opens.Load() != 0 {
		t.Fatalf("wrong queue fetched accepted body: gets=%d %v", objects.opens.Load(), err)
	}
	metadata, err := store.SnapshotMetadata(ctx, scope)
	if err != nil || metadata.Pending[0].Display != "" {
		t.Fatalf("foreign/stale metadata modified receipt: %+v %v", metadata, err)
	}
	if err := store.BackfillDisplay(ctx, scope, row.Turn.ID, row.Turn.Fingerprint, display); err != nil {
		t.Fatal(err)
	}
	if err := remote.BackfillDisplay(ctx, scope, row.Turn.ID, row.Turn.Fingerprint, []byte(`{"ID":"different"}`)); err != nil {
		t.Fatal(err)
	}
	metadata, err = store.SnapshotMetadata(ctx, scope)
	if err != nil || metadata.Pending[0].Display != string(display) {
		t.Fatalf("later reader rewrote immutable preview: %+v %v", metadata, err)
	}
	for _, bad := range [][]byte{nil, []byte("{broken"), bytes.Repeat([]byte(" "), MaxQueueDisplayBytes+1)} {
		if err := store.BackfillDisplay(ctx, scope, row.Turn.ID, row.Turn.Fingerprint, bad); err == nil {
			t.Fatal("unbounded or invalid preview accepted")
		}
	}
}

func TestDurableQueueRejectsUnboundedOrInvalidInlineDisplaysBeforeAdmission(t *testing.T) {
	_, store, _, scope := durableFixture(t)
	ctx := context.Background()
	for _, display := range [][]byte{[]byte("{broken"), bytes.Repeat([]byte(" "), MaxQueueDisplayBytes+1)} {
		intent := queueIntent("original accepted body", "one")
		intent.Display = display
		if _, err := store.Admit(ctx, scope, intent, 0, nil); err == nil {
			t.Fatal("invalid display became a durable accepted request")
		}
	}
	if snapshot, err := store.Metadata(ctx, scope); err != nil || snapshot.Waiting != 0 || snapshot.Busy {
		t.Fatalf("rejected display left accepted work: %+v %v", snapshot, err)
	}
}

func TestDurableQueueLegacyMetadataReadFailureRetainsIntentForRetry(t *testing.T) {
	_, store, remote, scope := durableFixture(t)
	objects := metadataObjects(t, store, remote)
	ctx := context.Background()
	cfg := metadataTestConfig()
	intent, err := cfg.Intent(durableTestTurn{ID: "legacy", Text: "exact original body"})
	if err != nil {
		t.Fatal(err)
	}
	intent.Display = nil
	if _, err := store.Admit(ctx, scope, intent, 0, nil); err != nil {
		t.Fatal(err)
	}
	viewer := NewDurableSession(remote, scope, cfg)
	objects.refuse.Store(true)
	if _, err := viewer.SnapshotChecked(); err == nil {
		t.Fatal("missing legacy body was presented as a complete empty queue")
	}
	if snapshot, err := store.SnapshotMetadata(ctx, scope); err != nil || len(snapshot.Pending) != 1 || snapshot.Pending[0].Display != "" {
		t.Fatalf("failed preview discarded or rewrote intent: %+v %v", snapshot, err)
	}
	objects.refuse.Store(false)
	if snapshot, err := viewer.SnapshotChecked(); err != nil || snapshot.Waiting != 1 || snapshot.Items[0]["id"] != intent.ID {
		t.Fatalf("retained legacy intent could not be viewed after recovery: %+v %v", snapshot, err)
	}
}
