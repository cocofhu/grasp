package handlers

import (
	"crypto/subtle"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

const doctorSessionTTL = 5 * time.Minute

type doctorArtifactSession struct {
	runA         string
	runB         string
	cleanupToken string
	timer        *time.Timer
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
	id := uuid.NewString()
	session := doctorArtifactSession{
		runA:         "doctor-a-" + uuid.NewString(),
		runB:         "doctor-b-" + uuid.NewString(),
		cleanupToken: uuid.NewString(),
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

	c.JSON(http.StatusCreated, gin.H{
		"id":            id,
		"run_a":         session.runA,
		"token_a":       tokenA,
		"run_b":         session.runB,
		"token_b":       tokenB,
		"cleanup_token": session.cleanupToken,
	})
}

func (h *Handlers) cleanupDoctorArtifactSession(id, cleanupToken string) error {
	h.doctorMu.Lock()
	session, ok := h.doctorSessions[id]
	if !ok || subtle.ConstantTimeCompare([]byte(cleanupToken), []byte(session.cleanupToken)) != 1 {
		h.doctorMu.Unlock()
		return os.ErrNotExist
	}
	delete(h.doctorSessions, id)
	h.doctorMu.Unlock()

	if session.timer != nil {
		session.timer.Stop()
	}
	h.MCP.UnregisterRun(session.runA)
	h.MCP.UnregisterRun(session.runB)
	return h.Arts.DeleteForRuns(session.runA, session.runB)
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
