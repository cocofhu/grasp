package handlers

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/cocofhu/grasp/internal/services"
	"github.com/gin-gonic/gin"
)

// SSHGatewayInfo is the connection data users need for the SSH gateway.
type SSHGatewayInfo struct {
	PublicHost         string
	PublicPort         int
	HostKeyFingerprint string
	AdminsOnly         bool
}

type addSSHKeyBody struct {
	Name      string `json:"name"`
	PublicKey string `json:"publicKey"`
}

// GetSSHGateway handles GET /api/ssh-gateway. A nil SSHGateway means disabled.
func (h *Handlers) GetSSHGateway(c *gin.Context) {
	username, ok := h.notificationUsername(c)
	if !ok {
		return
	}
	g := h.SSHGateway
	if g == nil {
		c.JSON(http.StatusOK, gin.H{"enabled": false})
		return
	}
	allowed := !g.AdminsOnly || (h.Auth != nil && h.Auth.IsAdmin(username))
	c.JSON(http.StatusOK, gin.H{
		"enabled":            true,
		"host":               g.PublicHost,
		"port":               g.PublicPort,
		"hostKeyFingerprint": g.HostKeyFingerprint,
		"adminsOnly":         g.AdminsOnly,
		"allowed":            allowed,
		"hasKey":             h.SSHKeys != nil && h.SSHKeys.HasKey(username),
	})
}

// ListSSHKeys handles GET /api/me/ssh-keys.
func (h *Handlers) ListSSHKeys(c *gin.Context) {
	username, ok := h.sshKeyUser(c)
	if !ok {
		return
	}
	keys, err := h.SSHKeys.List(username)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"keys": keys})
}

// AddSSHKey handles POST /api/me/ssh-keys.
func (h *Handlers) AddSSHKey(c *gin.Context) {
	username, ok := h.sshKeyUser(c)
	if !ok {
		return
	}
	var body addSSHKeyBody
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid body"})
		return
	}
	key, err := h.SSHKeys.Add(username, body.Name, body.PublicKey)
	var invalid *services.SSHKeyError
	switch {
	case errors.Is(err, services.ErrSSHKeyExists):
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
	case errors.As(err, &invalid):
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
	case err != nil:
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
	default:
		c.JSON(http.StatusCreated, key)
	}
}

// DeleteSSHKey handles DELETE /api/me/ssh-keys/:id and closes sessions using it.
func (h *Handlers) DeleteSSHKey(c *gin.Context) {
	username, ok := h.sshKeyUser(c)
	if !ok {
		return
	}
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	if err := h.SSHKeys.Delete(username, uint(id)); err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, services.ErrSSHKeyNotFound) {
			status = http.StatusNotFound
		}
		c.JSON(status, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

func (h *Handlers) sshKeyUser(c *gin.Context) (string, bool) {
	if h.SSHKeys == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "ssh keys unavailable"})
		return "", false
	}
	return h.notificationUsername(c)
}
