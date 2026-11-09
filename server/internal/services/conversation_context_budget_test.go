package services

import (
	"fmt"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/cocofhu/grasp/internal/models"
)

func TestContextPreambleLoadsShortHistoryBeyondFirstPage(t *testing.T) {
	_, db, sb := conversationHistoryFixture(t)
	at := time.Now().UTC()
	for i := range 17 {
		row := models.SoloMessage{ID: fmt.Sprintf("short-%02d", i), SessionID: sb.ThreadID, Role: "assistant", Content: fmt.Sprintf("independent completed result %02d", i), CreatedAt: at.Add(time.Duration(i) * time.Second)}
		if i == 16 {
			row.Role, row.Content = "user", "current message excluded"
		}
		if err := db.Create(&row).Error; err != nil {
			t.Fatal(err)
		}
	}
	solo := &SoloService{db: db}
	pre, err := solo.contextPreambleChecked(sb.ThreadID, "short-16", "context\n")
	if err != nil {
		t.Fatal(err)
	}
	last := -1
	for i := range 16 {
		text := fmt.Sprintf("independent completed result %02d", i)
		index := strings.Index(pre, text)
		if index <= last || strings.Count(pre, text) != 1 {
			t.Fatalf("history lost or reordered %q", text)
		}
		last = index
	}
	if len(pre) > 32000 || strings.Contains(pre, "current message excluded") || strings.Contains(pre, "条对话正文未逐字附上") {
		t.Fatal("short bounded history was omitted, unbounded or repeated current input")
	}
}

func TestContextPreambleRowCapHasExactRangeAndDoesNotCrossSession(t *testing.T) {
	_, db, sb := conversationHistoryFixture(t)
	at := time.Now().UTC()
	for i := range 75 {
		row := models.SoloMessage{ID: fmt.Sprintf("range-%02d", i), SessionID: sb.ThreadID, Role: "assistant", Content: "short", CreatedAt: at.Add(time.Duration(i) * time.Second)}
		if err := db.Create(&row).Error; err != nil {
			t.Fatal(err)
		}
		row.ID, row.SessionID, row.Content = fmt.Sprintf("foreign-%02d", i), "another-session", "foreign conversation must never enter"
		if err := db.Create(&row).Error; err != nil {
			t.Fatal(err)
		}
	}
	pre, err := (&SoloService{db: db}).contextPreambleChecked(sb.ThreadID, "", "context\n")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(pre, "（历史消息 ID：") != 64 || !strings.Contains(pre, "较早的 11 条对话正文") || !strings.Contains(pre, "range-11 至 range-74") || strings.Contains(pre, "foreign conversation") {
		t.Fatalf("row cap/range/session boundary incorrect: %s", pre)
	}
}

func TestContextPreambleByteBudgetAndUserLedgerAreBounded(t *testing.T) {
	_, db, sb := conversationHistoryFixture(t)
	at := time.Now().UTC()
	for i := range 80 {
		row := models.SoloMessage{ID: fmt.Sprintf("large-%02d", i), SessionID: sb.ThreadID, Role: "user", Content: strings.Repeat("必须保留的约束🙂", 1000), CreatedAt: at.Add(time.Duration(i) * time.Second)}
		if err := db.Create(&row).Error; err != nil {
			t.Fatal(err)
		}
	}
	pre, err := (&SoloService{db: db}).contextPreambleChecked(sb.ThreadID, "large-79", "context\n")
	if err != nil {
		t.Fatal(err)
	}
	if len(pre) > 32000 || !utf8.ValidString(pre) || !strings.Contains(pre, "摘录") || !strings.Contains(pre, "get_history") || !strings.Contains(pre, "较早的 15 条用户要求") || strings.Contains(pre, "large-79") {
		t.Fatalf("budget/disclosure/current-message boundary incorrect: bytes=%d valid=%v", len(pre), utf8.ValidString(pre))
	}
}

func TestContextPreambleByteLedgerRetainsNewestMultibyteCorrection(t *testing.T) {
	_, db, sb := conversationHistoryFixture(t)
	at := time.Now().UTC()
	for i := range 68 {
		row := models.SoloMessage{ID: fmt.Sprintf("decision-%02d", i), SessionID: sb.ThreadID, Role: "assistant", Content: "later progress", CreatedAt: at.Add(time.Duration(i) * time.Second)}
		if i < 4 {
			row.Role, row.Content = "user", strings.Repeat("中", 1850)
			if i == 3 {
				row.Content = "最新更正：去掉支付功能，保留导出。" + row.Content
			}
		}
		if err := db.Create(&row).Error; err != nil {
			t.Fatal(err)
		}
	}
	pre, err := (&SoloService{db: db}).contextPreambleChecked(sb.ThreadID, "", "context\n")
	if err != nil || len(pre) > 32000 || !utf8.ValidString(pre) || !strings.Contains(pre, "最新更正：去掉支付功能，保留导出。") || !strings.Contains(pre, "get_history") {
		t.Fatalf("latest multibyte correction discarded: bytes=%d error=%v", len(pre), err)
	}
}

func TestContextPreambleRetainsAnnotatedRequirementOutsideRecentBodies(t *testing.T) {
	_, db, sb := conversationHistoryFixture(t)
	at := time.Now().UTC()
	for i := range 65 {
		row := models.SoloMessage{ID: fmt.Sprintf("annotation-%02d", i), SessionID: sb.ThreadID, Role: "assistant", Content: "later progress", CreatedAt: at.Add(time.Duration(i) * time.Second)}
		if i == 0 {
			row.Role, row.Content = "user", "这个标题请改短"
			row.Annotations = []models.ReactAnnotation{{Selector: "#specific-heading", OuterHTML: "<h1>原圈选内容</h1>"}}
		}
		if err := db.Create(&row).Error; err != nil {
			t.Fatal(err)
		}
	}
	pre, err := (&SoloService{db: db}).contextPreambleChecked(sb.ThreadID, "", "context\n")
	if err != nil || !strings.Contains(pre, "#specific-heading") || !strings.Contains(pre, "原圈选内容") || !strings.Contains(pre, "这个标题请改短") {
		t.Fatalf("older annotated requirement disappeared: %v", err)
	}
}
