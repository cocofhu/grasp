package runtime

import (
	"context"
	"fmt"
	"strings"

	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/sandbox"
	"github.com/rs/zerolog/log"
)

const reviewDirtyFileCap = 80

// OfferCommitOnConfirm asks the parked agent to decide whether leftover dirty
// files should be committed (skipping temp files), then pushes already-committed
// working branches. No-op when the node does not touch repos, no session is
// parked, or the tree is clean and already pushed. Never fails the confirm.
// Clarify Agents get the offer for adopted Live edits; other Agents only when
// they commit code.
func (c *acpProvider) OfferCommitOnConfirm(ctx context.Context, req NodeReq) ReactTurn {
	if req.Caps == nil || (!req.Caps.Clarify() && !req.Caps.CommitsCode()) {
		// Agents that never commit keep review demos and Live trials in this
		// node's sandbox; downstream clones fresh.
		return ReactTurn{}
	}
	key := reactKey(req)
	c.mu.Lock()
	sess := c.sessions[key]
	c.mu.Unlock()
	if sess == nil || sess.sb == nil {
		return ReactTurn{}
	}

	ch := c.collectChanges(ctx, sess.sb, req)
	dirty, unpushed := reviewGitNeedsWrapUp(ch)
	if !dirty && !unpushed {
		return ReactTurn{}
	}

	var usage *models.TokenUsage
	var usageByModel models.TokenUsageByModel
	var events []models.AcpEvent
	var narration string

	if dirty {
		connected := sess.acp != nil && sess.acp.IsConnected()
		if !connected {
			log.Warn().Str("run", req.RunID).Str("node", req.NodeID).
				Msg("review confirm git wrap-up skipped: session disconnected")
		} else {
			files := formatDirtyFiles(ch)
			prompt := models.ReviewCommitWrapUpFor(files)
			chatCtx, cancel := c.turnCtx(ctx, req)
			res, err := c.streamChat(chatCtx, sess.acp, req, prompt, nil)
			cancel()
			if err != nil {
				log.Warn().Err(err).Str("run", req.RunID).Str("node", req.NodeID).
					Msg("review confirm git wrap-up chat failed")
			} else {
				absorbChat(&usage, &usageByModel, &events, res)
				narration = res.Narration
				c.host.TakePendingQuestions(req.RunID, req.NodeID)
			}
		}
	}

	log.Info().Str("run", req.RunID).Str("node", req.NodeID).
		Bool("dirty", dirty).Bool("unpushed", unpushed).
		Bool("asked_agent", narration != "").
		Msg("review confirm git wrap-up")

	c.pushWorkingBranches(ctx, sess.sb, req)
	return ReactTurn{Msg: narration, Events: events, Usage: usage, UsageByModel: usageByModel}
}

// reviewGitNeedsWrapUp reports whether the change report has an uncommitted
// working tree (dirty) and/or local commits that have not reached origin.
func reviewGitNeedsWrapUp(ch *sandbox.Changes) (dirty, unpushed bool) {
	if ch == nil || ch.VCS == "" || ch.VCS == "none" {
		return false, false
	}
	if ch.VCS == "multi" {
		for _, r := range ch.Repos {
			d, u := repoNeedsWrapUp(r.Dirty, r.Unpushed, r.Ahead, r.Pushed, len(r.Commits))
			dirty = dirty || d
			unpushed = unpushed || u
		}
		return dirty, unpushed
	}
	return repoNeedsWrapUp(ch.Dirty, ch.Unpushed, ch.Ahead, ch.Pushed, len(ch.Commits))
}

func repoNeedsWrapUp(dirtyFlag bool, unpushedCount, ahead int, pushed bool, commits int) (dirty, unpushed bool) {
	dirty = dirtyFlag
	unpushed = unpushedCount > 0 || (!pushed && (ahead > 0 || commits > 0))
	return dirty, unpushed
}

// formatDirtyFiles lists changed paths in dirty working trees for the wrap-up
// prompt (relative to the session baseline, including untracked files).
func formatDirtyFiles(ch *sandbox.Changes) string {
	if ch == nil {
		return ""
	}
	var b strings.Builder
	listed := 0
	if ch.VCS == "multi" {
		for _, r := range ch.Repos {
			if !r.Dirty {
				continue
			}
			if b.Len() > 0 {
				b.WriteByte('\n')
			}
			fmt.Fprintf(&b, "仓 `%s` (`%s`):\n", r.Name, r.Path)
			listed += writeDirtyFileLines(&b, r.ChangedFiles, listed)
		}
		return strings.TrimRight(b.String(), "\n")
	}
	if ch.Dirty {
		writeDirtyFileLines(&b, ch.ChangedFiles, 0)
	}
	return strings.TrimRight(b.String(), "\n")
}

func writeDirtyFileLines(b *strings.Builder, files []sandbox.ChangedFile, already int) int {
	wrote := 0
	for _, f := range files {
		if already+wrote >= reviewDirtyFileCap {
			fmt.Fprintf(b, "- …另有未列出的改动\n")
			return wrote + 1
		}
		if strings.TrimSpace(f.Path) == "" {
			continue
		}
		status := f.Status
		if status == "" {
			status = "modified"
		}
		fmt.Fprintf(b, "- %s %s\n", status, f.Path)
		wrote++
	}
	if wrote == 0 {
		b.WriteString("- (git status dirty,未能列出具体文件)\n")
		wrote++
	}
	return wrote
}
