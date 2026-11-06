package services

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/cocofhu/grasp/internal/filestore"
	"github.com/cocofhu/grasp/internal/models"
)

func TestArtifactFileImportVersionsAndSQLReferences(t *testing.T) {
	db := newTestDB(t)
	s := NewArtifactService(db)
	ctx := context.Background()
	first := []byte("%PDF-1.7\noriginal PDF bytes\n")
	a, err := s.SaveFile(ctx, "run", "writer", "报告.pdf", "session", bytes.NewReader(first))
	if err != nil {
		t.Fatal(err)
	}
	if a.Kind != "pdf" || a.MimeType != "application/pdf" || a.SizeBytes != len(first) || a.Revision != 1 || a.Content != "" {
		t.Fatalf("artifact=%+v", a)
	}
	var raw string
	if err = db.Table("artifacts").Select("content").Where("id = ?", a.ID).Row().Scan(&raw); err != nil {
		t.Fatal(err)
	}
	if !filestore.IsRef(raw) {
		t.Fatalf("SQL contains file: %q", raw)
	}
	unchanged, err := s.SaveFile(ctx, "run", "writer", "报告.pdf", "session", bytes.NewReader(first))
	if err != nil || unchanged.ID != a.ID || unchanged.Revision != 1 {
		t.Fatalf("idempotent write=%+v err=%v", unchanged, err)
	}
	second := []byte("%PDF-1.7\nupdated PDF bytes\n")
	a, err = s.SaveFile(ctx, "run", "writer", "报告.pdf", "session", bytes.NewReader(second))
	if err != nil || a.Revision != 2 {
		t.Fatalf("update=%+v err=%v", a, err)
	}
	for rev, want := range map[int][]byte{1: first, 2: second} {
		file, err := s.OpenFile(ctx, a.ID, rev)
		if err != nil {
			t.Fatal(err)
		}
		got, err := io.ReadAll(file.Reader)
		_ = file.Reader.Close()
		if err != nil || !bytes.Equal(got, want) || file.Artifact.Revision != rev {
			t.Fatalf("rev=%d got=%q err=%v", rev, got, err)
		}
	}
	versions := s.ListVersions(a.ID)
	if len(versions) != 1 || versions[0].Revision != 1 {
		t.Fatalf("versions=%+v", versions)
	}
	for _, name := range []string{"../bad.pdf", "plan.json", "bad\nname.pdf"} {
		if _, err = s.SaveFile(ctx, "run", "writer", name, "", bytes.NewReader(first)); err == nil {
			t.Fatalf("accepted %q", name)
		}
	}
}

func TestArtifactDeliveryDurableIdempotentAndTurnBound(t *testing.T) {
	db := newTestDB(t)
	s := NewArtifactDeliveryService(db, NewArtifactService(db), nil)
	turn := ArtifactTurn{SessionID: "solo-session", TurnID: "human-1", SandboxID: 3}
	s.CurrentTurn = func(string, string) (ArtifactTurn, error) { return turn, nil }
	s.OpenSource = func(context.Context, string, string, string) (io.ReadCloser, string, error) {
		return io.NopCloser(strings.NewReader("%PDF-1.7\nreport")), "solo-session", nil
	}
	ctx := context.Background()
	a, err := s.SendFile(ctx, "solo-run", "solo", "report.pdf", "", "Result")
	if err != nil {
		t.Fatal(err)
	}
	duplicate, err := s.SendFile(ctx, "solo-run", "solo", "report.pdf", "", "Result")
	if err != nil || a != duplicate {
		t.Fatalf("duplicate=%+v err=%v", duplicate, err)
	}
	var count int64
	db.Model(&models.ArtifactDelivery{}).Count(&count)
	if count != 1 {
		t.Fatalf("delivery count=%d", count)
	}
	var message models.SoloMessage
	if err = db.First(&message, "id = ?", "reply:human-1").Error; err != nil {
		t.Fatal(err)
	}
	if len(message.Files) != 1 || message.Files[0] != a || message.TurnID != "human-1" {
		t.Fatalf("durable reply=%+v", message)
	}
	encoded, _ := json.Marshal(message)
	if bytes.Contains(encoded, []byte(filestore.Prefix)) || bytes.Contains(encoded, []byte("%PDF")) {
		t.Fatalf("message exposes storage or bytes: %s", encoded)
	}
	s.OpenSource = func(context.Context, string, string, string) (io.ReadCloser, string, error) {
		turn.TurnID = "human-2"
		return io.NopCloser(strings.NewReader("%PDF-1.7\nnew report")), "solo-session", nil
	}
	if _, err = s.SendFile(ctx, "solo-run", "solo", "new.pdf", "", "late"); err == nil {
		t.Fatal("late import attached to another turn")
	}
	db.Model(&models.ArtifactDelivery{}).Count(&count)
	if count != 1 {
		t.Fatalf("late delivery count=%d", count)
	}
	s.CurrentTurn = func(string, string) (ArtifactTurn, error) { return ArtifactTurn{}, errors.New("turn ended") }
	if _, err = s.SendExisting(ctx, "solo-run", "solo", a.ArtifactID, 1, ""); err == nil {
		t.Fatal("sent without active turn")
	}
}
