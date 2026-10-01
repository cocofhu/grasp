package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/sandbox"
	"github.com/rs/zerolog/log"
)

// RunAgent executes an autonomous agent node end-to-end, transparently
// retrying in a fresh sandbox on a retryable sandbox/ACP fault (create/ready/
// connect failure, mid-turn connection drop, or idle stall). Non-retryable
// outcomes (agent errors, hard timeout, contract misses) return immediately so
// the engine's FSM handles them. Retries are logged/emitted but do not create a
// separate execution record.
func (c *acpProvider) RunAgent(ctx context.Context, req NodeReq) (NodeResult, error) {
	n := c.sandboxAttempts()
	// Tokens burned by attempts that are retried away still count.
	var spent *models.TokenUsage
	var spentByModel models.TokenUsageByModel
	for attempt := 1; ; attempt++ {
		res, err := c.runAgentOnce(ctx, req)
		if err == nil || !isRetryableSandboxErr(err) || attempt >= n || ctx.Err() != nil {
			res.Usage = models.AddTokenUsage(spent, res.Usage)
			res.UsageByModel = models.AddTokenUsageByModel(spentByModel, res.UsageByModel)
			return res, err
		}
		spent = models.AddTokenUsage(spent, res.Usage)
		spentByModel = models.AddTokenUsageByModel(spentByModel, res.UsageByModel)
		c.emitRetryNotice(req, attempt, n, err)
		if !c.backoff(ctx, attempt) {
			res.Usage, res.UsageByModel = spent, spentByModel
			return res, err
		}
	}
}

// runAgentOnce is a single sandbox attempt of an agent node. It owns its
// sandbox lifecycle: on success or a non-retryable outcome it retires the
// sandbox (kept for the debug TTL); on a retryable fault it discards (destroys)
// the broken sandbox so the caller can retry cleanly.
func (c *acpProvider) runAgentOnce(ctx context.Context, req NodeReq) (res NodeResult, err error) {

	c.host.ClearOutcome(req.RunID, req.NodeID)

	sb, acp, home, err := c.openSandbox(ctx, req)
	if err != nil {

		return NodeResult{}, err
	}

	keepForDebug := true

	parked := false
	var turnEvents []models.AcpEvent
	defer func() {
		if parked {
			return
		}
		if keepForDebug {
			c.retireRunSandbox(sb, acp, home)
		} else {
			snap := c.discardSandbox(context.Background(), req, sb, acp, home, turnEvents)
			if len(res.Events) == 0 && len(snap) > 0 {
				res.Events = snap
			}
		}
	}()
	c.registerLive(req, sb, acp)
	defer func() {
		if !parked {
			c.deregisterLive(req)
		}
	}()

	seeded := c.upstreamArtifacts(req)
	chatCtx, cancel := context.WithTimeout(ctx, c.nodeChatTimeout(req))
	defer cancel()
	var chatRes *sandbox.ChatResult
	var usage *models.TokenUsage
	var usageByModel models.TokenUsageByModel

	if req.NodeType == "app_preview" {
		ready := c.host.PreviewReadyChan(req.RunID, req.NodeID)
		go func() {
			select {
			case <-ready:
				cancel()
				if acp != nil {
					_ = acp.Cancel()
				}
				log.Info().Str("run", req.RunID).Str("node", req.NodeID).
					Msg("app_preview early finish: healthy set_preview signaled")
			case <-chatCtx.Done():
			}
		}()
	}

	chatRes, err = c.streamChat(chatCtx, acp, req, c.buildAgentPrompt(req, seeded), req.PromptImages)
	if err != nil {

		if req.NodeType == "app_preview" && c.host.HasHealthyPreviewPorts(req.RunID, req.NodeID) {
			log.Warn().Err(err).Str("run", req.RunID).Str("node", req.NodeID).
				Msg("app_preview chat ended after healthy preview; continuing to park/review")
			err = nil
			if chatRes == nil {
				chatRes = &sandbox.ChatResult{Narration: "预览已就绪(生产相提前结束)"}
			}
		} else {
			if isRetryableSandboxErr(err) {
				keepForDebug = false
			}
			absorbChat(&usage, &usageByModel, nil, chatRes)
			events := c.snapshotEvents(ctx, sb, turnEvents)
			return NodeResult{Events: events, Usage: usage, UsageByModel: usageByModel}, fmt.Errorf("agent chat: %w", err)
		}
	}
	absorbChat(&usage, &usageByModel, &turnEvents, chatRes)
	out := map[string]any{"content": chatRes.Narration, "narration_summary": firstLine(chatRes.Narration)}

	if req.NodeType == "implement" {
		if perr := c.ensurePlanComplete(ctx, req, acp, &turnEvents, &usage, &usageByModel); perr != nil {
			events := c.snapshotEvents(ctx, sb, turnEvents)
			return NodeResult{OutputMd: chatRes.Narration, Outputs: out, Events: events, Usage: usage, UsageByModel: usageByModel}, perr
		}
	}

	if req.NodeType == "app_preview" {
		if perr := c.ensurePreviewRegistered(ctx, req, acp, &turnEvents, &usage, &usageByModel); perr != nil {
			events := c.snapshotEvents(ctx, sb, turnEvents)
			return NodeResult{OutputMd: chatRes.Narration, Outputs: out, Events: events, Usage: usage, UsageByModel: usageByModel}, perr
		}
	}

	if _, serr := c.ensureRequiredProducts(ctx, req, acp, &turnEvents, &usage, &usageByModel); serr != nil {
		events := c.snapshotEvents(ctx, sb, turnEvents)
		return NodeResult{OutputMd: chatRes.Narration, Outputs: out, Events: events, Usage: usage, UsageByModel: usageByModel}, serr
	}

	events := c.snapshotEvents(ctx, sb, turnEvents)

	if produces := str2(req.Config["produces"]); produces != "" {

		if _, rerr := c.host.ReadArtifact(req.RunID, req.Token, produces); rerr != nil {
			if err := c.harvest(ctx, sb, req, produces, out, &events); err != nil {

				return NodeResult{OutputMd: chatRes.Narration, Outputs: out, Events: events, Usage: usage, UsageByModel: usageByModel}, err
			}
		}
	}

	if req.NodeType == "implement" {
		c.ensurePushed(ctx, sb, req)
	}

	git := c.captureChanges(ctx, sb, req, out)

	if b, _ := req.Config["detect_push"].(bool); b {
		if p := c.detectPush(ctx, sb, req); p != nil {
			if git == nil {
				git = &GitInfo{}
			}
			git.Pushed = p.Pushed
			git.PushedSHA = p.PushedSHA
			if p.Branch != "" {
				git.Branch = p.Branch
				out["branch"] = p.Branch
			}
			if p.MrURL != "" {
				git.MrURL = p.MrURL
				out["mr_url"] = p.MrURL
			}
			out["pushed_sha"] = p.PushedSHA
		}
	}

	if req.NodeType != "app_preview" || !c.host.HasHealthyPreviewPorts(req.RunID, req.NodeID) {
		if _, oerr := c.ensureOutcome(ctx, req, acp, &events, &usage, &usageByModel); oerr != nil {
			return NodeResult{OutputMd: chatRes.Narration, Outputs: out, Events: events, Git: git, Usage: usage, UsageByModel: usageByModel}, oerr
		}
	} else if c.host.HasHealthyPreviewPorts(req.RunID, req.NodeID) {
		out["preview_ready"] = true
	}

	if req.KeepAliveForReview {
		key := reactKey(req)
		c.mu.Lock()
		c.sessions[key] = &reactSession{sb: sb, acp: acp, home: home}
		c.live[key] = sb
		delete(c.inflightACP, key)
		c.mu.Unlock()
		parked = true
		log.Info().Str("run", req.RunID).Str("node", req.NodeID).
			Msg("parked live session for post-run ReAct review")
	}

	return NodeResult{OutputMd: chatRes.Narration, Outputs: out, Events: events, Git: git, Usage: usage, UsageByModel: usageByModel}, nil
}

// collectChanges builds the VCS-neutral change report over the SSH data plane,
// replacing the universal-sandbox's absent GET /api/changes. It runs git itself
// (via sandbox.GitChanges) rather than asking an in-container HTTP service:
//   - single-repo mode when the workspace root is itself a git repo (vcs:"git");
//   - otherwise multi-repo (flat) mode: one entry per configured repo under
//     /root/workspace/<name> that is a git repo (vcs:"multi").
//
// Returns {vcs:"none"} when nothing under the workspace is version-controlled.
func (c *acpProvider) collectChanges(ctx context.Context, sb *sandbox.Sandbox, req NodeReq) *sandbox.Changes {
	ws := sb.WorkspaceDir
	if ws == "" {
		ws = "/root/workspace"
	}
	if ch, ok := sb.GitChanges(ctx, ws); ok {
		return ch
	}
	var repos []sandbox.RepoChanges
	for _, r := range resolveRepos(req) {
		dir := repoWorkspacePath(r.Name)
		if rc, ok := sb.GitChanges(ctx, dir); ok {
			repos = append(repos, sandbox.RepoChanges{Name: r.Name, Path: dir, Changes: *rc})
		}
	}
	if len(repos) == 0 {
		return &sandbox.Changes{VCS: "none"}
	}
	return &sandbox.Changes{VCS: "multi", Repos: repos}
}

// captureChanges fetches the sandbox's VCS-neutral code-change report and maps
// it into the node outputs. The report is computed by approving over SSH (see
// collectChanges); the sandbox image exposes no change endpoint.
// Returns a *GitInfo carrying the branch so the engine can record Run.branch.
// Best-effort: any error degrades to no change report (returns nil).
// req is used in single-repo mode to emit vars.branches-compatible
// outputs["branches"] when the run has exactly one configured repo.
func (c *acpProvider) captureChanges(ctx context.Context, sb *sandbox.Sandbox, req NodeReq, out map[string]any) *GitInfo {
	ch := c.collectChanges(ctx, sb, req)
	if ch == nil || ch.VCS == "" || ch.VCS == "none" {
		return nil
	}

	if ch.VCS == "multi" {
		return captureMultiRepoChanges(ch, out)
	}
	out["branch"] = ch.Branch
	out["base_branch"] = ch.BaseBranch
	out["new_branch"] = ch.NewBranch
	out["commit_sha"] = ch.HeadSHA
	out["base_sha"] = ch.BaseSHA
	out["dirty"] = ch.Dirty
	out["commit_count"] = len(ch.Commits)
	out["changed_files_count"] = len(ch.ChangedFiles)
	out["diff_stat"] = ch.DiffStat

	out["pushed"] = ch.Pushed
	out["unpushed_count"] = ch.Unpushed
	if ch.Pushed {
		out["pushed_sha"] = ch.HeadSHA
	}
	if len(ch.ChangedFiles) > 0 {
		out["changed_files"] = ch.ChangedFiles
	}

	if ch.Branch != "" {
		if repos := resolveRepos(req); len(repos) == 1 {
			if b, err := json.Marshal(map[string]string{repos[0].Name: ch.Branch}); err == nil {
				out["branches"] = string(b)
			}
		}
	}
	if ch.Branch == "" {
		return nil
	}
	git := &GitInfo{Branch: ch.Branch, Pushed: ch.Pushed}
	if ch.Pushed {
		git.PushedSHA = ch.HeadSHA
	}
	return git
}

// captureMultiRepoChanges maps a multi-repo (flat) change report into node
// outputs: a per-repo change list (`repos_changes`), a name→branch map
// (`branches`) for downstream continuity, and an aggregate `pushed` flag (true
// only when every repo with changes has been pushed). Returns a *GitInfo whose
// Repos carries each repo's branch/pushed.
func captureMultiRepoChanges(ch *sandbox.Changes, out map[string]any) *GitInfo {
	if len(ch.Repos) == 0 {
		return nil
	}
	branches := map[string]string{}
	repoOut := make([]map[string]any, 0, len(ch.Repos))
	git := &GitInfo{}
	allPushed := true
	anyChange := false
	for _, r := range ch.Repos {
		if r.Branch != "" {
			branches[r.Name] = r.Branch
		}
		changed := len(r.ChangedFiles) > 0 || len(r.Commits) > 0 || r.Dirty
		if changed {
			anyChange = true
			if !r.Pushed {
				allPushed = false
			}
		}
		rg := RepoGit{Name: r.Name, Branch: r.Branch, Pushed: r.Pushed}
		if r.Pushed {
			rg.PushedSHA = r.HeadSHA
		}
		git.Repos = append(git.Repos, rg)
		repoOut = append(repoOut, map[string]any{
			"name":                r.Name,
			"path":                r.Path,
			"branch":              r.Branch,
			"base_branch":         r.BaseBranch,
			"new_branch":          r.NewBranch,
			"commit_sha":          r.HeadSHA,
			"base_sha":            r.BaseSHA,
			"dirty":               r.Dirty,
			"commit_count":        len(r.Commits),
			"changed_files_count": len(r.ChangedFiles),
			"diff_stat":           r.DiffStat,
			"pushed":              r.Pushed,
			"unpushed_count":      r.Unpushed,
			"changed_files":       r.ChangedFiles,
		})
	}
	out["repos_changes"] = repoOut
	if b, err := json.Marshal(branches); err == nil {
		out["branches"] = string(b)
	}
	git.Pushed = anyChange && allPushed
	out["pushed"] = git.Pushed
	if len(git.Repos) == 0 {
		return nil
	}
	return git
}

// harvest copies a declared produces file out of the container and writes it
// through the run-scoped MCP host into the platform artifact store.
func (c *acpProvider) harvest(ctx context.Context, sb *sandbox.Sandbox, req NodeReq, produces string, out map[string]any, events *[]models.AcpEvent) error {
	data, err := sb.ReadFile(ctx, produces)
	if err != nil {
		return fmt.Errorf("produces %q not found in sandbox: %w", produces, err)
	}
	kind := artifactKind(produces)
	id, err := c.host.WriteArtifact(req.RunID, req.Token, req.NodeID, produces, string(data), kind)
	if err != nil {
		return fmt.Errorf("write_artifact %s: %w", produces, err)
	}
	out["artifact_id"] = id
	*events = append(*events, models.AcpEvent{
		Kind: "tool_call", Title: "write_artifact(" + produces + ")", Status: "completed",
		Artifact: &models.ArtifactMeta{Name: produces, Kind: kind},
	})
	return nil
}

// ensurePushed guarantees the implement node's working branch reaches origin so
// downstream sandboxes (fresh clones) can check it out. It commits any leftover
// changes the agent didn't commit, then pushes the current branch. Fully
// best-effort: no repo / no remote / no credentials → silent no-op (never fails
// the node). Review confirm uses pushWorkingBranches instead so leftover temp
// files are not auto-committed.
func (c *acpProvider) ensurePushed(ctx context.Context, sb *sandbox.Sandbox, req NodeReq) {
	c.pushRepos(ctx, sb, req, true)
}

// pushWorkingBranches pushes already-committed working branches without
// git add/commit, so confirm-time leftover temp files stay untracked.
func (c *acpProvider) pushWorkingBranches(ctx context.Context, sb *sandbox.Sandbox, req NodeReq) {
	c.pushRepos(ctx, sb, req, false)
}

func (c *acpProvider) pushRepos(ctx context.Context, sb *sandbox.Sandbox, req NodeReq, autoCommit bool) {
	repos := resolveRepos(req)
	if len(repos) == 0 {
		return
	}
	for _, r := range repos {
		script := `cd ` + shellArg(repoWorkspacePath(r.Name)) + ` || exit 0
git rev-parse --git-dir >/dev/null 2>&1 || exit 0
git remote get-url origin >/dev/null 2>&1 || exit 0
branch=$(git rev-parse --abbrev-ref HEAD 2>/dev/null)
if [ -z "$branch" ] || [ "$branch" = "HEAD" ]; then exit 0; fi
# Never auto-push protected branches: if the agent forgot to create a feature
# branch and worked on main/master, don't pollute the trunk on their behalf.
case "$branch" in
  main|master|develop|release-*) echo "on protected branch $branch; skip auto push"; exit 0;;
esac
`
		if autoCommit {
			script += `git add -A 2>/dev/null || true
git diff --cached --quiet 2>/dev/null || git commit -m "chore(approving): implement 收尾自动提交" >/dev/null 2>&1 || true
`
		}
		script += `git push -u origin "$branch" 2>&1 || true`
		if out, err := sb.ExecScript(ctx, 90*time.Second, "bash", script); err != nil {
			log.Debug().Err(err).Str("repo", r.Name).Str("out", strings.TrimSpace(out)).Msg("implement ensurePushed (best-effort)")
		}
	}
}

// detectPush inspects the container repo for branch/HEAD and whether the
// branch exists on the remote (i.e. the agent pushed). MR creation via glab
// is gated: GitLab repo + GITLAB_TOKEN + create_mr config must all be set.
func (c *acpProvider) detectPush(ctx context.Context, sb *sandbox.Sandbox, req NodeReq) *GitInfo {
	dir, repo := c.nodeRepo(req)
	cd := "cd " + shellArg(dir) + " && "
	branch, _ := sb.ExecScript(ctx, 10*time.Second, "bash", cd+"git rev-parse --abbrev-ref HEAD 2>/dev/null || true")
	sha, _ := sb.ExecScript(ctx, 10*time.Second, "bash", cd+"git rev-parse HEAD 2>/dev/null || true")
	branch, sha = strings.TrimSpace(branch), strings.TrimSpace(sha)
	if sha == "" {
		return nil
	}
	info := &GitInfo{Branch: branch, PushedSHA: sha}

	remote, _ := sb.ExecScript(ctx, 15*time.Second, "bash",
		cd+"git ls-remote --heads origin "+shellArg(branch)+" 2>/dev/null || true")
	info.Pushed = strings.TrimSpace(remote) != ""

	createMR, _ := req.Config["create_mr"].(bool)
	if info.Pushed && createMR && c.gitToken(req) != "" && isGitLabRepo(repo, c.gitLabURL(req)) {
		if url := c.findOrCreateMR(ctx, sb, dir, branch); url != "" {
			info.MrURL = url
		}
	}
	return info
}

func (c *acpProvider) findOrCreateMR(ctx context.Context, sb *sandbox.Sandbox, dir, branch string) string {
	cd := "cd " + shellArg(dir) + " && "
	view, err := sb.ExecScript(ctx, 25*time.Second, "bash",
		cd+"glab mr list --source-branch "+shellArg(branch)+" -F json 2>/dev/null || true")
	if err == nil {
		if url := firstMRURL(view); url != "" {
			return url
		}
	}
	create, _ := sb.ExecScript(ctx, 40*time.Second, "bash",
		cd+"glab mr create --fill --yes --source-branch "+shellArg(branch)+" 2>&1 || true")
	for _, line := range strings.Split(create, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "http://") || strings.HasPrefix(line, "https://") {
			return line
		}
	}
	return ""
}

func firstMRURL(jsonOut string) string {
	url, _ := firstMR(jsonOut)
	return url
}

// firstMR parses `glab mr list -F json` output and returns the first MR's web
// URL and iid (0 when absent/unparseable).
func firstMR(jsonOut string) (url string, iid int) {
	var arr []map[string]any
	if err := json.Unmarshal([]byte(jsonOut), &arr); err != nil || len(arr) == 0 {
		return "", 0
	}
	if u, ok := arr[0]["web_url"].(string); ok {
		url = u
	}
	if v, ok := arr[0]["iid"].(float64); ok {
		iid = int(v)
	}
	return url, iid
}

func shellArg(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// mrBranches resolves a submit_mr node's source/target branches. Source falls
// back to vars.branches[repo] so the MR node picks up the implement node's
// working branch by default; target may be empty (== the repository's default
// branch, resolved by glab).
func mrBranches(req NodeReq) (source, target string) {
	source = strings.TrimSpace(str2(req.Config["source_branch"]))

	if source == "" {
		repo := strings.TrimSpace(str2(req.Config["repo"]))
		if repo == "" {
			if repos := resolveRepos(req); len(repos) == 1 {
				repo = repos[0].Name
			}
		}
		if repo != "" {
			if bm := parseBranchesVar(req.Vars["branches"]); bm != nil {
				source = strings.TrimSpace(bm[repo])
			}
		}
	}
	target = strings.TrimSpace(str2(req.Config["target_branch"]))
	return source, target
}

// mrTargetDisplay renders the target branch for prompts; an empty target means
// the repository default branch.
func mrTargetDisplay(target string) string {
	if target == "" {
		return "仓库默认分支"
	}
	return target
}

// AbortRun tears down live react sessions and in-flight agent ACP connections
// for a run. Called on cancel/fail so Cancel-during-agent unblocks RunAgent
// (and react sandboxes are not left busy forever).
//
// When an app_preview node already has a healthy keepalive registration, the
// sandbox is retired (ACP closed, container kept) instead of Destroy'd so the
// setsid-detached preview survives Cancel of the production turn. Full Run /
// gate / sandbox reclaim still Destroy via the normal lifecycle.
func (c *acpProvider) AbortRun(runID string) {
	prefix := runID + "|"
	c.mu.Lock()
	var sessionKeys []string
	for k := range c.sessions {
		if strings.HasPrefix(k, prefix) {
			sessionKeys = append(sessionKeys, k)
		}
	}
	var agentKeys []string
	var agentACPs []*sandbox.ACPClient
	var agentSBs []*sandbox.Sandbox
	var agentKeepPreview []bool
	for k, acp := range c.inflightACP {
		if !strings.HasPrefix(k, prefix) {
			continue
		}

		if _, parked := c.sessions[k]; parked {
			continue
		}
		agentKeys = append(agentKeys, k)
		agentACPs = append(agentACPs, acp)
		agentSBs = append(agentSBs, c.live[k])
		nodeID := k
		if i := strings.IndexByte(k, '|'); i >= 0 {
			nodeID = k[i+1:]
		}

		keep := false
		if c.host != nil {
			pids := c.host.ListPreviewKeepalivePIDs(runID, nodeID)
			keep = c.host.HasHealthyPreviewPorts(runID, nodeID) || len(pids) > 0
		}
		agentKeepPreview = append(agentKeepPreview, keep)
	}
	for _, k := range agentKeys {
		delete(c.inflightACP, k)
		delete(c.live, k)
		if c.timeline != nil {
			nodeID := k
			if i := strings.IndexByte(k, '|'); i >= 0 {
				nodeID = k[i+1:]
			}
			c.timeline.stop(runID, nodeID)
		}
	}
	c.mu.Unlock()
	for _, k := range sessionKeys {
		c.closeSession(k)
	}
	for i, acp := range agentACPs {
		sb := agentSBs[i]
		if agentKeepPreview[i] && sb != nil {

			home := ""
			c.retireRunSandbox(sb, acp, home)
			continue
		}
		if acp != nil {
			acp.Close()
		}
		if sb != nil {

			c.deregisterRunSandbox(sb.Name)
			sb.Destroy(context.Background())
		}
	}
}

func (c *acpProvider) closeSession(key string) {
	c.mu.Lock()
	sess := c.sessions[key]
	delete(c.sessions, key)
	delete(c.live, key)
	runID, nodeID := "", ""
	if i := strings.IndexByte(key, '|'); i >= 0 {
		runID, nodeID = key[:i], key[i+1:]
	}
	c.mu.Unlock()
	if c.timeline != nil && runID != "" && nodeID != "" {
		c.timeline.stop(runID, nodeID)
	}
	if sess != nil {

		c.retireRunSandbox(sess.sb, sess.acp, sess.home)
	}
}

// producesRetry caps how many times finishReact re-prompts the agent to write
// a missing declared produces artifact before falling back to the engine's
// contract-miss handling (which routes failure/rollback per the FSM).
const producesRetry = 3
