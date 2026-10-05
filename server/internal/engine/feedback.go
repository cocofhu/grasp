package engine

import (
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/cocofhu/grasp/internal/mcp"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/services"

	"github.com/rs/zerolog/log"
)

// recordFeedback appends one feedback event and renders the derived products.
// ReAct review/clarify products are rewritten as a single cumulative conclusion
// for the node execution; the feedback index remains the append-only timeline.
//
// It writes nothing at all when the event carries no substance — no opinion,
// no annotation, no attachment, no ReAct turn, no AgentSummary. A gate approval
// clicked through with an empty form is exactly that case, and flooding a run
// with empty shells would make the ledger useless.
//
// Best-effort, like persistRunErrorArtifact: every failure is logged and
// swallowed, because losing a feedback product must never stall the FSM.
func (e *Engine) recordFeedback(ev models.FeedbackEvent) {
	if e.db == nil || strings.TrimSpace(ev.RunID) == "" {
		return
	}
	if !ev.HasSubstance() {
		return
	}

	// A key distinct from the callers' runID+":"+nodeID resume locks: those are
	// already held on every path that lands here, so sharing one would deadlock.
	unlock := e.lockResume(ev.RunID + ":feedback")
	defer unlock()

	svc := services.NewFeedbackService(e.db)
	if err := svc.Append(&ev); err != nil {
		if !errors.Is(err, services.ErrFeedbackNoSubstance) {
			log.Warn().Str("run_id", ev.RunID).Str("node_id", ev.NodeID).Err(err).
				Msg("append feedback event failed")
		}
		return
	}
	e.renderFeedbackProducts(ev.RunID, svc)
}

// renderFeedbackProducts rewrites the index from the table. ReAct products are
// current-state summaries and are deliberately saved on every render; discrete
// gate/preview products retain their historical write-once backfill behavior.
func (e *Engine) renderFeedbackProducts(runID string, svc *services.FeedbackService) {
	if e.store == nil {
		return
	}
	events := svc.Events(runID)
	if len(events) == 0 {
		return
	}

	labels, types := e.nodeMeta(runID)
	rendered := map[string]bool{}
	for i, ev := range events {
		if ev.ArtifactName == "" {
			continue
		}
		if rendered[ev.ArtifactName] {
			continue
		}
		rendered[ev.ArtifactName] = true

		isReact := ev.Kind == models.FeedbackKindReview || ev.Kind == models.FeedbackKindClarify
		if !isReact {
			if _, ok := e.store.Get(runID, ev.ArtifactName); ok {
				continue
			}
		}

		var (
			body string
			err  error
		)
		node := services.NodeRef{Label: labels[ev.NodeID], Type: types[ev.NodeID]}
		if isReact {
			body, err = services.MarshalFeedbackSummaryJSON(eventsForExecution(events, ev), runID, node)
		} else {
			body, err = services.MarshalRoundJSON(ev, priorRoundsFor(events, i), runID, node)
		}
		if err != nil {
			log.Warn().Str("run_id", runID).Str("artifact", ev.ArtifactName).Err(err).
				Msg("marshal feedback product failed")
			continue
		}
		// The real node id keeps the product grouped under its node in the UI;
		// captureDeliverable skips feedback.* so this cannot suppress the
		// node's own deliverable capture.
		if _, err := e.store.Save(runID, ev.NodeID, ev.ArtifactName, "json", body); err != nil {
			log.Warn().Str("run_id", runID).Str("artifact", ev.ArtifactName).Err(err).
				Msg("save feedback product failed")
		}
	}

	body, err := services.MarshalIndexJSON(runID, events)
	if err != nil {
		log.Warn().Str("run_id", runID).Err(err).Msg("marshal feedback index failed")
		return
	}
	// The index spans the whole run, so it is deliberately unattributed to any
	// single node.
	if _, err := e.store.Save(runID, "", services.FeedbackIndexArtifactName, "json", body); err != nil {
		log.Warn().Str("run_id", runID).Err(err).Msg("save feedback index failed")
	}
}

// eventsForExecution returns the complete append-only audit trail that feeds a
// ReAct summary. Index-only auto-answers are included so an interrupted or
// dual-write-free path cannot erase earlier dialogue context.
func eventsForExecution(events []models.FeedbackEvent, current models.FeedbackEvent) []models.FeedbackEvent {
	out := make([]models.FeedbackEvent, 0)
	for _, ev := range events {
		if ev.NodeID == current.NodeID && ev.Iteration == current.Iteration {
			out = append(out, ev)
		}
	}
	return out
}

// priorRoundsFor returns the rounds of the same node+iteration that precede
// index i, which is the chain a single product summarizes.
func priorRoundsFor(events []models.FeedbackEvent, i int) []models.FeedbackEvent {
	cur := events[i]
	out := make([]models.FeedbackEvent, 0, i)
	for _, ev := range events[:i] {
		if ev.NodeID == cur.NodeID && ev.Iteration == cur.Iteration {
			out = append(out, ev)
		}
	}
	return out
}

// reviewFeedbackEvent assembles one in-place-revision round.
//
// The enqueue path (WS review_chat) carries no identity today, so the round is
// recorded as unattributable rather than inventing a reviewer name.
func (e *Engine) reviewFeedbackEvent(s *reviewSession, item *reviewQueueItem, iteration int,
	human, agent models.ReactMessage, targets []models.FeedbackTarget, interrupted bool, agentSummary string) models.FeedbackEvent {
	ev := models.FeedbackEvent{
		RunID:          s.runID,
		Kind:           models.FeedbackKindReview,
		NodeID:         s.producerID,
		Iteration:      iteration,
		CallerKind:     models.CallerKindPM,
		Unattributable: true,
		Action:         "revise",
		Text:           item.Text,
		AgentSummary:   strings.TrimSpace(agentSummary),
		Annotations:    item.Annotations,
		Attachments:    item.Images,
		Turns:          []models.ReactMessage{human, withoutParts(agent)},
		Targets:        targets,
		Interrupted:    interrupted,
		OccurredAt:     time.Now(),
	}
	if item.Source != "" {
		ev.Detail = map[string]any{"source": item.Source}
		if item.GateNodeID != "" {
			ev.Detail["gateNodeId"] = item.GateNodeID
		}
	}
	return ev
}

// clarifyFeedbackEvent assembles one human answer in a clarification dialogue.
// The questions being answered ride along in Detail so the round is readable on
// its own instead of as a dangling reply.
func (e *Engine) clarifyFeedbackEvent(s *reviewSession, item *reviewQueueItem, iteration int,
	answered []models.ReactQuestion, human, agent models.ReactMessage, interrupted bool, agentSummary string) models.FeedbackEvent {
	ev := models.FeedbackEvent{
		RunID:          s.runID,
		Kind:           models.FeedbackKindClarify,
		NodeID:         s.producerID,
		Iteration:      iteration,
		CallerKind:     models.CallerKindPM,
		Unattributable: true,
		Action:         "answer",
		Text:           item.Text,
		AgentSummary:   strings.TrimSpace(agentSummary),
		Annotations:    item.Annotations,
		Attachments:    item.Images,
		Turns:          []models.ReactMessage{human, withoutParts(agent)},
		Interrupted:    interrupted,
		OccurredAt:     time.Now(),
	}
	if len(answered) > 0 {
		ev.Detail = map[string]any{"questions": answered}
	}
	return ev
}

// confirmRoundFeedbackEvent assembles the「确认并流转」round, the only round that
// carries an AgentSummary: ordinary ReAct turns are pure narration, and the
// induction of the whole dialogue is produced once, by the hidden summary turn
// that runs after the human confirmed.
//
// Unlike clarifyFeedbackEvent / reviewFeedbackEvent this takes no
// *reviewSession, because both confirm paths (clarify force in ReactReply,
// review force in reviewReply) run synchronously outside the FIFO pump.
func (e *Engine) confirmRoundFeedbackEvent(runID, nodeID, kind string, iteration int,
	human, agent models.ReactMessage, agentSummary string) models.FeedbackEvent {
	// A silent confirm is a click, not feedback. Without written input and
	// without a summary the round would carry nothing but the agent's own
	// narration — exactly the empty shell recordFeedback exists to refuse — so
	// the zero value is returned for it to skip.
	if strings.TrimSpace(human.Text) == "" && len(human.Annotations) == 0 &&
		len(human.Images) == 0 && strings.TrimSpace(agentSummary) == "" {
		return models.FeedbackEvent{}
	}
	action := "answer"
	if kind == models.FeedbackKindReview {
		action = "revise"
	}
	return models.FeedbackEvent{
		RunID:          runID,
		Kind:           kind,
		NodeID:         nodeID,
		Iteration:      iteration,
		CallerKind:     models.CallerKindPM,
		Unattributable: true,
		Action:         action,
		Text:           human.Text,
		AgentSummary:   strings.TrimSpace(agentSummary),
		Annotations:    human.Annotations,
		Attachments:    human.Images,
		Turns:          []models.ReactMessage{human, withoutParts(agent)},
		Detail:         map[string]any{"confirm": true},
		OccurredAt:     time.Now(),
	}
}

// lastHumanMessage returns the most recent human turn, i.e. the text the
// reviewer submitted alongside「确认并流转」. Zero value when the dialogue holds
// none (a gate-seeded review conversation confirmed without any push-back).
func lastHumanMessage(msgs []models.ReactMessage) models.ReactMessage {
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == "human" {
			return msgs[i]
		}
	}
	return models.ReactMessage{}
}

// lastAgentQuestions returns the structured questions from the most recent
// agent turn, i.e. the ones the next human turn is answering.
func lastAgentQuestions(msgs []models.ReactMessage) []models.ReactQuestion {
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == "agent" {
			return msgs[i].Questions
		}
	}
	return nil
}

// recordGateFeedback records a gate decision, but only when the reviewer
// actually wrote something.
//
// A decision with an empty form is a click, not feedback: recording it would
// turn a batch sign-off into a pile of empty products and bury the rounds that
// carry real reasoning. HasSubstance enforces this downstream too; the explicit
// text extraction here is what gives the round its body.
func (e *Engine) recordGateFeedback(c *execCtx, node *models.Node, gate models.Gate,
	action string, form map[string]any, reviewer string, opts resumeGateOpts) {
	if c == nil || node == nil {
		return
	}
	actor := services.ActorFromUsername(reviewer)
	ev := models.FeedbackEvent{
		RunID:          c.run.ID,
		Kind:           models.FeedbackKindGate,
		NodeID:         node.ID,
		Iteration:      gate.Iteration,
		Actor:          actor.Username,
		Unattributable: actor.Unattributable,
		CallerKind:     firstNonEmptyStr(opts.callerKind, models.CallerKindPM),
		Action:         action,
		Text:           gateFormText(form),
		OccurredAt:     time.Now(),
	}
	detail := map[string]any{}
	if len(form) > 0 {
		detail["form"] = form
	}
	if opts.externalName != "" {
		detail["externalName"] = opts.externalName
		detail["external"] = true
	}
	if len(detail) > 0 {
		ev.Detail = detail
	}
	e.recordFeedback(ev)
}

// gateFormText joins the reviewer's written form values into the round's body.
// Non-text values are skipped: a checkbox is a decision, not an opinion.
func gateFormText(form map[string]any) string {
	keys := make([]string, 0, len(form))
	for k := range form {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var parts []string
	for _, k := range keys {
		s, ok := form[k].(string)
		if !ok {
			continue
		}
		if s = strings.TrimSpace(s); s == "" {
			continue
		}
		parts = append(parts, k+": "+s)
	}
	return strings.Join(parts, "\n")
}

// recordPreviewFeedback records a batch of preview issues as one round: the
// reviewer filed them as one pass over the page, and splitting them would lose
// that grouping. An empty snapshot records nothing.
//
// Screenshots are carried as blob refs only — PromptImage.Data is dropped
// before persistence, so no base64 reaches the ledger.
func (e *Engine) recordPreviewFeedback(c *execCtx, nodeID string, issues []models.PreviewIssue) {
	if c == nil || len(issues) == 0 {
		return
	}
	var texts []string
	var images []models.PromptImage
	items := make([]map[string]any, 0, len(issues))
	for _, is := range issues {
		body := strings.TrimSpace(is.Body)
		if body != "" {
			texts = append(texts, body)
		}
		images = append(images, is.Images...)
		item := map[string]any{"body": body}
		if s := strings.TrimSpace(is.Selector); s != "" {
			item["selector"] = s
		}
		if is.Port > 0 {
			item["port"] = is.Port
		}
		if n := len(is.Images); n > 0 {
			item["attachments"] = n
		}
		items = append(items, item)
	}
	e.recordFeedback(models.FeedbackEvent{
		RunID:       c.run.ID,
		Kind:        models.FeedbackKindPreview,
		NodeID:      nodeID,
		Iteration:   c.iter[nodeID],
		CallerKind:  models.CallerKindPM,
		Action:      "snapshot",
		Text:        strings.Join(texts, "\n"),
		Attachments: images,
		Detail:      map[string]any{"issues": items},
		OccurredAt:  time.Now(),
	})
}

// artifactDigests snapshots the content digests of a node's products, keyed by
// name. Ledger products and the outcome marker are excluded: they are platform
// bookkeeping, and listing them as "changed by this feedback" would be noise.
func (e *Engine) artifactDigests(runID, nodeID string) map[string]string {
	out := map[string]string{}
	if e.store == nil {
		return out
	}
	for _, a := range e.store.List(runID) {
		if a.Node != nodeID || a.Name == mcp.NodeOutcomeArtifactName ||
			services.IsFeedbackArtifactName(a.Name) {
			continue
		}
		content, ok := e.store.Get(runID, a.Name)
		if !ok {
			continue
		}
		out[a.Name] = services.FeedbackDigest(content)
	}
	return out
}

// diffDigests turns two snapshots into the per-product before/after record that
// answers "what did this round of feedback actually change".
func diffDigests(before, after map[string]string) []models.FeedbackTarget {
	names := make([]string, 0, len(after))
	for n := range after {
		names = append(names, n)
	}
	for n := range before {
		if _, ok := after[n]; !ok {
			names = append(names, n)
		}
	}
	sort.Strings(names)

	out := make([]models.FeedbackTarget, 0, len(names))
	for _, n := range names {
		b, a := before[n], after[n]
		out = append(out, models.FeedbackTarget{Name: n, Before: b, After: a, Changed: b != a})
	}
	return out
}

// nodeMeta reads label/type per node id from the run's stored graph so the
// products are readable without a graph lookup. Callers reach this helper from
// paths that have no execCtx, so the graph comes from the run row.
func (e *Engine) nodeMeta(runID string) (labels, types map[string]string) {
	labels, types = map[string]string{}, map[string]string{}
	var run models.Run
	if err := e.db.Select("graph").First(&run, "id = ?", runID).Error; err != nil {
		return labels, types
	}
	for _, n := range run.Graph.Nodes {
		if lbl := strings.TrimSpace(n.Label); lbl != "" {
			labels[n.ID] = lbl
		}
		types[n.ID] = n.Type
	}
	return labels, types
}

// withoutParts drops the chat timeline (tool inputs/outputs) from a turn kept
// in the feedback record; the reply text and tool names stay.
func withoutParts(m models.ReactMessage) models.ReactMessage {
	m.Parts = nil
	return m
}
