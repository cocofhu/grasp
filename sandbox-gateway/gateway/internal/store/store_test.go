package store

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"sandbox-gateway/internal/config"
	"sandbox-gateway/internal/database"
	"sandbox-gateway/internal/models"
)

func testStore(t *testing.T) *Store {
	t.Helper()
	db, err := database.Open(config.DBConfig{
		Driver: "sqlite",
		Path:   filepath.Join(t.TempDir(), "store.db"),
	})
	if err != nil {
		t.Fatal(err)
	}
	return New(db)
}

func TestStoreCRUD(t *testing.T) {
	st := testStore(t)
	sb := &models.Sandbox{ID: "a1", Name: "n1", Status: models.StatusCreating, Image: "img"}
	sb.SetEnv(map[string]string{"K": "V"})
	sb.SetLabels(map[string]string{"l": "1"})
	sb.SetEndpoints(map[int]string{80: "h:80"})
	if err := st.Create(context.Background(), sb); err != nil {
		t.Fatal(err)
	}
	got, err := st.Get(context.Background(), "a1")
	if err != nil {
		t.Fatal(err)
	}
	if got.Env()["K"] != "V" || got.Labels()["l"] != "1" || got.Endpoints()[80] != "h:80" {
		t.Fatalf("%+v env=%v", got, got.Env())
	}

	got.Status = models.StatusRunning
	if err := st.Save(context.Background(), got); err != nil {
		t.Fatal(err)
	}
	got2, _ := st.Get(context.Background(), "a1")
	if got2.Status != models.StatusRunning {
		t.Fatal(got2.Status)
	}

	list, err := st.List(context.Background(), ListFilter{})
	if err != nil || len(list) != 1 {
		t.Fatalf("list: %v %d", err, len(list))
	}

	byName, err := st.GetByName(context.Background(), "n1")
	if err != nil || byName.ID != "a1" {
		t.Fatalf("GetByName: %v %+v", err, byName)
	}
	if _, err := st.GetByName(context.Background(), "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetByName missing: %v", err)
	}

	if err := st.Delete(context.Background(), "a1"); err != nil {
		t.Fatal(err)
	}
	_, err = st.Get(context.Background(), "a1")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}

func TestListLabelFilter(t *testing.T) {
	st := testStore(t)
	a := &models.Sandbox{ID: "a", Name: "na", Status: models.StatusRunning, Image: "img"}
	a.SetLabels(map[string]string{"owner": "team-a", "env": "prod"})
	b := &models.Sandbox{ID: "b", Name: "nb", Status: models.StatusRunning, Image: "img"}
	b.SetLabels(map[string]string{"owner": "team-b", "env": "prod"})
	c := &models.Sandbox{ID: "c", Name: "nc", Status: models.StatusRunning, Image: "img"}
	c.SetLabels(map[string]string{"owner": "team-a", "env": "dev"})
	for _, sb := range []*models.Sandbox{a, b, c} {
		if err := st.Create(context.Background(), sb); err != nil {
			t.Fatal(err)
		}
	}

	got, err := st.List(context.Background(), ListFilter{Labels: map[string]string{"owner": "team-a"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("owner=team-a: want 2 got %d", len(got))
	}

	got, err = st.List(context.Background(), ListFilter{Labels: map[string]string{"owner": "team-a", "env": "prod"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != "a" {
		t.Fatalf("AND filter: %+v", got)
	}

	got, err = st.List(context.Background(), ListFilter{Labels: map[string]string{"owner": "missing"}})
	if err != nil || len(got) != 0 {
		t.Fatalf("want empty, got %v err=%v", got, err)
	}
}

func TestTransitionStatusYieldsToConcurrentWrites(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()
	sb := &models.Sandbox{ID: "a1", Name: "n1", Status: models.StatusRunning, Image: "img"}
	sb.SetEndpoints(map[int]string{80: "new:80"})
	if err := st.Create(ctx, sb); err != nil {
		t.Fatal(err)
	}
	if changed, err := st.TransitionStatus(ctx, "a1", models.StatusStopped, models.StatusRunning, ""); err != nil || changed {
		t.Fatalf("stale from-status applied: %v %v", changed, err)
	}
	if changed, err := st.TransitionStatus(ctx, "a1", models.StatusRunning, models.StatusError, "gone"); err != nil || !changed {
		t.Fatalf("matching transition not applied: %v %v", changed, err)
	}
	got, err := st.Get(ctx, "a1")
	if err != nil || got.Status != models.StatusError || got.Error != "gone" || got.Endpoints()[80] != "new:80" {
		t.Fatalf("transition changed more than status: %+v %v", got, err)
	}
	if err := st.Delete(ctx, "a1"); err != nil {
		t.Fatal(err)
	}
	if changed, err := st.TransitionStatus(ctx, "a1", models.StatusError, models.StatusRunning, ""); err != nil || changed {
		t.Fatalf("transition touched a deleted record: %v %v", changed, err)
	}
	if _, err := st.Get(ctx, "a1"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("deleted record resurrected: %v", err)
	}
}
