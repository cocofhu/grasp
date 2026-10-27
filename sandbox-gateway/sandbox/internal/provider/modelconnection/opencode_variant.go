package modelconnection

import (
	_ "embed"
	"encoding/json"
	"errors"
	"regexp"
	"strings"

	"backend/internal/backend/common"
)

//go:embed opencode_variants.json
var openCodeVariantCatalogJSON []byte

type openCodeVariantModel struct {
	Spell    string   `json:"spell"`
	Variants []string `json:"variants"`
}

var openCodeVariantCatalog = mustOpenCodeVariantCatalog()

func mustOpenCodeVariantCatalog() map[string]openCodeVariantModel {
	var out map[string]openCodeVariantModel
	if err := json.Unmarshal(openCodeVariantCatalogJSON, &out); err != nil {
		panic(err)
	}
	return out
}

var effortTokenPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._+\-]{0,63}$`)

// ApplyOpenCodeVariant attaches the effort for this run. An empty effort leaves
// the model unchanged so OpenCode uses its own default. The CLI selects the
// variant with --variant; appending it to the model makes it a different model
// ID. A custom variant, and every variant on a connected service, is declared
// on that model for this run.
func ApplyOpenCodeVariant(p Prepared, effort string) (Prepared, error) {
	effort = strings.TrimSpace(effort)
	base := stripVariant(strings.TrimSpace(p.Model))
	if base == "" || effort == "" {
		return p, nil
	}
	if !effortTokenPattern.MatchString(effort) {
		return p, errors.New("OpenCode reasoning effort is not a valid variant")
	}
	p.Model = base
	p.Args = append(append([]string(nil), p.Args...), "--variant", effort)
	var overlay map[string]any
	if p.Connection == nil {
		if !platformCatalogVariant(base, effort) {
			overlay = spellOverlay(platformSpell(base), effort)
		}
	} else {
		overlay = protocolOverlay(p.Connection.Protocol, effort)
	}
	return writeOpenCodeVariant(p, effort, overlay)
}

func platformCatalogVariant(model, effort string) bool {
	entry, ok := openCodeVariantCatalog[catalogID(model)]
	if !ok {
		return false
	}
	for _, item := range entry.Variants {
		if item == effort {
			return true
		}
	}
	return false
}

func platformSpell(model string) string {
	entry, ok := openCodeVariantCatalog[catalogID(model)]
	if !ok || entry.Spell == "" {
		return "chat"
	}
	return entry.Spell
}

func catalogID(model string) string {
	id := stripVariant(strings.TrimSpace(model))
	return strings.TrimPrefix(id, "opencode/")
}

func stripVariant(model string) string {
	if i := strings.LastIndex(model, "#"); i >= 0 {
		return model[:i]
	}
	return model
}

func spellOverlay(spell, effort string) map[string]any {
	switch spell {
	case "responses":
		return responsesOverlay(effort)
	case "anthropic":
		return anthropicOverlay(effort)
	case "google":
		return googleOverlay(effort)
	default:
		return chatOverlay(effort)
	}
}

func protocolOverlay(protocol, effort string) map[string]any {
	switch protocol {
	case Responses:
		return responsesOverlay(effort)
	case Messages:
		return anthropicOverlay(effort)
	default:
		return chatOverlay(effort)
	}
}

func chatOverlay(effort string) map[string]any {
	return map[string]any{"settings": map[string]any{"reasoningEffort": effort}}
}

func responsesOverlay(effort string) map[string]any {
	return map[string]any{"settings": map[string]any{
		"reasoningEffort":  effort,
		"reasoningSummary": "auto",
		"include":          []string{"reasoning.encrypted_content"},
	}}
}

func anthropicOverlay(effort string) map[string]any {
	return map[string]any{"settings": map[string]any{
		"thinking": map[string]any{"type": "adaptive", "display": "summarized"},
		"effort":   effort,
	}}
}

func googleOverlay(effort string) map[string]any {
	return map[string]any{"settings": map[string]any{
		"thinkingConfig": map[string]any{"includeThoughts": true, "thinkingLevel": effort},
	}}
}

func writeOpenCodeVariant(p Prepared, effort string, overlay map[string]any) (Prepared, error) {
	raw := common.GetEnvValue(p.Env, "OPENCODE_CONFIG_CONTENT")
	if raw == "" {
		return p, errors.New("OpenCode variant: configuration is missing")
	}
	var config map[string]any
	if err := json.Unmarshal([]byte(raw), &config); err != nil {
		return p, errors.New("OpenCode variant: configuration is invalid")
	}
	if overlay != nil {
		if err := declareVariant(config, p, effort, overlay); err != nil {
			return p, err
		}
	}
	data, err := json.Marshal(config)
	if err != nil {
		return p, err
	}
	p.Env = common.UpsertEnv(p.Env, "OPENCODE_CONFIG_CONTENT", string(data))
	return p, nil
}

func declareVariant(config map[string]any, p Prepared, effort string, overlay map[string]any) error {
	providerKey := "opencode"
	modelKey := catalogID(p.Model)
	if p.Connection != nil {
		providerKey = p.RouteID
		modelKey = p.ChatModelID()
	}
	providers, _ := config["provider"].(map[string]any)
	if providers == nil {
		providers = map[string]any{}
		config["provider"] = providers
	}
	entry, _ := providers[providerKey].(map[string]any)
	if entry == nil {
		entry = map[string]any{}
		providers[providerKey] = entry
	}
	models, _ := entry["models"].(map[string]any)
	if models == nil {
		models = map[string]any{}
		entry["models"] = models
	}
	model, _ := models[modelKey].(map[string]any)
	if model == nil {
		model = map[string]any{}
		models[modelKey] = model
	}
	variants, _ := model["variants"].(map[string]any)
	if variants == nil {
		variants = map[string]any{}
		model["variants"] = variants
	}
	variants[effort] = overlay
	return nil
}
