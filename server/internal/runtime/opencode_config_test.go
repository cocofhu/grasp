package runtime

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestNormalizeOpenCodeProvider(t *testing.T) {
	if got := NormalizeOpenCodeProvider(""); got != DefaultOpenCodeProvider {
		t.Fatalf("empty=%q", got)
	}
	if got := NormalizeOpenCodeProvider(" Anthropic "); got != "anthropic" {
		t.Fatalf("anthropic=%q", got)
	}
	// Any catalog id survives: the vendor list comes from models.dev, not from here.
	if got := NormalizeOpenCodeProvider("ZAI"); got != "zai" {
		t.Fatalf("catalog id=%q", got)
	}
	if got := NormalizeOpenCodeProvider("my-gateway.v2"); got != "my-gateway.v2" {
		t.Fatalf("gateway id=%q", got)
	}
	if got := NormalizeOpenCodeProvider("has space"); got != "custom" {
		t.Fatalf("malformed id=%q", got)
	}
}

func TestOpenCodeConfigForEnv_NilWhenBuiltInNoExtras(t *testing.T) {
	if OpenCodeConfigForEnv(BackendOpenCode, map[string]string{
		EnvOpenCodeProvider: "openai",
	}) != nil {
		t.Fatal("built-in vendor without baseURL/model should skip generated opencode.json")
	}
}

func TestAuthConfigFileExists_OpenCodeJSON(t *testing.T) {
	dir := t.TempDir()
	if AuthConfigFileExists(dir, BackendOpenCode) {
		t.Fatal("empty dir")
	}
	if err := os.WriteFile(filepath.Join(dir, "opencode.json"), []byte(`{}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if !AuthConfigFileExists(dir, BackendOpenCode) {
		t.Fatal("opencode.json should satisfy OpenCode auth gate")
	}
	if AuthConfigFileExists(dir, BackendCursor) {
		t.Fatal("cursor must not treat opencode.json as settings.json")
	}
}

func TestOpenCodeConfigForEnv_NilOnOtherBackends(t *testing.T) {
	if OpenCodeConfigForEnv(BackendCursor, map[string]string{EnvACPBridgeModel: "x"}) != nil {
		t.Fatal("cursor must not emit opencode.json")
	}
}

func TestOpenCodeConfigForEnv_ModelOnly(t *testing.T) {
	doc := OpenCodeConfigForEnv(BackendOpenCode, map[string]string{
		EnvOpenCodeProvider: "anthropic",
		EnvACPBridgeModel:   "anthropic/claude-sonnet-4-5",
	})
	if doc == nil {
		t.Fatal("expected config")
	}
	if doc["model"] != "anthropic/claude-sonnet-4-5" {
		t.Fatalf("model=%v", doc["model"])
	}
	if _, ok := doc["provider"]; ok {
		t.Fatalf("built-in without a key should omit provider: %#v", doc)
	}
}

// A vendor the catalog knows needs no base URL and no npm adapter, but it does
// need the key named on it: only a handful of vendors publish an env var this
// server knows, while OPENCODE_API_KEY always holds the Agent's key.
func TestOpenCodeConfigForEnv_CatalogVendorNamesTheKey(t *testing.T) {
	doc := OpenCodeConfigForEnv(BackendOpenCode, map[string]string{
		EnvOpenCodeProvider: "zai",
		EnvOpenCodeAPIKey:   "sk-oc",
		EnvACPBridgeModel:   "zai/glm-5",
	})
	if doc == nil {
		t.Fatal("expected config")
	}
	if doc["model"] != "zai/glm-5" {
		t.Fatalf("model=%v", doc["model"])
	}
	prov, _ := doc["provider"].(map[string]any)
	zai, _ := prov["zai"].(map[string]any)
	if zai == nil {
		t.Fatalf("provider block missing: %#v", doc)
	}
	opts, _ := zai["options"].(map[string]any)
	if opts["apiKey"] != "{env:OPENCODE_API_KEY}" {
		t.Fatalf("apiKey=%v", opts["apiKey"])
	}
	if _, ok := opts["baseURL"]; ok {
		t.Fatalf("catalog vendor must keep its own endpoint: %#v", opts)
	}
	if _, ok := zai["npm"]; ok {
		t.Fatalf("catalog vendor must keep its own adapter: %#v", zai)
	}
	if _, ok := zai["models"]; ok {
		t.Fatalf("catalog vendor must keep its own model list: %#v", zai)
	}
}

func TestOpenCodeConfigForEnv_CustomRequiresProviderBlock(t *testing.T) {
	doc := OpenCodeConfigForEnv(BackendOpenCode, map[string]string{
		EnvOpenCodeProvider: "custom",
		EnvOpenCodeBaseURL:  "https://llm.example/v1",
		EnvACPBridgeModel:   "my-model",
	})
	if doc == nil {
		t.Fatal("expected config")
	}
	if doc["model"] != "custom/my-model" {
		t.Fatalf("model=%v", doc["model"])
	}
	prov, _ := doc["provider"].(map[string]any)
	custom, _ := prov["custom"].(map[string]any)
	if custom["npm"] != openCodeCompatibleNPM {
		t.Fatalf("npm=%v", custom["npm"])
	}
	opts, _ := custom["options"].(map[string]any)
	if opts["baseURL"] != "https://llm.example/v1" {
		t.Fatalf("baseURL=%v", opts["baseURL"])
	}
}

// An aggregating gateway's model id contains a slash of its own. Cutting at
// that slash would name the wrong vendor, so the whole id keeps its shape and
// the picked provider is prefixed in front of it.
func TestOpenCodeConfigForEnv_SlashedModelIDKeepsVendor(t *testing.T) {
	doc := OpenCodeConfigForEnv(BackendOpenCode, map[string]string{
		EnvOpenCodeProvider: "openrouter",
		EnvOpenCodeAPIKey:   "sk-oc",
		EnvACPBridgeModel:   "anthropic/claude-sonnet-4-5",
	})
	if doc == nil {
		t.Fatal("expected config")
	}
	if doc["model"] != "openrouter/anthropic/claude-sonnet-4-5" {
		t.Fatalf("model=%v", doc["model"])
	}
}

// Prefixing is idempotent: a saved value already naming its vendor must not
// grow a second prefix.
func TestOpenCodeConfigForEnv_PrefixIsIdempotent(t *testing.T) {
	doc := OpenCodeConfigForEnv(BackendOpenCode, map[string]string{
		EnvOpenCodeProvider: "openrouter",
		EnvOpenCodeAPIKey:   "sk-oc",
		EnvACPBridgeModel:   "openrouter/anthropic/claude-sonnet-4-5",
	})
	if doc == nil {
		t.Fatal("expected config")
	}
	if doc["model"] != "openrouter/anthropic/claude-sonnet-4-5" {
		t.Fatalf("model=%v", doc["model"])
	}
}

// The catalog lists `openrouter/auto` under `openrouter`, so a value naming that
// model twice is not a doubled prefix — stripping one leaves `auto`, which the
// endpoint does not serve. Only the value the picker writes is authoritative.
func TestOpenCodeConfigForEnv_SelfPrefixedCatalogID(t *testing.T) {
	doc := OpenCodeConfigForEnv(BackendOpenCode, map[string]string{
		EnvOpenCodeProvider: "openrouter",
		EnvOpenCodeAPIKey:   "sk-oc",
		EnvACPBridgeModel:   "openrouter/openrouter/auto",
	})
	if doc == nil {
		t.Fatal("expected config")
	}
	if doc["model"] != "openrouter/openrouter/auto" {
		t.Fatalf("model=%v", doc["model"])
	}
}

func TestOpenCodeConfigForEnv_CustomDeclaresSlashedModel(t *testing.T) {
	doc := OpenCodeConfigForEnv(BackendOpenCode, map[string]string{
		EnvOpenCodeProvider: "custom",
		EnvOpenCodeBaseURL:  "https://tokenhub.example/v1",
		EnvACPBridgeModel:   "deepseek/deepseek-flash",
	})
	if doc == nil {
		t.Fatal("expected config")
	}
	if doc["model"] != "custom/deepseek/deepseek-flash" {
		t.Fatalf("model=%v", doc["model"])
	}
	prov, _ := doc["provider"].(map[string]any)
	custom, _ := prov["custom"].(map[string]any)
	models, _ := custom["models"].(map[string]any)
	if _, ok := models["deepseek/deepseek-flash"]; !ok {
		t.Fatalf("gateway model must be declared whole: %#v", models)
	}
}

func TestOpenCodeConfigForEnv_CustomWithoutModel(t *testing.T) {
	doc := OpenCodeConfigForEnv(BackendOpenCode, map[string]string{
		EnvOpenCodeProvider: "custom",
		EnvOpenCodeBaseURL:  "https://llm.example/v1",
	})
	if doc == nil {
		t.Fatal("expected config")
	}
	if doc["model"] != "custom/default" {
		t.Fatalf("model=%v", doc["model"])
	}
	prov, _ := doc["provider"].(map[string]any)
	custom, _ := prov["custom"].(map[string]any)
	models, _ := custom["models"].(map[string]any)
	if _, ok := models["default"]; !ok {
		t.Fatalf("models=%#v", models)
	}
}

// stubCatalog answers provider lookups without touching models.dev. models maps
// provider id → the ids that provider lists.
type stubCatalog struct {
	known     map[string]bool
	models    map[string][]string
	readable  bool
	lastQuery string
}

func (s *stubCatalog) KnowsProvider(_ context.Context, id string) (bool, bool) {
	s.lastQuery = id
	if !s.readable {
		return false, false
	}
	return s.known[id], true
}

func (s *stubCatalog) KnowsModel(_ context.Context, provider, model string) (bool, bool) {
	if !s.readable {
		return false, false
	}
	for _, m := range s.models[provider] {
		if m == model {
			return true, true
		}
	}
	return false, true
}

// Naming your own gateway has to work like picking a vendor from the list: the
// adapter and the model declaration are generated, not typed by the user.
func TestOpenCodeConfigForEnv_UnlistedVendorGetsAdapter(t *testing.T) {
	cat := &stubCatalog{known: map[string]bool{"openai": true}, readable: true}
	doc := OpenCodeConfigForEnvWithCatalog(context.Background(), BackendOpenCode, map[string]string{
		EnvOpenCodeProvider: "tokenhub",
		EnvOpenCodeBaseURL:  "https://tokenhub.example/v1",
		EnvOpenCodeAPIKey:   "sk-oc",
		EnvACPBridgeModel:   "deepseek/deepseek-flash",
	}, cat)
	if doc == nil {
		t.Fatal("expected config")
	}
	if doc["model"] != "tokenhub/deepseek/deepseek-flash" {
		t.Fatalf("model=%v", doc["model"])
	}
	prov, _ := doc["provider"].(map[string]any)
	th, _ := prov["tokenhub"].(map[string]any)
	if th == nil {
		t.Fatalf("provider block must keep the typed-in name: %#v", doc)
	}
	if th["npm"] != openCodeCompatibleNPM {
		t.Fatalf("npm=%v", th["npm"])
	}
	if th["name"] != "tokenhub" {
		t.Fatalf("name=%v", th["name"])
	}
	models, _ := th["models"].(map[string]any)
	if _, ok := models["deepseek/deepseek-flash"]; !ok {
		t.Fatalf("gateway model must be declared whole: %#v", models)
	}
	opts, _ := th["options"].(map[string]any)
	if opts["baseURL"] != "https://tokenhub.example/v1" {
		t.Fatalf("baseURL=%v", opts["baseURL"])
	}
}

// A vendor the catalog lists can still be missing the model: Tencent TokenHub is
// in the catalog with three `hy*` ids, while the endpoint serves many more. The
// vendor keeps its own adapter; only the model is declared.
func TestOpenCodeConfigForEnv_ListedVendorUnlistedModel(t *testing.T) {
	cat := &stubCatalog{
		known:    map[string]bool{"tencent-tokenhub": true},
		models:   map[string][]string{"tencent-tokenhub": {"hy3", "hy4-preview"}},
		readable: true,
	}
	doc := OpenCodeConfigForEnvWithCatalog(context.Background(), BackendOpenCode, map[string]string{
		EnvOpenCodeProvider: "tencent-tokenhub",
		EnvOpenCodeBaseURL:  "https://tokenhub.example/v1",
		EnvOpenCodeAPIKey:   "sk-oc",
		EnvACPBridgeModel:   "deepseek/deepseek-flash",
	}, cat)
	if doc["model"] != "tencent-tokenhub/deepseek/deepseek-flash" {
		t.Fatalf("model=%v", doc["model"])
	}
	prov, _ := doc["provider"].(map[string]any)
	th, _ := prov["tencent-tokenhub"].(map[string]any)
	models, _ := th["models"].(map[string]any)
	if _, ok := models["deepseek/deepseek-flash"]; !ok {
		t.Fatalf("unlisted model must be declared: %#v", th)
	}
	if _, ok := th["npm"]; ok {
		t.Fatalf("catalog vendor must keep its own adapter: %#v", th)
	}
	if _, ok := th["name"]; ok {
		t.Fatalf("catalog vendor must keep its own label: %#v", th)
	}
}

// A declared model gets image input, which OpenCode otherwise withholds: it
// assumes text-only for anything models.dev does not describe, and drops the
// attachment before the vendor can answer for itself.
func TestOpenCodeConfigForEnv_DeclaredModelAcceptsImages(t *testing.T) {
	cat := &stubCatalog{
		known:    map[string]bool{"tencent-tokenhub": true},
		models:   map[string][]string{"tencent-tokenhub": {"hy3"}},
		readable: true,
	}
	doc := OpenCodeConfigForEnvWithCatalog(context.Background(), BackendOpenCode, map[string]string{
		EnvOpenCodeProvider:    "tencent-tokenhub",
		EnvOpenCodeAPIKey:      "sk-oc",
		EnvACPBridgeModel:      "deepseek/deepseek-flash",
		EnvOpenCodeModelVision: "1",
	}, cat)
	prov, _ := doc["provider"].(map[string]any)
	th, _ := prov["tencent-tokenhub"].(map[string]any)
	models, _ := th["models"].(map[string]any)
	decl, _ := models["deepseek/deepseek-flash"].(map[string]any)
	if decl["attachment"] != true {
		t.Fatalf("attachment=%v", decl["attachment"])
	}
	modalities, _ := decl["modalities"].(map[string]any)
	input, _ := modalities["input"].([]string)
	if !slices.Contains(input, "image") {
		t.Fatalf("image input must be declared: %#v", decl)
	}
	if !slices.Contains(input, "text") {
		t.Fatalf("text input must survive: %#v", decl)
	}
}

func TestOpenCodeConfigForEnv_DeclaredModelVisionDefaultsOff(t *testing.T) {
	decl := openCodeModelDecl("text-only", false)
	if _, ok := decl["attachment"]; ok {
		t.Fatalf("attachment must require an explicit opt-in: %#v", decl)
	}
	if _, ok := decl["modalities"]; ok {
		t.Fatalf("modalities must require an explicit opt-in: %#v", decl)
	}
}

// A model the catalog does list needs nothing written for it.
func TestOpenCodeConfigForEnv_ListedVendorListedModel(t *testing.T) {
	cat := &stubCatalog{
		known:    map[string]bool{"openai": true},
		models:   map[string][]string{"openai": {"gpt-4.1"}},
		readable: true,
	}
	doc := OpenCodeConfigForEnvWithCatalog(context.Background(), BackendOpenCode, map[string]string{
		EnvOpenCodeProvider: "openai",
		EnvOpenCodeAPIKey:   "sk-oc",
		EnvACPBridgeModel:   "gpt-4.1",
	}, cat)
	prov, _ := doc["provider"].(map[string]any)
	openai, _ := prov["openai"].(map[string]any)
	if _, ok := openai["models"]; ok {
		t.Fatalf("listed model needs no declaration: %#v", openai)
	}
}

// A vendor OpenCode resolves itself must keep its own SDK and model list.
func TestOpenCodeConfigForEnv_ListedVendorKeepsItsAdapter(t *testing.T) {
	cat := &stubCatalog{
		known:    map[string]bool{"openai": true},
		models:   map[string][]string{"openai": {"gpt-4.1"}},
		readable: true,
	}
	doc := OpenCodeConfigForEnvWithCatalog(context.Background(), BackendOpenCode, map[string]string{
		EnvOpenCodeProvider: "openai",
		EnvOpenCodeBaseURL:  "https://proxy.example/v1",
		EnvOpenCodeAPIKey:   "sk-oc",
		EnvACPBridgeModel:   "gpt-4.1",
	}, cat)
	prov, _ := doc["provider"].(map[string]any)
	openai, _ := prov["openai"].(map[string]any)
	if openai == nil {
		t.Fatalf("provider block missing: %#v", doc)
	}
	if _, ok := openai["npm"]; ok {
		t.Fatalf("catalog vendor must keep its own adapter: %#v", openai)
	}
	if _, ok := openai["models"]; ok {
		t.Fatalf("catalog vendor must keep its own model list: %#v", openai)
	}
	if doc["model"] != "openai/gpt-4.1" {
		t.Fatalf("model=%v", doc["model"])
	}
}

// An unreadable catalog is not evidence that a vendor is absent, so overriding
// its adapter on a guess would break vendors that work today.
func TestOpenCodeConfigForEnv_UnreadableCatalogStaysConservative(t *testing.T) {
	cat := &stubCatalog{readable: false}
	doc := OpenCodeConfigForEnvWithCatalog(context.Background(), BackendOpenCode, map[string]string{
		EnvOpenCodeProvider: "tokenhub",
		EnvOpenCodeBaseURL:  "https://tokenhub.example/v1",
		EnvOpenCodeAPIKey:   "sk-oc",
		EnvACPBridgeModel:   "deepseek/deepseek-flash",
	}, cat)
	prov, _ := doc["provider"].(map[string]any)
	th, _ := prov["tokenhub"].(map[string]any)
	if _, ok := th["npm"]; ok {
		t.Fatalf("must not guess an adapter from an unreadable catalog: %#v", th)
	}
}

// The reserved id needs no lookup: it exists precisely to carry an adapter.
func TestOpenCodeConfigForEnv_CustomSkipsLookup(t *testing.T) {
	cat := &stubCatalog{readable: true}
	doc := OpenCodeConfigForEnvWithCatalog(context.Background(), BackendOpenCode, map[string]string{
		EnvOpenCodeProvider: "custom",
		EnvOpenCodeBaseURL:  "https://llm.example/v1",
		EnvACPBridgeModel:   "my-model",
	}, cat)
	if cat.lastQuery != "" {
		t.Fatalf("custom should not be looked up, queried %q", cat.lastQuery)
	}
	prov, _ := doc["provider"].(map[string]any)
	custom, _ := prov["custom"].(map[string]any)
	if custom["npm"] != openCodeCompatibleNPM {
		t.Fatalf("npm=%v", custom["npm"])
	}
	if custom["name"] != "Custom" {
		t.Fatalf("name=%v", custom["name"])
	}
}

func TestMergeAuthEnv_OpenCodeMapsNativeKey(t *testing.T) {
	out, err := MergeAuthEnv(BackendOpenCode, map[string]string{
		EnvGraspOpenCodeAPIKey: "sk-oc",
		EnvOpenCodeProvider:    "openai",
	})
	if err != nil {
		t.Fatal(err)
	}
	if out[EnvOpenCodeAPIKey] != "sk-oc" {
		t.Fatalf("OPENCODE_API_KEY=%q", out[EnvOpenCodeAPIKey])
	}
	if out["OPENAI_API_KEY"] != "sk-oc" {
		t.Fatalf("OPENAI_API_KEY=%q", out["OPENAI_API_KEY"])
	}
	if out[EnvOpenCodeProvider] != "openai" {
		t.Fatalf("provider=%q", out[EnvOpenCodeProvider])
	}
}

func TestMergeAuthEnv_OpenCodePrefixesBridgeModel(t *testing.T) {
	out, err := MergeAuthEnv(BackendOpenCode, map[string]string{
		EnvGraspOpenCodeAPIKey: "sk-oc",
		EnvOpenCodeProvider:    "tencent-tokenhub",
		EnvACPBridgeModel:      "deepseek/deepseek-flash",
	})
	if err != nil {
		t.Fatal(err)
	}
	if out[EnvACPBridgeModel] != "tencent-tokenhub/deepseek/deepseek-flash" {
		t.Fatalf("model=%q", out[EnvACPBridgeModel])
	}
}

func TestMergeAuthEnv_OpenCodeBridgeModelPrefixIdempotent(t *testing.T) {
	out, err := MergeAuthEnv(BackendOpenCode, map[string]string{
		EnvGraspOpenCodeAPIKey: "sk-oc",
		EnvOpenCodeProvider:    "openrouter",
		EnvACPBridgeModel:      "openrouter/openrouter/auto",
	})
	if err != nil {
		t.Fatal(err)
	}
	if out[EnvACPBridgeModel] != "openrouter/openrouter/auto" {
		t.Fatalf("model=%q", out[EnvACPBridgeModel])
	}
}

func TestMergeAuthEnv_OpenCodeBridgeModelStaysEmpty(t *testing.T) {
	out, err := MergeAuthEnv(BackendOpenCode, map[string]string{
		EnvGraspOpenCodeAPIKey: "sk-oc",
		EnvOpenCodeProvider:    "openai",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := out[EnvACPBridgeModel]; ok {
		t.Fatalf("should not invent a model: %#v", out)
	}
}

func TestPrepareAuthEnv_OpenCodeJSONSkipsKey(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "opencode.json"), []byte(`{"model":"openai/gpt-4.1"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := PrepareAuthEnv(BackendOpenCode, map[string]string{}, dir)
	if err != nil {
		t.Fatal(err)
	}
	if out[EnvOpenCodeAPIKey] != "" {
		t.Fatalf("should not invent key: %#v", out)
	}
}

func TestRequireOpenCodePlaceholderKey(t *testing.T) {
	if err := RequireOpenCodePlaceholderKey(nil, nil); err != nil {
		t.Fatal(err)
	}
	doc := OpenCodeConfigForEnv(BackendOpenCode, map[string]string{
		EnvOpenCodeProvider: "custom",
		EnvOpenCodeBaseURL:  "https://example.test/v1",
	})
	if err := RequireOpenCodePlaceholderKey(doc, map[string]string{}); err == nil {
		t.Fatal("empty OPENCODE_API_KEY must fail when we wrote the placeholder")
	}
	if err := RequireOpenCodePlaceholderKey(doc, map[string]string{EnvOpenCodeAPIKey: "k"}); err != nil {
		t.Fatal(err)
	}
	if err := RequireOpenCodePlaceholderKey(map[string]any{"model": "openai/gpt-4.1"}, nil); err != nil {
		t.Fatal(err)
	}
}
