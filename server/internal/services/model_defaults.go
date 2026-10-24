package services

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/cocofhu/grasp/internal/modelrouting"
)

// ModelDefaults is independent of the currently selected shared coding tool.
// Inheriting always selects one complete binding for the Agent's own backend.
type ModelDefaults struct {
	BindingsByBackend map[string]modelrouting.Binding `json:"bindingsByBackend"`
}

func (s *SharedAgentService) GetModelDefaults() (ModelDefaults, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.readModelDefaults()
}

func (s *SharedAgentService) readModelDefaults() (ModelDefaults, error) {
	cfg := ModelDefaults{BindingsByBackend: map[string]modelrouting.Binding{}}
	body, err := os.ReadFile(filepath.Join(s.root, "model-defaults.json"))
	if errors.Is(err, os.ErrNotExist) {
		return cfg, nil
	}
	if err != nil {
		return cfg, err
	}
	if err = json.Unmarshal(body, &cfg); err != nil {
		return cfg, fmt.Errorf("read workspace model defaults: %w", err)
	}
	if cfg.BindingsByBackend == nil {
		cfg.BindingsByBackend = map[string]modelrouting.Binding{}
	}
	return cfg, nil
}

// SaveModelDefaults performs a full replacement after validating every binding.
// The file contains references only, never plaintext or encrypted secret bodies.
func (s *SharedAgentService) SaveModelDefaults(cfg ModelDefaults, validate func(string, *modelrouting.Binding) error) error {
	for backend, binding := range cfg.BindingsByBackend {
		if binding.Kind == "inherit" {
			return fmt.Errorf("workspace default for %s cannot inherit itself", backend)
		}
		if validate == nil {
			return fmt.Errorf("model defaults validator unavailable")
		}
		if err := validate(backend, &binding); err != nil {
			return fmt.Errorf("%s: %w", backend, err)
		}
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err = os.MkdirAll(s.root, 0o755); err != nil {
		return err
	}
	file, err := os.CreateTemp(s.root, ".model-defaults-*")
	if err != nil {
		return err
	}
	path := file.Name()
	defer func() { _ = os.Remove(path) }()
	if _, err = file.Write(data); err != nil {
		_ = file.Close()
		return err
	}
	if err = file.Close(); err != nil {
		return err
	}
	return os.Rename(path, filepath.Join(s.root, "model-defaults.json"))
}

func (s *SharedAgentService) ConcreteModelBinding(backend string, binding *modelrouting.Binding) (*modelrouting.Binding, error) {
	if binding == nil {
		return nil, nil
	}
	if binding.Kind != "inherit" {
		copy := *binding
		return &copy, nil
	}
	cfg, err := s.GetModelDefaults()
	if err != nil {
		return nil, err
	}
	selected, ok := cfg.BindingsByBackend[backend]
	if !ok || selected.Kind == "inherit" {
		return nil, fmt.Errorf("%s has no workspace model default", backend)
	}
	return &selected, nil
}
