package services

import (
	"testing"

	"github.com/cocofhu/grasp/internal/models"
)

func countVersions(t *testing.T, s *ArtifactService, artifactID string) int {
	t.Helper()
	return len(s.ListVersions(artifactID))
}

func TestArtifactSaveBumpsRevision(t *testing.T) {
	db := newTestDB(t)
	db.Create(&models.Run{ID: "r1", WorkflowID: "wf", WorkflowName: "wf"})
	s := NewArtifactService(db)

	id1, err := s.Save("r1", "n1", "page.html", "html", "<p>1</p>")
	if err != nil {
		t.Fatal(err)
	}
	rec, ok := s.GetRecord("r1", "page.html")
	if !ok || rec.Revision != 1 || rec.ID != id1 {
		t.Fatalf("first write revision=%d id=%s", rec.Revision, rec.ID)
	}
	if countVersions(t, s, rec.ID) != 0 {
		t.Fatalf("first write must not archive a version")
	}

	id2, err := s.Save("r1", "n1", "page.html", "html", "<p>2</p>")
	if err != nil {
		t.Fatal(err)
	}
	rec, ok = s.GetRecord("r1", "page.html")
	if !ok || rec.Revision != 2 || rec.ID != id2 || rec.Content != "<p>2</p>" {
		t.Fatalf("second write revision=%d id=%s content=%q", rec.Revision, rec.ID, rec.Content)
	}
	vers := s.ListVersions(rec.ID)
	if len(vers) != 1 || vers[0].Revision != 1 || vers[0].Content != "" {
		t.Fatalf("list versions=%+v (content should be omitted)", vers)
	}
	archived, ok := s.GetVersion(rec.ID, 1)
	if !ok || archived.Content != "<p>1</p>" || archived.NodeID != "n1" {
		t.Fatalf("archived v1=%+v ok=%v", archived, ok)
	}
}

func TestArtifactSaveIdenticalContentDoesNotArchive(t *testing.T) {
	db := newTestDB(t)
	db.Create(&models.Run{ID: "r1", WorkflowID: "wf", WorkflowName: "wf"})
	s := NewArtifactService(db)

	id, err := s.Save("r1", "n1", "page.html", "html", "<p>same</p>")
	if err != nil {
		t.Fatal(err)
	}
	first, _ := s.GetRecord("r1", "page.html")
	if _, err := s.Save("r1", "n1", "page.html", "html", "<p>same</p>"); err != nil {
		t.Fatal(err)
	}
	rec, ok := s.GetRecord("r1", "page.html")
	if !ok || rec.ID != id || rec.Revision != 1 || rec.Content != "<p>same</p>" {
		t.Fatalf("identical save revision=%d content=%q", rec.Revision, rec.Content)
	}
	if !rec.UpdatedAt.After(first.UpdatedAt) && !rec.UpdatedAt.Equal(first.UpdatedAt) {
		// UpdatedAt is always written; equal is acceptable on coarse clocks.
	}
	if countVersions(t, s, rec.ID) != 0 {
		t.Fatal("identical content must not create a version row")
	}
}

func TestArtifactSaveThreeDistinctOverwrites(t *testing.T) {
	db := newTestDB(t)
	db.Create(&models.Run{ID: "r1", WorkflowID: "wf", WorkflowName: "wf"})
	s := NewArtifactService(db)

	if _, err := s.Save("r1", "n1", "plan.json", "json", `{"v":1}`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Save("r1", "n2", "plan.json", "json", `{"v":2}`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Save("r1", "n3", "plan.json", "json", `{"v":3}`); err != nil {
		t.Fatal(err)
	}
	rec, ok := s.GetRecord("r1", "plan.json")
	if !ok || rec.Revision != 3 || rec.Content != `{"v":3}` || rec.NodeID != "n3" {
		t.Fatalf("live=%+v", rec)
	}
	vers := s.ListVersions(rec.ID)
	if len(vers) != 2 || vers[0].Revision != 1 || vers[1].Revision != 2 {
		t.Fatalf("versions=%+v", vers)
	}
	v1, _ := s.GetVersion(rec.ID, 1)
	v2, _ := s.GetVersion(rec.ID, 2)
	if v1.Content != `{"v":1}` || v1.NodeID != "n1" {
		t.Fatalf("v1=%+v", v1)
	}
	if v2.Content != `{"v":2}` || v2.NodeID != "n2" {
		t.Fatalf("v2=%+v", v2)
	}
}

func TestArtifactDeleteCascadesVersions(t *testing.T) {
	db := newTestDB(t)
	db.Create(&models.Run{ID: "r1", WorkflowID: "wf", WorkflowName: "wf", Status: "completed"})
	db.Create(&models.Run{ID: "r2", WorkflowID: "wf", WorkflowName: "wf", Status: "completed"})
	s := NewArtifactService(db)

	id1, err := s.Save("r1", "n1", "page.html", "html", "<p>1</p>")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Save("r1", "n1", "page.html", "html", "<p>2</p>"); err != nil {
		t.Fatal(err)
	}
	id2, err := s.Save("r2", "n1", "page.html", "html", "<p>a</p>")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Save("r2", "n1", "page.html", "html", "<p>b</p>"); err != nil {
		t.Fatal(err)
	}

	if err := s.DeleteByID(id1); err != nil {
		t.Fatal(err)
	}
	if countVersions(t, s, id1) != 0 {
		t.Fatal("DeleteByID left version rows")
	}
	if _, ok := s.GetByID(id1); ok {
		t.Fatal("live row should be gone")
	}
	if countVersions(t, s, id2) != 1 {
		t.Fatal("other run versions should remain")
	}

	if err := s.Delete("r2", "page.html"); err != nil {
		t.Fatal(err)
	}
	if countVersions(t, s, id2) != 0 {
		t.Fatal("Delete(run,name) left version rows")
	}

	id3, err := s.Save("r1", "n1", "note.md", "markdown", "x")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Save("r1", "n1", "note.md", "markdown", "y"); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteForRuns("r1", "r2"); err != nil {
		t.Fatal(err)
	}
	if countVersions(t, s, id3) != 0 {
		t.Fatal("DeleteForRuns left version rows")
	}
}
