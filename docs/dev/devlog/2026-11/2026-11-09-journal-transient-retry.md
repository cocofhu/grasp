# 2026-11-09 会话日志瞬时失败有限重试

- 日期：2026-11-09
- 范围：`server/internal/chatsession/journal_prepared.go`、`journal_retry*.go`、`journal_writer.go`、`journal_writer_interval_test.go`
- 做了什么：对象准备和 SQL 提交遇到明确可重试的网络、5xx/限流、锁冲突时，在原批次至多 10 秒的截止时间内退避，最多六次瞬时失败；提交结果不确定时保留同一 PreparedJournal 的 ID、范围和正文引用。重试在当前 owner、外部执行世代和业务校验通过后核对完整原收据，避免已提交而应答丢失时重复追加。只有明确回滚的 head CAS 冲突可以重新准备。永久错误、损坏和所有权丢失仍立即失败。低流量周期刷新从 100ms 调整为 1 秒，显式 Flush/Close、256KiB 和 128 条阈值仍立即提交，保持 FIFO 和现有有界背压。
- 为什么：一次短暂存储故障不应直接终止运行中的 Agent；按原错误重新取 head 追加又会重复记录。降低小块输出的写请求放大，代价是低流量输出最多约 1 秒才出现在已提交历史中，前端仍不展示尚未持久化的内存后缀。
- 如何验证：Journal 定向测试及 `-race -count=3` 通过，覆盖 PUT 瞬时失败、已提交但应答丢失、完整收据身份/owner 校验、原 60ms 调用截止、错误和次数上限、FIFO、onFailure 不被瞬时失败触发，以及周期刷新与阈值/显式屏障的区别。
