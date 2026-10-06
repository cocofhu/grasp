package services

import (
	"strings"
	"testing"

	"github.com/cocofhu/grasp/internal/blob"
	"github.com/cocofhu/grasp/internal/models"
)

func TestAppendMessageExternalizesImages(t *testing.T) {
	db := setupPmDB(t)
	pm := NewPmService(db, nil)
	pm.SetBlobStore(blob.NewMemory())
	ps := NewProjectService(db)
	p, err := ps.Create("BlobProj", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	th, err := pm.CreateThread(p.ID, "alice", "", "agent", "user")
	if err != nil {
		t.Fatal(err)
	}
	msg, err := pm.AppendMessage(th.ID, "user", "pic", nil, nil, []models.PromptImage{
		{Data: "aGVsbG8=", MimeType: "image/png", Name: "a.png"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(msg.Images) != 1 || !strings.HasPrefix(msg.Images[0].Ref, "blob:") || msg.Images[0].Data != "" {
		t.Fatalf("msg images = %+v", msg.Images)
	}
	var stored models.ChatMessage
	if err := db.First(&stored, "id = ?", msg.ID).Error; err != nil {
		t.Fatal(err)
	}
	if len(stored.Images) != 1 || stored.Images[0].Data != "" || stored.Images[0].Ref == "" {
		t.Fatalf("stored = %+v", stored.Images)
	}
}
