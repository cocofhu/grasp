package runtime

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/sandbox"
)

func (c *acpProvider) buildAgentPrompt(req NodeReq, seeded []string) string {
	var b strings.Builder
	b.WriteString(str2(req.Config["prompt"]))
	if req.Caps.Clarify() {
		b.WriteString(runInputSeed(req))
	}
	if len(seeded) > 0 {
		b.WriteString(models.UpstreamArtifactsHeader)
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
	if note, ok := c.stuckNotes.Load(reactKey(req)); ok {
		b.WriteString(models.StuckRetryNoteFor(note.(string)))
	}
	b.WriteString(capabilityContracts(req.Caps))
	// A clarify Agent must not see the outcome contract before the human
	// confirms; the confirm turn introduces node_complete.
	if !req.Caps.Clarify() {
		b.WriteString(models.OutcomeContract)
	}
	if layout := multiRepoLayoutText(req); layout != "" {
		b.WriteString(layout)
	}
	b.WriteString(sandboxResourcesText(sandbox.DefaultMemoryMB()))
	b.WriteString(timeLimitsText(req, c.agentIdle()))
	return strings.TrimSpace(b.String())
}

// sandboxResourcesText tells the agent its sandbox memory budget. Exceeding it
// OOM-kills the whole sandbox and the node restarts from scratch, and agents
// that fan out parallel tool calls (several builds/tests at once) hit it first.
// Returns "" when no platform limit is configured.
func sandboxResourcesText(memoryMB int) string {
	if memoryMB <= 0 {
		return ""
	}
	heap := memoryMB * 3 / 8
	var b strings.Builder
	b.WriteString("\n\n## 沙箱资源\n")
	fmt.Fprintf(&b, "- 本沙箱内存上限 %d MiB(含 dockerd、浏览器与 Agent 自身);超出时整个沙箱会被杀掉,本节点从头重来。\n", memoryMB)
	b.WriteString("- 依赖安装、build、test、类型检查、lint 等重型命令必须逐个串行执行,不要用并行工具调用同时启动多个。\n")
	fmt.Fprintf(&b, "- 单个 Node 进程的 `NODE_OPTIONS=--max-old-space-size` 不超过 %d;vitest/jest 用 `--maxWorkers=2`。\n", heap)
	return b.String()
}

// timeLimitsText tells the Agent when the platform will stop it: after idle
// with no output, CPU or IO, and when the node's own time limit is used up.
func timeLimitsText(req NodeReq, idle time.Duration) string {
	var b strings.Builder
	if idle > 0 {
		fmt.Fprintf(&b, "- 连续 %d 分钟没有任何输出、CPU 或磁盘活动会被判定为卡住并终止;不要运行会无限等待的前台命令(watch / serve / 等待输入),耗时命令加超时或放到后台。\n", max(1, int(idle/time.Minute)))
	}
	if v, ok := toInt(req.Config["timeout"]); ok && v > 0 {
		fmt.Fprintf(&b, "- 本节点总时限 %d 分钟,用完即终止,请优先完成必须的产物。\n", v)
	}
	if b.Len() == 0 {
		return ""
	}
	return "\n\n## 时限\n" + b.String()
}

// capabilityContracts renders the platform protocol an Agent's capabilities
// imply: one field contract per declared product, the clarify dialogue rules,
// plan progress tracking and the preview protocol.
func capabilityContracts(caps *models.AgentCapabilities) string {
	if caps == nil {
		return ""
	}
	var b strings.Builder
	for _, w := range caps.Writes {
		b.WriteString(models.SchemaContract(w.Schema))
	}
	if caps.Clarify() {
		b.WriteString(models.ClarifyContract)
	}
	if caps.TracksPlanProgress() {
		b.WriteString(models.PlanProgressContract)
	}
	if caps.CanPreview() {
		b.WriteString(models.PreviewContract)
		if caps.Clarify() {
			b.WriteString(models.PreviewPageControlContract)
			b.WriteString(models.PreviewLiveIndex)
		}
	}
	return b.String()
}

// runInputSeed lists the run inputs as opening context of a clarify dialogue.
func runInputSeed(req NodeReq) string {
	var b strings.Builder
	b.WriteString("\n\n以下是本次运行输入,供对齐需求时参考。")
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

// clarifyPreviewExtras repeats a clarify Agent's preview protocol for chats
// that do not carry its opening prompt (visitor lanes).
func clarifyPreviewExtras(req NodeReq) string {
	if !req.Caps.Clarify() || !req.Caps.CanPreview() {
		return ""
	}
	return models.PreviewContract + models.PreviewPageControlContract + models.PreviewLiveIndex
}

// reviewCapabilityExtras is the review-phase toolset note for auto Agents
// with review on: Agents that never commit are told so, and preview-capable
// Agents get the page_* and Live instructions.
func reviewCapabilityExtras(req NodeReq) string {
	caps := req.Caps
	if !caps.ReviewEnabled() {
		return ""
	}
	out := models.ReviewCapabilityDevContract
	if !caps.CommitsCode() {
		out = models.ReviewCapabilityDesignContract
	}
	if caps.CanPreview() {
		out += models.PreviewPageControlContract + models.PreviewLiveIndex
	}
	return out
}

// liveVariantSkillDir is the platform skill copied in when Live variants are on.
const liveVariantSkillDir = "skills/live-variants"

// liveVariantsEnabled shares the capability gate with the engine and API.
func liveVariantsEnabled(req NodeReq) bool {
	return models.LiveVariantsEnabled(req.Caps)
}

// liveVariantSkills returns the platform skill dirs to embed for req.
func liveVariantSkills(req NodeReq) []string {
	if !liveVariantsEnabled(req) {
		return nil
	}
	return []string{liveVariantSkillDir}
}

// isLiveTurn reports whether human carries a platform-rendered Live request or
// an operable Live context. Comment-only context ("## Live 上下文(仅评论)")
// grants no edits and is excluded.
func isLiveTurn(human string) bool {
	return strings.Contains(human, "## Live 变体请求") || strings.Contains(human, "## Live 上下文\n")
}

// liveVariantPromptExtras is injected only on Live turns, every time: a resumed
// dialogue can still contain the Agent's own rule that forbids source edits.
func liveVariantPromptExtras(req NodeReq, human string) string {
	if !liveVariantsEnabled(req) || !isLiveTurn(human) {
		return ""
	}
	out := models.PreviewLiveContract
	switch {
	case req.Caps.Clarify():
		out += models.ClarifyLiveContract
	case !req.Caps.CommitsCode():
		out += models.DesignLiveContract
	}
	return out
}
