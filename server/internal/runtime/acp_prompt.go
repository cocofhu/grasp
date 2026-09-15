package runtime

import (
	"fmt"
	"slices"
	"strings"

	"github.com/cocofhu/grasp/internal/mcp"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/nodereg"
)

func (c *acpProvider) buildAgentPrompt(req NodeReq, seeded []string) string {
	var b strings.Builder
	if sys := str2(req.Config["system"]); sys != "" {
		b.WriteString(sys + "\n\n")
	}
	if nodereg.IsGrasp(req.NodeType) {
		b.WriteString(approveInputSeed(req))
	} else {
		b.WriteString(str2(req.Config["prompt"]))
	}
	prompts := c.agentPrompts(req)
	if len(seeded) > 0 {
		b.WriteString(prompts.UpstreamHeader())
		for _, n := range seeded {
			fmt.Fprintf(&b, "- `%s`\n", n)
		}
	}
	if n, cites := c.host.FeedbackBrief(req.RunID, req.NodeID); n > 0 {
		b.WriteString(models.FeedbackHeaderFor(n))
		for _, cite := range cites {
			fmt.Fprintf(&b, "- %s\n", cite)
		}
	}

	source, target := mrBranches(req)
	if clause := nodereg.PromptContractText(prompts, req.NodeType, source, mrTargetDisplay(target)); clause != "" {
		b.WriteString(clause)
	} else if produces := str2(req.Config["produces"]); produces != "" {
		b.WriteString(prompts.ProducesContractFor(produces))
	}

	if nodeNeedsOutcome(req.NodeType) {
		b.WriteString(prompts.OutcomeContractText())
	}

	if inject := conditionalInjection(req); inject != "" {
		b.WriteString("\n\n" + inject)
	}
	if extra := testNodePromptExtras(req); extra != "" {
		b.WriteString(extra)
	}
	if extra := previewNodePromptExtras(req); extra != "" {
		b.WriteString(extra)
	}

	if nodeTouchesRepos(req.NodeType) {
		if layout := multiRepoLayoutText(req); layout != "" {
			b.WriteString(layout)
		}
	}
	if note := submitMRRepoNote(req); note != "" {
		b.WriteString(note)
	}
	return strings.TrimSpace(b.String())
}

// approveInputSeed replaces the user prompt template for Approve: the node has
// no inspector prompt, so run vars are the only opening context.
func approveInputSeed(req NodeReq) string {
	var b strings.Builder
	b.WriteString("以下是本次运行输入,供对齐需求时参考。")
	names := make([]string, 0, len(req.Vars))
	for name := range req.Vars {
		names = append(names, name)
	}
	slices.Sort(names)
	wrote := false
	for _, name := range names {
		v := req.Vars[name]
		if models.IsBlankVar(v) {
			continue
		}
		text := strings.TrimSpace(models.VarDisplayText(v))
		if text == "" || text == "false" {
			continue
		}
		if !wrote {
			b.WriteString("\n")
			wrote = true
		}
		fmt.Fprintf(&b, "- %s: %s\n", name, text)
	}
	if !wrote {
		b.WriteString("当前没有可用的输入变量。\n")
	}
	return b.String()
}

// nodeTouchesRepos reports whether a node type operates on the cloned repos
// (and thus benefits from the flat multi-repo layout description).
// visual is included so the agent can read-only locate existing business UI;
// the VisualContract still forbids writing, formatting, or committing repo files.
func nodeTouchesRepos(nodeType string) bool {
	switch nodeType {
	case "agent", "implement", "review", "test", "submit_mr", "research", "app_preview", "grasp", "approve", "visual":
		return true
	default:
		return false
	}
}

// conditionalInjection returns the node's conditional_prompt text when its
// when_var global variable is present and non-empty; otherwise "". The text is
// already interpolated by the engine (nodeReq) before reaching here.
// Approve has no conditional injection; leftover config is ignored.
func conditionalInjection(req NodeReq) string {
	if nodereg.IsGrasp(req.NodeType) {
		return ""
	}
	cp, ok := req.Config["conditional_prompt"].(map[string]any)
	if !ok {
		return ""
	}
	whenVar := strings.TrimSpace(str2(cp["when_var"]))
	text := strings.TrimSpace(str2(cp["text"]))
	if whenVar == "" || text == "" {
		return ""
	}
	if v, ok := req.Vars[whenVar]; ok && !models.IsBlankVar(v) && models.VarDisplayText(v) != "false" {
		return text
	}
	return ""
}

// agentPrompts returns the Agent's per-profile prompt overrides (from its
// agent.json), or nil when unset — the *models.AgentPrompts helpers are all
// nil-safe and fall back to the built-in defaults.
func (c *acpProvider) agentPrompts(req NodeReq) *models.AgentPrompts {
	return c.effectiveAgent(req).Prompts
}

// upstreamArtifacts lists this run's existing artifact names so the agent can
// pull them on demand through the read_artifact MCP tool. It deliberately does
// NOT write anything into the workspace: seeding files under .grasp/artifacts/
// polluted the node's code-change report (they showed up as untracked changes)
// and is unnecessary, since the artifact-store MCP is always mounted in-sandbox.
func (c *acpProvider) upstreamArtifacts(req NodeReq) []string {
	infos, err := c.host.ListArtifacts(req.RunID, req.Token)
	if err != nil || len(infos) == 0 {
		return nil
	}
	names := make([]string, 0, len(infos))
	for _, info := range infos {
		names = append(names, info.Name)
	}
	return names
}

// testNodePromptExtras injects block_on_skipped and, in multi-repo mode, the
// repoScope testing guidance. The flat multi-repo layout itself is injected
// separately (multiRepoLayoutText) for every workspace-touching node.
func testNodePromptExtras(req NodeReq) string {
	if req.NodeType != "test" {
		return ""
	}
	var b strings.Builder
	if configTruthy(req.Config["block_on_skipped"]) {
		b.WriteString("\n\n## 节点配置:block_on_skipped\n本节点已启用 **block_on_skipped=true**:任一 skipped 用例将阻塞测试门禁,请尽量避免无理由跳过,或在 detail 说明具体原因。\n")
	}

	repos := parseReposVar(req.Vars["repos"])
	if len(repos) == 0 {
		return b.String()
	}
	repoScope := strings.TrimSpace(str2(req.Config["repoScope"]))
	if repoScope == "" {
		repoScope = "all"
	}
	b.WriteString("\n\n## 多仓测试范围\n")
	if strings.EqualFold(repoScope, "all") {
		b.WriteString("- **repoScope=all**:须对全部相关仓分别执行测试并汇总至单一 `set_test_result`;cases 的 `name` 使用 `[仓名] 用例描述` 前缀。\n")
	} else {
		fmt.Fprintf(&b, "- **repoScope=%s**:仅在该仓目录 `%s/` 内执行测试、读写文件与运行命令;不要操作其它仓。\n", repoScope, repoWorkspacePath(repoScope))
	}
	return b.String()
}

// multiRepoLayoutText describes the flat workspace layout for the agent: the
// workspace root is never a git repo and every repository — even a lone one —
// lives at /root/workspace/<name>/. Returns "" when no repos are configured
// (a pure artifact flow / empty workspace).
func multiRepoLayoutText(req NodeReq) string {
	repos := resolveRepos(req)
	if len(repos) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("\n\n## 工作区仓库布局(repos)\n")
	b.WriteString("- **平级布局**:工作区根 `/root/workspace` 本身不是 git 仓库;每个仓库(即使只有一个)位于 `/root/workspace/<name>/`,各自独立 git 根。\n")
	b.WriteString("- **仓库清单**:\n")
	for _, r := range repos {
		fmt.Fprintf(&b, "  - `%s` → `%s/`\n", r.Name, repoWorkspacePath(r.Name))
	}
	b.WriteString("- 操作某个仓前先 `cd` 进其目录;每个仓的 `git` 提交/推送、依赖安装、测试与建 MR 都在对应仓目录内进行。\n")
	return b.String()
}

// submitMRRepoNote tells a submit_mr node which repo directory to operate in
// when the run is multi-repo. Returns "" for single-repo or non-submit_mr nodes.
func submitMRRepoNote(req NodeReq) string {
	if req.NodeType != "submit_mr" {
		return ""
	}
	repos := resolveRepos(req)
	if len(repos) == 0 {
		return ""
	}
	repo := strings.TrimSpace(str2(req.Config["repo"]))
	if repo == "" {
		return "\n\n## 多仓 MR 目标仓\n本节点未配置 `repo`(目标仓名)。请对存在待合并工作分支的仓分别 `cd` 进其目录后完成 push 与按托管商建单（git + 对应 CLI glab/gh）。\n"
	}
	return fmt.Sprintf("\n\n## 多仓 MR 目标仓\n本节点针对仓 `%s`:所有 `git` 与对应 CLI（`glab`/`gh`）操作前先 `cd %s`,仅在该仓目录内 push 源分支并建合并请求。\n", repo, repoWorkspacePath(repo))
}

// previewNodePromptExtras overrides proxy/noVNC instructions when IP-direct is on.
func previewNodePromptExtras(req NodeReq) string {
	if !mcp.SetPreviewAllowed(req.NodeType) || !configTruthy(req.Config["direct_preview"]) {
		return ""
	}
	if !configDefaultOn(req.Config["auto_inject"]) {
		return models.DefaultPreviewDirectManualContract
	}
	return models.DefaultPreviewDirectContract
}
