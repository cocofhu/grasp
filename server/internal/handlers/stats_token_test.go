package handlers_test

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/services"
)

func TestTokenStatsLedgerEndpoints(t *testing.T) {
	hn := newHarness(t)
	now := time.Now().UTC()
	if err := hn.db.Create(&models.TokenUsageEvent{
		CreatedAt: now.Add(-time.Hour), Source: models.TokenLedgerSourceStudio, Status: models.TokenLedgerStatusOK,
		ProjectID: "p-x", ProjectName: "X", ModelKey: "m1", InputTokens: 2_000_000,
	}).Error; err != nil {
		t.Fatal(err)
	}

	w := hn.do("PUT", "/api/stats/token/pricing", map[string]any{"currency": "USD"})
	if w.Code != http.StatusForbidden {
		t.Fatalf("non-admin pricing write must be forbidden: %d %s", w.Code, w.Body.String())
	}
	if _, err := services.SaveTokenPricing(hn.db, services.TokenPricing{
		Currency: "USD", Models: map[string]services.TokenModelPrice{"m1": {Input: 1.5}},
	}); err != nil {
		t.Fatal(err)
	}
	w = hn.do("GET", "/api/stats/token/pricing", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("get pricing: %d", w.Code)
	}

	w = hn.do("GET", "/api/stats/token?window=24h&timezone=UTC&source=studio", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("stats: %d %s", w.Code, w.Body.String())
	}
	var stats struct {
		KPI struct {
			Total       int64   `json:"total"`
			StudioTotal int64   `json:"studioTotal"`
			Cost        float64 `json:"cost"`
		} `json:"kpi"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &stats); err != nil {
		t.Fatal(err)
	}
	if stats.KPI.Total != 2_000_000 || stats.KPI.StudioTotal != 2_000_000 || stats.KPI.Cost != 3 {
		t.Fatalf("kpi=%+v", stats.KPI)
	}

	w = hn.do("GET", "/api/stats/token/events?window=24h&timezone=UTC&page=1&pageSize=10", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("events: %d %s", w.Code, w.Body.String())
	}
	var events struct {
		Total int `json:"total"`
		Items []struct {
			Cost   float64 `json:"cost"`
			Source string  `json:"source"`
		} `json:"items"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &events); err != nil {
		t.Fatal(err)
	}
	if events.Total != 1 || events.Items[0].Source != "studio" || events.Items[0].Cost != 3 {
		t.Fatalf("events=%+v", events)
	}

	if w = hn.do("GET", "/api/stats/token?from=bad", nil); w.Code != http.StatusBadRequest {
		t.Fatalf("bad range: %d %s", w.Code, w.Body.String())
	}
}
