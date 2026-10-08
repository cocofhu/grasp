package api

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"sandbox-gateway/internal/config"
	"sandbox-gateway/internal/driver"
	"sandbox-gateway/internal/models"
	"sandbox-gateway/internal/service"
	"sandbox-gateway/internal/store"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog/log"
)

// Handler holds dependencies for the control-plane HTTP handlers.
type Handler struct {
	svc   *service.SandboxService
	ports config.PortsConfig
}

// NewHandler builds a Handler.
func NewHandler(svc *service.SandboxService, ports config.PortsConfig) *Handler {
	return &Handler{svc: svc, ports: ports}
}

// createRequest is the POST /sandboxes body.
type createRequest struct {
	Image string `json:"image"`
	// Provider selects the agent CLI (e.g. "cursor", "claude_code", "opencode").
	// The published image is one universal-sandbox; the gateway injects
	// AGENT_PROVIDER. Ignored when Image is set explicitly.
	Provider     string            `json:"provider"`
	Env          map[string]string `json:"env"`
	Labels       map[string]string `json:"labels"`
	WorkspaceDir string            `json:"workspaceDir"`
	Ports        []int             `json:"ports"`
	Mounts       []string          `json:"mounts"`
	Config       *configInject     `json:"config"`
	Resources    *resourceRequest  `json:"resources"`
}

// resourceRequest is the optional per-sandbox resource limit block.
// Zero / omitted fields use gateway defaults (see kubernetes.* in config).
type resourceRequest struct {
	CPUCores float64 `json:"cpuCores"`
	MemoryMB int64   `json:"memoryMB"`
	DiskGi   int64   `json:"diskGi"`
}

type configInject struct {
	ConfigRoot string `json:"configRoot"`
	HostPath   string `json:"hostPath"`
	BundleURL  string `json:"bundleUrl"`
	Headers    string `json:"headers"`
}

// sandboxDTO is the API representation of a sandbox.
type sandboxDTO struct {
	ID        string            `json:"id"`
	Name      string            `json:"name"`
	Status    string            `json:"status"`
	Image     string            `json:"image"`
	Namespace string            `json:"namespace,omitempty"`
	Error     string            `json:"error,omitempty"`
	Endpoints map[string]string `json:"endpoints"`
	Labels    map[string]string `json:"labels,omitempty"`
	Resources *resourceDTO      `json:"resources,omitempty"`
}

type resourceDTO struct {
	CPUCores float64 `json:"cpuCores"`
	MemoryMB int64   `json:"memoryMB"`
	DiskGi   int64   `json:"diskGi"`
}

// toDTO renders a Sandbox with named endpoints (session/ide/ssh/cdp/novnc + raw port keys).
func (h *Handler) toDTO(sb *models.Sandbox) sandboxDTO {
	eps := sb.Endpoints()
	named := map[string]string{}
	for port, addr := range eps {
		named[strconv.Itoa(port)] = addr
	}
	for name, port := range map[string]int{
		"session": h.ports.Session,
		"ide":     h.ports.CodeServer,
		"ssh":     h.ports.SSH,
		"cdp":     h.ports.CDP,
		"novnc":   h.ports.NoVNC,
	} {
		if addr := eps[port]; addr != "" {
			named[name] = addr
		}
	}
	dto := sandboxDTO{
		ID:        sb.ID,
		Name:      sb.Name,
		Status:    sb.Status,
		Image:     sb.Image,
		Namespace: sb.Namespace,
		Error:     sb.Error,
		Endpoints: named,
		Labels:    sb.Labels(),
	}
	if sb.CPUCores > 0 || sb.MemoryMB > 0 || sb.DiskGi > 0 {
		dto.Resources = &resourceDTO{
			CPUCores: sb.CPUCores,
			MemoryMB: sb.MemoryMB,
			DiskGi:   sb.DiskGi,
		}
	}
	return dto
}

// Create handles POST /sandboxes.
func (h *Handler) Create(c *gin.Context) {
	var req createRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	var cfg *driver.ConfigInject
	if req.Config != nil {
		cfg = &driver.ConfigInject{
			ConfigRoot: req.Config.ConfigRoot,
			HostPath:   req.Config.HostPath,
			BundleURL:  req.Config.BundleURL,
			Headers:    req.Config.Headers,
		}
	}
	create := service.CreateRequest{
		Image:        req.Image,
		Provider:     req.Provider,
		Env:          req.Env,
		Labels:       req.Labels,
		WorkspaceDir: req.WorkspaceDir,
		Ports:        req.Ports,
		Mounts:       req.Mounts,
		Config:       cfg,
	}
	if req.Resources != nil {
		create.CPUCores = req.Resources.CPUCores
		create.MemoryMB = req.Resources.MemoryMB
		create.DiskGi = req.Resources.DiskGi
	}
	sb, err := h.svc.Create(c.Request.Context(), create)
	if err != nil {
		// validation errors (bad resource bounds) are client faults
		status := http.StatusInternalServerError
		if isClientResourceError(err) {
			status = http.StatusBadRequest
			log.Warn().Err(err).Msg("create sandbox rejected")
		} else {
			log.Error().Err(err).Msg("create sandbox failed")
		}
		c.JSON(status, gin.H{"error": err.Error()})
		return
	}
	log.Info().Str("sandbox_id", sb.ID).Str("image", sb.Image).Msg("sandbox create accepted")
	c.JSON(http.StatusAccepted, h.toDTO(sb))
}

func isClientResourceError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "cpuCores") ||
		strings.Contains(msg, "memoryMB") ||
		strings.Contains(msg, "diskGi")
}

// List handles GET /sandboxes.
// Optional repeated query param: label=key:value (AND). Value may contain ':'.
func (h *Handler) List(c *gin.Context) {
	labels, err := parseLabelQuery(c.QueryArray("label"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	items, err := h.svc.List(service.ListFilter{Labels: labels})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	out := make([]sandboxDTO, 0, len(items))
	for i := range items {
		out = append(out, h.toDTO(&items[i]))
	}
	c.JSON(http.StatusOK, gin.H{"sandboxes": out})
}

// parseLabelQuery parses repeated "key:value" label filters.
func parseLabelQuery(raw []string) (map[string]string, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	out := make(map[string]string, len(raw))
	for _, item := range raw {
		key, value, ok := strings.Cut(item, ":")
		if !ok || key == "" {
			return nil, fmt.Errorf("invalid label filter %q, want key:value", item)
		}
		out[key] = value
	}
	return out, nil
}

// Get handles GET /sandboxes/:id.
func (h *Handler) Get(c *gin.Context) {
	sb, err := h.svc.Get(c.Param("id"))
	if err != nil {
		h.notFoundOr500(c, err)
		return
	}
	c.JSON(http.StatusOK, h.toDTO(sb))
}

// Start handles POST /sandboxes/:id/start.
func (h *Handler) Start(c *gin.Context) {
	if err := h.svc.Start(c.Request.Context(), c.Param("id")); err != nil {
		h.notFoundOr500(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "starting"})
}

// Stop handles POST /sandboxes/:id/stop.
func (h *Handler) Stop(c *gin.Context) {
	if err := h.svc.Stop(c.Request.Context(), c.Param("id")); err != nil {
		h.notFoundOr500(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "stopped"})
}

// Destroy handles DELETE /sandboxes/:id.
func (h *Handler) Destroy(c *gin.Context) {
	if err := h.svc.Destroy(c.Request.Context(), c.Param("id")); err != nil {
		h.notFoundOr500(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "destroyed"})
}

// Reinstall handles POST /sandboxes/:id/reinstall.
// Body: {"preserveData": true} — keep PVC/volumes; false or omit wipes data volumes.
func (h *Handler) Reinstall(c *gin.Context) {
	var req struct {
		PreserveData bool `json:"preserveData"`
	}
	if err := c.ShouldBindJSON(&req); err != nil && !errors.Is(err, io.EOF) {
		log.Warn().Err(err).Str("sandbox_id", c.Param("id")).Msg("reinstall body parse failed; using defaults")
	}
	preserve := req.PreserveData

	if err := h.svc.Reinstall(c.Request.Context(), c.Param("id"), preserve); err != nil {
		h.notFoundOr500(c, err)
		return
	}
	log.Info().Str("sandbox_id", c.Param("id")).Bool("preserve_data", preserve).Msg("sandbox reinstall accepted")
	msg := "sandbox reinstall started"
	if preserve {
		msg = "sandbox reinstall started (data volumes preserved)"
	}
	c.JSON(http.StatusAccepted, gin.H{
		"id":           c.Param("id"),
		"status":       "reinstalling",
		"preserveData": preserve,
		"message":      msg,
	})
}

// Status handles GET /sandboxes/:id/status.
func (h *Handler) Status(c *gin.Context) {
	st, err := h.svc.Status(c.Request.Context(), c.Param("id"))
	if err != nil {
		h.notFoundOr500(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": string(st)})
}

// Host handles GET /sandboxes/:id/hosts/:port.
func (h *Handler) Host(c *gin.Context) {
	port, err := strconv.Atoi(c.Param("port"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid port"})
		return
	}
	addr, err := h.svc.Host(c.Request.Context(), c.Param("id"), port)
	if err != nil {
		if errors.Is(err, service.ErrEndpointNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			return
		}
		h.notFoundOr500(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"port": port, "address": addr})
}

// PublishPort handles POST /sandboxes/:id/ports {port:N} — k8s Service mapping.
func (h *Handler) PublishPort(c *gin.Context) {
	var req struct {
		Port int `json:"port"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.Port < 1 || req.Port > 65535 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid port"})
		return
	}
	addr, err := h.svc.PublishPort(c.Request.Context(), c.Param("id"), req.Port)
	if err != nil {
		if errors.Is(err, service.ErrEndpointNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			return
		}
		h.notFoundOr500(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"port": req.Port, "address": addr})
}

// Logs handles GET /sandboxes/:id/logs?tail= — synchronous PID1 stdout/stderr.
func (h *Handler) Logs(c *gin.Context) {
	tail := 5000
	if raw := strings.TrimSpace(c.Query("tail")); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid tail"})
			return
		}
		if n > 0 {
			tail = n
		}
	}
	content, err := h.svc.Logs(c.Request.Context(), c.Param("id"), tail)
	if err != nil {
		if errors.Is(err, service.ErrLogsUnsupported) {
			c.JSON(http.StatusNotImplemented, gin.H{"error": err.Error()})
			return
		}
		h.notFoundOr500(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"content": content})
}

// LastExit handles GET /sandboxes/:id/exit: {"exit": ExitInfo|null}.
func (h *Handler) LastExit(c *gin.Context) {
	e, err := h.svc.LastExit(c.Request.Context(), c.Param("id"))
	if err != nil {
		h.notFoundOr500(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"exit": e})
}

func (h *Handler) notFoundOr500(c *gin.Context, err error) {
	if errors.Is(err, store.ErrNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"error": "sandbox not found"})
		return
	}
	log.Error().
		Err(err).
		Str("method", c.Request.Method).
		Str("raw_path", c.Request.URL.Path).
		Str("sandbox_id", c.Param("id")).
		Msg("request handler failed")
	c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
}
