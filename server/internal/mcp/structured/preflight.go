package structured

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// PreflightArtifactName is the reserved env-checklist product for preflight nodes.
const PreflightArtifactName = "preflight.json"

type preflightField struct {
	Name         string `json:"name"`
	Label        string `json:"label,omitempty"`
	Value        string `json:"value"`
	Verified     bool   `json:"verified,omitempty"`
	Verification string `json:"verification,omitempty"`
	Source       string `json:"source,omitempty"`
	Notes        string `json:"notes,omitempty"`
}

type preflightDoc struct {
	Summary    string          `json:"summary"`
	Confirmed  bool            `json:"confirmed"`
	Fields     []preflightField `json:"fields,omitempty"`
	Unresolved flexStrings     `json:"unresolved,omitempty"`
}

func normPreflightVerification(s string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "":
		return "", true
	case "sandbox_probe", "user_attested", "mixed":
		return strings.ToLower(strings.TrimSpace(s)), true
	default:
		return "", false
	}
}

func normPreflightSource(s string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "":
		return "", true
	case "form", "choice", "chat":
		return strings.ToLower(strings.TrimSpace(s)), true
	default:
		return "", false
	}
}

// ParsePreflight validates set_preflight args into a confirmed env checklist.
// summary must be non-empty; confirmed must be true; unresolved must be empty;
// fields may be empty (no-gap). Each field requires non-empty name+value
// (plaintext). Duplicate names: last wins.
func ParsePreflight(args map[string]any) (preflightDoc, error) {
	var doc preflightDoc
	if err := decodeArgs(args, &doc); err != nil {
		return doc, fmt.Errorf("解析环境确认失败: %w", err)
	}
	doc.Summary = strings.TrimSpace(doc.Summary)
	if doc.Summary == "" {
		return doc, errors.New("summary 不能为空")
	}
	if !doc.Confirmed {
		return doc, errors.New("confirmed 必须为 true")
	}
	for _, u := range doc.Unresolved {
		if strings.TrimSpace(u) != "" {
			return doc, errors.New("unresolved 必须为空")
		}
	}
	doc.Unresolved = nil

	byName := map[string]preflightField{}
	order := make([]string, 0, len(doc.Fields))
	for i, f := range doc.Fields {
		name := strings.TrimSpace(f.Name)
		value := strings.TrimSpace(f.Value)
		if name == "" || value == "" {
			return doc, fmt.Errorf("fields[%d] 需要非空 name 与 value", i)
		}
		ver, ok := normPreflightVerification(f.Verification)
		if !ok {
			return doc, fmt.Errorf("fields[%d].verification 须为 sandbox_probe|user_attested|mixed", i)
		}
		src, ok := normPreflightSource(f.Source)
		if !ok {
			return doc, fmt.Errorf("fields[%d].source 须为 form|choice|chat", i)
		}
		nf := preflightField{
			Name:         name,
			Label:        strings.TrimSpace(f.Label),
			Value:        value,
			Verified:     f.Verified,
			Verification: ver,
			Source:       src,
			Notes:        strings.TrimSpace(f.Notes),
		}
		if _, exists := byName[name]; !exists {
			order = append(order, name)
		}
		byName[name] = nf
	}
	out := make([]preflightField, 0, len(order))
	for _, name := range order {
		out = append(out, byName[name])
	}
	doc.Fields = out
	return doc, nil
}

// PreflightIncomplete reports why a stored preflight.json is not ready to
// finish: missing/unparseable, confirmed≠true, or unresolved nonempty.
// Empty string means the checklist is complete (including no-gap empty fields).
func PreflightIncomplete(content string) string {
	var doc preflightDoc
	if json.Unmarshal([]byte(content), &doc) != nil {
		return "preflight.json 无法解析"
	}
	if strings.TrimSpace(doc.Summary) == "" {
		return "summary 不能为空"
	}
	if !doc.Confirmed {
		return "confirmed 必须为 true"
	}
	for _, u := range doc.Unresolved {
		if strings.TrimSpace(u) != "" {
			return "仍有 unresolved 未决项"
		}
	}
	for i, f := range doc.Fields {
		if strings.TrimSpace(f.Name) == "" || strings.TrimSpace(f.Value) == "" {
			return fmt.Sprintf("fields[%d] 缺少 name 或 value", i)
		}
	}
	return ""
}

// RenderPreflightMarkdown renders preflight.json. Raw content on parse error.
func RenderPreflightMarkdown(content string) string {
	var doc preflightDoc
	if json.Unmarshal([]byte(content), &doc) != nil || strings.TrimSpace(doc.Summary) == "" {
		return content
	}
	var b strings.Builder
	b.WriteString("### 环境确认\n\n")
	b.WriteString(doc.Summary + "\n")
	if doc.Confirmed {
		b.WriteString("\n**已确认**\n")
	} else {
		b.WriteString("\n**未确认**\n")
	}
	if len(doc.Fields) > 0 {
		b.WriteString("\n#### 字段\n")
		for _, f := range doc.Fields {
			label := f.Label
			if label == "" {
				label = f.Name
			}
			b.WriteString(fmt.Sprintf("- **%s** (`%s`): %s", label, f.Name, f.Value))
			if f.Verification != "" {
				b.WriteString(" · " + f.Verification)
			}
			if f.Source != "" {
				b.WriteString(" · " + f.Source)
			}
			if f.Notes != "" {
				b.WriteString(" — " + f.Notes)
			}
			b.WriteString("\n")
		}
	}
	writeBulletSection(&b, "未决项", doc.Unresolved)
	return strings.TrimRight(b.String(), "\n")
}
