// Package opencodecatalog reads the OpenCode model catalog from models.dev.
//
// OpenCode resolves a `provider/model` id against that catalog at run time, so
// the ids offered in the UI come from the same source instead of a list written
// by hand: a hand-written list silently rots as vendors rename models, and the
// only symptom is `opencode run` exiting 1 on an unknown model.
package opencodecatalog

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
)

// DefaultURL is the published catalog OpenCode itself reads.
const DefaultURL = "https://models.dev/api.json"

const (
	defaultTTL     = 6 * time.Hour
	fetchTimeout   = 30 * time.Second
	maxResponseMiB = 32
)

// Model is one selectable model of a provider.
type Model struct {
	ID   string `json:"id"`
	Name string `json:"name,omitempty"`
}

// Provider is one vendor or gateway in the catalog.
type Provider struct {
	ID   string `json:"id"`
	Name string `json:"name,omitempty"`
	// API is the vendor's default base URL, when the catalog publishes one.
	API string `json:"api,omitempty"`
	// KeyEnv is the environment variable OpenCode reads this vendor's key from.
	KeyEnv string  `json:"keyEnv,omitempty"`
	Models []Model `json:"models,omitempty"`
}

// Store caches one catalog snapshot in memory.
//
// Nothing is persisted: a restart refetches once on first use, which costs a
// single request and keeps the snapshot from outliving a release.
type Store struct {
	url    string
	ttl    time.Duration
	client *http.Client

	// mu is held across the fetch on purpose: it serializes a burst of callers
	// into one upstream request, and readers only ever wait for a stale entry.
	mu        sync.Mutex
	providers []Provider
	fetchedAt time.Time
}

// New builds a store reading from url (empty => DefaultURL).
func New(url string) *Store {
	if strings.TrimSpace(url) == "" {
		url = DefaultURL
	}
	return &Store{
		url:    url,
		ttl:    defaultTTL,
		client: &http.Client{Timeout: fetchTimeout},
	}
}

// Providers returns the cached snapshot, refreshing it when stale. The returned
// time is when the snapshot was fetched. A refresh failure keeps serving the
// previous snapshot; the error surfaces only when there is nothing to serve.
func (s *Store) Providers(ctx context.Context) ([]Provider, time.Time, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.fresh() {
		return s.providers, s.fetchedAt, nil
	}
	providers, err := s.fetch(ctx)
	if err != nil {
		if len(s.providers) > 0 {
			return s.providers, s.fetchedAt, nil
		}
		return nil, time.Time{}, err
	}
	s.providers = providers
	s.fetchedAt = time.Now()
	return s.providers, s.fetchedAt, nil
}

// Models returns one provider's models. An unknown provider yields nil without
// an error: a custom gateway is not in the catalog, and its ids are typed by hand.
func (s *Store) Models(ctx context.Context, providerID string) ([]Model, error) {
	providers, _, err := s.Providers(ctx)
	if err != nil {
		return nil, err
	}
	want := strings.TrimSpace(providerID)
	for _, p := range providers {
		if strings.EqualFold(p.ID, want) {
			return p.Models, nil
		}
	}
	return nil, nil
}

// KnowsProvider reports whether the catalog lists the provider. The second
// result is false when the catalog could not be read at all, which is a
// different answer from "absent": a vendor absent from the catalog needs an
// adapter written into opencode.json, while an unreadable catalog is no reason
// to override a vendor OpenCode may well resolve on its own.
func (s *Store) KnowsProvider(ctx context.Context, providerID string) (bool, bool) {
	providers, _, err := s.Providers(ctx)
	if err != nil {
		return false, false
	}
	want := strings.TrimSpace(providerID)
	for _, p := range providers {
		if strings.EqualFold(p.ID, want) {
			return true, true
		}
	}
	return false, true
}

// KnowsModel reports whether the catalog lists the model under the provider.
// The second result is false when the catalog could not be read.
//
// A vendor in the catalog can still be missing the model someone wants: a
// gateway's published model list grows faster than the catalog snapshot, and
// OpenCode refuses an id it cannot find there.
func (s *Store) KnowsModel(ctx context.Context, providerID, modelID string) (bool, bool) {
	providers, _, err := s.Providers(ctx)
	if err != nil {
		return false, false
	}
	want := strings.TrimSpace(providerID)
	wantModel := strings.TrimSpace(modelID)
	for _, p := range providers {
		if !strings.EqualFold(p.ID, want) {
			continue
		}
		for _, m := range p.Models {
			if m.ID == wantModel {
				return true, true
			}
		}
		return false, true
	}
	return false, true
}

// KeyEnv reports the env var a provider's key belongs in, or "" when the
// catalog is unavailable or does not know the provider.
func (s *Store) KeyEnv(ctx context.Context, providerID string) string {
	providers, _, err := s.Providers(ctx)
	if err != nil {
		return ""
	}
	want := strings.TrimSpace(providerID)
	for _, p := range providers {
		if strings.EqualFold(p.ID, want) {
			return p.KeyEnv
		}
	}
	return ""
}

func (s *Store) fresh() bool {
	return len(s.providers) > 0 && time.Since(s.fetchedAt) < s.ttl
}

// wireProvider is the models.dev shape: providers keyed by id, each with its
// models keyed by id.
type wireProvider struct {
	ID     string   `json:"id"`
	Name   string   `json:"name"`
	API    string   `json:"api"`
	Env    []string `json:"env"`
	Models map[string]struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"models"`
}

func (s *Store) fetch(ctx context.Context) ([]Provider, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("catalog %s: unexpected status %d", s.url, resp.StatusCode)
	}
	body := io.LimitReader(resp.Body, maxResponseMiB<<20)
	var wire map[string]wireProvider
	if err := json.NewDecoder(body).Decode(&wire); err != nil {
		return nil, fmt.Errorf("catalog %s: %w", s.url, err)
	}
	return slim(wire), nil
}

// slim projects the catalog down to what the model picker needs, sorted so the
// same snapshot always renders in the same order.
func slim(wire map[string]wireProvider) []Provider {
	providers := make([]Provider, 0, len(wire))
	for key, wp := range wire {
		id := strings.TrimSpace(wp.ID)
		if id == "" {
			id = strings.TrimSpace(key)
		}
		if id == "" {
			continue
		}
		p := Provider{ID: id, Name: strings.TrimSpace(wp.Name), API: strings.TrimSpace(wp.API)}
		if len(wp.Env) > 0 {
			p.KeyEnv = strings.TrimSpace(wp.Env[0])
		}
		for modelKey, wm := range wp.Models {
			mid := strings.TrimSpace(wm.ID)
			if mid == "" {
				mid = strings.TrimSpace(modelKey)
			}
			if mid == "" {
				continue
			}
			p.Models = append(p.Models, Model{ID: mid, Name: strings.TrimSpace(wm.Name)})
		}
		sort.Slice(p.Models, func(i, j int) bool { return p.Models[i].ID < p.Models[j].ID })
		providers = append(providers, p)
	}
	sort.Slice(providers, func(i, j int) bool { return providers[i].ID < providers[j].ID })
	return providers
}
