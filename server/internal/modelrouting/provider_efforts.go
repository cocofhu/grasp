package modelrouting

import (
	_ "embed"
	"encoding/json"
	"net/url"
	"regexp"
	"strings"
)

//go:embed provider_efforts.json
var providerEffortsJSON []byte

type providerEffortEntry struct {
	IDs      []string            `json:"ids"`
	Hosts    []string            `json:"hosts"`
	Suffixes []string            `json:"suffixes"`
	Models   map[string][]string `json:"models"`
}

type providerEffortCatalog struct {
	Providers []providerEffortEntry `json:"providers"`
}

// Documented reasoning levels for connected model services. Official OpenCode
// keys use the generated variant catalog instead of this file.
var providerEffortTable = mustProviderEffortTable()

func mustProviderEffortTable() providerEffortCatalog {
	var out providerEffortCatalog
	if err := json.Unmarshal(providerEffortsJSON, &out); err != nil {
		panic(err)
	}
	return out
}

var effortDateSuffix = regexp.MustCompile(`-(?:\d{8}|\d{6}|\d{4})$`)

func connectionHostname(baseURL string) string {
	raw := strings.TrimSpace(baseURL)
	if raw == "" {
		return ""
	}
	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return strings.ToLower(parsed.Hostname())
}

func providerForConnection(connection *Connection) *providerEffortEntry {
	if connection == nil {
		return nil
	}
	id := strings.ToLower(strings.TrimSpace(connection.ProviderID))
	if id != "" {
		for i := range providerEffortTable.Providers {
			for _, candidate := range providerEffortTable.Providers[i].IDs {
				if candidate == id {
					return &providerEffortTable.Providers[i]
				}
			}
		}
	}
	host := connectionHostname(connection.BaseURL)
	if host == "" {
		return nil
	}
	for i := range providerEffortTable.Providers {
		entry := &providerEffortTable.Providers[i]
		for _, exact := range entry.Hosts {
			if host == exact {
				return entry
			}
		}
		for _, suffix := range entry.Suffixes {
			if suffix != "" && strings.HasSuffix(host, suffix) {
				return entry
			}
		}
	}
	return nil
}

func providerModelEfforts(connection *Connection, modelID string) []string {
	entry := providerForConnection(connection)
	if entry == nil {
		return nil
	}
	return lookupProviderEfforts(entry.Models, modelID)
}

func lookupProviderEfforts(models map[string][]string, modelID string) []string {
	id := strings.ToLower(strings.TrimSpace(modelID))
	if id == "" {
		return nil
	}
	keys := []string{id}
	if slash := strings.LastIndex(id, "/"); slash >= 0 {
		keys = append(keys, id[slash+1:])
	}
	for _, key := range keys {
		if efforts, ok := models[key]; ok {
			return append([]string{}, efforts...)
		}
	}
	for _, key := range keys {
		stripped := effortDateSuffix.ReplaceAllString(key, "")
		if stripped != key {
			if efforts, ok := models[stripped]; ok {
				return append([]string{}, efforts...)
			}
		}
	}
	return nil
}
