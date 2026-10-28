package modelconnection

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"backend/internal/backend/common"
	"backend/internal/provider"
	"gopkg.in/yaml.v3"
)

func TestNativeAdaptersHonorDeclaredImageInputs(t *testing.T) {
	yes, no := true, false
	for _, test := range []struct {
		name       string
		input      []string
		vision     *bool
		wantInput  []string
		wantImages *bool
	}{
		{name: "unknown"},
		{name: "vision enabled", vision: &yes, wantInput: []string{"text", "image"}, wantImages: &yes},
		{name: "vision disabled", vision: &no, wantInput: []string{"text"}, wantImages: &no},
		{name: "text input", input: []string{"text"}, wantInput: []string{"text"}, wantImages: &no},
		{name: "image input", input: []string{"image"}, wantInput: []string{"image"}, wantImages: &yes},
		{name: "text and image input", input: []string{"text", "image"}, wantInput: []string{"text", "image"}, wantImages: &yes},
		{name: "explicit text overrides vision", input: []string{"text"}, vision: &yes, wantInput: []string{"text"}, wantImages: &no},
		{name: "explicit image overrides vision", input: []string{"text", "image"}, vision: &no, wantInput: []string{"text", "image"}, wantImages: &yes},
	} {
		t.Run(test.name, func(t *testing.T) {
			for _, adapter := range []struct {
				name   string
				tool   provider.Name
				native bool
			}{
				{name: "CodeBuddy", tool: provider.CodeBuddy},
				{name: "OpenCode", tool: provider.OpenCode},
				{name: "DSH compatible", tool: provider.DeepSeekHarness},
				{name: "DSH native", tool: provider.DeepSeekHarness, native: true},
			} {
				t.Run(adapter.name, func(t *testing.T) {
					c := connection(Chat)
					c.Models[0].Input = test.input
					c.Models[0].Capabilities.Vision = test.vision
					if adapter.native {
						c.Protocol, c.ProviderID = Messages, "deepseek"
						c.Auth.Scheme = "x-api-key"
						c.BaseURL = "https://api.deepseek.com/anthropic"
						c.ToolSettings.RouteProfile = "deepseek-native"
					}
					p := prepare(t, adapter.tool, c)
					selected, other := nativeModelDeclarations(t, adapter.tool, p)
					if adapter.tool == provider.CodeBuddy {
						assertImageDeclaration(t, selected, "supportsImages", test.wantImages)
						assertImageDeclaration(t, other, "supportsImages", nil)
						return
					}
					inputKey := "input"
					if adapter.native {
						inputKey = "inputModalities"
					}
					inputContainer := selected
					if adapter.tool == provider.OpenCode {
						assertImageDeclaration(t, selected, "attachment", test.wantImages)
						assertImageDeclaration(t, other, "attachment", nil)
						inputContainer, _ = selected["modalities"].(map[string]any)
						if _, exists := other["modalities"]; exists {
							t.Fatal("selected model input leaked into another model")
						}
					} else if _, exists := other[inputKey]; exists {
						t.Fatal("selected model input leaked into another model")
					}
					if test.wantInput == nil {
						if _, exists := inputContainer[inputKey]; exists {
							t.Fatal("unknown input capability became an explicit declaration")
						}
						return
					}
					items, ok := inputContainer[inputKey].([]any)
					if !ok {
						t.Fatalf("missing input declaration: %#v", selected)
					}
					got := make([]string, len(items))
					for i, item := range items {
						got[i], _ = item.(string)
					}
					if !reflect.DeepEqual(got, test.wantInput) {
						t.Fatalf("input = %v, want %v", got, test.wantInput)
					}
				})
			}
		})
	}
}

func assertImageDeclaration(t *testing.T, model map[string]any, key string, want *bool) {
	t.Helper()
	value, exists := model[key]
	if want == nil {
		if exists {
			t.Fatalf("unknown %s became explicit: %#v", key, value)
		}
		return
	}
	if value != *want {
		t.Fatalf("%s = %#v, want %v", key, value, *want)
	}
}

func TestOpenCodePreservesDeclaredReasoningCapability(t *testing.T) {
	yes, no := true, false
	for _, test := range []struct {
		name      string
		reasoning *bool
	}{
		{name: "unknown"},
		{name: "enabled", reasoning: &yes},
		{name: "disabled", reasoning: &no},
	} {
		t.Run(test.name, func(t *testing.T) {
			c := connection(Chat)
			c.Models[0].Capabilities.Reasoning = test.reasoning
			p := prepare(t, provider.OpenCode, c)
			selected, other := nativeModelDeclarations(t, provider.OpenCode, p)
			assertImageDeclaration(t, selected, "reasoning", test.reasoning)
			assertImageDeclaration(t, other, "reasoning", nil)
			if _, exists := selected["variants"]; exists {
				t.Fatal("capability declaration forced a reasoning effort")
			}
		})
	}
}

func nativeModelDeclarations(t *testing.T, tool provider.Name, p Prepared) (map[string]any, map[string]any) {
	t.Helper()
	if tool == provider.CodeBuddy {
		path := filepath.Join(common.GetEnvValue(p.Env, "CODEBUDDY_CONFIG_DIR"), "models.json")
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var doc struct{ Models []map[string]any }
		if err := json.Unmarshal(data, &doc); err != nil {
			t.Fatal(err)
		}
		return doc.Models[0], doc.Models[1]
	}
	if tool == provider.OpenCode {
		var doc struct {
			Provider map[string]struct{ Models map[string]map[string]any }
		}
		if err := json.Unmarshal([]byte(common.GetEnvValue(p.Env, "OPENCODE_CONFIG_CONTENT")), &doc); err != nil {
			t.Fatal(err)
		}
		models := doc.Provider[p.RouteID].Models
		return models[p.Connection.Models[0].ID], models[p.Connection.Models[1].ID]
	}
	var rows []map[string]any
	if err := yaml.Unmarshal(p.DSHPatch, &rows); err != nil {
		t.Fatal(err)
	}
	for _, row := range rows {
		if _, disabled := row["disabled"]; disabled {
			continue
		}
		config, _ := row["config"].(map[string]any)
		if row["id"] == "llm-pi-ai" {
			config = config["providers"].(map[string]any)[p.RouteID].(map[string]any)
		} else if row["id"] != "llm-deepseek" {
			continue
		}
		models := config["models"].([]any)
		return models[0].(map[string]any), models[1].(map[string]any)
	}
	t.Fatal("DSH model declarations missing")
	return nil, nil
}
