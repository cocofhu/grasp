package engine

import (
	"strings"
	"testing"

	"github.com/cocofhu/grasp/internal/models"
)

func TestStartRunSnapshotsBoundRepos(t *testing.T) {
	g := models.Graph{
		Nodes: []models.Node{{ID: "in", Type: "input"}, {ID: "out", Type: "output"}},
		Edges: []models.Edge{{ID: "e1", Source: "in", Target: "out"}},
	}
	eng, db := setupEngineGraph(t, g)
	for _, p := range []models.Project{
		{ID: "p-web", Name: "web", RepoURL: "https://github.com/team/web.git", DefaultBranch: "main", RepoDirName: "web"},
		{ID: "p-api", Name: "api", RepoURL: "https://github.com/team/api.git", RepoDirName: "api"},
		{ID: "p-other", Name: "other", RepoURL: "https://github.com/team/other.git"},
	} {
		if err := db.Create(&p).Error; err != nil {
			t.Fatal(err)
		}
	}
	defaults := &models.WorkflowDefaults{Repos: []models.RepoChoice{{ProjectID: "p-web"}, {ProjectID: "p-api", Branch: "develop"}}}
	if err := db.Model(&models.WorkflowDef{ID: "wf"}).Select("Defaults").Updates(&models.WorkflowDef{Defaults: defaults}).Error; err != nil {
		t.Fatal(err)
	}
	start := func(picked *[]models.RepoChoice) (*models.Run, error) {
		return eng.StartRunWithFirstMessage("wf", nil, "test", "normal", nil, nil, "", nil, LaunchChoice{Repos: picked})
	}
	stored := func(t *testing.T, id string) []models.SessionRepo {
		t.Helper()
		var run models.Run
		if err := db.First(&run, "id = ?", id).Error; err != nil {
			t.Fatal(err)
		}
		return run.Repos
	}

	all, err := start(nil)
	if err != nil {
		t.Fatal(err)
	}
	if repos := stored(t, all.ID); len(repos) != 2 || repos[0].RepoDirName != "web" || repos[0].Branch != "main" || repos[1].RepoDirName != "api" || repos[1].Branch != "develop" {
		t.Fatalf("all bound = %+v", repos)
	}

	subset, err := start(&[]models.RepoChoice{{ProjectID: "p-api", Branch: "feature/x"}})
	if err != nil {
		t.Fatal(err)
	}
	if repos := stored(t, subset.ID); len(repos) != 1 || repos[0].ProjectID != "p-api" || repos[0].Branch != "feature/x" {
		t.Fatalf("subset = %+v", repos)
	}

	none, err := start(&[]models.RepoChoice{})
	if err != nil {
		t.Fatal(err)
	}
	if repos := stored(t, none.ID); len(repos) != 0 {
		t.Fatalf("explicit none = %+v", repos)
	}

	var before int64
	db.Model(&models.Run{}).Count(&before)
	if _, err := start(&[]models.RepoChoice{{ProjectID: "p-other"}}); err == nil || !strings.Contains(err.Error(), "没有绑定") {
		t.Fatalf("unbound repository accepted: %v", err)
	}
	if err := db.Delete(&models.Project{}, "id = ?", "p-web").Error; err != nil {
		t.Fatal(err)
	}
	if _, err := start(nil); err == nil || !strings.Contains(err.Error(), "已删除") {
		t.Fatalf("deleted repository accepted: %v", err)
	}
	var after int64
	db.Model(&models.Run{}).Count(&after)
	if after != before {
		t.Fatal("a rejected repository choice created a run")
	}
	if repos := stored(t, all.ID); len(repos) != 2 {
		t.Fatal("a started run lost its snapshot")
	}
}
