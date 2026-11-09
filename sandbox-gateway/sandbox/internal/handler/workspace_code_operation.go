package handler

import (
	"context"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

type workspaceCodeOperation struct {
	generation uint64
	expires    time.Time // guarded by the handler's fenceMu
	ctx        context.Context
	cancel     context.CancelCauseFunc
	changed    chan struct{}
}

func codeFence(c *gin.Context) (uint64, time.Time, bool) {
	generation, err := strconv.ParseUint(c.GetHeader("X-Grasp-Operation-Generation"), 10, 64)
	expires, dateErr := time.Parse(time.RFC3339Nano, c.GetHeader("X-Grasp-Operation-Expires-At"))
	now := time.Now()
	return generation, expires, err == nil && dateErr == nil && generation != 0 && expires.After(now) && !expires.After(now.Add(time.Minute))
}

func codeFenceError(c *gin.Context, code string) {
	c.JSON(http.StatusConflict, gin.H{"code": code, "error": code})
}

// Short physical leases remain the authority for long transfers. A successor
// cancels its predecessor, then waits for the handler to reap its subprocesses
// before entering this handler-wide lane, including across binding views.
func (h *WorkspaceCode) operation(c *gin.Context) (context.Context, context.CancelFunc, bool) {
	generation, expires, ok := codeFence(c)
	if !ok {
		codeFenceError(c, "workspace_operation_expired")
		return nil, nil, false
	}
	total, stopTotal := context.WithTimeout(c.Request.Context(), 30*time.Minute)
	ctx, cancel := context.WithCancelCause(total)
	operation := &workspaceCodeOperation{generation: generation, expires: expires, ctx: ctx, cancel: cancel, changed: make(chan struct{}, 1)}
	h.fenceMu.Lock()
	if generation <= h.generation {
		h.fenceMu.Unlock()
		stopTotal()
		cancel(context.Canceled)
		codeFenceError(c, "workspace_operation_superseded")
		return nil, nil, false
	}
	if h.active != nil {
		h.active.cancel(context.Canceled)
	}
	h.generation, h.active = generation, operation
	h.fenceMu.Unlock()
	// A derived deadline does not close an incoming HTTP body automatically.
	// Closing it also releases a stalled upload before a successor may enter.
	body, response := c.Request.Body, http.NewResponseController(c.Writer)
	bodyClosed := make(chan struct{})
	stopBody := context.AfterFunc(ctx, func() {
		defer close(bodyClosed)
		_ = response.SetReadDeadline(time.Now())
		_ = response.SetWriteDeadline(time.Now())
		_ = body.Close()
	})
	go h.watchCodeLease(operation)
	finish := func() {
		stopped := stopBody()
		cancel(context.Canceled)
		stopTotal()
		if !stopped {
			<-bodyClosed
		}
		h.fenceMu.Lock()
		if h.active == operation {
			h.active = nil
		}
		h.fenceMu.Unlock()
	}
	select {
	case h.lane <- struct{}{}:
	case <-ctx.Done():
		finish()
		codeFenceError(c, "workspace_operation_expired")
		return nil, nil, false
	}
	var once sync.Once
	release := func() { once.Do(func() { finish(); <-h.lane }) }
	if ctx.Err() != nil {
		release()
		codeFenceError(c, "workspace_operation_expired")
		return nil, nil, false
	}
	return ctx, release, true
}

func (h *WorkspaceCode) watchCodeLease(operation *workspaceCodeOperation) {
	h.fenceMu.Lock()
	expires := operation.expires
	h.fenceMu.Unlock()
	timer := time.NewTimer(max(time.Until(expires), 0))
	defer timer.Stop()
	for {
		select {
		case <-operation.ctx.Done():
			return
		case <-operation.changed:
		case <-timer.C:
		}
		h.fenceMu.Lock()
		expires = operation.expires
		if !expires.After(time.Now()) {
			operation.cancel(context.DeadlineExceeded)
			h.fenceMu.Unlock()
			return
		}
		h.fenceMu.Unlock()
		timer.Reset(time.Until(expires))
	}
}

// Renew only acknowledges the current, still-live generation. Replaying a
// request, changing generation or renewing after expiry cannot revive work.
func (h *WorkspaceCode) Renew(c *gin.Context) {
	generation, expires, ok := codeFence(c)
	if !ok {
		codeFenceError(c, "workspace_operation_expired")
		return
	}
	h.fenceMu.Lock()
	current := h.active
	if current == nil || current.generation != generation || current.ctx.Err() != nil || !current.expires.After(time.Now()) || expires.Before(current.expires) {
		h.fenceMu.Unlock()
		codeFenceError(c, "workspace_operation_superseded")
		return
	}
	current.expires = expires
	select {
	case current.changed <- struct{}{}:
	default:
	}
	h.fenceMu.Unlock()
	c.JSON(http.StatusOK, gin.H{"generation": generation, "expiresAt": expires.UTC().Format(time.RFC3339Nano)})
}
