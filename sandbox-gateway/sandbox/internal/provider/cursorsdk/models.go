package cursorsdk

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
)

// sdkModel is one entry of Cursor.models.list().
type sdkModel struct {
	ID          string         `json:"id"`
	DisplayName string         `json:"displayName"`
	Aliases     []string       `json:"aliases"`
	Parameters  []sdkParameter `json:"parameters"`
	Variants    []sdkVariant   `json:"variants"`
}

type sdkParameter struct {
	ID     string `json:"id"`
	Values []struct {
		Value string `json:"value"`
	} `json:"values"`
}

type sdkVariant struct {
	Params    []paramValue `json:"params"`
	IsDefault bool         `json:"isDefault"`
	IsMaxMode bool         `json:"isMaxMode"`
}

type paramValue struct {
	ID    string `json:"id"`
	Value string `json:"value"`
}

// selection is the SDK's ModelSelection.
type selection struct {
	ID      string       `json:"id"`
	Params  []paramValue `json:"params,omitempty"`
	MaxMode bool         `json:"maxMode,omitempty"`
}

func (s selection) label() string {
	parts := []string{s.ID}
	for _, p := range s.Params {
		parts = append(parts, p.ID+"="+p.Value)
	}
	return strings.Join(parts, " ")
}

// effortParams are the parameter ids Cursor models name their reasoning
// level by; a model has at most one of them.
var effortParams = []string{"effort", "reasoning", "reasoning_effort"}

// effortValues maps the platform's level names onto values some models
// spell differently.
var effortValues = map[string][]string{"xhigh": {"xhigh", "extra-high"}}

// resolveModel turns the chat's model, effort and speed tier into a selection
// the account offers. Parameters the chat leaves open take the model's
// default variant, except fast, which stays off unless chosen.
func resolveModel(catalog []sdkModel, modelID, effort, tier string) (selection, error) {
	modelID, effort, tier = strings.TrimSpace(modelID), strings.TrimSpace(effort), strings.TrimSpace(tier)
	if modelID == "" || modelID == "auto" {
		if effort != "" || tier != "" {
			return selection{}, fmt.Errorf("Cursor 自动选择模型时不能指定思考强度或速度档位")
		}
		return selection{ID: "default"}, nil
	}
	i := slices.IndexFunc(catalog, func(m sdkModel) bool { return m.ID == modelID })
	if i < 0 {
		i = slices.IndexFunc(catalog, func(m sdkModel) bool { return slices.Contains(m.Aliases, modelID) })
	}
	if i < 0 {
		return selection{}, fmt.Errorf("Cursor 不提供模型 %q", modelID)
	}
	m := catalog[i]
	want := map[string]string{}
	if effort != "" {
		param, value, ok := m.effort(effort)
		if !ok {
			if param == "" {
				return selection{}, fmt.Errorf("Cursor 模型 %s 不支持设置思考强度", m.ID)
			}
			return selection{}, fmt.Errorf("Cursor 模型 %s 不支持思考强度 %q", m.ID, effort)
		}
		want[param] = value
	}
	switch tier {
	case "":
		if m.has("fast", "false") {
			want["fast"] = "false"
		}
	case "fast":
		if !m.has("fast", "true") {
			return selection{}, fmt.Errorf("Cursor 模型 %s 没有 fast 档位", m.ID)
		}
		want["fast"] = "true"
	default:
		return selection{}, fmt.Errorf("Cursor 不支持速度档位 %q", tier)
	}
	if len(m.Variants) == 0 {
		return selection{ID: m.ID, Params: sortedParams(want)}, nil
	}
	v, ok := m.variant(want)
	if !ok {
		return selection{}, fmt.Errorf("Cursor 模型 %s 没有这个组合（%s）", m.ID, selection{ID: m.ID, Params: sortedParams(want)}.label())
	}
	return selection{ID: m.ID, Params: v.Params, MaxMode: v.IsMaxMode}, nil
}

func (m sdkModel) effort(level string) (param, value string, ok bool) {
	for _, p := range m.Parameters {
		if !slices.Contains(effortParams, p.ID) {
			continue
		}
		spellings := effortValues[level]
		if spellings == nil {
			spellings = []string{level}
		}
		for _, v := range p.Values {
			if slices.Contains(spellings, v.Value) {
				return p.ID, v.Value, true
			}
		}
		return p.ID, "", false
	}
	return "", "", false
}

func (m sdkModel) has(param, value string) bool {
	for _, p := range m.Parameters {
		if p.ID == param {
			return slices.ContainsFunc(p.Values, func(v struct {
				Value string `json:"value"`
			}) bool {
				return v.Value == value
			})
		}
	}
	return false
}

// variant is the variant with every wanted value that shares the most other
// values with the default variant.
func (m sdkModel) variant(want map[string]string) (sdkVariant, bool) {
	var def sdkVariant
	for _, v := range m.Variants {
		if v.IsDefault {
			def = v
		}
	}
	best, score := -1, -1
	for i, v := range m.Variants {
		if !v.matches(want) {
			continue
		}
		s := 0
		for _, p := range v.Params {
			if _, chosen := want[p.ID]; !chosen && def.value(p.ID) == p.Value {
				s++
			}
		}
		if s > score {
			best, score = i, s
		}
	}
	if best < 0 {
		return sdkVariant{}, false
	}
	return m.Variants[best], true
}

func (v sdkVariant) matches(want map[string]string) bool {
	for id, value := range want {
		if v.value(id) != value {
			return false
		}
	}
	return true
}

func (v sdkVariant) value(id string) string {
	for _, p := range v.Params {
		if p.ID == id {
			return p.Value
		}
	}
	return ""
}

func sortedParams(m map[string]string) []paramValue {
	out := make([]paramValue, 0, len(m))
	for id, value := range m {
		out = append(out, paramValue{ID: id, Value: value})
	}
	slices.SortFunc(out, func(a, b paramValue) int { return strings.Compare(a.ID, b.ID) })
	return out
}

// sdkMCPServers turns the session's ACP server list into the SDK's map.
func sdkMCPServers(raw json.RawMessage) (map[string]any, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	var servers []struct {
		Name    string      `json:"name"`
		Type    string      `json:"type"`
		URL     string      `json:"url"`
		Command string      `json:"command"`
		Args    []string    `json:"args"`
		Env     []nameValue `json:"env"`
		Headers []nameValue `json:"headers"`
	}
	if err := json.Unmarshal(raw, &servers); err != nil {
		return nil, fmt.Errorf("Cursor MCP 配置无法解析: %w", err)
	}
	out := map[string]any{}
	for _, s := range servers {
		if s.Name == "" {
			continue
		}
		switch {
		case s.URL != "":
			entry := map[string]any{"url": s.URL}
			if s.Type == "sse" {
				entry["type"] = "sse"
			} else {
				entry["type"] = "http"
			}
			if len(s.Headers) > 0 {
				entry["headers"] = pairs(s.Headers)
			}
			out[s.Name] = entry
		case s.Command != "":
			entry := map[string]any{"type": "stdio", "command": s.Command}
			if len(s.Args) > 0 {
				entry["args"] = s.Args
			}
			if len(s.Env) > 0 {
				entry["env"] = pairs(s.Env)
			}
			out[s.Name] = entry
		}
	}
	if len(out) == 0 {
		return nil, nil
	}
	return out, nil
}

type nameValue struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

func pairs(list []nameValue) map[string]string {
	out := make(map[string]string, len(list))
	for _, p := range list {
		out[p.Name] = p.Value
	}
	return out
}
