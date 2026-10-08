// Package acpx adapts an ACP panel (long-lived JSON-RPC-over-stdio session) to
// the transport-agnostic provider.Session interface.
package acpx

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"

	"backend/internal/acp"
	"backend/internal/provider"
)

// session wraps *acp.Panel to satisfy provider.Session.
type session struct {
	p *acp.Panel
}

// Wrap adapts an ACP panel into a provider.Session.
func Wrap(p *acp.Panel) provider.Session { return &session{p: p} }

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
	stopReason, err := s.p.Prompt(ctx, text, images)
	return provider.TurnResult{StopReason: stopReason}, err
}

// The ACP transport does not currently surface token usage.
func (s *session) ReportsUsage() bool                              { return false }
func (s *session) CumulativeUsage() map[string]provider.TokenUsage { return nil }

// AgentPIDs are the ACP agent subprocess and its terminals.
func (s *session) AgentPIDs() []int { return s.p.ProcessPIDs() }

func (s *session) Cancel() error { return s.p.Cancel() }
func (s *session) Close() error  { return s.p.Close() }

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
