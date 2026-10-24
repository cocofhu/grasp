package services

import (
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/cocofhu/grasp/internal/modelrouting"
	"github.com/cocofhu/grasp/internal/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// MigrateLegacyConnections adds an idempotent connection for each historical
// model credential. It never changes/deletes its secret or original metadata.
// Missing endpoint information remains visible as needs_configuration.
func (s *ModelConnectionService) MigrateLegacyConnections() error {
	var rows []models.Credential
	if err := s.db.Where("type = ? AND provider IN ?", "ai", []string{"opencode", "deepseek_harness"}).Find(&rows).Error; err != nil {
		return err
	}
	for _, credential := range rows {
		if credential.BuiltinSlot != "" && credential.ValueEnc == "" && credential.SecretRevision <= 1 {
			continue
		}
		connection := legacyConnection(credential)
		if connection == nil {
			continue
		}
		if err := s.db.Transaction(func(tx *gorm.DB) error {
			var count int64
			if err := tx.Model(&models.ModelRoutingMigration{}).Where("credential_id = ?", credential.ID).Count(&count).Error; err != nil {
				return err
			}
			if count != 0 {
				return nil
			}
			row := models.ModelConnection(*connection)
			if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&row).Error; err != nil {
				return err
			}
			return tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&models.ModelRoutingMigration{CredentialID: credential.ID, ConnectionID: connection.ID, CreatedAt: time.Now()}).Error
		}); err != nil {
			return err
		}
	}
	return nil
}

// OpenCode remains on its original catalog adapter: its legacy metadata does
// not identify a wire protocol or require a base URL. Only DSH's fixed native
// route is sufficiently explicit to migrate automatically.
func legacyConnection(credential models.Credential) *modelrouting.Connection {
	if credential.Provider != "deepseek_harness" {
		return nil
	}
	vision, _ := credential.Metadata["vision"].(bool)
	input := []string{"text"}
	if vision {
		input = append(input, "image")
	}
	catalog := []modelrouting.Model{
		{ID: "deepseek-flash", Source: "manual", Input: input, Capabilities: modelrouting.ModelCapabilities{Vision: &vision}},
		{ID: "deepseek-v4-pro", Source: "manual", Input: []string{"text", "image"}},
	}
	state := "active"
	if credential.ValueEnc == "" {
		state = "needs_configuration"
	}
	legacyID := credential.ID
	return &modelrouting.Connection{ID: "conn-legacy-" + credential.ID, Name: credential.Name, ProviderID: "deepseek", Protocol: modelrouting.ProtocolAnthropic, BaseURL: "https://api.deepseek.com/anthropic", CredentialID: credential.ID, Auth: modelrouting.Auth{Scheme: "x-api-key"}, Models: catalog, State: state, Revision: 1, LegacyCredentialID: &legacyID, CreatedAt: time.Now(), UpdatedAt: time.Now()}
}

// LegacyBinding maps only the explicitly selected legacy credential. It does
// not choose another key when that reference is missing or cleared.
func (s *ModelConnectionService) LegacyBinding(backend, aiCredentialID, openCodeCredentialID string, env map[string]string) (*modelrouting.Binding, error) {
	// Keep the original native-provider path until the user explicitly selects
	// a new model binding. That path validates the exact legacy credential ID.
	if backend == "opencode" {
		return nil, nil
	}
	id := strings.TrimSpace(aiCredentialID)
	if id == "" {
		return nil, nil
	}
	model := strings.TrimSpace(env["ACP_BRIDGE_MODEL"])
	if backend != "opencode" && backend != "deepseek_harness" {
		return &modelrouting.Binding{Kind: "platform", CredentialID: id, ModelID: model}, nil
	}
	if err := s.MigrateLegacyConnections(); err != nil {
		return &modelrouting.Binding{Kind: "connection", ConnectionID: "conn-legacy-" + id, ModelID: model}, err
	}
	connection, err := s.Get("conn-legacy-" + id)
	if err != nil {
		if errors.Is(err, ErrModelConnectionNotFound) {
			return &modelrouting.Binding{Kind: "connection", ConnectionID: "conn-legacy-" + id, ModelID: model}, nil
		}
		return nil, err
	}

	settings := modelrouting.ToolSettings{}
	if backend == "deepseek_harness" {
		settings.RouteProfile = "deepseek-native"
		var tuple []string
		if json.Unmarshal([]byte(model), &tuple) == nil && len(tuple) == 2 {
			model = tuple[1]
		}
		model = strings.TrimPrefix(model, "deepseek-official/")
		if model == "" || model == "auto" {
			model = "deepseek-flash"
		}
	}
	return &modelrouting.Binding{Kind: "connection", ConnectionID: connection.ID, ModelID: model, ToolSettings: settings}, nil
}
