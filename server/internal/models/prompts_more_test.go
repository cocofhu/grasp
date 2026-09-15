package models

import (
	"strings"
	"testing"
)

func TestAgentPromptsRemainingContracts(t *testing.T) {
	var nilP *AgentPrompts
	if nilP.ClarifiedOpenQuestionsRetryFor([]string{"q1", "q2"}) == "" {
		t.Fatal("nil clarified retry")
	}
	if nilP.MRContractFor("feat", "main") == "" {
		t.Fatal("nil MR")
	}
	if nilP.VisualContractText() == "" || nilP.PreviewContractText() == "" || nilP.PreviewRetryText() == "" {
		t.Fatal("nil visual/preview")
	}
	if nilP.ApproveContractText() == "" {
		t.Fatal("nil approve contract")
	}
	if nilP.PreflightContractText() == "" || nilP.PreflightRetryText("x") == "" {
		t.Fatal("nil preflight")
	}

	p := &AgentPrompts{
		ClarifiedOpenQuestionsRetry: "Q:{items}",
		MRContract:                  "from {source} to {target}",
		VisualContract:              "V",
		PreviewContract:             "P",
		PreviewRetry:                "R",
	}
	if got := p.ClarifiedOpenQuestionsRetryFor([]string{"a"}); !strings.Contains(got, "a") {
		t.Fatalf("retry=%q", got)
	}
	if got := p.MRContractFor("s", "t"); got != "from s to t" {
		t.Fatalf("mr=%q", got)
	}
	if p.VisualContractText() != "V" || p.PreviewContractText() != "P" || p.PreviewRetryText() != "R" {
		t.Fatal("overrides")
	}
	p.ApproveContract = "AC"
	if p.ApproveContractText() != "AC" {
		t.Fatal("approve override")
	}
	gotApprove := (&AgentPrompts{}).ApproveContractText()
	for _, want := range []string{
		"两份强制交付", "set_clarified_requirement", "set_plan", "不是「唯一交付」", "用户先说明目标",
		"至少两个方向不同", "禁止调用", "伪选择",
		"set_preview", "不会", "结束本节点",
		"结束时序", "确认前", "确认后", "node_complete",
	} {
		if !strings.Contains(gotApprove, want) {
			t.Fatalf("DefaultApproveContract missing %q\n%s", want, gotApprove)
		}
	}
	if strings.Contains(gotApprove, "可跳过确认自行") || strings.Contains(gotApprove, "平台会结束本节点") {
		t.Fatal("DefaultApproveContract must not imply skip-confirm or platform-ends-without-node_complete")
	}
	if !strings.Contains(DefaultApproveOpenSuffix, "真实分歧") {
		t.Fatal("DefaultApproveOpenSuffix")
	}
	if strings.Contains(DefaultApproveOpenSuffix, "第一回合必须调用 ask_question") {
		t.Fatal("DefaultApproveOpenSuffix must not force first-turn ask_question")
	}
	if strings.Contains(DefaultApproveOpenSuffix, "可直接 set_* 并 node_complete") {
		t.Fatal("DefaultApproveOpenSuffix must not auto node_complete")
	}
	if !strings.Contains(DefaultApproveOpenSuffix, "确认并流转") {
		t.Fatal("DefaultApproveOpenSuffix must wait for human confirm")
	}
	if !strings.Contains(DefaultApproveOpenSuffix, "未点「确认并流转」前禁止 node_complete") {
		t.Fatal("DefaultApproveOpenSuffix must forbid node_complete before confirm")
	}
	for _, want := range []string{"确认流转", "set_clarified_requirement", "set_plan", "node_complete",
		"不要再提问", "完整聊天记录", "补充或修正"} {
		if !strings.Contains(DefaultApproveConfirmSuffix, want) {
			t.Fatalf("DefaultApproveConfirmSuffix missing %q\n%s", want, DefaultApproveConfirmSuffix)
		}
	}
	if nilP.OutcomeContractText() == "" || nilP.OutcomeRetryText() == "" {
		t.Fatal("nil outcome")
	}
	p.OutcomeContract = "OC"
	p.OutcomeRetry = "OR"
	if p.OutcomeContractText() != "OC" || p.OutcomeRetryText() != "OR" {
		t.Fatal("outcome overrides")
	}
	if nilP.ReviewCommitWrapUpFor("a.go") == "" {
		t.Fatal("nil review commit wrap-up")
	}
	p.ReviewCommitWrapUp = "FILES:{files}"
	if got := p.ReviewCommitWrapUpFor("- untracked tmp.log"); got != "FILES:- untracked tmp.log" {
		t.Fatalf("wrap-up=%q", got)
	}
	got := (&AgentPrompts{}).ReviewCommitWrapUpFor("- untracked tmp.log")
	for _, want := range []string{"禁止 `git add -A`", "tmp.log", "临时文件", "git status"} {
		if !strings.Contains(got, want) {
			t.Fatalf("DefaultReviewCommitWrapUp missing %q\n%s", want, got)
		}
	}
}

// The confirm-time pair replaced the old per-turn dual-write contract: nothing
// asks for a summary until the human clicked「确认并流转」, and then it is two
// separate prompts — reconcile the products, then induce the dialogue.
func TestConfirmTimePromptsSplitReconcileFromSummary(t *testing.T) {
	var nilP *AgentPrompts
	if nilP.ReviewConfirmReconcileText() != DefaultReviewConfirmReconcile {
		t.Fatal("nil review reconcile must fall back to the default")
	}
	if nilP.ConfirmSummaryContractText() != DefaultConfirmSummaryContract {
		t.Fatal("nil summary contract must fall back to the default")
	}
	p := &AgentPrompts{ReviewConfirmReconcile: "R", ConfirmSummaryContract: "S"}
	if p.ReviewConfirmReconcileText() != "R" || p.ConfirmSummaryContractText() != "S" {
		t.Fatal("confirm-time overrides")
	}
	// Blank overrides are the same as absent.
	blank := &AgentPrompts{ReviewConfirmReconcile: "  ", ConfirmSummaryContract: "\n"}
	if blank.ReviewConfirmReconcileText() != DefaultReviewConfirmReconcile ||
		blank.ConfirmSummaryContractText() != DefaultConfirmSummaryContract {
		t.Fatal("whitespace override must fall back to the default")
	}

	reconcile := (&AgentPrompts{}).ReviewConfirmReconcileText()
	for _, want := range []string{"确认并流转", "完整聊天记录", "补充或修正", "不要提问"} {
		if !strings.Contains(reconcile, want) {
			t.Fatalf("DefaultReviewConfirmReconcile missing %q\n%s", want, reconcile)
		}
	}
	// A review producer already marked its outcome in the production phase, so a
	// stray node_complete here would leak into the next node's outcome read.
	if !strings.Contains(reconcile, "不要调用 `node_complete`") {
		t.Fatalf("DefaultReviewConfirmReconcile must forbid node_complete\n%s", reconcile)
	}

	summary := (&AgentPrompts{}).ConfirmSummaryContractText()
	for _, want := range []string{"agentSummary", "完整聊天记录", "只输出一个 fenced JSON",
		"不要调用任何工具", "不要输出 JSON 之外"} {
		if !strings.Contains(summary, want) {
			t.Fatalf("DefaultConfirmSummaryContract missing %q\n%s", want, summary)
		}
	}

	// The react counterpart names no set_* tool (a react node's deliverable comes
	// from its own contract) but must still reconcile and wrap up.
	for _, want := range []string{"确认流转", "完整聊天记录", "补充或修正", "不要再提问"} {
		if !strings.Contains(DefaultReactConfirmSuffix, want) {
			t.Fatalf("DefaultReactConfirmSuffix missing %q\n%s", want, DefaultReactConfirmSuffix)
		}
	}
	if strings.Contains(DefaultReactConfirmSuffix, "set_plan") {
		t.Fatal("DefaultReactConfirmSuffix must not demand a plan a react node never writes")
	}
	// The retired per-turn contract must not creep back into any confirm prompt.
	for _, p := range []string{DefaultApproveConfirmSuffix, DefaultReactConfirmSuffix, reconcile} {
		if strings.Contains(p, "agentSummary") {
			t.Fatalf("reconcile prompts must not carry the summary contract\n%s", p)
		}
	}
}

func TestDefaultOutcomeContractDetachesLongRunningServices(t *testing.T) {
	got := DefaultOutcomeContract
	for _, want := range []string{
		"setsid",
		"nohup",
		"禁止前台或未脱钩的命令占住 Agent 回合",
		"不要为收尾杀掉这些进程",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("DefaultOutcomeContract missing %q\n---\n%s", want, got)
		}
	}
}

func TestDefaultMRContractHostRoutingAndFailurePhrases(t *testing.T) {
	got := (&AgentPrompts{}).MRContractFor("feat/x", "main")
	mustContain := []string{
		"glab mr create --source-branch feat/x --target-branch main --fill --yes",
		"gh pr create --base main --head feat/x --fill",
		"GITLAB_URL",
		"GITHUB_URL",
		"冲突已解决",
		"源分支已推送",
		"推送成功即可 success",
		"outputs.mr_url",
		// list-first + idempotent success (Demo s1/s2/s3)
		"list open",
		"不得直接判 failed",
		"already exists",
		"No commits between",
		"跳过新建",
		"已合并无新提交",
		"无历史单已同步",
		"无差异且无历史单可复用",
		// closed + true failures must stay failed (Demo s4 / s5)
		"closed 未合并",
		"不得仅因存在 closed URL 而 success",
		"无法 push",
		"鉴权/权限失败",
		"冲突未解决",
	}
	for _, want := range mustContain {
		if !strings.Contains(got, want) {
			t.Fatalf("DefaultMRContract missing %q\n---\n%s", want, got)
		}
	}
}

func TestDefaultPreviewContractAcceptsURL(t *testing.T) {
	contract := DefaultPreviewContract
	retry := DefaultPreviewRetry
	for _, want := range []string{
		"port 与 url 必须恰好提供其一",
		"外部 URL",
		`set_preview(url=`,
		"不做服务端探测",
		"不要为了走 port 而在沙箱里反代外部站点",
		"禁止",
		"dsh web",
	} {
		if !strings.Contains(contract, want) {
			t.Fatalf("DefaultPreviewContract missing %q\n%s", want, contract)
		}
	}
	if strings.Contains(contract, "port 必填") {
		t.Fatal("DefaultPreviewContract must not require port")
	}
	for _, want := range []string{
		"port?, url?, label?",
		"port 与 url 恰好其一",
		`set_preview(url=`,
		"不要在沙箱起反代",
		"也不要改用 port",
	} {
		if !strings.Contains(retry, want) {
			t.Fatalf("DefaultPreviewRetry missing %q\n%s", want, retry)
		}
	}
	if strings.Contains(retry, "set_preview(port, label?)") && !strings.Contains(retry, "url") {
		t.Fatal("DefaultPreviewRetry must not be port-only")
	}
}

func TestDefaultVisualContractHighFidelitySemantics(t *testing.T) {
	got := DefaultVisualContract
	mustContain := []string{
		"只读检查现有业务 UI",
		"设计令牌",
		"布局",
		"业务文案",
		"改后目标态",
		"无既有前端基线时(降级)",
		"不构成任何写入授权",
		"git add",
		"write_artifact",
		"page.html",
		"<!doctype html>",
		"localStorage",
		"sessionStorage",
		"不引用任何外部资源",
		"只读定位",
		"真实页面骨架",
	}
	for _, want := range mustContain {
		if !strings.Contains(got, want) {
			t.Fatalf("DefaultVisualContract missing %q\n---\n%s", want, got)
		}
	}
	if strings.Contains(got, "简洁美观") {
		t.Fatal("DefaultVisualContract must not fall back to generic demo wording")
	}
}
