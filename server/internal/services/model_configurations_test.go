package services

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/cocofhu/grasp/internal/modelrouting"
	"github.com/cocofhu/grasp/internal/models"
	"gorm.io/gorm"
)

func newModelConfigurationFixture(t *testing.T) (*ModelConnectionService, *ModelConfigurationService, CredentialView, modelrouting.Connection, modelrouting.Configuration) {
	t.Helper()
	setCredentialKey(t)
	db := newTestDB(t)
	connections, configurations := NewModelConnectionService(db), NewModelConfigurationService(db)
	key := newModelKey(t, NewCredentialService(db), "Saved model key")
	connection, err := connections.Create(connectionInput(key.ID))
	if err != nil {
		t.Fatal(err)
	}
	configuration, err := configurations.Create(modelrouting.Configuration{Name: "  OpenCode team  ", Backend: "opencode", Binding: modelrouting.Binding{Kind: "connection", ConnectionID: connection.ID, ModelID: connection.Models[0].ID}})
	if err != nil {
		t.Fatal(err)
	}
	return connections, configurations, key, connection, configuration
}

func TestModelConfigurationLifecycleAndMetadata(t *testing.T) {
	connections, configurations, key, connection, configuration := newModelConfigurationFixture(t)
	if configuration.Name != "OpenCode team" || configuration.State != "active" || configuration.Revision != 1 || configuration.CreatedAt.IsZero() {
		t.Fatalf("invalid saved configuration: %+v", configuration)
	}
	items, err := configurations.List()
	if err != nil || len(items) != 1 || items[0].Binding.ConnectionID != connection.ID {
		t.Fatalf("list: %+v %v", items, err)
	}
	encoded, err := json.Marshal(items)
	if err != nil || strings.Contains(string(encoded), "private-model-key") || strings.Contains(string(encoded), "valueEnc") || strings.Contains(string(encoded), `"secret"`) {
		t.Fatalf("configuration contains a secret: %s %v", encoded, err)
	}
	second, err := connections.Create(connectionInput(key.ID))
	if err != nil {
		t.Fatal(err)
	}
	configuration.Name, configuration.Binding.ConnectionID = "Updated configuration", second.ID
	configuration.Binding.ToolSettings.ModelRoles = map[string]string{"fast": second.Models[0].ID}
	updated, err := configurations.Update(configuration.ID, configuration)
	if err != nil || updated.Revision != 2 || updated.Binding.ConnectionID != second.ID || !updated.CreatedAt.Equal(configuration.CreatedAt) {
		t.Fatalf("update: %+v %v", updated, err)
	}
	stored, err := configurations.Get(configuration.ID)
	if err != nil || stored.Binding.ToolSettings.ModelRoles["fast"] != second.Models[0].ID {
		t.Fatalf("read persisted binding: %+v %v", stored, err)
	}
	configuration.Backend = "codebuddy"
	if _, err := configurations.Update(configuration.ID, configuration); !errors.Is(err, ErrModelConfigurationInvalid) {
		t.Fatalf("coding tool changed: %v", err)
	}
	stored.Backend, stored.State = "", "disabled"
	disabled, err := configurations.Update(stored.ID, stored)
	if err != nil || disabled.Backend != "opencode" || disabled.State != "disabled" || disabled.Revision != 3 {
		t.Fatalf("disable: %+v %v", disabled, err)
	}
	if err := configurations.Delete(configuration.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := configurations.Get(configuration.ID); !errors.Is(err, ErrModelConfigurationNotFound) {
		t.Fatalf("get deleted: %v", err)
	}
	if err := configurations.Delete(configuration.ID); !errors.Is(err, ErrModelConfigurationNotFound) {
		t.Fatalf("delete missing: %v", err)
	}
	if _, err := configurations.Update(configuration.ID, configuration); !errors.Is(err, ErrModelConfigurationNotFound) {
		t.Fatalf("update missing: %v", err)
	}
	items, err = configurations.List()
	if err != nil || items == nil || len(items) != 0 {
		t.Fatalf("empty list must remain an array: %+v %v", items, err)
	}
}

func TestModelConfigurationRequiresConcreteValidSource(t *testing.T) {
	_, configurations, _, connection, configuration := newModelConfigurationFixture(t)
	for name, mutate := range map[string]func(*modelrouting.Configuration){
		"missing name":    func(c *modelrouting.Configuration) { c.Name = " " },
		"missing backend": func(c *modelrouting.Configuration) { c.Backend = "" },
		"unknown backend": func(c *modelrouting.Configuration) { c.Backend = "unknown" },
		"invalid state":   func(c *modelrouting.Configuration) { c.State = "deleted" },
		"inherit":         func(c *modelrouting.Configuration) { c.Binding = modelrouting.Binding{Kind: "inherit"} },
		"nested configuration": func(c *modelrouting.Configuration) {
			c.Binding = modelrouting.Binding{Kind: "configuration", ConfigurationID: configuration.ID}
		},
		"mixed reference":    func(c *modelrouting.Configuration) { c.Binding.ConfigurationID = configuration.ID },
		"all configurations": func(c *modelrouting.Configuration) { c.Binding.AllConfigurations = true },
		"missing connection": func(c *modelrouting.Configuration) { c.Binding.ConnectionID = "absent" },
		"missing model":      func(c *modelrouting.Configuration) { c.Binding.ModelID = "absent/model" },
		"incompatible tool":  func(c *modelrouting.Configuration) { c.Backend = "cursor" },
	} {
		t.Run(name, func(t *testing.T) {
			candidate := modelrouting.Configuration{Name: "Candidate", Backend: "opencode", Binding: modelrouting.Binding{Kind: "connection", ConnectionID: connection.ID, ModelID: connection.Models[0].ID}}
			mutate(&candidate)
			if _, err := configurations.Create(candidate); !errors.Is(err, ErrModelConfigurationInvalid) {
				t.Fatalf("invalid configuration accepted: %v", err)
			}
		})
	}
	if items, err := configurations.List(); err != nil || len(items) != 1 {
		t.Fatalf("failed creates changed saved data: %+v %v", items, err)
	}
}

func TestModelConfigurationResolvesConcreteBindingAndCurrentSecret(t *testing.T) {
	connections, configurations, key, connection, configuration := newModelConfigurationFixture(t)
	ref := modelrouting.Binding{Kind: "configuration", ConfigurationID: configuration.ID}
	resolved, err := connections.Resolve("opencode", ref)
	if err != nil || resolved.Binding.Kind != "connection" || resolved.Binding.ConnectionID != connection.ID || resolved.Secret != "private-model-key" {
		t.Fatalf("configuration was not expanded: %+v %v", resolved, err)
	}
	credentials := NewCredentialService(configurations.db)
	if _, err := credentials.Update(key.ID, CredentialInput{Value: "rotated-model-key"}); err != nil {
		t.Fatal(err)
	}
	resolved, err = connections.Resolve("opencode", ref)
	if err != nil || resolved.Secret != "rotated-model-key" || resolved.SecretRevision != 2 {
		t.Fatalf("configuration cached an old secret: %+v %v", resolved, err)
	}
	platform, err := credentials.Create(CredentialInput{Purpose: "tool_auth", ToolID: "cursor", Name: "Cursor platform", Value: "cursor-platform-key"})
	if err != nil {
		t.Fatal(err)
	}
	platformConfig, err := configurations.Create(modelrouting.Configuration{Name: "Cursor tool default", Backend: "cursor", Binding: modelrouting.Binding{Kind: "platform", CredentialID: platform.ID}})
	if err != nil {
		t.Fatal("platform modelId may be empty to explicitly select the tool default", err)
	}
	resolved, err = connections.Resolve("cursor", modelrouting.Binding{Kind: "configuration", ConfigurationID: platformConfig.ID})
	if err != nil || resolved.Binding.Kind != "platform" || resolved.CredentialID != platform.ID || resolved.Secret != "cursor-platform-key" {
		t.Fatalf("platform configuration: %+v %v", resolved, err)
	}
}

func TestModelConfigurationFailsClosedAfterReferencesChange(t *testing.T) {
	for _, reason := range []string{"disabled", "deleted", "different tool", "cleared key", "disabled connection", "deleted connection", "removed model", "corrupt recursive binding", "mixed reference"} {
		t.Run(reason, func(t *testing.T) {
			connections, configurations, key, connection, configuration := newModelConfigurationFixture(t)
			ref := modelrouting.Binding{Kind: "configuration", ConfigurationID: configuration.ID}
			backend := "opencode"
			var err error
			switch reason {
			case "disabled":
				configuration.State = "disabled"
				_, err = configurations.Update(configuration.ID, configuration)
			case "deleted":
				err = configurations.Delete(configuration.ID)
			case "different tool":
				backend = "codebuddy"
			case "cleared key":
				err = NewCredentialService(configurations.db).Clear(key.ID)
			case "disabled connection":
				connection.State = "disabled"
				_, err = connections.Update(connection.ID, connection)
			case "deleted connection":
				err = connections.Delete(connection.ID)
			case "removed model":
				connection.Models = nil
				_, err = connections.Update(connection.ID, connection)
			case "corrupt recursive binding":
				configuration.Binding = ref
				row := models.ModelConfiguration(configuration)
				err = configurations.db.Save(&row).Error
			case "mixed reference":
				ref.ConnectionID = connection.ID
			}
			if err != nil {
				t.Fatal(err)
			}
			resolved, err := connections.Resolve(backend, ref)
			if err == nil || resolved.Secret != "" {
				t.Fatalf("invalid reference did not fail closed: %+v %v", resolved, err)
			}
			if reason != "deleted" && reason != "corrupt recursive binding" {
				if _, err := configurations.Get(configuration.ID); err != nil {
					t.Fatal("metadata must remain readable when references become invalid", err)
				}
			}
		})
	}
}

func TestModelConfigurationAuxiliaryModels(t *testing.T) {
	connections, configurations, _, connection, _ := newModelConfigurationFixture(t)
	connection.Models = append(connection.Models, modelrouting.Model{ID: "vendor/aux-b", Source: "manual"}, modelrouting.Model{ID: "vendor/aux-a", Source: "manual"})
	updatedConnection, err := connections.Update(connection.ID, connection)
	if err != nil {
		t.Fatal(err)
	}
	saved, err := configurations.Create(modelrouting.Configuration{
		Name: "With auxiliaries", Backend: "opencode",
		Binding: modelrouting.Binding{Kind: "connection", ConnectionID: updatedConnection.ID, ModelID: " " + updatedConnection.Models[0].ID + " ", AuxiliaryModelIDs: []string{" vendor/aux-a ", "vendor/aux-b"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if saved.Binding.ModelID != updatedConnection.Models[0].ID || len(saved.Binding.AuxiliaryModelIDs) != 2 || saved.Binding.AuxiliaryModelIDs[0] != "vendor/aux-a" {
		t.Fatalf("stored order: %+v", saved.Binding)
	}
	stored, err := configurations.Get(saved.ID)
	if err != nil || len(stored.Binding.AuxiliaryModelIDs) != 2 {
		t.Fatalf("read: %+v %v", stored.Binding, err)
	}
	for _, bad := range []modelrouting.Binding{
		{Kind: "connection", ConnectionID: updatedConnection.ID, ModelID: updatedConnection.Models[0].ID, AuxiliaryModelIDs: []string{updatedConnection.Models[0].ID}},
		{Kind: "connection", ConnectionID: updatedConnection.ID, ModelID: updatedConnection.Models[0].ID, AuxiliaryModelIDs: []string{"vendor/aux-a", "vendor/aux-a"}},
		{Kind: "connection", ConnectionID: updatedConnection.ID, ModelID: updatedConnection.Models[0].ID, AuxiliaryModelIDs: []string{"missing/aux"}},
		{Kind: "connection", ConnectionID: updatedConnection.ID, AuxiliaryModelIDs: []string{"vendor/aux-a"}},
		{Kind: "connection", ConnectionID: updatedConnection.ID, ModelID: updatedConnection.Models[0].ID, AuxiliaryModelIDs: []string{" "}},
	} {
		if _, err := configurations.Create(modelrouting.Configuration{Name: "Bad", Backend: "opencode", Binding: bad}); !errors.Is(err, ErrModelConfigurationInvalid) {
			t.Fatalf("accepted %+v: %v", bad, err)
		}
	}
	credentials := NewCredentialService(configurations.db)
	platform, err := credentials.Create(CredentialInput{Purpose: "tool_auth", ToolID: "cursor", Name: "Cursor", Value: "cursor-key"})
	if err != nil {
		t.Fatal(err)
	}
	auto, err := configurations.Create(modelrouting.Configuration{Name: "Auto plus aux", Backend: "cursor", Binding: modelrouting.Binding{Kind: "platform", CredentialID: platform.ID, AuxiliaryModelIDs: []string{"composer-2.5"}}})
	if err != nil || auto.Binding.ModelID != "" || len(auto.Binding.AuxiliaryModelIDs) != 1 {
		t.Fatalf("platform auto with auxiliary: %+v %v", auto, err)
	}
	resolved, err := connections.Resolve("cursor", modelrouting.Binding{Kind: "configuration", ConfigurationID: auto.ID})
	if err != nil || resolved.Binding.ModelID != "" || len(resolved.Binding.AuxiliaryModelIDs) != 1 || resolved.Binding.AuxiliaryModelIDs[0] != "composer-2.5" {
		t.Fatalf("resolved primary stayed empty: %+v %v", resolved.Binding, err)
	}
}

func TestModelConfigurationStorageErrors(t *testing.T) {
	_, configurations, _, _, configuration := newModelConfigurationFixture(t)
	sqlDB, err := configurations.db.DB()
	if err != nil {
		t.Fatal(err)
	}
	if err := sqlDB.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := configurations.List(); err == nil {
		t.Fatal("list ignored database error")
	}
	if _, err := configurations.Get(configuration.ID); err == nil {
		t.Fatal("get ignored database error")
	}
	if err := configurations.Delete(configuration.ID); err == nil {
		t.Fatal("delete ignored database error")
	}
}

func TestInvalidModelConfigurationCanBeDisabledWithoutEditingItsBinding(t *testing.T) {
	for _, reason := range []string{"cleared credential", "revoked credential", "deleted connection", "removed model"} {
		t.Run(reason, func(t *testing.T) {
			connections, configurations, key, connection, configuration := newModelConfigurationFixture(t)
			var err error
			switch reason {
			case "cleared credential":
				err = NewCredentialService(configurations.db).Clear(key.ID)
			case "revoked credential":
				err = NewCredentialService(configurations.db).Revoke(key.ID)
			case "deleted connection":
				err = connections.Delete(connection.ID)
			case "removed model":
				connection.Models = nil
				_, err = connections.Update(connection.ID, connection)
			}
			if err != nil {
				t.Fatal(err)
			}
			configuration.State = "disabled"
			changed := configuration
			changed.Binding.ModelID = "another-model"
			if _, err := configurations.Update(configuration.ID, changed); err == nil {
				t.Fatal("disabling bypassed validation of a route edit")
			}
			changed = configuration
			changed.Name = "Renamed model"
			if _, err := configurations.Update(configuration.ID, changed); err == nil {
				t.Fatal("disabling bypassed validation of a configuration edit")
			}
			disabled, err := configurations.Update(configuration.ID, configuration)
			if err != nil || disabled.State != "disabled" || disabled.Revision != 2 {
				t.Fatalf("invalid configuration could not be disabled: %+v %v", disabled, err)
			}
			disabled.State = "active"
			if _, err := configurations.Update(configuration.ID, disabled); err == nil {
				t.Fatal("invalid configuration could be enabled again")
			}
		})
	}
}

func TestModelConfigurationDoesNotReportFailedWritesAsSaved(t *testing.T) {
	_, configurations, _, _, configuration := newModelConfigurationFixture(t)
	failure := errors.New("configuration storage unavailable")
	if err := configurations.db.Callback().Create().Before("gorm:create").Register("reject-model-configuration-create", func(tx *gorm.DB) {
		if tx.Statement.Table == "model_configurations" {
			tx.AddError(failure)
		}
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := configurations.Create(configuration); !errors.Is(err, failure) {
		t.Fatalf("failed create was reported as saved: %v", err)
	}
	if err := configurations.db.Callback().Update().Before("gorm:update").Register("reject-model-configuration-update", func(tx *gorm.DB) {
		if tx.Statement.Table == "model_configurations" {
			tx.AddError(failure)
		}
	}); err != nil {
		t.Fatal(err)
	}
	configuration.Name = "Unsaved change"
	if _, err := configurations.Update(configuration.ID, configuration); !errors.Is(err, failure) {
		t.Fatalf("failed update was reported as saved: %v", err)
	}
	stored, err := configurations.Get(configuration.ID)
	if err != nil || stored.Name == "Unsaved change" || stored.Revision != 1 {
		t.Fatalf("failed update changed persisted revision: %+v %v", stored, err)
	}
}
