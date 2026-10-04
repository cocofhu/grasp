package services

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/models"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func setupRequirementDraftDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+strings.ReplaceAll(t.Name(), "/", "_")+"?mode=memory&cache=shared"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.AutoMigrate(&models.Project{}, &models.RequirementDraft{}, &models.ProjectCredential{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	now := time.Now()
	if err := db.Create(&models.Project{ID: "proj-1", Name: "P1", CreatedAt: now, UpdatedAt: now}).Error; err != nil {
		t.Fatalf("seed project: %v", err)
	}
	return db
}

func TestRequirementDraftCRUD(t *testing.T) {
	db := setupRequirementDraftDB(t)
	svc := NewRequirementDraftService(db)

	created, err := svc.Create("proj-1", RequirementDraftCreateInput{})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if created.Title != DefaultRequirementDraftTitle || created.Status != models.RequirementDraftStatusOpen || created.BodyMarkdown != "" {
		t.Fatalf("unexpected create: %+v", created)
	}
	if created.Kind != models.RequirementDraftKindRequirement || created.StartAt != "" || created.DueAt != "" || created.Progress != 0 {
		t.Fatalf("unexpected schedule defaults: %+v", created)
	}

	saved, err := svc.UpdateContent("proj-1", created.ID, "  支付失败重试  ", "## 要点\n- a")
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if saved.Title != "支付失败重试" || saved.BodyMarkdown != "## 要点\n- a" {
		t.Fatalf("unexpected save: %+v", saved)
	}

	if _, err := svc.UpdateContent("proj-1", created.ID, "   ", ""); !errors.Is(err, ErrRequirementDraftEmptyTitle) {
		t.Fatalf("empty title err=%v", err)
	}

	done, err := svc.UpdateStatus("proj-1", created.ID, models.RequirementDraftStatusDone)
	if err != nil || done.Status != models.RequirementDraftStatusDone {
		t.Fatalf("status: %+v err=%v", done, err)
	}
	if done.Progress != 0 {
		t.Fatalf("status must not change progress: %+v", done)
	}

	openList, err := svc.List("proj-1", RequirementDraftListFilter{Status: "open"})
	if err != nil || len(openList) != 0 {
		t.Fatalf("open list=%v err=%v", openList, err)
	}
	all, err := svc.List("proj-1", RequirementDraftListFilter{Status: "all", Query: "支付"})
	if err != nil || len(all) != 1 {
		t.Fatalf("search=%v err=%v", all, err)
	}

	if err := svc.Delete("proj-1", created.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := svc.Get("proj-1", created.ID); !errors.Is(err, ErrRequirementDraftNotFound) {
		t.Fatalf("get after delete: %v", err)
	}
}

func TestRequirementDraftProjectScopeAndCascade(t *testing.T) {
	db := setupRequirementDraftDB(t)
	svc := NewRequirementDraftService(db)
	projSvc := NewProjectService(db)

	d1, err := svc.Create("proj-1", RequirementDraftCreateInput{})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := svc.Get("missing", d1.ID); !errors.Is(err, ErrProjectNotFound) {
		t.Fatalf("cross/missing project: %v", err)
	}
	if _, err := svc.Create("missing", RequirementDraftCreateInput{}); !errors.Is(err, ErrProjectNotFound) {
		t.Fatalf("create missing project: %v", err)
	}

	if err := projSvc.Delete("proj-1"); err != nil {
		t.Fatalf("delete project: %v", err)
	}
	var n int64
	db.Model(&models.RequirementDraft{}).Where("project_id = ?", "proj-1").Count(&n)
	if n != 0 {
		t.Fatalf("cascade left %d drafts", n)
	}
}

func TestRequirementDraftCreateMilestoneRequiresDue(t *testing.T) {
	db := setupRequirementDraftDB(t)
	svc := NewRequirementDraftService(db)

	if _, err := svc.Create("proj-1", RequirementDraftCreateInput{Kind: models.RequirementDraftKindMilestone}); !errors.Is(err, ErrRequirementDraftMilestoneDueRequired) {
		t.Fatalf("missing due: %v", err)
	}
	ms, err := svc.Create("proj-1", RequirementDraftCreateInput{
		Kind:  models.RequirementDraftKindMilestone,
		DueAt: "2026-08-20",
		Title: "发布日",
	})
	if err != nil {
		t.Fatalf("create milestone: %v", err)
	}
	if ms.Kind != models.RequirementDraftKindMilestone || ms.DueAt != "2026-08-20" || ms.StartAt != "" {
		t.Fatalf("unexpected milestone: %+v", ms)
	}
}

func TestRequirementDraftUpdateScheduleValidation(t *testing.T) {
	db := setupRequirementDraftDB(t)
	svc := NewRequirementDraftService(db)

	req, err := svc.Create("proj-1", RequirementDraftCreateInput{})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	start := "2026-08-20"
	due := "2026-08-10"
	if _, err := svc.UpdateSchedule("proj-1", req.ID, RequirementDraftScheduleInput{StartAt: &start, DueAt: &due}); !errors.Is(err, ErrRequirementDraftDueBeforeStart) {
		t.Fatalf("due before start: %v", err)
	}

	okDue := "2026-08-25"
	patched, err := svc.UpdateSchedule("proj-1", req.ID, RequirementDraftScheduleInput{StartAt: &start, DueAt: &okDue})
	if err != nil {
		t.Fatalf("schedule: %v", err)
	}
	if patched.StartAt != start || patched.DueAt != okDue {
		t.Fatalf("unexpected schedule: %+v", patched)
	}
	// Title/body untouched
	if patched.Title != DefaultRequirementDraftTitle || patched.BodyMarkdown != "" {
		t.Fatalf("schedule must not touch content: %+v", patched)
	}

	prog := 100
	p2, err := svc.UpdateSchedule("proj-1", req.ID, RequirementDraftScheduleInput{Progress: &prog})
	if err != nil || p2.Progress != 100 || p2.Status != models.RequirementDraftStatusOpen {
		t.Fatalf("progress must not change status: %+v err=%v", p2, err)
	}

	bad := "2026-13-99"
	if _, err := svc.UpdateSchedule("proj-1", req.ID, RequirementDraftScheduleInput{DueAt: &bad}); !errors.Is(err, ErrRequirementDraftInvalidDate) {
		t.Fatalf("invalid date: %v", err)
	}

	// Clear milestone due is rejected
	ms, err := svc.Create("proj-1", RequirementDraftCreateInput{
		Kind:  models.RequirementDraftKindMilestone,
		DueAt: "2026-08-30",
	})
	if err != nil {
		t.Fatalf("ms: %v", err)
	}
	empty := ""
	if _, err := svc.UpdateSchedule("proj-1", ms.ID, RequirementDraftScheduleInput{DueAt: &empty}); !errors.Is(err, ErrRequirementDraftMilestoneDueRequired) {
		t.Fatalf("clear milestone due: %v", err)
	}
}

func TestRequirementDraftParentAndDeletePromote(t *testing.T) {
	db := setupRequirementDraftDB(t)
	svc := NewRequirementDraftService(db)

	parent, err := svc.Create("proj-1", RequirementDraftCreateInput{Title: "模块"})
	if err != nil {
		t.Fatalf("parent: %v", err)
	}
	child, err := svc.Create("proj-1", RequirementDraftCreateInput{Title: "子需求", ParentID: &parent.ID})
	if err != nil {
		t.Fatalf("child: %v", err)
	}
	if child.ParentID == nil || *child.ParentID != parent.ID {
		t.Fatalf("parent not set: %+v", child)
	}

	// milestone cannot be parent
	ms, err := svc.Create("proj-1", RequirementDraftCreateInput{
		Kind:  models.RequirementDraftKindMilestone,
		DueAt: "2026-09-01",
		Title: "MS",
	})
	if err != nil {
		t.Fatalf("ms: %v", err)
	}
	if _, err := svc.UpdateSchedule("proj-1", child.ID, RequirementDraftScheduleInput{
		ParentID: ptrToPtr(&ms.ID),
	}); !errors.Is(err, ErrRequirementDraftInvalidParent) {
		t.Fatalf("milestone parent: %v", err)
	}

	// parent with children cannot convert to milestone
	if _, err := svc.UpdateSchedule("proj-1", parent.ID, RequirementDraftScheduleInput{
		Kind:  strPtr(models.RequirementDraftKindMilestone),
		DueAt: strPtr("2026-09-02"),
	}); !errors.Is(err, ErrRequirementDraftHasChildren) {
		t.Fatalf("has children convert: %v", err)
	}

	// parent with children cannot hang under another
	other, err := svc.Create("proj-1", RequirementDraftCreateInput{Title: "其他"})
	if err != nil {
		t.Fatalf("other: %v", err)
	}
	if _, err := svc.UpdateSchedule("proj-1", parent.ID, RequirementDraftScheduleInput{
		ParentID: ptrToPtr(&other.ID),
	}); !errors.Is(err, ErrRequirementDraftHasChildren) {
		t.Fatalf("nested parent: %v", err)
	}

	// delete parent promotes children
	if err := svc.Delete("proj-1", parent.ID); err != nil {
		t.Fatalf("delete parent: %v", err)
	}
	got, err := svc.Get("proj-1", child.ID)
	if err != nil {
		t.Fatalf("get child: %v", err)
	}
	if got.ParentID != nil {
		t.Fatalf("child not promoted: %+v", got)
	}
	if _, err := svc.Get("proj-1", parent.ID); !errors.Is(err, ErrRequirementDraftNotFound) {
		t.Fatalf("parent still exists: %v", err)
	}
}

func TestRequirementDraftKindConversion(t *testing.T) {
	db := setupRequirementDraftDB(t)
	svc := NewRequirementDraftService(db)

	req, err := svc.Create("proj-1", RequirementDraftCreateInput{
		StartAt: "2026-08-01",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	ms, err := svc.UpdateSchedule("proj-1", req.ID, RequirementDraftScheduleInput{
		Kind: strPtr(models.RequirementDraftKindMilestone),
	})
	if err != nil {
		t.Fatalf("to milestone: %v", err)
	}
	if ms.Kind != models.RequirementDraftKindMilestone || ms.DueAt != "2026-08-01" || ms.StartAt != "" {
		t.Fatalf("mapped: %+v", ms)
	}

	back, err := svc.UpdateSchedule("proj-1", ms.ID, RequirementDraftScheduleInput{
		Kind: strPtr(models.RequirementDraftKindRequirement),
	})
	if err != nil {
		t.Fatalf("to requirement: %v", err)
	}
	if back.Kind != models.RequirementDraftKindRequirement || back.DueAt != "2026-08-01" || back.StartAt != "" {
		t.Fatalf("back: %+v", back)
	}

	empty, err := svc.Create("proj-1", RequirementDraftCreateInput{})
	if err != nil {
		t.Fatalf("empty: %v", err)
	}
	if _, err := svc.UpdateSchedule("proj-1", empty.ID, RequirementDraftScheduleInput{
		Kind: strPtr(models.RequirementDraftKindMilestone),
	}); !errors.Is(err, ErrRequirementDraftKindNeedsDate) {
		t.Fatalf("needs date: %v", err)
	}
}

func TestRequirementDraftScheduleDoesNotClobberContent(t *testing.T) {
	db := setupRequirementDraftDB(t)
	svc := NewRequirementDraftService(db)
	row, err := svc.Create("proj-1", RequirementDraftCreateInput{})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	saved, err := svc.UpdateContent("proj-1", row.ID, "标题A", "正文未保存到别处也会保留")
	if err != nil {
		t.Fatalf("content: %v", err)
	}
	due := "2026-08-15"
	patched, err := svc.UpdateSchedule("proj-1", saved.ID, RequirementDraftScheduleInput{DueAt: &due})
	if err != nil {
		t.Fatalf("schedule: %v", err)
	}
	if patched.Title != "标题A" || patched.BodyMarkdown != "正文未保存到别处也会保留" {
		t.Fatalf("content clobbered: %+v", patched)
	}
}

func strPtr(s string) *string { return &s }

func ptrToPtr(s *string) **string { return &s }
