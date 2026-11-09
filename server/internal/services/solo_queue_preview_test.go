package services

import (
	"strings"
	"testing"

	"github.com/cocofhu/grasp/internal/models"
)

func TestSoloQueuePreviewRemainsBoundedWithoutChangingAcceptedInput(t *testing.T) {
	turn := &soloTurn{ID: "accepted", ClientMessageID: "stable-client", Text: strings.Repeat("正文", 100000)}
	for range 32 {
		turn.Images = append(turn.Images, models.PromptImage{Data: strings.Repeat("image", 10000), Ref: "blob:" + strings.Repeat("a", 10000), MimeType: strings.Repeat("type", 1000), Name: strings.Repeat("name", 1000)})
		turn.Annotations = append(turn.Annotations, models.ReactAnnotation{Selector: strings.Repeat("selector", 1000), OuterHTML: strings.Repeat("html", 10000), URL: strings.Repeat("url", 1000)})
		turn.References = append(turn.References, models.MessageReference{ID: "server-reference", Kind: "github_issue", Target: strings.Repeat("target", 1000), Title: strings.Repeat("title", 1000)})
	}
	preview, err := soloQueueDisplay(turn)
	if err != nil || len(preview) > 65535 {
		t.Fatalf("preview exceeded SQL display bound: bytes=%d err=%v", len(preview), err)
	}
	item, err := decodeSoloMetadata(models.ChatQueueTurn{ID: turn.ID, ClientMessageID: turn.ClientMessageID, Display: string(preview)})
	if err != nil || item.ID != turn.ID || item.ClientMessageID != turn.ClientMessageID || len(item.Images) != 16 || len(item.References) != 16 {
		t.Fatalf("preview lost stable identity: %+v %v", item, err)
	}
	if len(turn.Images) != 32 || turn.Images[0].Data == "" || turn.Annotations[0].OuterHTML == "" || len(turn.Text) < 100000 {
		t.Fatal("display preparation modified the accepted execution payload")
	}
	for i := range turn.References {
		turn.References[i].Title = strings.Repeat("<>", 10000)
		turn.References[i].State = strings.Repeat("<>", 10000)
	}
	preview, err = soloQueueDisplay(turn)
	if err != nil || len(preview) > 65535 {
		t.Fatalf("escaped legacy metadata exceeded byte budget: bytes=%d err=%v", len(preview), err)
	}
}
