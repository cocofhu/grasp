package service

import (
	"context"
	"log"
	"os"
	"strings"
	"time"

	"backend/internal/agents"
	"backend/internal/provider/modelconnection"
)

// AgentModel 对应模型目录中的一项。
type AgentModel struct {
	ID               string   `json:"id"`
	Name             string   `json:"name"`
	Vendor           string   `json:"vendor,omitempty"`
	IsDefault        bool     `json:"isDefault,omitempty"`
	Unavailable      bool     `json:"unavailable,omitempty"`
	ReasoningEfforts []string `json:"reasoningEfforts,omitempty"`
	ServiceTiers     []string `json:"serviceTiers,omitempty"`
}

// ListAgentModels 返回当前 provider 的模型目录；没有目录时返回空（=auto，交由 CLI 自行决定）。
func ListAgentModels() ([]AgentModel, error) {
	p := agents.Current()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	catalog, err := p.ListModels(ctx)
	if err != nil {
		if strings.TrimSpace(os.Getenv(modelconnection.EnvConfig)) != "" {
			return nil, err
		}
		log.Printf("acp-bridge: provider %s ListModels 失败: %v", p.Name(), err)
		return []AgentModel{}, nil
	}
	out := make([]AgentModel, 0, len(catalog))
	for _, m := range catalog {
		out = append(out, AgentModel{ID: m.ID, Name: m.Name, IsDefault: m.IsDefault})
	}
	return out, nil
}
