package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/sandbox"

	"github.com/rs/zerolog/log"
)

// acpHostPort resolves the live ACP host/port for a sandbox, preferring the
// in-memory connection and falling back to attaching to the running container.
func (s *SandboxService) acpHostPort(ctx context.Context, id uint) (string, int, error) {
	host, port, _, err := s.acpConnection(ctx, id)
	return host, port, err
}

// acpConnection keeps the bridge secret with its resolved address. The persisted
// token survives platform restarts, when there is no in-memory ACP connection.
func (s *SandboxService) acpConnection(ctx context.Context, id uint) (string, int, string, error) {
	row, err := s.Get(id)
	if err != nil {
		return "", 0, "", err
	}
	s.mu.Lock()
	ls := s.live[id]
	s.mu.Unlock()
	host, port, password := row.Host, row.ACPPort, row.Token
	if ls != nil && ls.sb != nil {
		host, port = ls.sb.Host, ls.sb.Port
		if password == "" {
			password = ls.sb.Password
		}
	}
	if host == "" || port == 0 {
		if s.mgr.Status(ctx, row.Name) != "running" {
			return "", 0, "", fmt.Errorf("sandbox container 不在运行")
		}
		sb, aerr := s.mgr.Attach(ctx, row.Name)
		if aerr != nil {
			return "", 0, "", fmt.Errorf("attach: %w", aerr)
		}
		host, port = sb.Host, sb.Port
		if password == "" {
			password = sb.Password
		}
	}
	return host, port, password, nil
}

// ACPUpstream returns the reachable "host:port" for the sandbox ACP bridge
// (session endpoint). Aligns iframe reverse-proxy dialing with chat/events.
func (s *SandboxService) ACPUpstream(ctx context.Context, id uint) (string, error) {
	// Prefer the gateway-published session endpoint when available so K8s
	// ClusterIP/node hosts win over a stale DB host.
	if row, err := s.Get(id); err == nil && s.mgr != nil && row.Name != "" {
		if addr, eerr := s.mgr.EndpointAddr(ctx, row.Name, "session"); eerr == nil && strings.TrimSpace(addr) != "" {
			return addr, nil
		}
	}
	host, port, err := s.acpHostPort(ctx, id)
	if err != nil {
		return "", err
	}
	if host == "" {
		host = "127.0.0.1"
	}
	return fmt.Sprintf("%s:%d", host, port), nil
}

// IDEUpstream returns the reachable "host:port" for the sandbox code-server
// (ide endpoint). Never hard-codes 127.0.0.1; Docker gateways that publish
// loopback endpoints still resolve to 127.0.0.1:<port> naturally.
func (s *SandboxService) IDEUpstream(ctx context.Context, id uint) (string, error) {
	row, err := s.Get(id)
	if err != nil {
		return "", err
	}
	if row.CodeServerPort == 0 {
		return "", fmt.Errorf("no code-server for this sandbox")
	}
	if s.mgr != nil && row.Name != "" {
		if addr, eerr := s.mgr.EndpointAddr(ctx, row.Name, "ide"); eerr == nil && strings.TrimSpace(addr) != "" {
			return addr, nil
		}
		if addr, herr := s.mgr.HostForPort(ctx, row.Name, row.CodeServerPort); herr == nil && strings.TrimSpace(addr) != "" {
			return addr, nil
		}
		if s.mgr.Status(ctx, row.Name) == "running" {
			if sb, aerr := s.mgr.Attach(ctx, row.Name); aerr == nil && sb != nil && sb.CodeServerPort > 0 {
				host := sb.Host
				if host == "" {
					host = sb.SSHHost
				}
				if host == "" {
					host = "127.0.0.1"
				}
				return fmt.Sprintf("%s:%d", host, sb.CodeServerPort), nil
			}
		}
	}
	host := row.Host
	if host == "" {
		host = "127.0.0.1"
	}
	return fmt.Sprintf("%s:%d", host, row.CodeServerPort), nil
}

// Events reads the sandbox's full agent event log directly from the container
// (the cursor-acp bridge is the single source of truth) and returns it as the
// AcpEvent timeline. Works the same way for every sandbox — interactive test
// sandboxes here and per-run node sandboxes in the engine.
func (s *SandboxService) Events(ctx context.Context, id uint) ([]models.AcpEvent, error) {
	host, port, password, err := s.acpConnection(ctx, id)
	if err != nil {
		return nil, err
	}
	res, _, ferr := sandbox.FetchEventLogWithPassword(ctx, host, port, password)
	if ferr != nil {
		return nil, ferr
	}
	return res.AcpEvents(), nil
}

// EventLog reads the sandbox's raw agent event frames (unaggregated), used to
// rebuild the interactive chat transcript (with the original user prompts) when
// reopening a reused sandbox. Returns the frames as raw JSON messages.
func (s *SandboxService) EventLog(ctx context.Context, id uint) ([]json.RawMessage, error) {
	host, port, password, err := s.acpConnection(ctx, id)
	if err != nil {
		return nil, err
	}
	frames, _, ferr := sandbox.FetchEventLogRawWithPassword(ctx, host, port, password)
	if ferr != nil {
		return nil, ferr
	}
	return frames, nil
}

// EventLogPage returns one page of raw event frames with cursor metadata.
func (s *SandboxService) EventLogPage(ctx context.Context, id uint, cursor string, limit int) (*sandbox.EventLogPageResult, error) {
	host, port, password, err := s.acpConnection(ctx, id)
	if err != nil {
		return nil, err
	}
	return sandbox.FetchEventLogPageWithPassword(ctx, host, port, cursor, limit, password)
}

// OpenTerminal attaches an interactive PTY shell to a sandbox over SSH.
func (s *SandboxService) OpenTerminal(ctx context.Context, id uint) (*sandbox.SSHTerminal, error) {
	row, err := s.Get(id)
	if err != nil {
		return nil, err
	}
	if s.mgr.Status(ctx, row.Name) != "running" {
		return nil, fmt.Errorf("container 不在运行")
	}
	return s.mgr.ExecPTY(ctx, row.Name, nil)
}

// List returns all interactive sandboxes with live-derived status.
// Container statuses are filled from a single batch docker ps (ListStatuses)
// instead of N serial inspects. If Docker is unavailable, every row gets
// ContainerStatus "unknown" and the list still succeeds (HTTP 200).
func (s *SandboxService) List(ctx context.Context) ([]SandboxView, error) {
	var rows []models.Sandbox
	if err := s.db.Order("created_at desc").Find(&rows).Error; err != nil {
		return nil, err
	}
	statusMap, dockerErr := s.mgr.ListStatuses(ctx)
	if dockerErr != nil {
		log.Warn().Err(dockerErr).Msg("sandbox list: batch docker ps failed; container status unknown")
	}
	out := make([]SandboxView, 0, len(rows))
	for i := range rows {
		out = append(out, s.viewWithStatus(ctx, &rows[i], statusMap, dockerErr != nil))
	}
	return out, nil
}

// Get returns the persisted record for one sandbox.
func (s *SandboxService) Get(id uint) (*models.Sandbox, error) {
	var row models.Sandbox
	if err := s.db.First(&row, id).Error; err != nil {
		return nil, fmt.Errorf("not found")
	}
	return &row, nil
}

// GetView returns one sandbox with live-derived status (used by the UI to poll
// a "creating" sandbox until it turns running/error). It also lazily attaches
// gateway Endpoints for the detail modal; gateway failures degrade to an empty
// map so the rest of the view still succeeds.
func (s *SandboxService) GetView(ctx context.Context, id uint) (*SandboxView, error) {
	row, err := s.Get(id)
	if err != nil {
		return nil, err
	}
	v := s.view(ctx, row)
	v.Endpoints = map[string]string{}
	if gw := s.mgr.Gateway(); gw != nil {
		if sb, err := gw.Get(ctx, row.Name); err != nil {
			log.Debug().Err(err).Str("sandbox", row.Name).Msg("sandbox GetView: gateway Get failed; endpoints empty")
		} else if sb != nil && sb.Endpoints != nil {
			v.Endpoints = publicSandboxEndpoints(sb.Endpoints)
		}
	}
	return &v, nil
}

// WaitReady polls a sandbox until it runs, reporting each status (pulling,
// creating, …) to onStatus. It fails on status error or when ctx ends.
func (s *SandboxService) WaitReady(ctx context.Context, id uint, onStatus func(string)) error {
	for {
		row, err := s.Get(id)
		if err != nil {
			return err
		}
		v := s.view(ctx, row)
		if onStatus != nil {
			onStatus(v.Status)
		}
		switch v.Status {
		case "running":
			return nil
		case "error":
			if v.Error != "" {
				return errors.New(v.Error)
			}
			return errors.New("sandbox error")
		}
		wait := time.Second
		if v.Status == "pulling" {
			wait = 2 * time.Second
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("sandbox not ready (%s): %w", v.Status, ctx.Err())
		case <-time.After(wait):
		}
	}
}

var userVisibleEndpointKeys = map[string]struct{}{
	"session": {},
	"ide":     {},
	"ssh":     {},
}

// publicSandboxEndpoints is the user-side GetView whitelist: only session/ide/ssh.
// Named cdp/novnc, numeric 9222/6080, and host:port values ending in those
// ports are dropped. VNC WS paths are not written here — the UI builds them.
func publicSandboxEndpoints(eps map[string]string) map[string]string {
	out := map[string]string{}
	if eps == nil {
		return out
	}
	for k, v := range eps {
		if _, ok := userVisibleEndpointKeys[k]; !ok {
			continue
		}
		if isSensitiveDirectAddr(v) {
			continue
		}
		out[k] = v
	}
	return out
}

func isSensitiveDirectAddr(addr string) bool {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return false
	}
	return strings.HasSuffix(addr, ":9222") || strings.HasSuffix(addr, ":6080")
}

func (s *SandboxService) view(ctx context.Context, row *models.Sandbox) SandboxView {
	return s.viewWithStatus(ctx, row, nil, false)
}

// viewWithStatus builds a SandboxView. When statusMap is non-nil (batch List path),
// ContainerStatus is looked up from the map (missing → "not_found"). When
// dockerFailed is true, every row gets "unknown". Otherwise (GetView path) a
// single docker inspect is used via mgr.Status.
func (s *SandboxService) viewWithStatus(ctx context.Context, row *models.Sandbox, statusMap map[string]string, dockerFailed bool) SandboxView {
	s.mu.Lock()
	ls := s.live[row.ID]
	busy := ls != nil && ls.busy
	if row.Purpose == "run" && s.runActive[row.Name] {
		busy = true
	}
	connected := ls != nil && ls.acp != nil && ls.acp.IsConnected()
	s.mu.Unlock()

	var containerStatus string
	switch {
	case dockerFailed:
		containerStatus = "unknown"
	case statusMap != nil:
		if st, ok := statusMap[row.Name]; ok {
			containerStatus = st
		} else {
			containerStatus = "not_found"
		}
	default:
		containerStatus = s.mgr.Status(ctx, row.Name)
	}

	v := SandboxView{
		Sandbox:         *row,
		ContainerStatus: containerStatus,
		Busy:            busy,
		Connected:       connected,
		HasCodeServer:   row.CodeServerPort > 0,
		HasACP:          row.ACPPort > 0,
		Password:        row.Token,
	}
	s.applyGatewayLifecycle(ctx, row, &v)
	return v
}

// applyGatewayLifecycle overlays gateway store status onto a still-booting
// local row so UI polls see status=pulling while the image downloads (plan g2.2).
// Local DB may still say "creating" because WaitRunning blocks inside Manager.Create;
// the gateway record (by id or grasp.name correlation) is the source of truth.
func (s *SandboxService) applyGatewayLifecycle(ctx context.Context, row *models.Sandbox, v *SandboxView) {
	if row == nil || v == nil {
		return
	}
	local := strings.TrimSpace(row.Status)
	if local != "creating" && local != "pulling" {
		return
	}
	gwStatus := s.lookupGatewayLifecycleStatus(ctx, row.Name)
	switch gwStatus {
	case "pulling":
		v.Status = "pulling"
		// Best-effort persist so list badges stay aligned without waiting for Register.
		if local != "pulling" {
			_ = s.db.Model(&models.Sandbox{}).Where("id = ? AND status IN ?", row.ID, []string{"creating", "pulling"}).
				Updates(map[string]any{"status": "pulling"}).Error
		}
	case "creating":
		if local == "pulling" {
			v.Status = "creating"
			_ = s.db.Model(&models.Sandbox{}).Where("id = ? AND status = ?", row.ID, "pulling").
				Updates(map[string]any{"status": "creating"}).Error
		}
	case "error":
		// Leave local status; startContainer / WaitRunning will fail the row.
	}
}

func (s *SandboxService) lookupGatewayLifecycleStatus(ctx context.Context, nameOrID string) string {
	nameOrID = strings.TrimSpace(nameOrID)
	if nameOrID == "" || s.mgr == nil {
		return ""
	}
	gw := s.mgr.Gateway()
	if gw == nil {
		return ""
	}
	if sb, err := gw.Get(ctx, nameOrID); err == nil && sb != nil {
		return strings.TrimSpace(sb.Status)
	}
	all, err := s.mgr.ListManaged(ctx)
	if err != nil {
		return ""
	}
	key := sandbox.CorrelationNameKey()
	for i := range all {
		if all[i].Labels[key] == nameOrID {
			return strings.TrimSpace(all[i].Status)
		}
	}
	return ""
}
