package structured

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// MergeRequestArtifactName is the delivery product: one MR/PR per changed repo.
const MergeRequestArtifactName = "merge_request.json"

// Merge request providers.
const (
	MRProviderGitHub = "github"
	MRProviderGitLab = "gitlab"
	MRProviderOther  = "other"
)

// Merge request states.
const (
	MRStateCreated     = "created"
	MRStateReused      = "reused"
	MRStateMerged      = "merged"
	MRStateUnsupported = "unsupported"
)

type mergeRequestItem struct {
	Repo         string `json:"repo"`
	SourceBranch string `json:"sourceBranch"`
	TargetBranch string `json:"targetBranch"`
	URL          string `json:"url,omitempty"`
	Provider     string `json:"provider"`
	State        string `json:"state"`
	Note         string `json:"note,omitempty"`
}

type mergeRequestDoc struct {
	Summary string             `json:"summary"`
	Items   []mergeRequestItem `json:"items"`
}

// ParseMergeRequest validates and normalizes a merge_request.json payload.
func ParseMergeRequest(args map[string]any) (mergeRequestDoc, error) {
	var doc mergeRequestDoc
	if err := decodeArgs(args, &doc); err != nil {
		return doc, fmt.Errorf("解析合并请求失败: %w", err)
	}
	doc.Summary = strings.TrimSpace(doc.Summary)
	if doc.Summary == "" {
		return doc, errors.New("summary 不能为空")
	}
	if len(doc.Items) == 0 {
		return doc, errors.New("items 至少需要 1 条(每个有改动的仓库一条)")
	}
	items := make([]mergeRequestItem, 0, len(doc.Items))
	for i, it := range doc.Items {
		path := fmt.Sprintf("items[%d]", i)
		it.Repo = strings.TrimSpace(it.Repo)
		it.SourceBranch = strings.TrimSpace(it.SourceBranch)
		it.TargetBranch = strings.TrimSpace(it.TargetBranch)
		it.URL = strings.TrimSpace(it.URL)
		it.Provider = strings.ToLower(strings.TrimSpace(it.Provider))
		it.State = strings.ToLower(strings.TrimSpace(it.State))
		it.Note = strings.TrimSpace(it.Note)
		for _, field := range []struct{ name, val string }{
			{"repo", it.Repo},
			{"sourceBranch", it.SourceBranch},
			{"targetBranch", it.TargetBranch},
		} {
			if field.val == "" {
				return doc, fmt.Errorf("%s.%s 不能为空", path, field.name)
			}
		}
		switch it.Provider {
		case "":
			it.Provider = MRProviderOther
		case MRProviderGitHub, MRProviderGitLab, MRProviderOther:
		default:
			return doc, fmt.Errorf("%s.provider 须为 github|gitlab|other", path)
		}
		switch it.State {
		case MRStateCreated, MRStateReused, MRStateMerged, MRStateUnsupported:
		default:
			return doc, fmt.Errorf("%s.state 须为 created|reused|merged|unsupported", path)
		}
		if it.State != MRStateUnsupported && it.URL == "" {
			return doc, fmt.Errorf("%s.url 不能为空(仅 state=unsupported 时可省略)", path)
		}
		items = append(items, it)
	}
	doc.Items = items
	return doc, nil
}

var mrStateLabels = map[string]string{
	MRStateCreated:     "已创建",
	MRStateReused:      "已复用",
	MRStateMerged:      "已合并",
	MRStateUnsupported: "不支持",
}

// RenderMergeRequestMarkdown renders merge_request.json. Raw content on parse error.
func RenderMergeRequestMarkdown(content string) string {
	var doc mergeRequestDoc
	if json.Unmarshal([]byte(content), &doc) != nil || strings.TrimSpace(doc.Summary) == "" {
		return content
	}
	var b strings.Builder
	b.WriteString(strings.TrimSpace(doc.Summary) + "\n")
	if len(doc.Items) > 0 {
		b.WriteString("\n#### 合并请求\n")
		for _, it := range doc.Items {
			state := mrStateLabels[it.State]
			if state == "" {
				state = it.State
			}
			fmt.Fprintf(&b, "- **%s** `%s` → `%s` · %s", it.Repo, it.SourceBranch, it.TargetBranch, state)
			if it.URL != "" {
				fmt.Fprintf(&b, " · [%s](%s)", mrLinkLabel(it.Provider), it.URL)
			}
			if it.Note != "" {
				b.WriteString(" — " + it.Note)
			}
			b.WriteString("\n")
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

func mrLinkLabel(provider string) string {
	switch provider {
	case MRProviderGitHub:
		return "PR"
	case MRProviderGitLab:
		return "MR"
	default:
		return "链接"
	}
}
