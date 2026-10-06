# Server deadcode disposition (PR1)

Baseline: `deadcode -test=false ./...` → **69** unreachable symbols (2026-08-10, `feature/server-backend-refactor`).

Legend: **内化** = unexport / keep as test helper; **删除** = remove thin wrapper + update tests; **保留** = keep exported or intentional.

## Summary

| Action | Count (approx) | Notes |
|--------|----------------|-------|
| 内化 | majority of test-only exports | Prefer shrinking export surface |
| 删除 | thin aliases with no production callers | SelectRecommendedOption, ClarifyInboxKind, IsInboxReviewNode, NormalizePmEnabledMcps, BuildPmPlatformMCPSpecs, EnsureChildGroup, CreateSession |
| 保留 | sandboxtest/*, *ForTest, OpenSQLiteTest, blob.Memory, SetExecHook, TeamEmbedPackageNames, already-unexported internals | Not business dead modules |
| PR2 | memory/context/scheduler/pm Host.Register | Production mint path unification |

## Full checklist (69)

| # | Symbol | Disposition | Rationale |
|---|--------|-------------|-----------|
| 1 | auth.Service.CreateSession | 删除 | Thin wrapper over createSession; tests call createSession |
| 2 | auth.Service.RateLimiter | 内化→rateLimiter | Test-only getter |
| 3 | blob.IngestBytes | 内化→ingestBytes | No production callers |
| 4–7 | blob.NewMemory / Memory.* | 保留 | In-memory Store for tests |
| 8 | channels.ClassifyProgressFromACP | 内化→classifyProgressFromACP | Test helper; live path uses accumulator |
| 9 | config.KnownSandboxBackends | 内化→knownSandboxBackendKeys | Test/introspection only |
| 10 | contextmcp.Host.Register | PR2 收口 | Production uses Restore |
| 11–13 | database.OpenSQLiteTest / ensureSQLiteTemplate / closeGorm | 保留 | Test DB helpers |
| 14 | engine.waitReviewReadyForTest | 保留 | *ForTest |
| 15 | gateshare.IPLimiter.Allow | 保留 | Public limiter API (tests + future) |
| 16 | gateshare.IPLimiter.LenForTest | 保留 | *ForTest |
| 17 | memorymcp.Host.Register | PR2 收口 | Production uses Restore |
| 18 | models.SelectRecommendedOption | 删除 | Thin over SelectRecommendedOptions |
| 19 | models.SumTokenUsageByModel | 内化→sumTokenUsageByModel | Test-only |
| 20 | models.ValidTrigger | 内化→validTrigger | Test-only |
| 21 | nodereg.KnownTypes | 内化→knownTypes | Test-only |
| 22–23 | runtime.newACPProvider / gitRepoScheme | 保留 | Already unexported; RTA false-ish |
| 24–27 | sandbox Supports*/Fetch* | 内化 | Test-only gateway probes |
| 28 | sandbox.quoteShellPath | 保留 | Already unexported; used in tests |
| 29 | sandbox.safeCmd.String | 保留 | Method on unexported type |
| 30 | sandbox.SetExecHook | 保留 | Cross-package test hook (documented) |
| 31–50 | sandboxtest.FakeGateway.* | 保留 | Test package; not business dead code |
| 51 | schedulermcp.Host.Register | PR2 收口 | Production uses Restore |
| 52–53 | CronScheduler.SetMaxParallel / SetClaimStaleMinutes | 内化 | Test setters |
| 54–55 | ClarifyInboxKind / IsInboxReviewNode | 删除 | Thin over clarifyInboxKind |
| 56 | NotifyPoliciesEqual | 内化→notifyPoliciesEqual | Test-only; Workflow* used in prod |
| 57 | BuildOnboardingLightGraphForTest | 保留 | *ForTest |
| 58–59 | ApplyDeleteGroup / ApplyMoveAgent | 内化 | Pure helpers; tests only |
| 60 | NormalizePmEnabledMcps | 删除 | Alias of EffectivePmEnabledMcps |
| 61–62 | ProjectService.TotalTokens* | 内化 | Test aggregations |
| 63–64 | Format*DeepLinkForTest | 保留 | *ForTest |
| 65 | BuildPmPlatformMCPSpecs | 删除 | Thin compose of Agent+PmRole specs |
| 66 | TeamService.EnsureChildGroup | 删除 | No callers (prod or test) |
| 67 | TeamEmbedPackageNames | 保留 | Embed sync guard (services_test) |
| 68 | SetRenameAgentProfileRefsFailHookForTest | 保留 | *ForTest |
| 69 | shutdown.Coordinator.GracePeriod | 内化→gracePeriod | Test-only |

## Explicit non-goals (记账)

- **embed Agent 树**：`agents/`、`team_embed`、`first_install_embed` — 保留。
- **pmmcp.Host.Register**：随 PR2 与生产铸币统一一并收口（基线 deadcode 未单列因生产仍调用）。

## Evidence

- Baseline: `deadcode -test=false ./...` → **69** (pre-change).
- After PR1+PR2+PR3: **58** remaining.
- Cleared from production export surface (examples): `Host.Register`×3 (+ pmmcp production callers), `CreateSession`, `SelectRecommendedOption`, `ClarifyInboxKind`, `IsInboxReviewNode`, `NormalizePmEnabledMcps`, `BuildPmPlatformMCPSpecs`, `EnsureChildGroup`, plus many same-package unexports.
- Remaining 58 are intentional keeps: `sandboxtest/*` (~20), `*ForTest`, `OpenSQLiteTest`, `blob.Memory`, `SetExecHook` (cross-package test hook), `TeamEmbedPackageNames` (embed sync), already-unexported internals, and test-only unexported helpers still unreachable from `main` (expected with `-test=false`).
- Gate: `golangci-lint` 0 issues; `go vet` clean; `go test ./...` green; `cover-check-server.sh 90` → **90.1%**.
