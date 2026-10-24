package service

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"
	"time"

	"backend/internal/agents"
	"backend/internal/provider"
	"backend/internal/provider/modelconnection"
)

// AgentModel 对应模型目录中的一项（provider 目录或 CLI 探测所得）。
type AgentModel struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	IsDefault bool   `json:"isDefault,omitempty"`
}

// ListAgentModels 返回当前 provider 的模型目录：优先用 provider 声明的目录，
// 其次对 cursor 走 `cursor-agent --list-models` 探测，其余无目录时返回空（=auto）。
func ListAgentModels() ([]AgentModel, error) {
	p := agents.Current()
	ctxModels, cancelModels := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancelModels()
	if catalog, err := p.ListModels(ctxModels); err != nil {
		if strings.TrimSpace(os.Getenv(modelconnection.EnvConfig)) != "" {
			return nil, err
		}
		log.Printf("acp-bridge: provider %s ListModels 失败: %v", p.Name(), err)
	} else if len(catalog) > 0 {
		out := make([]AgentModel, 0, len(catalog))
		for _, m := range catalog {
			out = append(out, AgentModel{ID: m.ID, Name: m.Name, IsDefault: m.IsDefault})
		}
		return out, nil
	}
	if p.Name() != provider.Cursor {
		// 无静态目录且非 cursor：交由 CLI 自行决定模型（auto）。
		return []AgentModel{}, nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "cursor-agent", "--list-models")
	out, err := cmd.CombinedOutput()
	if err != nil {
		log.Printf("acp-bridge: cursor-agent --list-models 失败: %v (output=%d bytes)", err, len(out))
		if len(out) > 0 {
			models := parseListModels(out)
			if len(models) > 0 {
				return models, nil
			}
		}
		return nil, fmt.Errorf("cursor-agent --list-models: %w", err)
	}
	models := parseListModels(out)
	log.Printf("acp-bridge: cursor-agent --list-models 解析到 %d 个模型", len(models))
	return models, nil
}

func parseListModels(data []byte) []AgentModel {
	var models []AgentModel
	sc := bufio.NewScanner(bytes.NewReader(data))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || !strings.Contains(line, " - ") {
			continue
		}
		parts := strings.SplitN(line, " - ", 2)
		id := strings.TrimSpace(parts[0])
		name := strings.TrimSpace(parts[1])
		if id == "" {
			continue
		}
		m := AgentModel{ID: id, Name: name}
		if strings.HasSuffix(name, "(default)") {
			m.IsDefault = true
			m.Name = strings.TrimSpace(strings.TrimSuffix(name, "(default)"))
		}
		models = append(models, m)
	}
	return models
}
