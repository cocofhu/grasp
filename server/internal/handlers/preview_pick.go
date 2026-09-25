package handlers

import (
	_ "embed"
	"net/http"

	"github.com/gin-gonic/gin"
)

//go:embed preview-pick.js
var previewPickJS []byte

//go:embed page-control.js
var pageControlJS []byte

// PreviewPickScript serves the cooperative pick.js for IP-direct app preview.
// The script runs in the app origin (loaded via <script src>) and postMessages
// selector / URL back to the Grasp parent. No auth: it is public static JS.
func (h *Handlers) PreviewPickScript(c *gin.Context) {
	c.Header("Cache-Control", "public, max-age=300")
	c.Header("Access-Control-Allow-Origin", "*")
	c.Header("Cross-Origin-Resource-Policy", "cross-origin")
	c.Data(http.StatusOK, "application/javascript; charset=utf-8", previewPickJS)
}

// PageControlScript serves the executor preview-pick.js loads, from the same
// place, once the user lets the agent operate the page.
func (h *Handlers) PageControlScript(c *gin.Context) {
	c.Header("Cache-Control", "public, max-age=300")
	c.Header("Access-Control-Allow-Origin", "*")
	c.Header("Cross-Origin-Resource-Policy", "cross-origin")
	c.Data(http.StatusOK, "application/javascript; charset=utf-8", pageControlJS)
}
