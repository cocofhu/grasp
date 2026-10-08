package sandbox

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// ExitInfo is the gateway's account of the sandbox container's last exit.
type ExitInfo struct {
	Reason    string    `json:"reason"`
	ExitCode  int       `json:"exitCode"`
	OOMKilled bool      `json:"oomKilled"`
	Message   string    `json:"message,omitempty"`
	Restarts  int       `json:"restarts"`
	MemoryMB  int64     `json:"memoryMB,omitempty"`
	At        time.Time `json:"at,omitempty"`
}

// Describe renders the exit for node errors and the timeline.
func (e *ExitInfo) Describe() string {
	switch {
	case e.OOMKilled && e.MemoryMB > 0:
		return fmt.Sprintf("沙箱 OOM 被杀(%dMiB)", e.MemoryMB)
	case e.OOMKilled:
		return "沙箱 OOM 被杀"
	case e.Reason == "Evicted":
		return strings.TrimSpace("沙箱被驱逐 " + e.Message)
	}
	return fmt.Sprintf("沙箱容器退出(%s,退出码 %d)", e.Reason, e.ExitCode)
}

// ErrExitUnknown means the gateway cannot report exits (older gateway, or the
// sandbox record is gone).
var ErrExitUnknown = errors.New("gateway does not report sandbox exits")

// LastExit asks the gateway why sandbox id's container last exited; nil when
// it never has.
func (g *GatewayClient) LastExit(ctx context.Context, id string) (*ExitInfo, error) {
	var out struct {
		Exit *ExitInfo `json:"exit"`
	}
	if err := g.do(ctx, http.MethodGet, "/api/v1/sandboxes/"+id+"/exit", nil, &out); err != nil {
		if strings.Contains(err.Error(), fmt.Sprintf(": %d ", http.StatusNotFound)) {
			return nil, ErrExitUnknown
		}
		return nil, err
	}
	return out.Exit, nil
}

// ExitError wraps a chat failure with the container exit that caused it.
type ExitError struct {
	Err  error
	Exit *ExitInfo
}

func (e *ExitError) Error() string { return e.Exit.Describe() + ": " + e.Err.Error() }
func (e *ExitError) Unwrap() error { return e.Err }

// AsExitError returns the container exit carried by err, if any.
func AsExitError(err error) (*ExitInfo, bool) {
	var ee *ExitError
	if errors.As(err, &ee) {
		return ee.Exit, true
	}
	return nil, false
}

var (
	exitProbeTries = 5
	exitProbeEvery = 2 * time.Second
)

// ExplainLoss attaches the container's last exit to a lost-connection error.
// Kubelet records the termination a moment after the connection drops, so it
// polls briefly; any other error, or no recorded exit, is returned unchanged.
func (s *Sandbox) ExplainLoss(ctx context.Context, err error) error {
	if s == nil || s.mgr == nil || s.mgr.gw == nil ||
		!(errors.Is(err, ErrConnClosed) || errors.Is(err, ErrChatIdle)) {
		return err
	}
	for i := 0; i < exitProbeTries; i++ {
		if i > 0 {
			select {
			case <-ctx.Done():
				return err
			case <-time.After(exitProbeEvery):
			}
		}
		pctx, cancel := context.WithTimeout(ctx, 10*time.Second)
		e, gerr := s.mgr.gw.LastExit(pctx, s.ID)
		cancel()
		if gerr != nil {
			return err
		}
		if e != nil {
			return &ExitError{Err: err, Exit: e}
		}
	}
	return err
}
