package sandbox

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestDirectGatewayDiscoversCurrentCapabilitiesWithoutTunnel(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v2/info" || r.Header.Get("Authorization") != "Bearer fixture-key" {
			t.Errorf("wrong discovery request: %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"version": 2, "driver": "e2b", "capabilities": map[string]string{"stopMode": "pause", "diskMode": "provider"}})
	}))
	defer server.Close()
	info, err := NewGatewayClient(server.URL, "fixture-key").InfoFor(context.Background(), "guest")
	if err != nil || info.Driver != "e2b" || info.Capabilities.StopMode != "pause" {
		t.Fatalf("direct mode missed native pause capability: %+v %v", info, err)
	}
}

func TestDirectGatewayDoesNotDowngradeOnDiscoveryAuthFailure(t *testing.T) {
	var paths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()
	if _, err := NewGatewayClient(server.URL, "fixture-key").InfoFor(context.Background(), "guest"); err == nil {
		t.Fatal("unauthenticated discovery succeeded")
	}
	if len(paths) != 1 || paths[0] != "/api/v2/info" {
		t.Fatalf("discovery incorrectly downgraded after auth rejection: %v", paths)
	}
}
