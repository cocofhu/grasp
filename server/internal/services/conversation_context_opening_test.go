package services

import (
	"fmt"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/cocofhu/grasp/internal/models"
)

func TestContextPreamblePreservesOpeningAndRecentAnchorsBeyond64Users(t *testing.T) {
	_, db, sb := conversationHistoryFixture(t)
	at := time.Now().UTC()
	for i := range 81 {
		row := models.SoloMessage{ID: fmt.Sprintf("opening-%02d", i), SessionID: sb.ThreadID, Role: "user", Content: fmt.Sprintf("requirement %02d", i), CreatedAt: at}
		if i == 0 {
			row.Content = "任务目标：实现账单核对与导出，保留原始审计依据。"
		}
		if i == 79 {
			row.Content = "最新更正：取消外部支付，只保留只读导出。"
		}
		if i == 80 {
			row.Content = "current explicit input must not enter the preamble"
		}
		if err := db.Create(&row).Error; err != nil {
			t.Fatal(err)
		}
	}
	pre, err := (&SoloService{db: db}).contextPreambleChecked(sb.ThreadID, "opening-80", "context\n")
	if err != nil || !strings.Contains(pre, "任务目标：实现账单核对与导出") || !strings.Contains(pre, "最新更正：取消外部支付") || strings.Contains(pre, "current explicit input") {
		t.Fatalf("opening/latest/current boundary lost: %v", err)
	}
	if strings.Count(pre, "[用户要求 · ") != 64 || !strings.Contains(pre, "省略了中间 16 条") || !strings.Contains(pre, "opening-00 至 opening-03") || !strings.Contains(pre, "opening-20 至 opening-79") || !strings.Contains(pre, "get_history") {
		t.Fatalf("anchor cap, gap or precise retained ranges incorrect: %s", pre)
	}
	last := -1
	for _, i := range append([]int{0, 1, 2, 3}, contextOpeningRange(20, 80)...) {
		index := strings.Index(pre, fmt.Sprintf("[用户要求 · opening-%02d]", i))
		if index <= last {
			t.Fatal("opening/recent anchor order was not chronological")
		}
		last = index
	}
}

func contextOpeningRange(from, through int) []int {
	var result []int
	for i := from; i < through; i++ {
		result = append(result, i)
	}
	return result
}

func TestContextPreambleOpeningHasIndependentBudgetBesideLongRecentCorrections(t *testing.T) {
	_, db, sb := conversationHistoryFixture(t)
	at := time.Now().UTC()
	for i := range 80 {
		row := models.SoloMessage{ID: fmt.Sprintf("budget-opening-%02d", i), SessionID: sb.ThreadID, Role: "user", Content: strings.Repeat("中文更新🙂", 1000), CreatedAt: at.Add(time.Duration(i) * time.Second)}
		if i == 0 {
			row.Content = "最初任务目的：只做账单审计与数据导出。" + strings.Repeat("早期背景。", 2000)
		}
		if i == 79 {
			row.Content = "最新决定：禁止外部写入与支付，导出增加订单号。" + strings.Repeat("较新的更正。", 2000)
		}
		if err := db.Create(&row).Error; err != nil {
			t.Fatal(err)
		}
	}
	pre, err := (&SoloService{db: db}).contextPreambleChecked(sb.ThreadID, "", "context\n")
	if err != nil || len(pre) > 32000 || !utf8.ValidString(pre) || !strings.Contains(pre, "最初任务目的：只做账单审计与数据导出。") || !strings.Contains(pre, "最新决定：禁止外部写入与支付") || !strings.Contains(pre, "get_history") || !strings.Contains(pre, "摘录") {
		t.Fatalf("opening or latest correction evicted by byte budget: bytes=%d error=%v", len(pre), err)
	}
}

func TestContextPreambleOverlappingOpeningRecentAnchorsAreUnique(t *testing.T) {
	_, db, sb := conversationHistoryFixture(t)
	at := time.Now().UTC()
	for i := range 63 {
		row := models.SoloMessage{ID: fmt.Sprintf("overlap-%02d", i), SessionID: sb.ThreadID, Role: "user", Content: "short", CreatedAt: at}
		if err := db.Create(&row).Error; err != nil {
			t.Fatal(err)
		}
	}
	pre, err := (&SoloService{db: db}).contextPreambleChecked(sb.ThreadID, "", "context\n")
	if err != nil || strings.Count(pre, "[用户要求 · ") != 63 || strings.Contains(pre, "省略了中间") {
		t.Fatalf("overlap duplicated or lost anchors: %v", err)
	}
}

func TestContextPreambleSingleUserKeepsLargerRequirementAndAnnotation(t *testing.T) {
	_, db, sb := conversationHistoryFixture(t)
	row := models.SoloMessage{ID: "single-opening", SessionID: sb.ThreadID, Role: "user", Content: strings.Repeat("x", 4500) + "最新完整决定必须保留", Annotations: []models.ReactAnnotation{{Selector: "#original-selection", OuterHTML: "<h1>唯一圈选</h1>"}}, CreatedAt: time.Now().Add(-time.Minute).UTC()}
	if err := db.Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	for i := range 64 {
		if err := db.Create(&models.SoloMessage{ID: fmt.Sprintf("later-output-%02d", i), SessionID: sb.ThreadID, Role: "assistant", Content: "later output"}).Error; err != nil {
			t.Fatal(err)
		}
	}
	pre, err := (&SoloService{db: db}).contextPreambleChecked(sb.ThreadID, "", "context\n")
	if err != nil || !strings.Contains(pre, "最新完整决定必须保留") || !strings.Contains(pre, "#original-selection") || !strings.Contains(pre, "唯一圈选") {
		t.Fatalf("single user was needlessly restricted to opening-only budget: %v", err)
	}
}
