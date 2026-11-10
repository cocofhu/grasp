package services

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/models"
)

func TestSoloMessagePagesKeepStableOrderAndSeparateLegacyProcess(t *testing.T) {
	db := newTestDB(t)
	solo := NewSoloService(db, nil, nil, nil)
	if err := db.Create(&models.SoloSession{ID: "history", UserID: "alice", Status: models.SoloStatusClosed}).Error; err != nil {
		t.Fatal(err)
	}
	at := time.Now().UTC().Truncate(time.Second)
	for i := range 123 {
		message := models.SoloMessage{ID: fmt.Sprintf("m-%03d", i), SessionID: "history", Role: "user", Content: fmt.Sprintf("message %d", i), CreatedAt: at.Add(time.Duration(i/3) * time.Nanosecond)}
		if i == 122 {
			message.Role = "assistant"
			message.Events = make([]json.RawMessage, 6000)
			for k := range message.Events {
				message.Events[k] = json.RawMessage(`{"type":"thought"}`)
			}
		}
		if err := db.Create(&message).Error; err != nil {
			t.Fatal(err)
		}
	}
	ctx := context.Background()
	seen := map[string]bool{}
	before := ""
	for {
		page, err := solo.MessagePage(ctx, "history", "alice", before, 17)
		if err != nil {
			t.Fatal(err)
		}
		if len(page.Items) > 17 {
			t.Fatal("message page exceeded its requested size")
		}
		for i, item := range page.Items {
			if seen[item.ID] {
				t.Fatalf("duplicate message after concurrent insertion: %s", item.ID)
			}
			if len(item.Events) > 0 {
				t.Fatal("metadata page fetched a legacy process body")
			}
			if i > 0 && page.Items[i-1].ID >= item.ID {
				t.Fatal("equal-time messages lost their ID order")
			}
			if item.ID == "m-122" && !item.LegacyEvents {
				t.Fatal("legacy execution process disappeared")
			}
			seen[item.ID] = true
		}
		if before == "" {
			if err := db.Create(&models.SoloMessage{ID: "new", SessionID: "history", Role: "user", Content: "arrived during paging", CreatedAt: at.Add(time.Hour)}).Error; err != nil {
				t.Fatal(err)
			}
		}
		if !page.HasMore {
			break
		}
		before = page.Before
	}
	if len(seen) != 123 || seen["new"] {
		t.Fatalf("older windows changed under insertion: %d", len(seen))
	}
	latest, err := solo.MessagePage(ctx, "history", "alice", "", 17)
	if err != nil || latest.Items[len(latest.Items)-1].ID != "new" {
		t.Fatal("tail did not include the new message")
	}
	for _, size := range []int{0, 201} {
		if _, err := solo.MessagePage(ctx, "history", "alice", "", size); err == nil {
			t.Fatal("invalid page bound accepted")
		}
	}
	if _, err := solo.MessagePage(ctx, "history", "bob", "", 17); err == nil {
		t.Fatal("history ACL bypass")
	}
	if _, err := solo.MessagePage(ctx, "history", "alice", "broken", 17); err == nil {
		t.Fatal("invalid cursor accepted")
	}
	part, err := solo.MessageEvents(ctx, "history", "alice", "m-122", -1)
	if err != nil || len(part.Items) != 256 || !part.HasMore || !part.LegacyIncomplete {
		t.Fatalf("legacy process page lost its migration warning: %v", err)
	}
	if err := db.Create(&models.SoloMessage{ID: "foreign", SessionID: "other", Role: "assistant", Events: []json.RawMessage{json.RawMessage(`{}`)}}).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := solo.MessageEvents(ctx, "history", "alice", "foreign", -1); err == nil {
		t.Fatal("foreign message process was exposed")
	}
}
