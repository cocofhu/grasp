package services

import "testing"

func TestArtifactListsHideHistoricalFeedbackNames(t *testing.T) {
	db := newTestDB(t)
	svc := NewArtifactService(db)
	runID := "run-vis"
	for _, name := range []string{"research.json", "feedback_index.json", "feedback.clarify.c1.i1.json", "plan.json"} {
		if _, err := svc.Save(runID, "n1", name, "json", `{"name":"`+name+`"}`); err != nil {
			t.Fatal(err)
		}
	}

	listed := map[string]bool{}
	for _, a := range svc.List(runID) {
		listed[a.Name] = true
	}
	if !listed["research.json"] || !listed["plan.json"] {
		t.Fatalf("formal products missing: %+v", listed)
	}
	if listed["feedback_index.json"] || listed["feedback.clarify.c1.i1.json"] {
		t.Fatalf("list exposed feedback files: %+v", listed)
	}
	if _, ok := svc.Get(runID, "feedback_index.json"); ok {
		t.Fatal("get by feedback name must miss")
	}
	if _, ok := svc.Get(runID, "feedback.clarify.c1.i1.json"); ok {
		t.Fatal("get by feedback round name must miss")
	}
	if body, ok := svc.Get(runID, "research.json"); !ok || body == "" {
		t.Fatal("formal product must still be readable")
	}

	page, total := svc.AllPage(ArtifactFilter{Q: "feedback"}, 1, 20)
	if total != 0 || len(page) != 0 {
		t.Fatalf("search must not find feedback files: total=%d page=%d", total, len(page))
	}
	page, total = svc.AllPage(ArtifactFilter{Q: "research"}, 1, 20)
	if total != 1 || len(page) != 1 || page[0].Name != "research.json" {
		t.Fatalf("formal search: total=%d page=%+v", total, page)
	}
	byRun := svc.ByRun(runID)
	if len(byRun) != 2 {
		t.Fatalf("ByRun len=%d, want 2 formal products", len(byRun))
	}
}
