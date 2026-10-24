package services

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/cocofhu/grasp/internal/crypto"
	"github.com/cocofhu/grasp/internal/modelrouting"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

var (
	ErrModelConnectionNotFound = errors.New("model connection not found")
	ErrModelConnectionInvalid  = errors.New("invalid model connection")
	ErrModelBindingInvalid     = errors.New("invalid model binding")
)

type ModelConnectionService struct{ db *gorm.DB }

func NewModelConnectionService(db *gorm.DB) *ModelConnectionService {
	return &ModelConnectionService{db: db}
}

func (s *ModelConnectionService) List() ([]modelrouting.Connection, error) {
	var rows []models.ModelConnection
	if err := s.db.Order("created_at desc").Find(&rows).Error; err != nil {
		return nil, err
	}
	connections := make([]modelrouting.Connection, 0, len(rows))
	for _, row := range rows {
		connections = append(connections, modelrouting.Connection(row))
	}
	return connections, nil
}

func (s *ModelConnectionService) Get(id string) (modelrouting.Connection, error) {
	var row models.ModelConnection
	if err := s.db.First(&row, "id = ?", strings.TrimSpace(id)).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return modelrouting.Connection{}, ErrModelConnectionNotFound
		}
		return modelrouting.Connection{}, err
	}
	return modelrouting.Connection(row), nil
}

func (s *ModelConnectionService) Create(connection modelrouting.Connection) (modelrouting.Connection, error) {
	connection.ID = "conn-" + uuid.NewString()[:12]
	connection.Revision = 1
	connection.CreatedAt, connection.UpdatedAt = time.Now(), time.Now()
	if err := s.validateConnection(&connection); err != nil {
		return modelrouting.Connection{}, err
	}
	row := models.ModelConnection(connection)
	if err := s.db.Create(&row).Error; err != nil {
		return modelrouting.Connection{}, err
	}
	return connection, nil
}

func (s *ModelConnectionService) Update(id string, connection modelrouting.Connection) (modelrouting.Connection, error) {
	current, err := s.Get(id)
	if err != nil {
		return modelrouting.Connection{}, err
	}
	connection.ID, connection.CreatedAt = current.ID, current.CreatedAt
	connection.Revision, connection.UpdatedAt = current.Revision+1, time.Now()
	if err := s.validateConnection(&connection); err != nil {
		return modelrouting.Connection{}, err
	}
	row := models.ModelConnection(connection)
	if err := s.db.Save(&row).Error; err != nil {
		return modelrouting.Connection{}, err
	}
	return connection, nil
}

func (s *ModelConnectionService) Delete(id string) error {
	result := s.db.Delete(&models.ModelConnection{}, "id = ?", strings.TrimSpace(id))
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrModelConnectionNotFound
	}
	return nil
}

func (s *ModelConnectionService) validateConnection(c *modelrouting.Connection) error {
	c.Name, c.ProviderID = strings.TrimSpace(c.Name), strings.TrimSpace(c.ProviderID)
	c.BaseURL = strings.TrimRight(strings.TrimSpace(c.BaseURL), "/")
	c.CredentialID = strings.TrimSpace(c.CredentialID)
	if c.Name == "" || c.ProviderID == "" {
		return fmt.Errorf("%w: name and providerId are required", ErrModelConnectionInvalid)
	}
	if c.Protocol != modelrouting.ProtocolChat && c.Protocol != modelrouting.ProtocolResponses && c.Protocol != modelrouting.ProtocolAnthropic {
		return fmt.Errorf("%w: unsupported protocol", ErrModelConnectionInvalid)
	}
	u, err := url.Parse(c.BaseURL)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return fmt.Errorf("%w: baseUrl must be an HTTP(S) base URL without credentials, query or fragment", ErrModelConnectionInvalid)
	}
	if c.Auth.Scheme == "" {
		c.Auth.Scheme = "bearer"
		if c.Protocol == modelrouting.ProtocolAnthropic {
			c.Auth.Scheme = "x-api-key"
		}
	}
	if c.Auth.Scheme != "bearer" && c.Auth.Scheme != "x-api-key" {
		return fmt.Errorf("%w: unsupported authentication scheme", ErrModelConnectionInvalid)
	}
	if c.Auth.HeaderName != "" {
		return fmt.Errorf("%w: custom authentication headers are not supported", ErrModelConnectionInvalid)
	}
	if c.State == "" {
		c.State = "active"
	}
	if c.State != "active" && c.State != "disabled" {
		return fmt.Errorf("%w: unsupported state", ErrModelConnectionInvalid)
	}
	if _, err := s.modelCredential(c.CredentialID); err != nil {
		return fmt.Errorf("%w: %w", ErrModelConnectionInvalid, err)
	}
	if c.Models == nil {
		c.Models = []modelrouting.Model{}
	}
	seen := map[string]bool{}
	for i := range c.Models {
		model := &c.Models[i]
		model.ID, model.Name = strings.TrimSpace(model.ID), strings.TrimSpace(model.Name)
		if model.ID == "" || seen[model.ID] {
			return fmt.Errorf("%w: model IDs must be nonempty and unique", ErrModelConnectionInvalid)
		}
		seen[model.ID] = true
		if model.Source == "" {
			model.Source = "manual"
		}
		if model.Source != "manual" && model.Source != "discovered" {
			return fmt.Errorf("%w: unsupported model source", ErrModelConnectionInvalid)
		}
		if model.ContextWindow < 0 || model.MaxTokens < 0 {
			return fmt.Errorf("%w: model token limits must be positive", ErrModelConnectionInvalid)
		}
		for _, input := range model.Input {
			if input != "text" && input != "image" {
				return fmt.Errorf("%w: unsupported model input", ErrModelConnectionInvalid)
			}
		}
	}
	return nil
}

func (s *ModelConnectionService) modelCredential(id string) (models.Credential, error) {
	var credential models.Credential
	if err := s.db.First(&credential, "id = ?", strings.TrimSpace(id)).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return credential, ErrCredentialNotFound
		}
		return credential, err
	}
	purpose, _, _, _ := credentialDomain(credential)
	if !currentCredential(credential) || purpose != "model_api" {
		return credential, fmt.Errorf("model connection requires a model_api credential")
	}
	return credential, nil
}

// Resolve expands a named configuration or validates its concrete internal source.
// No key, model or connection fallback occurs here.
func (s *ModelConnectionService) Resolve(backend string, binding modelrouting.Binding) (modelrouting.Resolved, error) {
	resolved := modelrouting.Resolved{Backend: backend, Binding: binding}
	if binding.Kind == "configuration" {
		if binding.ConfigurationID == "" || binding.CredentialID != "" || binding.ConnectionID != "" || binding.ModelID != "" || len(binding.ToolSettings.ModelRoles) != 0 || binding.ToolSettings.RouteProfile != "" {
			return resolved, fmt.Errorf("%w: configuration binding requires only configurationId", ErrModelBindingInvalid)
		}
		configuration, err := NewModelConfigurationService(s.db).Get(binding.ConfigurationID)
		if err != nil {
			return resolved, fmt.Errorf("%w: %w", ErrModelBindingInvalid, err)
		}
		if configuration.State != "active" || configuration.Backend != backend {
			return resolved, fmt.Errorf("%w: model configuration is disabled or belongs to a different coding tool", ErrModelBindingInvalid)
		}
		if configuration.Binding.Kind != "platform" && configuration.Binding.Kind != "connection" {
			return resolved, fmt.Errorf("%w: model configuration must contain a concrete binding", ErrModelBindingInvalid)
		}
		concrete, err := s.Resolve(backend, configuration.Binding)
		return concrete, err
	}
	var credential models.Credential
	var err error
	if binding.Kind == "connection" {
		connection, getErr := s.Get(binding.ConnectionID)
		if getErr != nil {
			return resolved, getErr
		}
		resolved.Connection = &connection
		if connection.State != "active" {
			return resolved, fmt.Errorf("%w: connection is not active", ErrModelBindingInvalid)
		}
		credential, err = s.modelCredential(connection.CredentialID)
	} else if binding.Kind == "platform" {
		err = s.db.First(&credential, "id = ?", binding.CredentialID).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			err = ErrCredentialNotFound
		}
		if err == nil {
			purpose, _, tool, _ := credentialDomain(credential)
			if !currentCredential(credential) || purpose != "tool_auth" || tool != backend {
				err = fmt.Errorf("platform credential does not match coding tool")
			}
		}
	} else {
		return resolved, fmt.Errorf("%w: concrete binding is required", ErrModelBindingInvalid)
	}
	if err != nil {
		return resolved, fmt.Errorf("%w: %w", ErrModelBindingInvalid, err)
	}
	if err := modelrouting.ValidateBinding(backend, binding, resolved.Connection); err != nil {
		return resolved, fmt.Errorf("%w: %w", ErrModelBindingInvalid, err)
	}
	if resolved.Connection != nil {
		if err := validateResolvedModels(binding, *resolved.Connection); err != nil {
			return resolved, err
		}
	}
	if !credential.Enabled || credential.RevokedAt != nil || strings.TrimSpace(credential.ValueEnc) == "" {
		return resolved, fmt.Errorf("%w: credential is cleared, revoked or disabled", ErrModelBindingInvalid)
	}
	secret, err := crypto.Decrypt(credential.ValueEnc)
	if err != nil || strings.TrimSpace(secret) == "" {
		return resolved, fmt.Errorf("%w: credential could not be decrypted", ErrModelBindingInvalid)
	}
	_, _, _, resolved.SecretKind = credentialDomain(credential)
	if binding.Kind == "connection" || backend != "codex" {
		if resolved.SecretKind != "api_key" && resolved.SecretKind != "token" {
			return resolved, fmt.Errorf("%w: credential has an incompatible secret kind", ErrModelBindingInvalid)
		}
	} else if resolved.SecretKind != "login_file" {
		return resolved, fmt.Errorf("%w: Codex requires a login file", ErrModelBindingInvalid)
	}
	resolved.CredentialID, resolved.SecretRevision, resolved.Secret = credential.ID, credential.SecretRevision, secret
	return resolved, nil
}

func validateResolvedModels(binding modelrouting.Binding, connection modelrouting.Connection) error {
	modelsByID := map[string]modelrouting.Model{}
	for _, model := range connection.Models {
		modelsByID[model.ID] = model
	}
	selected, exists := modelsByID[binding.ModelID]
	if !exists {
		return fmt.Errorf("%w: selected model is absent from this connection", ErrModelBindingInvalid)
	}
	if selected.Capabilities.ToolCalls != nil && !*selected.Capabilities.ToolCalls {
		return fmt.Errorf("%w: selected model does not support tools", ErrModelBindingInvalid)
	}
	for _, modelID := range binding.ToolSettings.ModelRoles {
		if _, exists := modelsByID[modelID]; !exists {
			return fmt.Errorf("%w: role model is absent from this connection", ErrModelBindingInvalid)
		}
	}
	return nil
}
