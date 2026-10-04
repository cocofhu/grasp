package services

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/cocofhu/grasp/internal/sandbox"
)

const platformRulesSubdir = "platform-rules"

// RuleSource identifies where a platform rule was loaded from.
type RuleSource string

const (
	RuleSourceOverride RuleSource = "override"
	RuleSourceGlobal   RuleSource = "global"
	RuleSourceEmbed    RuleSource = "embed"
)

// PlatformRuleMeta describes one platform rule file and its effective source.
type PlatformRuleMeta struct {
	File   string     `json:"file"`
	Source RuleSource `json:"source"`
	Mtime  *time.Time `json:"mtime,omitempty"`
}

// PlatformRuleContent is a rule file with content and metadata.
type PlatformRuleContent struct {
	PlatformRuleMeta
	Content string `json:"content"`
}

// PlatformRuleService manages global platform rules and per-agent overrides.
type PlatformRuleService struct {
	globalDir    string
	profilesRoot string
	allowed      map[string]bool
}

// NewPlatformRuleService builds the service and seeds missing global files.
func NewPlatformRuleService(globalDir, profilesRoot string) (*PlatformRuleService, error) {
	files, err := sandbox.EmbeddedRuleBasenames()
	if err != nil {
		return nil, err
	}
	allowed := make(map[string]bool, len(files))
	for _, f := range files {
		allowed[f] = true
	}
	s := &PlatformRuleService{
		globalDir:    globalDir,
		profilesRoot: profilesRoot,
		allowed:      allowed,
	}
	_ = s.Seed()
	return s, nil
}

// Seed writes embedded defaults for any missing global platform-rule files.
func (s *PlatformRuleService) Seed() error {
	if err := os.MkdirAll(s.globalDir, 0o755); err != nil {
		return err
	}
	files, err := s.ruleFiles()
	if err != nil {
		return err
	}
	for _, file := range files {
		dst := filepath.Join(s.globalDir, file)
		if _, err := os.Stat(dst); err == nil {
			continue
		} else if !os.IsNotExist(err) {
			return err
		}
		b, err := sandbox.ReadEmbeddedRule("rules/" + file)
		if err != nil {
			return fmt.Errorf("read embed %s: %w", file, err)
		}
		if err := os.WriteFile(dst, b, 0o644); err != nil {
			return err
		}
	}
	return nil
}

func (s *PlatformRuleService) ruleFiles() ([]string, error) {
	return sandbox.EmbeddedRuleBasenames()
}

func (s *PlatformRuleService) validateFile(file string) error {
	if file == "" || file != filepath.Base(file) || strings.Contains(file, "/") || strings.Contains(file, "\\") || strings.Contains(file, "..") {
		return fmt.Errorf("invalid platform rule file: %s", file)
	}
	if !s.allowed[file] {
		return fmt.Errorf("invalid platform rule file: %s", file)
	}
	return nil
}

// ListGlobal returns metadata for all platform rules using global effective source.
func (s *PlatformRuleService) ListGlobal() ([]PlatformRuleMeta, error) {
	files, err := s.ruleFiles()
	if err != nil {
		return nil, err
	}
	out := make([]PlatformRuleMeta, 0, len(files))
	for _, file := range files {
		meta, err := s.metaForGlobal(file)
		if err != nil {
			return nil, err
		}
		out = append(out, meta)
	}
	return out, nil
}

func (s *PlatformRuleService) metaForGlobal(file string) (PlatformRuleMeta, error) {
	if err := s.validateFile(file); err != nil {
		return PlatformRuleMeta{}, err
	}
	p := filepath.Join(s.globalDir, file)
	if fi, err := os.Stat(p); err == nil {
		t := fi.ModTime()
		return PlatformRuleMeta{File: file, Source: RuleSourceGlobal, Mtime: &t}, nil
	}
	return PlatformRuleMeta{File: file, Source: RuleSourceEmbed}, nil
}

// GetGlobal returns the global effective content for one file.
func (s *PlatformRuleService) GetGlobal(file string) (PlatformRuleContent, error) {
	if err := s.validateFile(file); err != nil {
		return PlatformRuleContent{}, err
	}
	meta, err := s.metaForGlobal(file)
	if err != nil {
		return PlatformRuleContent{}, err
	}
	b, err := s.resolveGlobal(file)
	if err != nil {
		return PlatformRuleContent{}, err
	}
	return PlatformRuleContent{PlatformRuleMeta: meta, Content: string(b)}, nil
}

func (s *PlatformRuleService) resolveGlobal(file string) ([]byte, error) {
	p := filepath.Join(s.globalDir, file)
	if b, err := os.ReadFile(p); err == nil {
		return b, nil
	}
	return sandbox.ReadEmbeddedRule("rules/" + file)
}

// SaveGlobal persists a global platform rule file.
func (s *PlatformRuleService) SaveGlobal(file, content string) (PlatformRuleContent, error) {
	if err := s.validateFile(file); err != nil {
		return PlatformRuleContent{}, err
	}
	if err := os.MkdirAll(s.globalDir, 0o755); err != nil {
		return PlatformRuleContent{}, err
	}
	p := filepath.Join(s.globalDir, file)
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		return PlatformRuleContent{}, err
	}
	return s.GetGlobal(file)
}

// DeleteGlobal removes a customized global file so runtime falls back to embed.
func (s *PlatformRuleService) DeleteGlobal(file string) error {
	if err := s.validateFile(file); err != nil {
		return err
	}
	p := filepath.Join(s.globalDir, file)
	if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// ResetGlobal overwrites the global file with the embedded default.
func (s *PlatformRuleService) ResetGlobal(file string) (PlatformRuleContent, error) {
	if err := s.validateFile(file); err != nil {
		return PlatformRuleContent{}, err
	}
	b, err := sandbox.ReadEmbeddedRule("rules/" + file)
	if err != nil {
		return PlatformRuleContent{}, err
	}
	if err := os.MkdirAll(s.globalDir, 0o755); err != nil {
		return PlatformRuleContent{}, err
	}
	p := filepath.Join(s.globalDir, file)
	if err := os.WriteFile(p, []byte(b), 0o644); err != nil {
		return PlatformRuleContent{}, err
	}
	return s.GetGlobal(file)
}

// ReadEmbedDefault returns the built-in embedded default for one file.
func (s *PlatformRuleService) ReadEmbedDefault(file string) (PlatformRuleContent, error) {
	if err := s.validateFile(file); err != nil {
		return PlatformRuleContent{}, err
	}
	b, err := sandbox.ReadEmbeddedRule("rules/" + file)
	if err != nil {
		return PlatformRuleContent{}, err
	}
	return PlatformRuleContent{
		PlatformRuleMeta: PlatformRuleMeta{File: file, Source: RuleSourceEmbed},
		Content:          string(b),
	}, nil
}

func (s *PlatformRuleService) agentOverridePath(agent, file string) string {
	return filepath.Join(s.profilesRoot, sanitize(agent), platformRulesSubdir, file)
}

// ListAgent returns metadata for all platform rules for one agent.
func (s *PlatformRuleService) ListAgent(agent string) ([]PlatformRuleMeta, error) {
	if agent == "" {
		return nil, errors.New("agent name required")
	}
	files, err := s.ruleFiles()
	if err != nil {
		return nil, err
	}
	out := make([]PlatformRuleMeta, 0, len(files))
	for _, file := range files {
		meta, err := s.metaForAgent(agent, file)
		if err != nil {
			return nil, err
		}
		out = append(out, meta)
	}
	return out, nil
}

func (s *PlatformRuleService) metaForAgent(agent, file string) (PlatformRuleMeta, error) {
	if err := s.validateFile(file); err != nil {
		return PlatformRuleMeta{}, err
	}
	p := s.agentOverridePath(agent, file)
	if fi, err := os.Stat(p); err == nil {
		t := fi.ModTime()
		return PlatformRuleMeta{File: file, Source: RuleSourceOverride, Mtime: &t}, nil
	}
	return s.metaForGlobal(file)
}

// GetAgent returns the effective content for one agent rule file.
func (s *PlatformRuleService) GetAgent(agent, file string) (PlatformRuleContent, error) {
	if agent == "" {
		return PlatformRuleContent{}, errors.New("agent name required")
	}
	if err := s.validateFile(file); err != nil {
		return PlatformRuleContent{}, err
	}
	meta, err := s.metaForAgent(agent, file)
	if err != nil {
		return PlatformRuleContent{}, err
	}
	b, err := s.resolveAgent(agent, file)
	if err != nil {
		return PlatformRuleContent{}, err
	}
	return PlatformRuleContent{PlatformRuleMeta: meta, Content: string(b)}, nil
}

func (s *PlatformRuleService) resolveAgent(agent, file string) ([]byte, error) {
	p := s.agentOverridePath(agent, file)
	if b, err := os.ReadFile(p); err == nil {
		return b, nil
	}
	return s.resolveGlobal(file)
}

// SaveAgent creates or updates an agent platform-rule override.
func (s *PlatformRuleService) SaveAgent(agent, file, content string) (PlatformRuleContent, error) {
	if agent == "" {
		return PlatformRuleContent{}, errors.New("agent name required")
	}
	if err := s.validateFile(file); err != nil {
		return PlatformRuleContent{}, err
	}
	dir := filepath.Join(s.profilesRoot, sanitize(agent), platformRulesSubdir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return PlatformRuleContent{}, err
	}
	p := filepath.Join(dir, file)
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		return PlatformRuleContent{}, err
	}
	return s.GetAgent(agent, file)
}

// DeleteAgent removes an agent override so the global/embed chain applies.
func (s *PlatformRuleService) DeleteAgent(agent, file string) error {
	if agent == "" {
		return errors.New("agent name required")
	}
	if err := s.validateFile(file); err != nil {
		return err
	}
	p := s.agentOverridePath(agent, file)
	if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}
