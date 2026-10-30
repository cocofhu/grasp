package services

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/cocofhu/grasp/internal/mcp"
	"github.com/cocofhu/grasp/internal/models"

	"github.com/rs/zerolog/log"
	"gorm.io/gorm"
)

// ConversationScopeSource resolves a current, authorized sandbox generation.
// Production uses the same resolver as persisted artifact-store authorization.
type ConversationScopeSource func(runID string) (models.Sandbox, bool)

type ConversationHistoryService struct {
	db      *gorm.DB
	binding ConversationScopeSource
}

func NewConversationHistoryService(db *gorm.DB, binding ConversationScopeSource) *ConversationHistoryService {
	return &ConversationHistoryService{db: db, binding: binding}
}

// Version 1 cursors page the caller's own conversation; version 2 cursors
// page a referenced conversation and are bound to that reference.
type conversationCursor struct {
	Version    int    `json:"v"`
	SessionID  string `json:"s"`
	SnapshotID string `json:"h"`
	BeforeID   string `json:"b,omitempty"`
	RefID      string `json:"r,omitempty"`
}

// historyScope is the conversation one read may see. An empty AnchorID reads
// up to the latest message; RefID selects the reference cursor format.
type historyScope struct {
	SessionID string
	Title     string
	AnchorID  string
	RefID     string
}

func (h historyScope) cursorVersion() int {
	if h.RefID != "" {
		return 2
	}
	return 1
}

func (h historyScope) readTool() string {
	if h.RefID != "" {
		return referenceReadTool(h.RefID)
	}
	return ownHistoryTool
}

func referenceReadTool(refID string) string {
	return fmt.Sprintf("artifact-store.read_reference(ref=%q)", refID)
}

func encodeConversationCursor(cursor conversationCursor) string {
	data, _ := json.Marshal(cursor)
	return base64.RawURLEncoding.EncodeToString(data)
}

func (s *ConversationHistoryService) sessionForRun(runID string) (*models.SoloSession, error) {
	if s.db == nil || s.binding == nil {
		return nil, errors.New("当前会话历史不可用")
	}
	sb, ok := s.binding(runID)
	if !ok || sb.RunID != runID || sb.Purpose != SandboxPurposeSolo || sb.ThreadID == "" {
		return nil, errors.New("当前会话历史不可用")
	}
	var session models.SoloSession
	err := s.db.Where("id = ? AND sandbox_id = ? AND agent_name = ? AND status = ?", sb.ThreadID, sb.ID, sb.Profile, models.SoloStatusRunning).First(&session).Error
	if err != nil {
		return nil, errors.New("当前会话历史不可用")
	}
	return &session, nil
}

// ReadConversationHistory reads the original message rows; no artifact or
// secondary conversation store is created. Cursors cannot cross session scope.
func (s *ConversationHistoryService) ReadConversationHistory(runID string, req mcp.ConversationHistoryRequest) (mcp.ConversationHistoryPage, error) {
	session, err := s.sessionForRun(runID)
	if err != nil {
		return mcp.ConversationHistoryPage{Messages: []mcp.ConversationHistoryMessage{}}, err
	}
	return s.readSession(historyScope{SessionID: session.ID, Title: session.Title}, req)
}

// readSession pages one conversation inside scope. It does not check the
// session status: callers authorize the scope before reading.
func (s *ConversationHistoryService) readSession(scope historyScope, req mcp.ConversationHistoryRequest) (mcp.ConversationHistoryPage, error) {
	page := mcp.ConversationHistoryPage{Messages: []mcp.ConversationHistoryMessage{}, ConversationID: scope.SessionID, Ref: scope.RefID}
	var err error
	if req.View == "" {
		req.View = "summary"
	}
	if req.View != "summary" && req.View != "messages" {
		return page, errors.New("view must be summary or messages")
	}
	if req.Limit == 0 {
		req.Limit = 20
	}
	if req.Limit < 1 || req.Limit > 50 {
		return page, errors.New("limit must be between 1 and 50")
	}
	version := scope.cursorVersion()
	cursor := conversationCursor{Version: version, SessionID: scope.SessionID, RefID: scope.RefID}
	if req.Cursor != "" {
		if len(req.Cursor) > 4096 {
			return page, errors.New("invalid history cursor")
		}
		data, decodeErr := base64.RawURLEncoding.DecodeString(req.Cursor)
		if decodeErr != nil || json.Unmarshal(data, &cursor) != nil || cursor.Version != version || cursor.SessionID != scope.SessionID ||
			cursor.RefID != scope.RefID || cursor.SnapshotID == "" || (scope.AnchorID != "" && cursor.SnapshotID != scope.AnchorID) {
			return page, errors.New("invalid history cursor")
		}
	}
	base := s.db.Model(&models.SoloMessage{}).Where("session_id = ?", scope.SessionID)
	var snapshot models.SoloMessage
	switch {
	case cursor.SnapshotID != "":
		err = base.Session(&gorm.Session{}).Where("id = ?", cursor.SnapshotID).First(&snapshot).Error
	case scope.AnchorID != "":
		err = base.Session(&gorm.Session{}).Where("id = ?", scope.AnchorID).First(&snapshot).Error
	case scope.RefID != "":
		// A reference pinned while the target had no messages stays empty.
		err = gorm.ErrRecordNotFound
	default:
		err = base.Session(&gorm.Session{}).Order("created_at desc, id desc").First(&snapshot).Error
	}
	if errors.Is(err, gorm.ErrRecordNotFound) && req.Cursor == "" && scope.AnchorID == "" {
		if req.View == "summary" {
			page.Summary = "当前任务：" + scope.Title + "\n尚无历史对话。"
		}
		return page, nil
	}
	if err != nil {
		return page, errors.New("history snapshot unavailable")
	}
	cursor.SnapshotID = snapshot.ID
	page.HistoryVersion = encodeConversationCursor(conversationCursor{Version: version, SessionID: scope.SessionID, SnapshotID: snapshot.ID, RefID: scope.RefID})
	query := base.Session(&gorm.Session{}).Where("created_at < ? OR (created_at = ? AND id <= ?)", snapshot.CreatedAt, snapshot.CreatedAt, snapshot.ID)
	if cursor.BeforeID != "" {
		var before models.SoloMessage
		if err := base.Session(&gorm.Session{}).Where("id = ?", cursor.BeforeID).First(&before).Error; err != nil {
			return page, errors.New("history cursor unavailable")
		}
		query = query.Where("created_at < ? OR (created_at = ? AND id < ?)", before.CreatedAt, before.CreatedAt, before.ID)
	}
	var rows []models.SoloMessage
	if err := query.Order("created_at desc, id desc").Limit(req.Limit + 1).Find(&rows).Error; err != nil {
		return page, err
	}
	page.HasMore = len(rows) > req.Limit
	if page.HasMore {
		rows = rows[:req.Limit]
		cursor.BeforeID = rows[len(rows)-1].ID
		page.NextCursor = encodeConversationCursor(cursor)
	}
	for i := len(rows) - 1; i >= 0; i-- {
		page.Messages = append(page.Messages, conversationMessage(rows[i]))
	}
	if req.View == "summary" {
		var anchors []models.SoloMessage
		if err := base.Session(&gorm.Session{}).Where("role = ? AND (created_at < ? OR (created_at = ? AND id <= ?))", "user", snapshot.CreatedAt, snapshot.CreatedAt, snapshot.ID).
			Order("created_at asc, id asc").Find(&anchors).Error; err != nil {
			return page, err
		}
		page.Summary = renderContextBrief(scope.Title, anchors, scope.readTool())
	}
	return page, nil
}

// ReadReference reads a conversation the user explicitly attached to the
// current Solo session. The reference must sit on one of this session's user
// messages and the target must still belong to the same user.
func (s *ConversationHistoryService) ReadReference(runID, refID string, req mcp.ConversationHistoryRequest) (mcp.ConversationHistoryPage, error) {
	empty := mcp.ConversationHistoryPage{Messages: []mcp.ConversationHistoryMessage{}}
	session, err := s.sessionForRun(runID)
	if err != nil {
		return empty, err
	}
	ref, err := findAttachedReference(s.db, session.ID, refID)
	if err != nil {
		return empty, err
	}
	if ref.Kind != models.ReferenceKindSolo {
		return empty, &mcp.ReferenceHintError{Hint: githubReferenceHint(*ref)}
	}
	var target models.SoloSession
	if err := s.db.Where("id = ? AND user_id = ?", ref.Target, session.UserID).First(&target).Error; err != nil {
		return empty, errors.New("引用的会话已被删除或不可用")
	}
	log.Debug().Str("session", session.ID).Str("ref", ref.ID).Str("target_session", target.ID).Msg("reference_read")
	return s.readSession(historyScope{SessionID: target.ID, Title: target.Title, AnchorID: ref.Anchor, RefID: ref.ID}, req)
}

// findAttachedReference looks up refID among the references persisted on the
// session's own user messages. Nothing else can name a referenced session.
func findAttachedReference(db *gorm.DB, sessionID, refID string) (*models.MessageReference, error) {
	refID = strings.TrimSpace(refID)
	if refID == "" {
		return nil, errors.New("ref is required")
	}
	var rows []models.SoloMessage
	if err := db.Select("id", "message_references").Where("session_id = ? AND role = ? AND message_references LIKE ? ESCAPE ?", sessionID, "user", "%"+escapeLikePattern(refID)+"%", `\`).
		Find(&rows).Error; err != nil {
		return nil, err
	}
	for _, row := range rows {
		for i := range row.References {
			if row.References[i].ID == refID {
				return &row.References[i], nil
			}
		}
	}
	return nil, errors.New("ref not attached to this conversation")
}

func conversationMessage(row models.SoloMessage) mcp.ConversationHistoryMessage {
	images := append([]models.PromptImage(nil), row.Images...)
	for i := range images {
		images[i].Data = ""
	}
	return mcp.ConversationHistoryMessage{ID: row.ID, Role: row.Role, Content: row.Content, Status: row.Status,
		Error: row.Error, Images: images, CreatedAt: row.CreatedAt, ToolResults: conversationTools(row.Events)}
}

// Read only visible tool text/status from ACP updates, not the provider's raw
// event envelope or inline image bytes. Updates merge into their tool call.
func conversationTools(events []json.RawMessage) []mcp.ConversationHistoryToolResult {
	var tools []mcp.ConversationHistoryToolResult
	indices := map[string]int{}
	for _, raw := range events {
		var event struct {
			Params struct {
				Update struct {
					Kind    string `json:"sessionUpdate"`
					ID      string `json:"toolCallId"`
					Title   string `json:"title"`
					Status  string `json:"status"`
					Content []struct {
						Content struct {
							Text string `json:"text"`
						} `json:"content"`
					} `json:"content"`
				} `json:"update"`
			} `json:"params"`
		}
		if json.Unmarshal(raw, &event) != nil {
			continue
		}
		update := event.Params.Update
		if (update.Kind != "tool_call" && update.Kind != "tool_call_update") || update.ID == "" {
			continue
		}
		index, exists := indices[update.ID]
		if !exists {
			index = len(tools)
			indices[update.ID] = index
			tools = append(tools, mcp.ConversationHistoryToolResult{ID: update.ID})
		}
		if update.Title != "" {
			tools[index].Title = update.Title
		}
		if update.Status != "" {
			tools[index].Status = update.Status
		}
		var text []string
		for _, block := range update.Content {
			if block.Content.Text != "" {
				text = append(text, block.Content.Text)
			}
		}
		if len(text) > 0 {
			tools[index].Text = strings.Join(text, "\n")
		}
	}
	for i := range tools {
		// History results themselves contain prior messages and tool results.
		// Re-embedding those responses would recursively duplicate the same
		// history on every read. Keep the call reference and its outcome only.
		var result map[string]json.RawMessage
		isHistoryResponse := json.Unmarshal([]byte(tools[i].Text), &result) == nil && result["history_version"] != nil && result["messages"] != nil
		if title := strings.ToLower(tools[i].Title); strings.Contains(title, "get_history") || strings.Contains(title, "read_reference") || isHistoryResponse {
			tools[i].Text = "（历史查询正文已省略；使用 get_history 回查原始消息。）"
		}
	}
	return tools
}

// ownHistoryTool reads the current conversation back; a referenced one is
// read with read_reference and its handle instead.
const ownHistoryTool = "artifact-store.get_history"

func historyExcerpt(content string, budget int, via string) string {
	runes := []rune(content)
	if len(runes) <= budget {
		return content
	}
	return string(runes[:budget]) + "\n（此处为摘录；完整原文可通过 " + via + " 的 messages 分页回查。）"
}

func renderContextBrief(title string, anchors []models.SoloMessage, via string) string {
	var b strings.Builder
	b.WriteString("当前任务：" + title + "\n")
	b.WriteString(renderUserInstructions(anchors, 12000, via))
	b.WriteString("近期完整对话见 messages；更早原文见 next_cursor。任务进度以助手实际结果和当前工作区为准。")
	return b.String()
}

// Include corrections from the entire conversation, not just its beginning.
// When a ledger itself exceeds budget, retain later user decisions first and
// disclose the missing range instead of silently presenting stale constraints.
func renderUserInstructions(rows []models.SoloMessage, budget int, via string) string {
	var parts []string
	omitted := 0
	for i := len(rows) - 1; i >= 0; i-- {
		row := rows[i]
		if budget < 300 {
			omitted = i + 1
			break
		}
		part := fmt.Sprintf("[用户要求 · %s] %s\n\n", row.ID, historyExcerpt(row.Content, min(2000, budget-200), via))
		parts = append(parts, part)
		budget -= len([]rune(part))
	}
	var b strings.Builder
	b.WriteString("用户要求与更正（按时间顺序，较新的更正优先）：\n")
	if omitted > 0 {
		fmt.Fprintf(&b, "（较早的 %d 条用户消息未装入本次要求记录，范围 %s 至 %s；涉及先前约束时须先用 %s 分页回查，不得据旧要求推断当前决定。）\n", omitted, rows[0].ID, rows[omitted-1].ID, via)
	}
	for i := len(parts) - 1; i >= 0; i-- {
		b.WriteString(parts[i])
	}
	return b.String()
}

// contextPreamble gives a new CLI its initial task context without relying on
// the model to remember to call a tool. Original messages remain available via
// get_history. Current user input is excluded and appended exactly once by Solo.
func (s *SoloService) contextPreamble(sessionID, exceptMsgID, header string) string {
	text, _ := s.contextPreambleChecked(sessionID, exceptMsgID, header)
	return text
}

func (s *SoloService) contextPreambleChecked(sessionID, exceptMsgID, header string) (string, error) {
	base := s.db.Model(&models.SoloMessage{}).Where("session_id = ? AND role <> ? AND id <> ?", sessionID, "system", exceptMsgID)
	var recent, anchors []models.SoloMessage
	if err := base.Session(&gorm.Session{}).Order("created_at desc, id desc").Limit(12).Find(&recent).Error; err != nil {
		return "", err
	}
	if len(recent) == 0 {
		return "", nil
	}
	if err := base.Session(&gorm.Session{}).Where("role = ?", "user").Order("created_at asc, id asc").Find(&anchors).Error; err != nil {
		return "", err
	}
	var total int64
	if err := base.Session(&gorm.Session{}).Count(&total).Error; err != nil {
		return "", err
	}
	var b strings.Builder
	b.WriteString(header)
	b.WriteString("此前对话来自同一任务，请保留已有约束并遵守更新后的决定。需要补充历史时，调用 artifact-store 的 get_history(view=\"messages\")，按 next_cursor 向前读取。\n\n")
	b.WriteString(renderUserInstructions(anchors, 12000, ownHistoryTool))
	b.WriteString(renderReferenceIndex(anchors))
	if total > int64(len(recent)) {
		fmt.Fprintf(&b, "（较早的 %d 条对话正文未逐字附上；用户要求与更正已单独整理在上方，工具过程与完整原文通过 get_history 回查。）\n\n", total-int64(len(recent)))
	}
	// Keep complete recent turns when they fit. Very large individual messages
	// are explicitly marked as excerpts with a stable ID for full retrieval.
	budget := 32000 - len([]rune(b.String()))
	var parts []string
	for _, row := range recent {
		if budget <= 500 {
			b.WriteString("\n（部分近期对话未装入本次输入，请通过 get_history 读取完整记录。）\n")
			break
		}
		var part strings.Builder
		content := historyExcerpt(strings.TrimSpace(row.Content), min(budget-200, 8000), ownHistoryTool)
		role := "用户"
		if row.Role == "assistant" {
			role = "助手"
		}
		fmt.Fprintf(&part, "[%s] %s\n（历史消息 ID：%s）\n", role, content, row.ID)
		for _, image := range row.Images {
			fmt.Fprintf(&part, "附件：%s (%s，引用 %s)\n", image.Name, image.MimeType, image.Ref)
		}
		if row.Status == "failed" || row.Status == "stopped" {
			fmt.Fprintf(&part, "本轮状态：%s %s\n", row.Status, row.Error)
		}
		tools := conversationTools(row.Events)
		for _, tool := range tools[max(0, len(tools)-3):] {
			fmt.Fprintf(&part, "工具结果：%s [%s] %s\n", tool.Title, tool.Status, historyExcerpt(tool.Text, 1000, ownHistoryTool))
		}
		part.WriteString("\n")
		parts = append(parts, part.String())
		budget -= len([]rune(part.String()))
	}
	for i := len(parts) - 1; i >= 0; i-- {
		b.WriteString(parts[i])
	}
	return b.String(), nil
}
