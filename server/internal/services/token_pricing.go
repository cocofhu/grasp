package services

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/cocofhu/grasp/internal/models"
	"gorm.io/gorm"
)

// TokenPricingSettingKey is the Setting row holding the model price table.
const TokenPricingSettingKey = "token_pricing"

// Supported pricing currencies.
const (
	TokenPricingCurrencyUSD = "USD"
	TokenPricingCurrencyCNY = "CNY"
)

// ErrInvalidTokenPricing is returned for malformed price tables.
var ErrInvalidTokenPricing = errors.New("invalid token pricing")

// TokenModelPrice is one model's unit price per 1M tokens for each component.
type TokenModelPrice struct {
	Input      float64 `json:"input"`
	Output     float64 `json:"output"`
	CacheRead  float64 `json:"cacheRead"`
	CacheWrite float64 `json:"cacheWrite"`
}

// TokenPricing is the platform price table used for cost estimation. Costs are
// computed at query time, so editing a price re-prices history.
type TokenPricing struct {
	Currency  string                     `json:"currency"`
	Models    map[string]TokenModelPrice `json:"models"`
	UpdatedAt *time.Time                 `json:"updatedAt,omitempty"`
}

// LoadTokenPricing reads the price table (empty table + USD when unset).
func LoadTokenPricing(db *gorm.DB) TokenPricing {
	out := TokenPricing{Currency: TokenPricingCurrencyUSD, Models: map[string]TokenModelPrice{}}
	if db == nil {
		return out
	}
	var row models.Setting
	if err := db.Where(&models.Setting{Key: TokenPricingSettingKey}).Limit(1).Find(&row).Error; err != nil || row.Key == "" {
		return out
	}
	var p TokenPricing
	if err := json.Unmarshal([]byte(row.Value), &p); err != nil {
		return out
	}
	if p.Currency == "" {
		p.Currency = TokenPricingCurrencyUSD
	}
	if p.Models == nil {
		p.Models = map[string]TokenModelPrice{}
	}
	at := row.UpdatedAt
	p.UpdatedAt = &at
	return p
}

// SaveTokenPricing validates and persists the price table.
func SaveTokenPricing(db *gorm.DB, p TokenPricing) (TokenPricing, error) {
	p.Currency = strings.ToUpper(strings.TrimSpace(p.Currency))
	if p.Currency == "" {
		p.Currency = TokenPricingCurrencyUSD
	}
	if p.Currency != TokenPricingCurrencyUSD && p.Currency != TokenPricingCurrencyCNY {
		return TokenPricing{}, fmt.Errorf("%w: currency %q", ErrInvalidTokenPricing, p.Currency)
	}
	clean := make(map[string]TokenModelPrice, len(p.Models))
	for k, v := range p.Models {
		key := strings.TrimSpace(k)
		if key == "" {
			continue
		}
		for _, f := range []float64{v.Input, v.Output, v.CacheRead, v.CacheWrite} {
			if f < 0 || math.IsNaN(f) || math.IsInf(f, 0) {
				return TokenPricing{}, fmt.Errorf("%w: model %q has invalid price", ErrInvalidTokenPricing, key)
			}
		}
		clean[key] = v
	}
	p.Models = clean
	p.UpdatedAt = nil
	raw, err := json.Marshal(p)
	if err != nil {
		return TokenPricing{}, err
	}
	if err := setSetting(db, TokenPricingSettingKey, string(raw)); err != nil {
		return TokenPricing{}, err
	}
	return LoadTokenPricing(db), nil
}

// TokenPricing returns the current price table.
func (s *ProjectService) TokenPricing() TokenPricing { return LoadTokenPricing(s.db) }

// SaveTokenPricing validates and persists the price table.
func (s *ProjectService) SaveTokenPricing(p TokenPricing) (TokenPricing, error) {
	return SaveTokenPricing(s.db, p)
}

// price resolves a model's price by exact key, then case-insensitively.
func (p TokenPricing) price(modelKey string) (TokenModelPrice, bool) {
	if v, ok := p.Models[modelKey]; ok {
		return v, true
	}
	lk := strings.ToLower(strings.TrimSpace(modelKey))
	for k, v := range p.Models {
		if strings.ToLower(k) == lk {
			return v, true
		}
	}
	return TokenModelPrice{}, false
}

// Cost prices one usage slice. ok=false means the model has no price.
func (p TokenPricing) Cost(modelKey string, u models.TokenUsage) (float64, bool) {
	pr, ok := p.price(modelKey)
	if !ok {
		return 0, false
	}
	c := (float64(u.InputTokens)*pr.Input +
		float64(u.OutputTokens)*pr.Output +
		float64(u.CacheReadTokens)*pr.CacheRead +
		float64(u.CacheWriteTokens)*pr.CacheWrite) / 1e6
	return c, true
}
