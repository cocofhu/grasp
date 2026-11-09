package handlers

import (
	"context"
	"crypto/subtle"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/cocofhu/grasp/internal/sandbox"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

const doctorSessionTTL = 5 * time.Minute

type doctorArtifactSession struct {
	runA          string
	runB          string
	cleanupToken  string
	timer         *time.Timer
	cleanupBundle func()
}

// DoctorArtifactSession provisions short-lived run credentials for the local
// doctor process. The caller then exercises the normal /mcp HTTP endpoint.
func (h *Handlers) DoctorArtifactSession(c *gin.Context) {
	if !isLoopbackRequest(c.Request) || !validDoctorToken(c.GetHeader("Authorization")) {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	if h.MCP == nil || h.Arts == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "artifact service unavailable"})
		return
	}

	switch c.Request.Method {
	case http.MethodPost:
		h.startDoctorArtifactSession(c)
	case http.MethodDelete:
		if err := h.cleanupDoctorArtifactSession(c.Param("id"), c.GetHeader("X-Grasp-Doctor-Cleanup")); err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "session not found"})
			return
		}
		c.Status(http.StatusNoContent)
	default:
		c.Status(http.StatusMethodNotAllowed)
	}
}

func (h *Handlers) startDoctorArtifactSession(c *gin.Context) {
	withSandbox := c.Query("sandbox") == "true"
	if value, present := c.GetQuery("sandbox"); present && value != "true" && value != "false" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid sandbox diagnostic option"})
		return
	}
	id := uuid.NewString()
	session := doctorArtifactSession{
		runA:         "doctor-a-" + uuid.NewString(),
		runB:         "doctor-b-" + uuid.NewString(),
		cleanupToken: uuid.NewString(),
	}
	if err := h.Arts.DiagnosticRuns(c.Request.Context(), true, session.runA, session.runB); err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "diagnostic execution unavailable"})
		return
	}
	var bootstrap *sandbox.DoctorBootstrap
	if withSandbox {
		var manager *sandbox.Manager
		if h.Sbx != nil {
			manager = h.Sbx.Manager()
		}
		prepared, cleanup, err := manager.PrepareDoctorBootstrap(c.Request.Context())
		if err != nil {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			_ = h.Arts.DiagnosticRuns(ctx, false, session.runA, session.runB)
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "diagnostic sandbox bootstrap unavailable"})
			return
		}
		bootstrap, session.cleanupBundle = &prepared, cleanup
	}
	tokenA := h.MCP.RegisterRun(session.runA)
	tokenB := h.MCP.RegisterRun(session.runB)

	h.doctorMu.Lock()
	if h.doctorSessions == nil {
		h.doctorSessions = make(map[string]doctorArtifactSession)
	}
	h.doctorSessions[id] = session
	h.doctorMu.Unlock()

	session.timer = time.AfterFunc(doctorSessionTTL, func() {
		_ = h.cleanupDoctorArtifactSession(id, session.cleanupToken)
	})
	h.doctorMu.Lock()
	if current, ok := h.doctorSessions[id]; ok {
		current.timer = session.timer
		h.doctorSessions[id] = current
	}
	h.doctorMu.Unlock()

	response := gin.H{
		"id":            id,
		"run_a":         session.runA,
		"token_a":       tokenA,
		"run_b":         session.runB,
		"token_b":       tokenB,
		"cleanup_token": session.cleanupToken,
	}
	if bootstrap != nil {
		response["bootstrap"] = bootstrap
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusCreated, response)
}

func (h *Handlers) cleanupDoctorArtifactSession(id, cleanupToken string) error {
	h.doctorMu.Lock()
	session, ok := h.doctorSessions[id]
	if !ok || subtle.ConstantTimeCompare([]byte(cleanupToken), []byte(session.cleanupToken)) != 1 {
		h.doctorMu.Unlock()
		return os.ErrNotExist
	}
	h.doctorMu.Unlock()
	// Revoking disposable download credentials does not mutate SQL or need
	// execution authority. Keep the session receipt if guarded SQL cleanup fails.
	if session.cleanupBundle != nil {
		session.cleanupBundle()
	}
	h.MCP.UnregisterRun(session.runA)
	h.MCP.UnregisterRun(session.runB)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := h.Arts.DiagnosticRuns(ctx, false, session.runA, session.runB); err != nil {
		return err
	}
	h.doctorMu.Lock()
	delete(h.doctorSessions, id)
	h.doctorMu.Unlock()
	if session.timer != nil {
		session.timer.Stop()
	}
	return nil
}

func validDoctorToken(header string) bool {
	want := os.Getenv("GRASP_DOCTOR_TOKEN")
	got := bearer(header)
	return want != "" && subtle.ConstantTimeCompare([]byte(got), []byte(want)) == 1
}

func isLoopbackRequest(r *http.Request) bool {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	ip := net.ParseIP(strings.Trim(host, "[]"))
	return ip != nil && ip.IsLoopback()
}
