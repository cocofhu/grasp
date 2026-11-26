package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/sandbox"

	"github.com/rs/zerolog/log"
	"golang.org/x/crypto/ssh"
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
	switching := ls != nil && ls.switching
	s.mu.Unlock()
	if switching {
		return "", 0, "", fmt.Errorf("sandbox 正在切换编码工具")
	}
	host, port, password := row.Host, row.ACPPort, row.Token
	if ls != nil && ls.sb != nil {
		host, port = ls.sb.Host, ls.sb.Port
		if password == "" {
			password = ls.sb.Password
		}
	}
	// Gateway listeners are local to this Grasp process. Never trust a saved
	// loopback port after restart, or fall back to it when the gateway is down.
	if s.mgr != nil && s.mgr.Gateway() != nil && s.mgr.Gateway().UsesGatewayTransport(row.Name) {
		addr, err := s.mgr.EndpointAddr(ctx, row.Name, "session")
		if err != nil {
			return "", 0, "", err
		}
		host, portString, err := net.SplitHostPort(addr)
		if err != nil {
			return "", 0, "", err
		}
		port, err := strconv.Atoi(portString)
		return host, port, password, err
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

// ErrChatModelUnknown means the sandbox did not report its current chat model.
// AlignChatSettings must not call POST /api/model in that case: that call
// restarts the coding agent. The caller keeps the already applied model for
// this turn and retries on a later one.
var ErrChatModelUnknown = errors.New("chat model unknown")

func (s *SandboxService) ChatModelSettings(ctx context.Context, id uint, chat ...string) (sandbox.ChatModelSettings, error) {
	host, port, password, err := s.acpConnection(ctx, id)
	if err != nil {
		return sandbox.ChatModelSettings{}, err
	}
	if len(chat) > 0 {
		return sandbox.GetChatModelSettingsFor(ctx, host, port, password, chat[0])
	}
	return sandbox.GetChatModelSettings(ctx, host, port, password)
}

// ChatSwitch is the outcome of aligning a chat's model settings.
type ChatSwitch struct {
	// Restarted: the settings differed, so the coding agent was restarted.
	Restarted bool
	// Resumed: the restarted agent continues its native conversation and
	// still has the dialogue.
	Resumed bool
}

// AlignChatSettings changes the default chat's model, effort and speed tier as
// one unit at a turn boundary; empty model follows the primary model. Matching
// settings do not restart the coding agent, nor does a failed read. Older
// bundles continue to support model-only switches; they cannot silently
// accept a requested reasoning effort that they do not expose.
func (s *SandboxService) AlignChatSettings(ctx context.Context, id uint, model, effort, tier string) (ChatSwitch, error) {
	host, port, password, err := s.acpConnection(ctx, id)
	if err != nil {
		return ChatSwitch{}, err
	}
	current, err := sandbox.GetChatModelSettings(ctx, host, port, password)
	if err != nil {
		return ChatSwitch{}, fmt.Errorf("%w: %w", ErrChatModelUnknown, err)
	}
	model, effort, tier = strings.TrimSpace(model), strings.TrimSpace(effort), strings.TrimSpace(tier)
	var wantTier *string
	currentTier := ""
	if current.ServiceTier != nil {
		wantTier, currentTier = &tier, *current.ServiceTier
	} else if tier != "" {
		return ChatSwitch{}, errors.New("sandbox does not expose service tiers")
	}
	var resumed bool
	if current.ReasoningEffort == nil {
		if effort != "" {
			return ChatSwitch{}, errors.New("sandbox does not expose reasoning effort")
		}
		if current.Selected == model {
			return ChatSwitch{}, nil
		}
		resumed, err = sandbox.SetChatModel(ctx, host, port, password, model)
	} else {
		if current.Selected == model && *current.ReasoningEffort == effort && currentTier == tier {
			return ChatSwitch{}, nil
		}
		resumed, err = sandbox.SetChatModelSettings(ctx, host, port, password, model, effort, wantTier)
	}
	if err != nil {
		return ChatSwitch{}, err
	}
	return ChatSwitch{Restarted: true, Resumed: resumed}, nil
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
	if err := s.resumeForAccess(ctx, row); err != nil {
		return "", err
	}
	if row.CodeServerPort == 0 {
		return "", fmt.Errorf("no code-server for this sandbox")
	}
	if s.mgr != nil && row.Name != "" {
		if gw := s.mgr.Gateway(); gw != nil && gw.UsesGatewayTransport(row.Name) {
			addr, err := s.mgr.EndpointAddr(ctx, row.Name, "ide")
			if err == nil {
				return addr, nil
			}
			if rerr := s.resumeForAccess(ctx, row); rerr != nil {
				return "", rerr
			}
			return s.mgr.EndpointAddr(ctx, row.Name, "ide")
		}
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
// (the acp-bridge is the single source of truth) and returns it as the
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
	if err := s.resumeForAccess(ctx, row); err != nil {
		return nil, err
	}
	if status := s.mgr.Status(ctx, row.Name); status != "running" {
		return nil, fmt.Errorf("container 不在运行(状态:%s)", status)
	}
	return s.mgr.ExecPTYWithToken(ctx, row.Name, row.Token, nil)
}

// resumeForAccess restores every sandbox purpose for an explicit IDE or
// terminal request. Polling/list endpoints never call it.
func (s *SandboxService) resumeForAccess(ctx context.Context, row *models.Sandbox) error {
	if s.mgr == nil || row.Name == "" {
		return nil
	}
	if err := s.soloStoppedByUser(row, false); err != nil {
		return err
	}
	_, _, err := s.EnsureRunning(ctx, row.ID)
	return err
}

// soloStoppedByUser rejects a Solo environment the user stopped (or, with
// closed, also one whose session was closed).
func (s *SandboxService) soloStoppedByUser(row *models.Sandbox, closed bool) error {
	if row.Purpose != SandboxPurposeSolo {
		return nil
	}
	var session models.SoloSession
	if s.db.Where("sandbox_id = ?", row.ID).First(&session).Error != nil {
		return nil
	}
	switch {
	case session.Status == models.SoloStatusStopped || session.Status == models.SoloStatusStopping:
		return errors.New("会话已手动停止，请先恢复")
	case closed && session.Status == models.SoloStatusClosed:
		return errors.New("会话已关闭")
	}
	return nil
}

// DialSSH opens a root SSH client to sandbox id for the SSH gateway and holds
// it against idle reclaim until release is called. Like the web terminal it
// wakes a paused environment, but never one the user stopped. Waking, dialing
// and taking the hold share the recovery lock, so an idle save either finishes
// before the wake or sees the hold.
func (s *SandboxService) DialSSH(ctx context.Context, id uint) (*ssh.Client, func(), error) {
	row, err := s.Get(id)
	if err != nil {
		return nil, nil, errSSHNoAccess(id)
	}
	if s.mgr == nil || row.Name == "" {
		return nil, nil, errors.New("沙箱尚未就绪")
	}
	unlock, err := s.lockSoloRecovery(ctx, row.ID)
	if err != nil {
		return nil, nil, err
	}
	defer unlock()
	// Checked under the lock: a manual stop that just finished must not be
	// undone by this wake.
	if err := s.soloStoppedByUser(row, true); err != nil {
		return nil, nil, err
	}
	sb, _, err := s.ensureRunningLocked(ctx, row.ID)
	if err != nil {
		return nil, nil, err
	}
	cli, err := sb.DialSSH(ctx)
	if err != nil {
		return nil, nil, err
	}
	return cli, s.AcquireExternalSession(row.ID), nil
}

func errSSHNoAccess(id uint) error {
	return fmt.Errorf("沙箱 %d 不存在或无权访问", id)
}

// SetSSHAdminLookup reports admin accounts for SSH gateway authorization.
func (s *SandboxService) SetSSHAdminLookup(isAdmin func(username string) bool) {
	s.mu.Lock()
	s.sshAdmin = isAdmin
	s.mu.Unlock()
}

// AuthorizeSSH lets the owner of a Solo session into its sandbox and admins
// into any sandbox. Workflow and test sandboxes have no owner, so only admins
// may enter them. Missing and forbidden sandboxes give the same error.
func (s *SandboxService) AuthorizeSSH(username string, id uint) error {
	row, err := s.Get(id)
	if err != nil || strings.TrimSpace(username) == "" {
		return errSSHNoAccess(id)
	}
	s.mu.Lock()
	isAdmin := s.sshAdmin
	s.mu.Unlock()
	if isAdmin != nil && isAdmin(username) {
		return nil
	}
	if row.Purpose == SandboxPurposeSolo {
		var n int64
		if s.db.Model(&models.SoloSession{}).Where("sandbox_id = ? AND user_id = ?", row.ID, username).Count(&n).Error == nil && n > 0 {
			return nil
		}
	}
	return errSSHNoAccess(id)
}

// EnsureRunningByName wakes an explicitly accessed environment, including a
// workflow sandbox waiting for a human response.
func (s *SandboxService) EnsureRunningByName(ctx context.Context, name string) error {
	var row models.Sandbox
	if err := s.db.Where("name = ?", name).First(&row).Error; err != nil {
		return err
	}
	return s.resumeForAccess(ctx, &row)
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
		if gw.UsesGatewayTransport(row.Name) {
			v.EndpointTransport = "gateway"
		}
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
	busy := ls != nil && (ls.busy || ls.switching)
	if row.Purpose == "run" && (s.runActive[row.Name] || s.runTasks[row.Name] > 0) {
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
	v.DestroyAt = nil
	if kind, ok := s.cachedRecoveryKind(row); ok && kind == "snapshot" && row.Status == "running" && !busy && !s.keepAlive(row) && !s.externalBusy(row.ID) {
		deadline := s.reclaimIdleSince(row).Add(s.IdleTTL())
		v.DestroyAt = &deadline
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
