# 2026-11-10（修复工作流节点快照对克隆仓库必然失败、运行卡在已暂停）

- 日期：2026-11-10
- 范围：`sandbox-gateway/sandbox/internal/workspacesnapshot/{types,git,git_restore,pack,verify}.go`、`sandbox-gateway/sandbox/internal/workspacejob/service.go`、`server/internal/workspacefiles/{manifest,verify}.go`、`server/internal/services/workflow_workspace_files.go`、`server/internal/engine/run_control.go`、`web/src/lib/run/{runLive,useRunLive}.ts`、`CHANGELOG.md`
- 做了什么：快照清单的 Git 引用新增 `target` 字段，`refs/remotes/origin/HEAD` 等符号引用按原样采集、由沙箱与服务端双重校验（直接引用与符号引用二选一、目标须为合法 ref、不得自指），还原时用 `git symbolic-ref` 重建；HEAD 指向的分支本身是符号引用时按 Git 规则有限深度解析。快照执行器失败时在沙箱日志记录真实错误，不再只剩 `executor_failed`。快照任务确定失败（failed/cancelled/expired）后，「继续」或 reconcile 以新请求号重新采集并在成功后按封存结果流转；结果不确定（unknown）的仍须核对原任务。前端将 `paused` 的运行视为节点之间等待：对话页不再因跳回自身而一直「加载运行…」，运行中途的保存暂停也不再被当成运行结束。
- 为什么：`e24c2eac` 起每个节点完成后先保存工作区快照，而快照只接受指向对象的引用；`git clone` 必然写入的 `origin/HEAD` 符号引用使所有带仓库的工作流在首个节点结束时快照失败，运行停在「已暂停」，且失败的 checkpoint 没有任何恢复路径。默认运行页改为对话视图后，`paused` 又会跳转到同一地址导致页面一直加载。
- 如何验证：在与线上相同布局的完整克隆上，用沙箱快照代码复现修复前的 `workspace Git metadata cannot be safely materialized`，仅删除 `origin/HEAD` 即可通过；修复后同一克隆采集成功（844 个引用）。新增回归覆盖：克隆仓库（含 packed-refs、附注 tag、stash、分支切换、符号分支别名、未提交与未跟踪文件）的采集—还原往返；沙箱与服务端畸形符号引用拒绝；首次快照失败后「继续」以新请求号重采并流转到下一节点、unknown 不重试；前端 paused/queued 运行留在对话页（修复前 3 项失败）。
