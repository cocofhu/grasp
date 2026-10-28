package opencodecatalog

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

const sample = `{
  "deepseek": {"id":"deepseek","name":"DeepSeek","api":"https://api.deepseek.com","env":["DEEPSEEK_API_KEY"],
    "models":{"deepseek-v4-pro":{"id":"deepseek-v4-pro","name":"DeepSeek V4 Pro"},
              "deepseek-v4-flash":{"id":"deepseek-v4-flash","name":"DeepSeek V4 Flash"}}},
  "anthropic": {"id":"anthropic","name":"Anthropic","env":["ANTHROPIC_API_KEY"],
    "models":{"claude-opus-5":{"id":"claude-opus-5","name":"Claude Opus 5"}}}
}`

func serve(t *testing.T, body string, status int) (*Store, *int) {
	t.Helper()
	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return New(srv.URL), &hits
}

func TestProvidersSortedAndSlimmed(t *testing.T) {
	s, _ := serve(t, sample, http.StatusOK)
	got, at, err := s.Providers(context.Background())
	if err != nil {
		t.Fatalf("Providers: %v", err)
	}
	if at.IsZero() {
		t.Fatal("fetchedAt not set")
	}
	if len(got) != 2 || got[0].ID != "anthropic" || got[1].ID != "deepseek" {
		t.Fatalf("providers not sorted by id: %+v", got)
	}
	ds := got[1]
	if ds.Name != "DeepSeek" || ds.API != "https://api.deepseek.com" || ds.KeyEnv != "DEEPSEEK_API_KEY" {
		t.Fatalf("provider fields lost: %+v", ds)
	}
	if len(ds.Models) != 2 || ds.Models[0].ID != "deepseek-v4-flash" || ds.Models[1].ID != "deepseek-v4-pro" {
		t.Fatalf("models not sorted by id: %+v", ds.Models)
	}
	if ds.Models[1].Name != "DeepSeek V4 Pro" {
		t.Fatalf("model display name lost: %+v", ds.Models[1])
	}
}

func TestCatalogExcludesModelsOpenCodeCannotResolveByDefault(t *testing.T) {
	s, _ := serve(t, `{
  "opencode-go": {"name":"OpenCode Go", "models":{
    "gemini-4-argon-contributor":{"name":"Gemini 4 Argon Contributor", "status":"deprecated"},
    "gemini-4-argon":{"name":"Gemini 4 Argon", "status":"active"},
    "next-alpha":{"name":"Next Alpha", "status":"alpha"},
    "preview-beta":{"name":"Preview Beta", "status":"beta"},
    "unversioned":{"name":"Unversioned"}
  }}
}`, http.StatusOK)
	ctx := context.Background()
	providers, _, err := s.Providers(ctx)
	if err != nil {
		t.Fatalf("Providers: %v", err)
	}
	if len(providers) != 1 || len(providers[0].Models) != 3 {
		t.Fatalf("provider model counts must exclude unavailable models: %+v", providers)
	}
	models, err := s.Models(ctx, "opencode-go")
	if err != nil {
		t.Fatalf("Models: %v", err)
	}
	for i, want := range []string{"gemini-4-argon", "preview-beta", "unversioned"} {
		if models[i].ID != want {
			t.Fatalf("models[%d].ID = %q, want %q", i, models[i].ID, want)
		}
		if known, readable := s.KnowsModel(ctx, "opencode-go", want); !known || !readable {
			t.Fatalf("KnowsModel(%q) = %v, %v", want, known, readable)
		}
	}
	if models[0].Name != "Gemini 4 Argon" {
		t.Fatalf("active model display name lost: %+v", models[0])
	}
	for _, id := range []string{"gemini-4-argon-contributor", "next-alpha"} {
		if known, readable := s.KnowsModel(ctx, "opencode-go", id); known || !readable {
			t.Fatalf("excluded model %q must not be considered selectable: known=%v readable=%v", id, known, readable)
		}
	}
}

func TestProviderMetadataSurvivesWhenAllModelsAreExcluded(t *testing.T) {
	s, _ := serve(t, `{
  "opencode-go": {"name":"OpenCode Go", "env":["OPENCODE_API_KEY"], "models":{
    "old":{"status":"deprecated"},
    "experimental":{"status":"alpha"}
  }}
}`, http.StatusOK)
	ctx := context.Background()
	providers, _, err := s.Providers(ctx)
	if err != nil {
		t.Fatalf("Providers: %v", err)
	}
	if len(providers) != 1 || providers[0].ID != "opencode-go" || len(providers[0].Models) != 0 {
		t.Fatalf("provider should remain with no selectable models: %+v", providers)
	}
	if known, readable := s.KnowsProvider(ctx, "opencode-go"); !known || !readable {
		t.Fatalf("KnowsProvider = %v, %v", known, readable)
	}
	if env := s.KeyEnv(ctx, "opencode-go"); env != "OPENCODE_API_KEY" {
		t.Fatalf("provider key environment lost: %q", env)
	}
}

func TestFreshSnapshotIsNotRefetched(t *testing.T) {
	s, hits := serve(t, sample, http.StatusOK)
	for i := 0; i < 3; i++ {
		if _, _, err := s.Providers(context.Background()); err != nil {
			t.Fatalf("Providers: %v", err)
		}
	}
	if *hits != 1 {
		t.Fatalf("want 1 upstream fetch, got %d", *hits)
	}
}

func TestStaleSnapshotSurvivesFailedRefresh(t *testing.T) {
	fail := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if fail {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		_, _ = w.Write([]byte(sample))
	}))
	defer srv.Close()
	s := New(srv.URL)
	if _, _, err := s.Providers(context.Background()); err != nil {
		t.Fatalf("first fetch: %v", err)
	}
	s.ttl = 0 // force every later call to attempt a refresh
	fail = true
	got, _, err := s.Providers(context.Background())
	if err != nil {
		t.Fatalf("stale snapshot should be served, got error: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("stale snapshot lost: %+v", got)
	}
}

func TestErrorWhenNothingCached(t *testing.T) {
	s, _ := serve(t, "", http.StatusInternalServerError)
	if _, _, err := s.Providers(context.Background()); err == nil {
		t.Fatal("want error when the first fetch fails")
	}
}

func TestModelsAndKeyEnvLookup(t *testing.T) {
	s, _ := serve(t, sample, http.StatusOK)
	ctx := context.Background()
	models, err := s.Models(ctx, "DeepSeek")
	if err != nil {
		t.Fatalf("Models: %v", err)
	}
	if len(models) != 2 {
		t.Fatalf("lookup should be case-insensitive: %+v", models)
	}
	if env := s.KeyEnv(ctx, "anthropic"); env != "ANTHROPIC_API_KEY" {
		t.Fatalf("KeyEnv = %q", env)
	}
	// A custom gateway is absent from the catalog: no models, no error.
	unknown, err := s.Models(ctx, "my-gateway")
	if err != nil || unknown != nil {
		t.Fatalf("unknown provider: models=%+v err=%v", unknown, err)
	}
	if env := s.KeyEnv(ctx, "my-gateway"); env != "" {
		t.Fatalf("KeyEnv for unknown provider = %q", env)
	}
	if known, readable := s.KnowsProvider(ctx, "DeepSeek"); !known || !readable {
		t.Fatalf("KnowsProvider(deepseek) = %v, %v", known, readable)
	}
	if known, readable := s.KnowsProvider(ctx, "my-gateway"); known || !readable {
		t.Fatalf("KnowsProvider(my-gateway) = %v, %v", known, readable)
	}
	if known, readable := s.KnowsModel(ctx, "deepseek", "deepseek-v4-pro"); !known || !readable {
		t.Fatalf("KnowsModel(listed) = %v, %v", known, readable)
	}
	if known, readable := s.KnowsModel(ctx, "deepseek", "deepseek/r2"); known || !readable {
		t.Fatalf("KnowsModel(unlisted) = %v, %v", known, readable)
	}
}

func TestKnowsProviderAndModelReportUnreadableCatalog(t *testing.T) {
	s, _ := serve(t, "", http.StatusInternalServerError)
	ctx := context.Background()
	if known, readable := s.KnowsProvider(ctx, "deepseek"); known || readable {
		t.Fatalf("KnowsProvider = %v, %v", known, readable)
	}
	if known, readable := s.KnowsModel(ctx, "deepseek", "deepseek-v4-pro"); known || readable {
		t.Fatalf("KnowsModel = %v, %v", known, readable)
	}
}

func TestProviderIDFallsBackToMapKey(t *testing.T) {
	s, _ := serve(t, `{"zai":{"name":"Z.ai","models":{"glm-5":{"name":"GLM 5"}}}}`, http.StatusOK)
	got, _, err := s.Providers(context.Background())
	if err != nil {
		t.Fatalf("Providers: %v", err)
	}
	if len(got) != 1 || got[0].ID != "zai" {
		t.Fatalf("provider id should fall back to the map key: %+v", got)
	}
	if len(got[0].Models) != 1 || got[0].Models[0].ID != "glm-5" {
		t.Fatalf("model id should fall back to the map key: %+v", got[0].Models)
	}
}

func TestSnapshotRefreshedAfterTTL(t *testing.T) {
	s, hits := serve(t, sample, http.StatusOK)
	if _, _, err := s.Providers(context.Background()); err != nil {
		t.Fatalf("first fetch: %v", err)
	}
	s.fetchedAt = time.Now().Add(-2 * defaultTTL)
	if _, _, err := s.Providers(context.Background()); err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if *hits != 2 {
		t.Fatalf("want a refetch after the TTL, got %d fetches", *hits)
	}
}
