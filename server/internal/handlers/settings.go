package handlers

import (
	"net/http"

	"github.com/cocofhu/grasp/internal/auth"
	"github.com/cocofhu/grasp/internal/services"
	"github.com/gin-gonic/gin"
)

type updateSettingsBody struct {
	MaxConcurrentRuns *int    `json:"max_concurrent_runs"`
	RunSandboxTTLMin  *int    `json:"run_sandbox_ttl_minutes"`
	TestSandboxTTLMin *int    `json:"test_sandbox_ttl_minutes"`
	MaxTestSandboxes  *int    `json:"max_test_sandboxes"`
	NodeAutoRetryMax  *int    `json:"node_auto_retry_max"`
	SandboxMemoryMB   *int    `json:"sandbox_memory_mb"`
	BrandProductName  *string `json:"brand_product_name"`
	BrandHomeSubtitle *string `json:"brand_home_subtitle"`
}

// GetSettings returns scheduling params and instance brand overrides to every
// authenticated shell user.
func (h *Handlers) GetSettings(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"items": h.Settings.Effective(), "brand": h.Settings.Brand()})
}

// UpdateSettings persists a patch of platform scheduling params and applies
// them at runtime. Only keys present are changed; env-locked keys are ignored.
func (h *Handlers) UpdateSettings(c *gin.Context) {
	var body updateSettingsBody
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid body"})
		return
	}
	if (body.BrandProductName != nil || body.BrandHomeSubtitle != nil) && !h.requireAdmin(c) {
		return
	}
	patch := map[string]int{}
	for key, value := range map[string]*int{
		services.KeyMaxConcurrentRuns: body.MaxConcurrentRuns,
		services.KeyRunSandboxTTLMin:  body.RunSandboxTTLMin,
		services.KeyTestSandboxTTLMin: body.TestSandboxTTLMin,
		services.KeyMaxTestSandboxes:  body.MaxTestSandboxes,
		services.KeyNodeAutoRetryMax:  body.NodeAutoRetryMax,
		services.KeySandboxMemoryMB:   body.SandboxMemoryMB,
	} {
		if value != nil {
			patch[key] = *value
		}
	}
	items, err := h.Settings.UpdateWithBrand(patch, services.BrandPatch{
		ProductName: body.BrandProductName, HomeSubtitle: body.BrandHomeSubtitle,
	})
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "brand": h.Settings.Brand()})
}

// requireAdmin writes 403 and returns false unless the session is an admin.
func (h *Handlers) requireAdmin(c *gin.Context) bool {
	sess, ok := auth.GetSession(c)
	if !ok || h.Auth == nil || !h.Auth.IsAdmin(sess.Username) {
		c.JSON(http.StatusForbidden, gin.H{"error": "admin required"})
		return false
	}
	return true
}
