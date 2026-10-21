# 节点接管（node takeover）后续事项

本 PR 有意不处理以下几项（GraspReview S3 / S4 / S8），留作后续。

## S3 接管中途崩溃后一律走冷沙箱，即使容器还活着
- 位置：`server/internal/engine/reattach.go`（reconcileInterrupted 里 manual 分支）、`engine/takeover.go` 的 `reconcileManualVisit`。
- 现状：如果控制面在接管过程中重启（StateRun 是 `running + manual`），这次执行会直接停成 `waiting_human` 的人工会话。第一个人工回合会重新冷启动一个沙箱。原沙箱不会被 adopt，最后由 `ReconcileOnStartup` 按 TTL 回收。工作区在 TTL 到期前其实还在，只是没有被用上，**未提交的工作区改动会丢失**。
- 建议：如果 `adoptableSandbox` 命中，就 attach 原沙箱，作为停放的人工会话使用（不发 prompt、不等待回合），并把它加入 `AdoptedSandboxes`。

## S4 崩溃窗口：saveState(waiting_human) 之后、finish(waiting_human) 之前
- 位置：`engine/reattach.go` 的 orphan 判定。
- 现状：StateRun 已经是 `waiting_human`，但 Run 还是 `running`，启动对账时会被当成 orphan，判为 `failed`。复审暂停也有同样的窗口，是已有问题。
- 建议：对账时如果发现最新一次执行是 `manual + waiting_human`（复审暂停可以一并处理），而 run 仍是 `running`，把 run 置为 `waiting_human`，不要判 failed。

## S8 `sort=last_activity` 用不上索引
- 位置：`server/internal/services/run.go` 的 `runListOrderBy`。
- 现状：排序表达式是 `COALESCE(last_activity_at, CASE … started_at/created_at END)`，用不上 `last_activity_at` 上的索引，表大了会全表排序。现有的 `started_at` 排序里的 CASE 也有同样问题。
- 建议：建 run 时就写入 `last_activity_at`，并回填历史行，然后直接 `ORDER BY last_activity_at`。

## Pre-existing flake: engine TestClarifyFirstMessageRedeliveredAfterCancelResume
- Fails intermittently under `-race` on main `8e8a4823` too, so it is not caused by this PR.
- Reproduced on a clean main worktree (2026-10-21) with
  `GOAMD64=v3 go test -race -count=300 -cpu 1,2,8 -run 'TestClarifyFirstMessageRedeliveredAfterCancelResume$' ./internal/engine`: 1 failure.
- Failure: `clarify_first_message_test.go:163: run … did not reach "waiting_human" (got "failed")`, after 45 s.
- Runs that did not fail: 100 single runs, and 8 full engine `-race` runs.
- Not fixed here. If it turns CI red, re-run the job.
