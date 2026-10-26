package services

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/cocofhu/grasp/internal/modelrouting"
)

func discoveryFixture(t *testing.T, handler http.HandlerFunc, protocol, scheme, prefix string) (*ModelConnectionService, *CredentialService, modelrouting.Connection) {
	t.Helper()
	setCredentialKey(t)
	db := newTestDB(t)
	credentials := NewCredentialService(db)
	key := newModelKey(t, credentials, "Discovery account")
	upstream := httptest.NewServer(handler)
	t.Cleanup(upstream.Close)
	service := NewModelConnectionService(db)
	connection, err := service.Create(modelrouting.Connection{
		Name: "Discovery service", ProviderID: "custom", Protocol: protocol,
		BaseURL: upstream.URL + prefix, CredentialID: key.ID, Auth: modelrouting.Auth{Scheme: scheme},
		Models: []modelrouting.Model{{ID: "saved-only", Name: "Existing", Source: "manual"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	return service, credentials, connection
}

func TestDiscoverModelsUsesSavedProtocolCredentialAndPreservesSavedModels(t *testing.T) {
	for _, test := range []struct{ protocol, scheme, prefix, path string }{
		{modelrouting.ProtocolChat, "bearer", "/gateway/v1/", "/gateway/v1/models"},
		{modelrouting.ProtocolResponses, "bearer", "/api/v3", "/api/v3/models"},
		{modelrouting.ProtocolAnthropic, "x-api-key", "", "/v1/models"},
		{modelrouting.ProtocolAnthropic, "bearer", "/anthropic", "/anthropic/v1/models"},
		{modelrouting.ProtocolAnthropic, "x-api-key", "/api/anthropic/v1", "/api/anthropic/v1/models"},
	} {
		t.Run(test.protocol+test.prefix+test.scheme, func(t *testing.T) {
			service, _, connection := discoveryFixture(t, func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet || r.URL.Path != test.path || r.Header.Get("Accept") != "application/json" {
					t.Errorf("unexpected discovery request: %s %s", r.Method, r.URL.Path)
				}
				if test.scheme == "bearer" {
					if r.Header.Get("Authorization") != "Bearer private-model-key" || r.Header.Get("x-api-key") != "" {
						t.Error("saved bearer account was not used exclusively")
					}
				} else if r.Header.Get("x-api-key") != "private-model-key" || r.Header.Get("Authorization") != "" {
					t.Error("saved API key account was not used exclusively")
				}
				if test.protocol == modelrouting.ProtocolAnthropic && (r.Header.Get("anthropic-version") != "2023-06-01" || r.URL.Query().Get("limit") != "1000") {
					t.Error("Messages protocol version/pagination was not applied")
				}
				_, _ = w.Write([]byte(`{"data":[{"id":"kimi-k3.1-pro","display_name":"Kimi","max_input_tokens":128000,"max_output_tokens":8192,"input":["text","image","audio"],"reasoning_efforts":["deep","quiet","deep"],"capabilities":{"tool_calls":true,"vision":{"supported":true},"reasoning":false}},{"id":"kimi-k3.1-pro"},{"id":"other-thinking"},{"id":""},{"id":"extension-model","input":{"modalities":["text"]},"capabilities":{"reasoning":false},"context_window":"128k"},{"id":42}]}`))
			}, test.protocol, test.scheme, test.prefix)
			before, err := service.Get(connection.ID)
			if err != nil {
				t.Fatal(err)
			}
			models, err := service.DiscoverModels(context.Background(), connection.ID)
			if err != nil || len(models) != 3 {
				t.Fatalf("discovery: %+v %v", models, err)
			}
			first := models[0]
			if first.ID != "kimi-k3.1-pro" || first.Name != "Kimi" || first.Source != "discovered" || first.ContextWindow != 128000 || first.MaxTokens != 8192 || strings.Join(first.Input, ",") != "text,image" || strings.Join(first.ReasoningEfforts, ",") != "deep,quiet" || first.Capabilities.ToolCalls == nil || !*first.Capabilities.ToolCalls || first.Capabilities.Vision == nil || !*first.Capabilities.Vision || first.Capabilities.Reasoning == nil || *first.Capabilities.Reasoning {
				t.Fatalf("explicit metadata was lost: %+v", first)
			}
			if models[1].Capabilities.Reasoning != nil || len(models[1].ReasoningEfforts) != 0 {
				t.Fatal("reasoning support was guessed from the model name")
			}
			if models[2].ID != "extension-model" || models[2].Source != "discovered" || models[2].Capabilities.Reasoning == nil || *models[2].Capabilities.Reasoning {
				t.Fatal("nonstandard optional metadata hid a valid model ID")
			}
			stored, err := service.Get(connection.ID)
			if err != nil || !reflect.DeepEqual(stored, before) {
				t.Fatal("reading the provider catalog changed the saved connection")
			}
		})
	}
}

func TestDiscoverModelsAnthropicPagination(t *testing.T) {
	var calls atomic.Int32
	service, _, connection := discoveryFixture(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.URL.Query().Get("after_id") == "first/raw" {
			_, _ = w.Write([]byte(`{"data":[{"id":"second"}],"has_more":false}`))
			return
		}
		_, _ = w.Write([]byte(`{"data":[{"id":"first/raw"}],"has_more":true,"last_id":"first/raw"}`))
	}, modelrouting.ProtocolAnthropic, "x-api-key", "/gateway")
	models, err := service.DiscoverModels(context.Background(), connection.ID)
	if err != nil || len(models) != 2 || models[1].ID != "second" || calls.Load() != 2 {
		t.Fatalf("pagination: %+v %v calls=%d", models, err, calls.Load())
	}
}

func TestDiscoverModelsFailsSafelyAndCanRetry(t *testing.T) {
	var fail atomic.Bool
	fail.Store(true)
	service, _, connection := discoveryFixture(t, func(w http.ResponseWriter, r *http.Request) {
		if fail.Load() {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":"private-model-key is denied"}`))
			return
		}
		_, _ = w.Write([]byte(`{"data":[{"id":"retry-worked"}]}`))
	}, modelrouting.ProtocolChat, "bearer", "/v1")
	models, err := service.DiscoverModels(context.Background(), connection.ID)
	if !errors.Is(err, ErrModelDiscovery) || !strings.Contains(err.Error(), "401") || strings.Contains(err.Error(), "private-model-key") || len(models) != 0 {
		t.Fatalf("unsafe provider error: %v", err)
	}
	fail.Store(false)
	models, err = service.DiscoverModels(context.Background(), connection.ID)
	if err != nil || len(models) != 1 || models[0].ID != "retry-worked" {
		t.Fatalf("retry did not read the provider again: %+v %v", models, err)
	}
}

func TestDiscoverModelsNeverFollowsRedirects(t *testing.T) {
	var targetCalls atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		targetCalls.Add(1)
	}))
	t.Cleanup(target.Close)
	service, _, connection := discoveryFixture(t, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL+"/models", http.StatusTemporaryRedirect)
	}, modelrouting.ProtocolChat, "bearer", "/v1")
	if _, err := service.DiscoverModels(context.Background(), connection.ID); !errors.Is(err, ErrModelDiscovery) || targetCalls.Load() != 0 {
		t.Fatalf("redirect could expose selected authorization: calls=%d %v", targetCalls.Load(), err)
	}
}

func TestDiscoverModelsRejectsUnavailableCredentialAndMissingConnection(t *testing.T) {
	var calls atomic.Int32
	service, credentials, connection := discoveryFixture(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
	}, modelrouting.ProtocolChat, "bearer", "/v1")
	if _, err := service.DiscoverModels(context.Background(), "missing"); !errors.Is(err, ErrModelConnectionNotFound) {
		t.Fatalf("missing connection: %v", err)
	}
	if err := credentials.Revoke(connection.CredentialID); err != nil {
		t.Fatal(err)
	}
	if _, err := service.DiscoverModels(context.Background(), connection.ID); !errors.Is(err, ErrModelDiscovery) || calls.Load() != 0 {
		t.Fatalf("revoked credential reached upstream: calls=%d %v", calls.Load(), err)
	}
}

func TestDiscoverModelsResponseLimitsAndCancellation(t *testing.T) {
	largeList := `{"data":[` + strings.Repeat(`{"id":"model"},`, modelDiscoveryCount) + `{"id":"extra"}]}`
	for _, body := range []string{`{"error":"private-model-key"}`, `not-json private-model-key`, `{"data":null}`, `{"data":[]}` + strings.Repeat(" ", modelDiscoveryBytes), largeList} {
		service, _, connection := discoveryFixture(t, func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(body))
		}, modelrouting.ProtocolChat, "bearer", "/v1")
		if _, err := service.DiscoverModels(context.Background(), connection.ID); !errors.Is(err, ErrModelDiscovery) || strings.Contains(err.Error(), "private-model-key") {
			t.Fatalf("invalid response (%d bytes): %v", len(body), err)
		}
	}
	service, _, connection := discoveryFixture(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, `{"data":[]}`)
	}, modelrouting.ProtocolChat, "bearer", "/v1")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := service.DiscoverModels(ctx, connection.ID); !errors.Is(err, ErrModelDiscovery) {
		t.Fatalf("cancellation ignored: %v", err)
	}
}
