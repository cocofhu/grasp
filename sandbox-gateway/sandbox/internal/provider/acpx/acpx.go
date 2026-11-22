// Package acpx adapts an ACP panel (long-lived JSON-RPC-over-stdio session) to
// the transport-agnostic provider.Session interface.
package acpx

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"sync"

	"backend/internal/acp"
	"backend/internal/provider"
)

// session wraps *acp.Panel to satisfy provider.Session.
type session struct {
	p           *acp.Panel
	cleanup     func()
	cleanupOnce sync.Once
	// failed is set by the event filter when the agent reported, as text,
	// that the turn failed.
	failed *turnFailure

	mu         sync.Mutex
	cumulative map[string]provider.TokenUsage
}

// Wrap adapts an ACP panel into a provider.Session.
func Wrap(p *acp.Panel) provider.Session { return &session{p: p} }

func wrapWithCleanup(p *acp.Panel, cleanup func()) provider.Session {
	s := &session{p: p, cleanup: cleanup}
	go func() { <-s.Done(); s.clean() }()
	return s
}

func (s *session) clean() {
	s.cleanupOnce.Do(func() {
		if s.cleanup != nil {
			s.cleanup()
		}
	})
}

func (s *session) SessionID() string { return s.p.SessionID }
func (s *session) CWD() string       { return s.p.CWD }
func (s *session) FSRoot() string    { return s.p.FSRoot }

func (s *session) Info() provider.AgentInfo {
	return provider.AgentInfo{
		Name:      s.p.AgentName,
		Title:     s.p.AgentTitle,
		Version:   s.p.AgentVersion,
		ModelID:   s.p.ModelID,
		ModelName: s.p.ModelName,
	}
}

func (s *session) Prompt(ctx context.Context, text string, images []provider.PromptImage) (provider.TurnResult, error) {
	if s.failed != nil {
		s.failed.reset()
	}
	res, err := s.p.PromptTurn(ctx, text, images)
	out := provider.TurnResult{StopReason: res.StopReason, Usage: res.Usage, UsagePartial: err != nil && len(res.Usage) > 0}
	if err == nil && s.failed != nil && s.failed.seen() {
		out.StopReason = "failed"
	}
	if len(res.Usage) > 0 {
		s.mu.Lock()
		if s.cumulative == nil {
			s.cumulative = map[string]provider.TokenUsage{}
		}
		provider.AddUsage(s.cumulative, res.Usage)
		s.mu.Unlock()
	}
	return out, err
}

// ReportsUsage is true for agents whose prompt result carries turn usage.
func (s *session) ReportsUsage() bool { return s.p.ReportsUsage() }

func (s *session) CumulativeUsage() map[string]provider.TokenUsage {
	s.mu.Lock()
	defer s.mu.Unlock()
	return provider.CopyUsage(s.cumulative)
}

// Capabilities: ACP has no mid-turn input; resume depends on the agent.
func (s *session) Capabilities() provider.Capabilities {
	return provider.Capabilities{Resume: s.p.CanResume()}
}

// AgentPIDs are the ACP agent subprocess and its terminals.
func (s *session) AgentPIDs() []int { return s.p.ProcessPIDs() }

func (s *session) Cancel() error { return s.p.Cancel() }
func (s *session) Close() error  { err := s.p.Close(); s.clean(); return err }

var closedCh = func() chan struct{} {
	c := make(chan struct{})
	close(c)
	return c
}()

func (s *session) Done() <-chan struct{} {
	if c := s.p.Conn(); c != nil {
		return c.Done()
	}
	return closedCh
}

// ExitInfo reaps the subprocess and composes the operator-facing exit banner.
func (s *session) ExitInfo() (string, error) {
	conn := s.p.Conn()
	if conn == nil {
		return "Agent 子进程已结束。", nil
	}
	waitErr := conn.Wait()
	var sb strings.Builder
	sb.WriteString("Agent 子进程已结束。")
	if waitErr != nil {
		var ee *exec.ExitError
		if errors.As(waitErr, &ee) {
			fmt.Fprintf(&sb, " 退出码=%d。", ee.ExitCode())
		} else {
			fmt.Fprintf(&sb, " %v。", waitErr)
		}
	}
	if re := conn.ReadErr(); re != nil {
		fmt.Fprintf(&sb, " stdout: %v。", re)
	}
	sb.WriteString(" 请在运行 backend 的同一终端查看「agent stderr」完整输出；" +
		"确认 agent CLI 可运行且已完成 login 或配置 API 密钥。")
	return sb.String(), waitErr
}
