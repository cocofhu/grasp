package handlers

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/cocofhu/grasp/internal/gateshare"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/sandbox"
	"github.com/cocofhu/grasp/internal/textutil"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

const appPreviewGuestProtocol = "grasp-preview-guest"

type previewGuestRequest struct {
	previewChatMessage
	Type            string  `json:"type,omitempty"`
	ID              string  `json:"id,omitempty"`
	ModelID         *string `json:"modelId"`
	ReasoningEffort string  `json:"reasoningEffort"`
	ServiceTier     *string `json:"serviceTier"`
	ctx             context.Context
	finish          context.CancelFunc
}

// Register before enqueueing so even a cancel sent immediately after a message
// reaches that turn. IDs keep a late cancel from aborting the next reply.
type previewGuestTurns struct {
	mu      sync.Mutex
	cancels map[string]context.CancelFunc
}

func (t *previewGuestTurns) prepare(ctx context.Context, request *previewGuestRequest) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	if request.ID == "" {
		request.ID = uuid.NewString() // Older clients do not supply turn IDs.
	}
	if _, exists := t.cancels[request.ID]; exists {
		return false
	}
	if t.cancels == nil {
		t.cancels = make(map[string]context.CancelFunc)
	}
	turnCtx, cancel := context.WithCancel(ctx)
	id := request.ID
	t.cancels[id] = cancel
	request.ctx = turnCtx
	request.finish = func() {
		t.mu.Lock()
		delete(t.cancels, id)
		t.mu.Unlock()
		cancel()
	}
	return true
}

func (t *previewGuestTurns) cancel(id string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if cancel := t.cancels[id]; cancel != nil {
		cancel()
	}
}

func (h *Handlers) writePreviewGuestModelCatalog(c *gin.Context, sb models.Sandbox) {
	if h.Sbx == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "model catalog unavailable"})
		return
	}
	settings, err := h.Sbx.ChatModelSettings(c.Request.Context(), sb.ID)
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "model catalog unavailable"})
		return
	}
	models := []sandbox.ChatModelOption{}
	if settings.Configured {
		models = append(models, settings.Models...)
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, gin.H{"runtime": gin.H{"models": models, "default": settings.Default}})
}

// PublicAppPreviewGuestChat owns one independent bridge chat per open page.
// Each turn and its sanitized timeline are scoped to the shared app.
func (h *Handlers) PublicAppPreviewGuestChat(c *gin.Context) {
	if !h.publicRateLimit(c, gateshare.RateBucketTicket) {
		return
	}
	protocols := websocket.Subprotocols(c.Request)
	if !sameOriginRequest(c) || h.GateShareTickets == nil || h.Sbx == nil ||
		len(protocols) != 2 || protocols[0] != appPreviewGuestProtocol {
		c.Status(http.StatusUnauthorized)
		return
	}
	claims, _ := h.GateShareTickets.Lookup(protocols[1])
	link, sb := h.activeAppPreviewTicket(claims)
	if link == nil || link.PreviewChatMode != models.PreviewChatIsolated {
		c.Status(http.StatusUnauthorized)
		return
	}
	u := upgrader
	u.Subprotocols = []string{appPreviewGuestProtocol}
	conn, err := u.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		return
	}
	conn.SetReadLimit(maxPreviewChatRequest)
	configureEventHeartbeat(conn, eventPongWait)
	ctx, cancel := context.WithDeadline(c.Request.Context(), link.ExpiresAt)
	requests := make(chan previewGuestRequest, 1)
	go readPreviewGuestRequests(ctx, cancel, conn, requests, &previewGuestTurns{})
	go h.watchAppPreviewChat(ctx, cancel, conn, claims)
	var client *sandbox.ACPClient
	var closeChat func()
	var history []string
	var restoreHistory bool
	var selection *previewGuestRequest
	var finishTurn context.CancelFunc
	defer func() {
		cancel()
		if finishTurn != nil {
			finishTurn()
		}
		_ = conn.Close()
		if closeChat != nil {
			closeChat()
		}
	}()
	for {
		if finishTurn != nil {
			finishTurn()
			finishTurn = nil
		}
		select {
		case <-ctx.Done():
			return
		case request := <-requests:
			finishTurn = request.finish
			turnCtx := request.ctx
			if active, _ := h.activeAppPreviewTicket(claims); active == nil {
				return
			}
			if turnCtx.Err() != nil {
				writePreviewGuestUnavailable(conn, request)
				continue
			}
			if h.Shutdown != nil && h.Shutdown.IsDraining() {
				writePreviewGuestUnavailable(conn, request)
				continue
			}
			if client == nil || !client.IsConnected() || client.BridgeState().Desynced {
				if closeChat != nil {
					closeChat()
				}
				restoreHistory = len(history) > 0
				client, closeChat, err = h.Sbx.OpenPreviewGuest(turnCtx, sb)
				if err != nil {
					writePreviewGuestUnavailable(conn, request)
					continue
				}
				if selection != nil && request.ModelID == nil {
					if _, err := client.SetIndependentModelSettings(turnCtx, *selection.ModelID, selection.ReasoningEffort, selection.ServiceTier); err != nil {
						closeChat()
						client, closeChat = nil, nil
						writePreviewGuestUnavailable(conn, request)
						continue
					}
				}
			}
			if request.ModelID == nil && request.ServiceTier != nil {
				current, readErr := h.Sbx.ChatModelSettings(turnCtx, sb.ID, client.ChatID())
				if readErr != nil {
					if writeChatJSON(conn, gin.H{"type": "model", "ok": false}) != nil {
						return
					}
					continue
				}
				completePreviewGuestTierSelection(&request, current)
			}
			if request.ModelID != nil {
				restarted, err := client.SetIndependentModelSettings(turnCtx, *request.ModelID, request.ReasoningEffort, request.ServiceTier)
				restoreHistory = restoreHistory || restarted
				if err == nil {
					// Remember the actual value after an implicit model switch, which
					// can clear a speed tier unsupported by the new model.
					if current, readErr := h.Sbx.ChatModelSettings(turnCtx, sb.ID, client.ChatID()); readErr == nil {
						request.ServiceTier = current.ServiceTier
					} else if request.ServiceTier == nil && selection != nil {
						request.ServiceTier = selection.ServiceTier
					}
					selection = &request
				}
				if writeChatJSON(conn, gin.H{"type": "model", "ok": err == nil}) != nil {
					return
				}
				continue
			}
			refs, err := h.prepareAppPreviewReferences(turnCtx, link, sb, request.References)
			if err != nil {
				writePreviewGuestUnavailable(conn, request)
				continue
			}
			instruction := models.RenderAnnotatedPrompt(request.Text, request.Annotations)
			if len(refs) > 0 {
				instruction = h.Solo.RenderReferenceBlock(refs) + "\n\n" + instruction
			}
			prompt := instruction
			if restoreHistory && len(history) > 0 {
				prompt = "Earlier messages in this preview chat (context only):\n" +
					strings.Join(history, "\n\n") + "\n\nCurrent user message:\n" + prompt
			}
			client.WithExecution(h.Sbx.PreviewGuestExecution(turnCtx, sb, client.ChatID()))
			chat := func(ctx context.Context, client *sandbox.ACPClient, prompt string, files []models.PromptImage, onProgress func(*sandbox.ChatResult)) (*sandbox.ChatResult, error) {
				return h.Sbx.ChatPreviewGuestTurn(ctx, sb, link.RunID, link.NodeID, client, prompt, files, onProgress)
			}
			narration, completed, sent := writePreviewGuestTurn(turnCtx, cancel, conn, client, chat, prompt, request.Images)
			if !sent {
				return
			}
			if completed || errors.Is(turnCtx.Err(), context.Canceled) {
				restoreHistory = false
				role := "Agent: "
				if !completed {
					role = "Agent (interrupted): "
				}
				history = append(history, "User: "+textutil.TruncateBytes(instruction, 2048, "…")+
					"\n"+role+textutil.TruncateBytes(narration, 2048, "…"))
				if len(history) > 8 {
					history = history[len(history)-8:]
				}
			}
		}
	}
}

func writePreviewGuestUnavailable(conn *websocket.Conn, request previewGuestRequest) {
	if request.ctx != nil && errors.Is(request.ctx.Err(), context.Canceled) && request.ModelID == nil {
		_ = writeChatJSON(conn, gin.H{"type": "done", "interrupted": true, "endedAt": time.Now().Format(time.RFC3339Nano)})
		return
	}
	if request.ModelID != nil {
		_ = writeChatJSON(conn, gin.H{"type": "model", "ok": false})
		return
	}
	_ = writeChatJSON(conn, gin.H{"type": "error", "code": "unavailable"})
}

func readPreviewGuestRequests(ctx context.Context, cancel context.CancelFunc, conn *websocket.Conn, requests chan<- previewGuestRequest, turns *previewGuestTurns) {
	defer cancel()
	for ctx.Err() == nil {
		var frame previewGuestRequest
		if err := conn.ReadJSON(&frame); err != nil {
			return
		}
		if len(frame.ID) > 128 {
			return
		}
		if frame.Type == "cancel" {
			if frame.ID == "" || frame.ModelID != nil || frame.Text != "" || frame.ReasoningEffort != "" || frame.ServiceTier != nil || len(frame.Annotations)+len(frame.Images)+len(frame.References) != 0 {
				return
			}
			turns.cancel(frame.ID)
			continue
		}
		if frame.Type != "" {
			return
		}
		if frame.ModelID == nil && frame.ServiceTier == nil {
			if !frame.normalize() {
				return
			}
		} else if frame.Text != "" || len(frame.Annotations)+len(frame.Images)+len(frame.References) != 0 || frame.ModelID != nil && len(*frame.ModelID) > 512 || len(frame.ReasoningEffort) > 64 || frame.ServiceTier != nil && len(*frame.ServiceTier) > 64 || frame.ModelID == nil && frame.ReasoningEffort != "" {
			return
		}
		if !turns.prepare(ctx, &frame) {
			return
		}
		select {
		case requests <- frame:
		default:
			frame.finish()
			return
		}
	}
}

func (h *Handlers) watchAppPreviewChat(ctx context.Context, cancel context.CancelFunc, conn *websocket.Conn, claims *gateshare.PreviewTicketClaims) {
	defer cancel()
	defer func() { _ = conn.Close() }()
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if link, _ := h.activeAppPreviewTicket(claims); link == nil {
				return
			}
			if err := conn.WriteControl(websocket.PingMessage, nil, time.Now().Add(chatWriteWait)); err != nil {
				return
			}
		}
	}
}

// previewGuestChat runs one visitor turn on client; production ledgers it.
type previewGuestChat func(ctx context.Context, client *sandbox.ACPClient, prompt string, files []models.PromptImage, onProgress func(*sandbox.ChatResult)) (*sandbox.ChatResult, error)

func writePreviewGuestTurn(ctx context.Context, cancel context.CancelFunc, conn *websocket.Conn, client *sandbox.ACPClient, chat previewGuestChat, text string, images []models.PromptImage) (string, bool, bool) {
	last := time.Time{}
	lastSteps, lastSegments := -1, -1
	var lastTools []sandbox.ACPToolCall
	started := time.Now()
	execution := client.Execution()
	if execution != nil {
		execution.StartedAt = started
	}
	frame := func(kind string, result *sandbox.ChatResult) gin.H {
		narration := previewGuestNarration(result)
		return gin.H{"type": kind, "text": narration,
			"parts":     gateshare.SanitizeParts(models.PartsForReply(result.AcpEvents(), narration)),
			"startedAt": started.Format(time.RFC3339Nano), "execution": previewExecution(execution)}
	}
	result, err := chat(ctx, client, text, images, func(progress *sandbox.ChatResult) {
		if progress == nil {
			return
		}
		// Text bursts may coalesce, but a tool can run silently for seconds:
		// never throttle a step boundary or a changed tool status.
		changed := len(progress.Timeline) != lastSteps || len(progress.Segments) != lastSegments || len(progress.ToolCalls) != len(lastTools)
		if !changed {
			for i, tool := range progress.ToolCalls {
				if tool.Title != lastTools[i].Title || tool.Status != lastTools[i].Status {
					changed = true
					break
				}
			}
		}
		if !changed && time.Since(last) < 125*time.Millisecond {
			return
		}
		last = time.Now()
		lastSteps, lastSegments = len(progress.Timeline), len(progress.Segments)
		lastTools = append(lastTools[:0], progress.ToolCalls...)
		if writeChatJSON(conn, frame("reply", progress)) != nil {
			cancel()
		}
	})
	failed := err != nil || result == nil || result.Failed || result.Interrupted || result.ErrorText != ""
	narration := previewGuestNarration(result)
	done := frame("done", result)
	done["failed"] = failed
	done["interrupted"] = (result != nil && result.Interrupted) || errors.Is(ctx.Err(), context.Canceled)
	done["endedAt"] = time.Now().Format(time.RFC3339Nano)
	done["durationMs"] = time.Since(started).Milliseconds()
	if result != nil {
		done["usage"], done["usageByModel"] = result.Usage, result.UsageByModel
	}
	return narration, !failed, writeChatJSON(conn, done) == nil
}

func previewGuestNarration(result *sandbox.ChatResult) string {
	if result == nil {
		return ""
	}
	parts := make([]string, 0, len(result.Segments)+1)
	for _, segment := range result.Segments {
		if segment.Narration != "" {
			parts = append(parts, segment.Narration)
		}
	}
	return strings.Join(append(parts, result.Narration), "\n\n")
}

// Tier-only guest settings preserve the independent chat's actual model and effort.
func completePreviewGuestTierSelection(request *previewGuestRequest, current sandbox.ChatModelSettings) {
	model := current.Selected
	request.ModelID = &model
	if current.ReasoningEffort != nil {
		request.ReasoningEffort = *current.ReasoningEffort
	}
}
