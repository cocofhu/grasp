package services

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/models"
)

func TestListMessagesWindowTailAndBefore(t *testing.T) {
	db := setupPmDB(t)
	pm := NewPmService(db, nil)
	ps := NewProjectService(db)
	p, err := ps.Create("WinProj", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	th, err := pm.CreateThread(p.ID, "alice", "tail", "agent-a", "user")
	if err != nil {
		t.Fatal(err)
	}

	ids := make([]string, 0, 25)
	base := time.Now().Add(-30 * time.Minute)
	for i := 0; i < 25; i++ {
		msg := models.ChatMessage{
			ID:        fmt.Sprintf("msg-%02d", i),
			ThreadID:  th.ID,
			Role:      "user",
			Content:   fmt.Sprintf("m%d", i),
			Status:    "ok",
			CreatedAt: base.Add(time.Duration(i) * time.Second),
		}
		if err := db.Create(&msg).Error; err != nil {
			t.Fatal(err)
		}
		ids = append(ids, msg.ID)
	}

	// Full list still works.
	all, err := pm.ListMessages(th.ID)
	if err != nil || len(all) != 25 {
		t.Fatalf("full list len=%d err=%v", len(all), err)
	}

	// Tail window (no before): newest 20, oldest→newest, hasMore=true.
	page, hasMore, err := pm.ListMessagesWindow(th.ID, 20, "")
	if err != nil {
		t.Fatal(err)
	}
	if !hasMore {
		t.Fatal("expected hasMore for 25 messages with limit 20")
	}
	if len(page) != 20 {
		t.Fatalf("tail len=%d", len(page))
	}
	if page[0].ID != ids[5] || page[len(page)-1].ID != ids[24] {
		t.Fatalf("tail range got %s..%s want %s..%s", page[0].ID, page[len(page)-1].ID, ids[5], ids[24])
	}
	for i := 1; i < len(page); i++ {
		if !page[i].CreatedAt.After(page[i-1].CreatedAt) && page[i].CreatedAt.Equal(page[i-1].CreatedAt) == false {
			t.Fatalf("not oldest→newest at %d", i)
		}
		if page[i].CreatedAt.Before(page[i-1].CreatedAt) {
			t.Fatalf("order broken at %d: %v < %v", i, page[i].CreatedAt, page[i-1].CreatedAt)
		}
	}

	// before=earliest loaded → older page of 5, hasMore=false.
	older, hasMore, err := pm.ListMessagesWindow(th.ID, 20, page[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if hasMore {
		t.Fatal("expected no more after first older page")
	}
	if len(older) != 5 {
		t.Fatalf("older len=%d want 5", len(older))
	}
	if older[0].ID != ids[0] || older[4].ID != ids[4] {
		t.Fatalf("older range %s..%s", older[0].ID, older[4].ID)
	}

	// Exactly 20: hasMore=false.
	th2, err := pm.CreateThread(p.ID, "alice", "exact", "agent-a", "user")
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 20; i++ {
		if _, err := pm.AppendMessage(th2.ID, "user", fmt.Sprintf("e%d", i), nil, nil, nil); err != nil {
			t.Fatal(err)
		}
	}
	exact, hasMore, err := pm.ListMessagesWindow(th2.ID, 20, "")
	if err != nil || hasMore || len(exact) != 20 {
		t.Fatalf("exact: len=%d hasMore=%v err=%v", len(exact), hasMore, err)
	}

	// Empty thread.
	th3, err := pm.CreateThread(p.ID, "alice", "empty", "agent-a", "user")
	if err != nil {
		t.Fatal(err)
	}
	empty, hasMore, err := pm.ListMessagesWindow(th3.ID, 20, "")
	if err != nil || hasMore || len(empty) != 0 {
		t.Fatalf("empty: len=%d hasMore=%v err=%v", len(empty), hasMore, err)
	}

	// Unknown before.
	if _, _, err := pm.ListMessagesWindow(th.ID, 20, "missing-msg"); !errors.Is(err, ErrPmMessageNotFound) {
		t.Fatalf("want ErrPmMessageNotFound, got %v", err)
	}

	// Default/clamp limit.
	short, hasMore, err := pm.ListMessagesWindow(th.ID, 0, "")
	if err != nil || !hasMore || len(short) != 20 {
		t.Fatalf("default limit: len=%d hasMore=%v err=%v", len(short), hasMore, err)
	}
}
