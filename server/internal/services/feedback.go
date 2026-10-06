package services

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/cocofhu/grasp/internal/models"

	"gorm.io/gorm"
)

// Budgets for rendered feedback products. A reviewer can paste a lot; the row
// keeps what they wrote, the product keeps a bounded version so one verbose
// round cannot dominate the ledger or an Agent's context window.
const (
	feedbackTextMax         = 8 * 1024
	feedbackTurnMax         = 4 * 1024
	feedbackSummaryMax      = 240
	feedbackAgentSummaryMax = 4 * 1024
)

// ErrFeedbackNoSubstance is returned by Append when the event carries no human
// input at all. Callers treat it as a no-op, not a failure.
var ErrFeedbackNoSubstance = errors.New("feedback event has no substance")

// FeedbackService persists human feedback rounds and renders them into
// products. The table is the source of truth; the artifacts are derived, the
// same split run_error.json uses over StateRun.
type FeedbackService struct{ db *gorm.DB }

// NewFeedbackService builds the service.
func NewFeedbackService(db *gorm.DB) *FeedbackService { return &FeedbackService{db: db} }

// Append assigns the two ordinals and inserts the event.
//
// Seq is run-global; Round is scoped to (NodeID, Iteration) and is what makes
// the per-round artifact name unique. Both are allocated inside one
// transaction so two concurrent rounds cannot claim the same name and
// overwrite each other in the artifact store.
//
// Events without substance are rejected with ErrFeedbackNoSubstance and never
// reach the table.
func (s *FeedbackService) Append(ev *models.FeedbackEvent) error {
	if ev == nil {
		return errors.New("nil feedback event")
	}
	if !ev.HasSubstance() {
		return ErrFeedbackNoSubstance
	}
	normalizeFeedbackEvent(ev)

	return s.db.Transaction(func(tx *gorm.DB) error {
		var maxSeq struct{ V int }
		if err := tx.Model(&models.FeedbackEvent{}).
			Select("COALESCE(MAX(seq), 0) AS v").
			Where("run_id = ?", ev.RunID).Scan(&maxSeq).Error; err != nil {
			return err
		}
		var maxRound struct{ V int }
		if err := tx.Model(&models.FeedbackEvent{}).
			Select("COALESCE(MAX(round), 0) AS v").
			Where("run_id = ? AND node_id = ? AND iteration = ?", ev.RunID, ev.NodeID, ev.Iteration).
			Scan(&maxRound).Error; err != nil {
			return err
		}
		ev.Seq = maxSeq.V + 1
		ev.Round = maxRound.V + 1
		if ev.OccurredAt.IsZero() {
			ev.OccurredAt = time.Now()
		}
		if ev.WantsArtifact() {
			ev.ArtifactName = FeedbackArtifactName(ev.Kind, ev.NodeID, ev.Iteration, ev.Round)
		}
		return tx.Create(ev).Error
	})
}

// Events returns a run's feedback rounds in chronological order.
func (s *FeedbackService) Events(runID string) []models.FeedbackEvent {
	var out []models.FeedbackEvent
	s.db.Where("run_id = ?", runID).Order("seq asc, id asc").Find(&out)
	return out
}

// normalizeFeedbackEvent enforces the storage invariants: attachments carry
// refs only (never base64), and free text is bounded.
func normalizeFeedbackEvent(ev *models.FeedbackEvent) {
	ev.Text = truncateStr(strings.TrimSpace(ev.Text), feedbackTextMax)
	ev.AgentSummary = truncateStr(strings.TrimSpace(ev.AgentSummary), feedbackAgentSummaryMax)
	ev.Attachments = stripAttachmentData(ev.Attachments)
	for i := range ev.Turns {
		ev.Turns[i].Text = truncateStr(strings.TrimSpace(ev.Turns[i].Text), feedbackTurnMax)
		ev.Turns[i].Images = stripAttachmentData(ev.Turns[i].Images)
	}
}

// stripAttachmentData clears inline base64 so only blob refs are persisted and
// rendered. Inlining images into a text product is forbidden platform-wide.
func stripAttachmentData(in []models.PromptImage) []models.PromptImage {
	if len(in) == 0 {
		return nil
	}
	out := make([]models.PromptImage, len(in))
	for i, im := range in {
		im.Data = ""
		out[i] = im
	}
	return out
}

// FeedbackSummary renders the one-line gist used by the index, the MCP history
// overview and the UI collapsed row.
func FeedbackSummary(ev models.FeedbackEvent) string {
	if t := strings.TrimSpace(ev.Text); t != "" {
		return truncateStr(firstNonEmptyLine(t), feedbackSummaryMax)
	}
	for _, a := range ev.Annotations {
		if n := strings.TrimSpace(a.Note); n != "" {
			return truncateStr(n, feedbackSummaryMax)
		}
	}
	for _, t := range ev.Turns {
		if t.Role == "human" && strings.TrimSpace(t.Text) != "" {
			return truncateStr(firstNonEmptyLine(t.Text), feedbackSummaryMax)
		}
	}
	if len(ev.Attachments) > 0 {
		return "(仅附件)"
	}
	return "(无正文)"
}

func firstNonEmptyLine(s string) string {
	for _, ln := range strings.Split(s, "\n") {
		if t := strings.TrimSpace(ln); t != "" {
			return t
		}
	}
	return strings.TrimSpace(s)
}

// FeedbackDigest is the short content hash used for product before/after
// comparison in FeedbackTarget.
func FeedbackDigest(content string) string {
	if content == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(content))
	return hex.EncodeToString(sum[:])[:16]
}

// MarshalRoundJSON renders one round's standalone product, the discrete-product
// representation used by gate and preview feedback.
//
// The body holds only this round's increment. Prior rounds appear as one-line
// summaries plus a pointer to the previous product, so the file is
// self-describing without duplicating earlier bodies — reading the whole
// history stays a deliberate, incremental choice rather than a fixed cost.
func MarshalRoundJSON(ev models.FeedbackEvent, prior []models.FeedbackEvent, runID string, node NodeRef) (string, error) {
	payload := map[string]any{
		"runId":     runID,
		"kind":      ev.Kind,
		"node":      node.toMap(ev.NodeID),
		"iteration": ev.Iteration,
		"round":     ev.Round,
		"seq":       ev.Seq,
		"at":        ev.OccurredAt.Format(time.RFC3339),
		"actor":     feedbackActor(ev),
		"index":     FeedbackIndexArtifactName,
	}
	if ev.Action != "" {
		payload["action"] = ev.Action
	}
	if ev.Interrupted {
		payload["interrupted"] = true
	}
	// agentSummary is Agent-authored card copy, never FeedbackSummary (index gist).
	if s := strings.TrimSpace(ev.AgentSummary); s != "" {
		payload["agentSummary"] = s
	}

	fb := map[string]any{"text": ev.Text}
	if len(ev.Annotations) > 0 {
		fb["annotations"] = ev.Annotations
	}
	if len(ev.Attachments) > 0 {
		fb["attachments"] = ev.Attachments
	}
	payload["feedback"] = fb

	if len(ev.Turns) > 0 {
		payload["transcript"] = renderFeedbackTurns(ev.Turns)
	}
	if len(ev.Targets) > 0 {
		payload["targets"] = ev.Targets
	}
	if len(ev.Detail) > 0 {
		payload["detail"] = ev.Detail
	}

	if rounds := priorRoundSummaries(prior); len(rounds) > 0 {
		payload["priorRounds"] = rounds
	}
	if prev := prevRoundArtifact(prior); prev != "" {
		payload["prev"] = prev
	}

	b, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// MarshalFeedbackSummaryJSON renders the one current-state product for a ReAct
// node execution. Its primary content is a conclusion over every event already
// recorded for that node+iteration; round-level transcript and raw detail stay
// in the append-only event table and feedback_index.json.
func MarshalFeedbackSummaryJSON(events []models.FeedbackEvent, runID string, node NodeRef) (string, error) {
	if len(events) == 0 {
		return "", errors.New("cannot summarize empty feedback events")
	}
	latest := events[len(events)-1]
	conclusions := make([]string, 0, len(events))
	var annotations []models.ReactAnnotation
	var attachments []models.PromptImage
	for _, ev := range events {
		part := strings.TrimSpace(ev.AgentSummary)
		if part == "" {
			part = FeedbackSummary(ev)
		}
		conclusions = append(conclusions, fmt.Sprintf("第%d轮：%s", ev.Round, part))
		annotations = append(annotations, ev.Annotations...)
		attachments = append(attachments, ev.Attachments...)
	}
	payload := map[string]any{
		"runId":       runID,
		"kind":        latest.Kind,
		"node":        node.toMap(latest.NodeID),
		"iteration":   latest.Iteration,
		"roundCount":  len(events),
		"latestRound": latest.Round,
		"at":          latest.OccurredAt.Format(time.RFC3339),
		"index":       FeedbackIndexArtifactName,
		"summary":     "截至第" + fmt.Sprint(latest.Round) + "轮的归纳结论：" + strings.Join(conclusions, "；"),
	}
	// The confirm round is the only one that carries an AgentSummary, and it is
	// the last event of the execution — so the latest non-empty one is the
	// induction of the whole dialogue, shown as the card's「Agent 总结」.
	if s := latestAgentSummary(events); s != "" {
		payload["agentSummary"] = s
	}
	if latest.Interrupted {
		payload["interrupted"] = true
	}
	if len(annotations) > 0 || len(attachments) > 0 {
		feedback := map[string]any{}
		if len(annotations) > 0 {
			feedback["annotations"] = annotations
		}
		if len(attachments) > 0 {
			feedback["attachments"] = attachments
		}
		payload["feedback"] = feedback
	}
	b, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// latestAgentSummary returns the newest Agent-authored induction in the chain.
// It scans backwards rather than reading only the last event so an interrupted
// or index-only round appended after the confirm turn cannot hide the summary.
func latestAgentSummary(events []models.FeedbackEvent) string {
	for i := len(events) - 1; i >= 0; i-- {
		if s := strings.TrimSpace(events[i].AgentSummary); s != "" {
			return s
		}
	}
	return ""
}

// NodeRef carries the display metadata for the node a round belongs to.
type NodeRef struct {
	Label string
	Type  string
}

func (n NodeRef) toMap(id string) map[string]any {
	m := map[string]any{"id": id}
	if n.Label != "" {
		m["label"] = n.Label
	}
	if n.Type != "" {
		m["type"] = n.Type
	}
	return m
}

func feedbackActor(ev models.FeedbackEvent) map[string]any {
	m := map[string]any{}
	if ev.Actor != "" {
		m["name"] = ev.Actor
	}
	if ev.CallerKind != "" {
		m["callerKind"] = ev.CallerKind
	}
	if ev.Unattributable {
		m["unattributable"] = true
	}
	return m
}

func renderFeedbackTurns(turns []models.ReactMessage) []map[string]any {
	out := make([]map[string]any, 0, len(turns))
	for _, t := range turns {
		m := map[string]any{"role": t.Role, "text": t.Text}
		if t.At != "" {
			m["at"] = t.At
		}
		if len(t.Annotations) > 0 {
			m["annotations"] = t.Annotations
		}
		if len(t.Images) > 0 {
			m["attachments"] = t.Images
		}
		if len(t.Questions) > 0 {
			m["questions"] = t.Questions
		}
		if t.Interrupted {
			m["interrupted"] = true
		}
		out = append(out, m)
	}
	return out
}

// priorRoundSummaries condenses the rounds that came before into one line each.
func priorRoundSummaries(prior []models.FeedbackEvent) []map[string]any {
	out := make([]map[string]any, 0, len(prior))
	for _, p := range prior {
		out = append(out, map[string]any{
			"round":   p.Round,
			"kind":    p.Kind,
			"at":      p.OccurredAt.Format(time.RFC3339),
			"summary": FeedbackSummary(p),
		})
	}
	return out
}

// prevRoundArtifact returns the most recent prior round that has a standalone
// product, so the chain skips index-only rounds instead of dangling.
func prevRoundArtifact(prior []models.FeedbackEvent) string {
	for i := len(prior) - 1; i >= 0; i-- {
		if prior[i].ArtifactName != "" {
			return prior[i].ArtifactName
		}
	}
	return ""
}

// MarshalIndexJSON renders the run-level index over every recorded round.
// Rounds with no standalone product (platform auto-answers) appear without an
// "artifact" field but are still listed — the breadth is never trimmed.
func MarshalIndexJSON(runID string, events []models.FeedbackEvent) (string, error) {
	counts := map[string]int{}
	rounds := make([]map[string]any, 0, len(events))
	for _, ev := range events {
		counts[ev.Kind]++
		r := map[string]any{
			"seq":       ev.Seq,
			"kind":      ev.Kind,
			"node":      ev.NodeID,
			"iteration": ev.Iteration,
			"round":     ev.Round,
			"at":        ev.OccurredAt.Format(time.RFC3339),
			"summary":   FeedbackSummary(ev),
		}
		if ev.Actor != "" {
			r["actor"] = ev.Actor
		}
		if ev.Action != "" {
			r["action"] = ev.Action
		}
		if n := len(ev.Attachments); n > 0 {
			r["attachments"] = n
		}
		if len(ev.Annotations) > 0 {
			r["annotations"] = len(ev.Annotations)
		}
		if ev.Interrupted {
			r["interrupted"] = true
		}
		if ev.ArtifactName != "" {
			r["artifact"] = ev.ArtifactName
		}
		rounds = append(rounds, r)
	}
	payload := map[string]any{
		"runId":       runID,
		"generatedAt": time.Now().Format(time.RFC3339),
		"totalRounds": len(events),
		"counts":      counts,
		"rounds":      rounds,
	}
	b, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return "", err
	}
	return string(b), nil
}
