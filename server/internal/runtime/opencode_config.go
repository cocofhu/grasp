package runtime

import (
	"context"
	"fmt"
	"regexp"
	"strings"
)

const (
	EnvOpenCodeAPIKey       = "OPENCODE_API_KEY"
	EnvGraspOpenCodeAPIKey  = "GRASP_OPENCODE_API_KEY"
	EnvOpenCodeProvider     = "GRASP_OPENCODE_PROVIDER"
	EnvOpenCodeBaseURL      = "GRASP_OPENCODE_BASE_URL"
	EnvOpenCodeModelVision  = "GRASP_OPENCODE_MODEL_VISION"
	EnvACPBridgeModel       = "ACP_BRIDGE_MODEL"
	DefaultOpenCodeProvider = "openai"
	openCodeCompatibleNPM   = "@ai-sdk/openai-compatible"
)

// openCodeProviderID is the shape of an OpenCode catalog provider id.
var openCodeProviderID = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]*$`)

// OpenCodeCatalog answers whether OpenCode can resolve a provider id on its own.
//
// A vendor absent from the catalog is not an error: it just has to bring its own
// adapter and model list in opencode.json, which is what lets someone name their
// own gateway instead of picking a placeholder vendor.
type OpenCodeCatalog interface {
	// KnowsProvider reports whether the catalog lists the provider. The second
	// result is false when the catalog could not be read, leaving it unknown.
	KnowsProvider(ctx context.Context, providerID string) (bool, bool)
	// KnowsModel reports whether the catalog lists the model under the provider,
	// with the same unreadable-catalog convention.
	KnowsModel(ctx context.Context, providerID, modelID string) (bool, bool)
}

// NormalizeOpenCodeProvider keeps a catalog provider id as it is. OpenCode
// resolves providers against models.dev, which holds hundreds of them and grows
// without our involvement, so the id is only checked for shape; anything else
// collapses onto "custom", which carries its own base URL and model.
func NormalizeOpenCodeProvider(raw string) string {
	id := strings.ToLower(strings.TrimSpace(raw))
	if id == "" {
		return DefaultOpenCodeProvider
	}
	if !openCodeProviderID.MatchString(id) {
		return "custom"
	}
	return id
}

func openCodeNativeAPIKey(provider string) string {
	switch NormalizeOpenCodeProvider(provider) {
	case "openai":
		return "OPENAI_API_KEY"
	case "anthropic":
		return "ANTHROPIC_API_KEY"
	case "google":
		return "GOOGLE_GENERATIVE_AI_API_KEY"
	case "openrouter":
		return "OPENROUTER_API_KEY"
	case "deepseek":
		return "DEEPSEEK_API_KEY"
	case "moonshotai":
		return "MOONSHOT_API_KEY"
	case "alibaba":
		return "DASHSCOPE_API_KEY"
	case "xai":
		return "XAI_API_KEY"
	default:
		return ""
	}
}

func mergeOpenCodeVendorEnv(env map[string]string) {
	if env == nil {
		return
	}
	provider := NormalizeOpenCodeProvider(env[EnvOpenCodeProvider])
	env[EnvOpenCodeProvider] = provider
	// The bridge hands this value to `opencode run --model`, and the flag wins over
	// opencode.json, so the prefix has to be on the env var too: a bare
	// `deepseek/deepseek-flash` would route to DeepSeek instead of the vendor.
	if modelID := openCodeModelID(env[EnvACPBridgeModel], provider); modelID != "" {
		env[EnvACPBridgeModel] = provider + "/" + modelID
	}
	key := strings.TrimSpace(env[EnvOpenCodeAPIKey])
	if key == "" {
		return
	}
	if native := openCodeNativeAPIKey(provider); native != "" {
		if strings.TrimSpace(env[native]) == "" {
			env[native] = key
		}
	}
}

// OpenCodeConfigForEnv returns opencode.json contents generated from Agent env,
// without consulting the catalog. See OpenCodeConfigForEnvWithCatalog.
func OpenCodeConfigForEnv(backend AcpBackend, env map[string]string) map[string]any {
	return OpenCodeConfigForEnvWithCatalog(context.Background(), backend, env, nil)
}

// OpenCodeConfigForEnvWithCatalog returns opencode.json contents generated from
// Agent env. Nil means do not write a file (rely on native vendor env).
// User-authored opencode.json in the config home is never overwritten.
//
// A vendor the catalog does not list gets the OpenAI-compatible adapter and its
// model declared, so naming your own gateway works the same as picking a vendor
// from the list — which is the whole point of letting the id be typed in. A nil
// or unreadable catalog keeps the conservative behavior: only the reserved
// `custom` id brings an adapter, since overriding a vendor OpenCode resolves
// natively would cost it its own SDK.
func OpenCodeConfigForEnvWithCatalog(
	ctx context.Context,
	backend AcpBackend,
	env map[string]string,
	catalog OpenCodeCatalog,
) map[string]any {
	if NormalizeBackend(string(backend)) != BackendOpenCode {
		return nil
	}
	provider := NormalizeOpenCodeProvider("")
	baseURL := ""
	model := ""
	key := ""
	if env != nil {
		provider = NormalizeOpenCodeProvider(env[EnvOpenCodeProvider])
		baseURL = strings.TrimSpace(env[EnvOpenCodeBaseURL])
		model = strings.TrimSpace(env[EnvACPBridgeModel])
		key = strings.TrimSpace(env[EnvOpenCodeAPIKey])
	}
	ownAdapter := openCodeNeedsAdapter(ctx, provider, catalog)
	if !ownAdapter && baseURL == "" && model == "" && key == "" {
		return nil
	}
	doc := map[string]any{
		"$schema": "https://opencode.ai/config.json",
	}
	modelID := openCodeModelID(model, provider)
	if modelID != "" {
		doc["model"] = provider + "/" + modelID
	}
	// A vendor in the catalog can still lack the model: a gateway publishes new
	// ids faster than the snapshot, and OpenCode rejects what it cannot find.
	// Declaring the id costs nothing and keeps the vendor's own adapter.
	declareModel := ownAdapter || openCodeNeedsModelDecl(ctx, provider, modelID, catalog)
	// Naming the key on the provider is what lets any catalog vendor work: only a
	// handful publish an env var we know by name, and OPENCODE_API_KEY is the one
	// place the Agent's key always lands.
	if !ownAdapter && !declareModel && baseURL == "" && key == "" {
		return doc
	}
	options := map[string]any{
		"apiKey": "{env:OPENCODE_API_KEY}",
	}
	if baseURL != "" {
		options["baseURL"] = baseURL
	}
	prov := map[string]any{"options": options}
	if ownAdapter {
		prov["npm"] = openCodeCompatibleNPM
		prov["name"] = openCodeProviderName(provider)
		if modelID == "" {
			modelID = "default"
			doc["model"] = provider + "/" + modelID
		}
	}
	if declareModel && modelID != "" {
		vision := env != nil && EnvEnabled(env[EnvOpenCodeModelVision])
		prov["models"] = map[string]any{
			modelID: openCodeModelDecl(modelID, vision),
		}
	}
	doc["provider"] = map[string]any{provider: prov}
	return doc
}

const openCodeAPIKeyPlaceholder = "{env:OPENCODE_API_KEY}"

// RequireOpenCodePlaceholderKey errors when the generated opencode.json names
// {env:OPENCODE_API_KEY} but that env var is empty. A nil/empty doc (user-owned
// auth, or no OpenCode hints) is left alone.
func RequireOpenCodePlaceholderKey(doc map[string]any, env map[string]string) error {
	if !containsStringValue(doc, openCodeAPIKeyPlaceholder) {
		return nil
	}
	if strings.TrimSpace(env[EnvOpenCodeAPIKey]) != "" {
		return nil
	}
	return fmt.Errorf("OpenCode 配置引用了 %s，但 %s 为空", openCodeAPIKeyPlaceholder, EnvOpenCodeAPIKey)
}

func containsStringValue(v any, want string) bool {
	switch t := v.(type) {
	case string:
		return t == want
	case map[string]any:
		for _, child := range t {
			if containsStringValue(child, want) {
				return true
			}
		}
	case []any:
		for _, child := range t {
			if containsStringValue(child, want) {
				return true
			}
		}
	}
	return false
}

// openCodeModelDecl describes a model the catalog cannot describe for us.
//
// OpenCode takes model capabilities from models.dev and falls back to text-only
// input for anything declared by hand, which makes it drop images before they
// reach the endpoint. The Agent env explicitly opts such a model into image
// input because approving cannot infer an unlisted upstream model's capability.
func openCodeModelDecl(modelID string, vision bool) map[string]any {
	decl := map[string]any{"name": modelID}
	if vision {
		decl["attachment"] = true
		decl["modalities"] = map[string]any{
			"input":  []string{"text", "image"},
			"output": []string{"text"},
		}
	}
	return decl
}

// openCodeNeedsAdapter reports whether opencode.json must carry the provider's
// adapter because OpenCode's own catalog cannot resolve the id.
func openCodeNeedsAdapter(ctx context.Context, provider string, catalog OpenCodeCatalog) bool {
	if provider == "custom" {
		return true
	}
	if catalog == nil {
		return false
	}
	known, ok := catalog.KnowsProvider(ctx, provider)
	return ok && !known
}

// openCodeNeedsModelDecl reports whether the model has to be declared because
// the catalog knows the vendor but not this id.
func openCodeNeedsModelDecl(
	ctx context.Context,
	provider, modelID string,
	catalog OpenCodeCatalog,
) bool {
	if catalog == nil || modelID == "" {
		return false
	}
	known, ok := catalog.KnowsModel(ctx, provider, modelID)
	return ok && !known
}

// openCodeProviderName is the label shown by OpenCode for a vendor we declare.
// The reserved id keeps its historical label; a typed-in id is its own name.
func openCodeProviderName(provider string) string {
	if provider == "custom" {
		return "Custom"
	}
	return provider
}

// openCodeModelID is the vendor's own model id, with a redundant `provider/`
// prefix dropped so prefixing is idempotent across saved values.
//
// The remainder is kept whole, slashes and all: aggregating gateways publish ids
// that contain one (OpenRouter's `anthropic/claude-sonnet-4-5`, TokenHub's
// `deepseek/deepseek-flash`). Cutting at the first slash would hand OpenCode
// `deepseek` as the vendor, which resolves to a different endpoint entirely.
func openCodeModelID(model, provider string) string {
	m := strings.TrimSpace(model)
	if m == "" {
		return ""
	}
	if rest, ok := strings.CutPrefix(m, provider+"/"); ok && rest != "" {
		return rest
	}
	return m
}
