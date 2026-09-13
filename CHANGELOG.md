# Changelog

All notable public-release changes are documented here.

## Unreleased

## 1.0.0 — 2026-09-14

- First stable public release (not a beta / prerelease tag).
- Hover-ink landing cover on nav and buttons (#593); keep icon/label from
  shifting after left-edge overflow, leave, or Vue class patch (#595–#597).
- Split status bar into Token / run-zone KPI cards (#594).
- Artifacts: pack by run (#586) and page loading states (#588).
- README: demo video, screenshots, star history (#584–#587, #590, #591).
- Replace private/internal host fixtures with example.com (#592).
- Freeze Approve opening hint copy (#589).
- Default `./start.sh` / `.env.example` / `compose.release.yaml` pins GHCR
  images to `*:1.0.0`.

## 0.5.4 — 2026-09-13

- Revert high-contrast origin-fill button hover (#576 / #581); restore prior
  button hover styling.
- Default `./start.sh` / `.env.example` / `compose.release.yaml` pins GHCR
  images to `*:0.5.4`.

## 0.5.3 — 2026-09-13

- **Fix:** `publish-image` can build again. Web `npm run build` runs the brand
  guard at `../scripts/assert-no-approving-brand.mjs`; the image now keeps
  `web/` and `scripts/` under `/src` so the walk root is the project, not `/`
  (which 404'd on `v0.5.2`, then OOM'd when the script sat at `/scripts`).
- CodeQL on default runners frees unused SDK disk before analyze so incremental
  analysis does not fail the `security` job.
- README (EN / zh-CN) leads with the FSM differentiator: designed
  success / fail / rollback, visual clarify, parallel human gates.
- Default `./start.sh` / `.env.example` / `compose.release.yaml` pins GHCR
  images to `*:0.5.3`.

## 0.5.2 — 2026-09-13

- Clear remaining product Approving brand identifiers (storage keys, MIME,
  preview-pick path, doctor header, sandbox prefixes, fixtures) to grasp with
  one-shot read-old-write-new migration; add a CI brand guard. GitHub repo URLs
  are unchanged.
- Clarify composer: unanswered ask_question cards send text/images as a fixed
  three-line skip envelope instead of applying recommended choices.
- ReAct empty/failed idle slots stay visible with a trailing-turn retry; later
  turns hide retry so the API is not called on a buried failure.
- Restore the connecting preview tab with HardLoadLayer; connecting→ready no
  longer flashes HardLoadLayer or the pipeline grid.
- Keep the Run detail canvas/timeline switcher in the left pane (no overlay on
  right-side node tabs).
- Agent Studio: move import / new agent / create-team into the AGENTS list
  header; reclaim vertical space below the org list.
- Settings general no longer duplicates platform rules and integrations cards
  (still reachable via settings subnav).
- High-contrast origin-fill hover on buttons; login tab title waits for locale
  so it shows 登录/Login · Grasp instead of raw i18n keys; zh-CN shared agent
  project tab label is「共享Agent配置」.
- Default `./start.sh` / `.env.example` / `compose.release.yaml` pins GHCR
  images to `*:0.5.2`.

## 0.5.1 — 2026-09-13

- **Fix:** Grasp now injects `AGENT_PROVIDER` (and no longer `ACP_BACKEND`) so
  the five backends select the right CLI. 0.5.0 baked `ENV AGENT_PROVIDER=cursor`
  into `universal-sandbox` and only set `ACP_BACKEND`, so claude_code /
  codebuddy / trae / opencode all started Cursor CLI. **The server-side fix
  also works against the already-published `universal-sandbox:0.5.0` image**;
  you do not have to swap the image first.
- The sandbox image no longer bakes `AGENT_PROVIDER=cursor`. Runtime selection
  is `AGENT_PROVIDER` only; the `ACP_BACKEND` env alias is gone.
- Auth chain no longer fails silently: empty keys warn (and generated OpenCode
  `{env:OPENCODE_API_KEY}` without a value errors); dropping a platform-level
  official CLI key logs a WARN; Agent-session and workflow Token merge both
  keep the shared Token when both sides set one; Agent sessions also read the
  project shared workspace auth files; invalid `settings.json` errors instead
  of being overwritten.
- Remove leftover `APPROVING_*` recognition from server and web (no compat
  layer). `git grep APPROVING_` should only hit CHANGELOG and the agent-pack
  guard test. Remove `skill_profile` dual-read / migrate, `cursor/` workdir
  fallback, and gateway `SBGW_IMAGE_TEMPLATE` / `SBGW_IMAGE_MAP` / byProvider
  image resolution.
- Startup logs the SQLite path and whether the file already existed.
- Default `./start.sh` / `.env.example` / `compose.release.yaml` pins GHCR
  images to `*:0.5.1`.

## 0.5.0 — 2026-09-12

- **Breaking:** publish one sandbox image `ghcr.io/cocofhu/universal-sandbox`
  instead of per-backend `universal-sandbox-{cursor,claude_code,codebuddy,trae,opencode}`.
  The image preinstalls the five CLIs; runtime `AGENT_PROVIDER` / `ACP_BACKEND`
  selects the live backend. Upgrade pins in `.env` / compose from the old
  per-provider tags to `universal-sandbox:<tag>`. Existing `0.4.0` split tags
  stay on GHCR. `GRASP_SANDBOX_IMAGE_*` remains an optional per-backend
  override.
- Default `./start.sh` / `.env.example` / `compose.release.yaml` pins GHCR
  images to `*:0.5.0`.

## 0.4.0 — 2026-09-12

- **Breaking:** remove the approving → grasp compatibility window. The
  control plane no longer reads `APPROVING_*`, rewrites `.env`, migrates
  `approving.db` / `.approving`, aliases `approving-local-demo`, or folds
  leftover Agent / MCP `APPROVING_*` keys and interpolations. Upgrade to
  0.3.17-beta first if you still have old names, then set `GRASP_*` only.
- Default `./start.sh` / `.env.example` / `compose.release.yaml` pins GHCR
  images to `*:0.4.0`.

## 0.3.17-beta — 2026-09-12

- Fold leftover `${APPROVING_*}` interpolations in Agent / shared-Agent MCP
  url / headers / env on boot, read, and save. Runtime template vars keep
  `APPROVING_*` aliases so unsaved old templates still resolve until the next
  minor. Run-start sandbox env also denies leftover `APPROVING_*` auth /
  artifact keys.
- Default `./start.sh` / `.env.example` / `compose.release.yaml` pins GHCR
  images to `*:0.3.17-beta`.

## 0.3.16-beta — 2026-09-12

- Product wordmark, login/home splash, favicon, default notify prefix, DingTalk
  card title, run-log export header, and docs site brand are Grasp. A stored
  product name of `Approving` falls back to Grasp so upgraded instances do not
  keep the old logo. The Approve node display name is Grasp (type stays
  `approve`).
- Default `./start.sh` / `.env.example` / `compose.release.yaml` pins GHCR
  images to `*:0.3.16-beta`.

## 0.3.15-beta — 2026-09-12

- Fold stored Agent / shared-Agent `APPROVING_*` env keys to `GRASP_*` on
  read, save, and boot (the 0.3.14-beta rename left existing
  `APPROVING_CURSOR_API_KEY` rows in Studio). Runtime still accepts the old
  names until the next minor.
- Default `./start.sh` / `.env.example` / `compose.release.yaml` pins GHCR
  images to `*:0.3.15-beta`.

## 0.3.14-beta — 2026-09-12

- **Breaking:** rename the public package, image, and env prefix from Approving
  / `APPROVING_*` to Grasp / `GRASP_*`. Go module is now
  `github.com/cocofhu/grasp`; the app image is `ghcr.io/cocofhu/grasp`; compose
  service / binaries are `grasp` / `/app/grasp` / `/app/grasp-server`; default
  SQLite file is `grasp.db`.
- This release auto-migrates old config: the control-plane server rewrites
  `APPROVING_*` keys in nearby `.env` files on boot and still reads
  `APPROVING_*` (GRASP wins when both are set). `./start.sh` only exports
  aliases for compose interpolation. Default `approving.db` / `.approving`
  are renamed when the new path is free. The local-demo gateway key is
  `grasp-local-demo` and still accepts `approving-local-demo`.
- **Next release removes this compatibility.** Update compose, agent env
  templates, and secrets to `GRASP_*` now. Custom agent env that still
  injects `APPROVING_*` will not be dual-emitted into sandboxes.
- Default `./start.sh` / `.env.example` / `compose.release.yaml` pins GHCR
  images to `*:0.3.14-beta`.

## 0.3.13-beta — 2026-09-11

- OpenCode models that models.dev does not describe can opt into image input
  with `APPROVING_OPENCODE_MODEL_VISION=1`, so vision models on a gateway are
  not treated as text-only.
- Onboarding, Agent create, Agent meta, and shared Agent show a Vision switch
  for custom vendors and for catalog vendors with a typed-in model.
- Default `./start.sh` / `.env.example` / `compose.release.yaml` pins GHCR
  images to `*:0.3.13-beta`.

## 0.3.12-beta — 2026-09-11

- OpenCode's bridge model override now receives the normalized
  `provider/model` value, preventing `--model` from routing slashed upstream
  model IDs to the wrong provider.
- Agent, platform, and browser MCP servers are translated into OpenCode's
  native `opencode.json` `mcp` block; `mcp.json` remains available to the other
  ACP backends.
- Existing user-authored OpenCode configuration is merged without replacing
  user values, and malformed configuration is rejected instead of overwritten.
- Default `./start.sh` / `.env.example` / `compose.release.yaml` pins GHCR
  images to `*:0.3.12-beta`.

## 0.3.11-beta — 2026-09-11

- OpenCode custom providers can be named directly (for example
  `tencent-tokenhub`) instead of exposing a `custom/` routing prefix to users.
- Model IDs containing `/` are preserved end to end, so gateway IDs such as
  `deepseek/deepseek-flash` reach the upstream request unchanged.
- Provider and model membership use the same models.dev snapshot as the UI.
  Missing providers receive an OpenAI-compatible adapter; missing models are
  declared without replacing a catalog provider's native adapter.
- Catalog-absent providers require an API Base URL in every save flow.
- Default `./start.sh` / `.env.example` / `compose.release.yaml` pins GHCR
  images to `*:0.3.11-beta`.

## 0.3.10-beta — 2026-09-11

- Public beta follow-up on [`v0.3.10-beta`](https://github.com/cocofhu/approving/releases/tag/v0.3.10-beta)
  (relative to `v0.3.9-beta`: PRs #542–#551). Full notes on the GitHub Release.
- Default `./start.sh` / `.env.example` / `compose.release.yaml` pins GHCR
  `*:0.3.10-beta`, including `universal-sandbox-opencode` (tag publish does not
  rewrite these files).
- Highlights: Agent create wizards align with onboarding start paths; two-tone
  page slogans; micro-interactions / motion tokens; project Agents onboarding
  embed; global Agents sidebar + `/agents` Studio; home pipeline create entry;
  Agent Git step always shows provider choices.

## 0.3.9-beta — 2026-09-10

- Public beta follow-up on [`v0.3.9-beta`](https://github.com/cocofhu/approving/releases/tag/v0.3.9-beta)
  (relative to `v0.3.8-beta`: PRs #538–#541). Full notes on the GitHub Release.
- Default GHCR pins stayed at `*:0.3.8-beta` until `0.3.10-beta` (OpenCode
  sandbox image was first published on this tag).
- Highlights: OpenCode as fifth ACP backend; Visual `page.html` fidelity;
  from-baseline workflows on home; sidebar ZH copy + chrome motion.

## 0.3.8-beta — 2026-09-10

- Public beta follow-up on [`v0.3.8-beta`](https://github.com/cocofhu/approving/releases/tag/v0.3.8-beta)
  (relative to `v0.3.7-beta`: PRs #531–#536). Full notes on the GitHub Release.
- Default `./start.sh` / `.env.example` / `compose.release.yaml` pins GHCR
  `*:0.3.8-beta` (tag publish does not rewrite these files).
- Highlights: StatusMetrics opens `/stats`; home baseline-workflow modal;
  ReAct connecting loader; live run-detail chrome during clarify; public
  approval inbox parity; token analytics stacked-bar dimensions.

## 0.3.7-beta — 2026-09-10

- Public beta follow-up on [`v0.3.7-beta`](https://github.com/cocofhu/approving/releases/tag/v0.3.7-beta)
  (relative to `v0.3.6-beta`: PRs #526–#529). Full notes on the GitHub Release.
- Default `./start.sh` / `.env.example` / `compose.release.yaml` pins GHCR
  `*:0.3.7-beta` (tag publish does not rewrite these files).
- Highlights: home pipeline cards show project name; baseline workflow create
  requires a name; Prompts tab no longer false-dirties Agent Studio; unified
  embedded composer toolbar.

## 0.3.6-beta — 2026-09-09

- Public beta follow-up on [`v0.3.6-beta`](https://github.com/cocofhu/approving/releases/tag/v0.3.6-beta)
  (relative to `v0.3.5-beta`: PRs #501–#524). Full notes on the GitHub Release.
- Default `./start.sh` / `.env.example` / `compose.release.yaml` pins GHCR
  `*:0.3.6-beta` (tag publish does not rewrite these files).
- Highlights: home Composer run priority; instance brand settings; dashboard
  pipeline menu; workflow create from scratch/baseline; Runs in workspace nav;
  today's tokens in the topbar.

## 0.3.5-beta — 2026-09-09

- Public beta follow-up on [`v0.3.5-beta`](https://github.com/cocofhu/approving/releases/tag/v0.3.5-beta)
  (relative to `v0.3.4-beta`: PRs #494–#499). Full notes on the GitHub Release.
- Default `./start.sh` / `.env.example` / `compose.release.yaml` pins GHCR
  `*:0.3.5-beta` (tag publish does not rewrite these files).
- Highlights: first-install agents clone `vars.repos` on every node; default
  workflow shown on Home; floating workspace nav; ReAct stream while still
  replying.

## 0.3.4-beta — 2026-09-08

- Public beta follow-up on [`v0.3.4-beta`](https://github.com/cocofhu/approving/releases/tag/v0.3.4-beta)
  (relative to `v0.3.3-beta`: PRs #485–#492). Full notes on the GitHub Release.
- Default `./start.sh` / `.env.example` / `compose.release.yaml` pins GHCR
  `*:0.3.4-beta` (tag publish does not rewrite these files).
- Highlights: Approve `set_preview` live app preview; first-install wizard
  (default team + workflow); integrations in Settings modal; mobile ReviewShell
  drawer fill; pending-queue annotation re-edit.

## 0.3.3-beta — 2026-09-04

- Public beta follow-up on [`v0.3.3-beta`](https://github.com/cocofhu/approving/releases/tag/v0.3.3-beta)
  (relative to `v0.3.2-beta`: PRs #441–#483). Full notes on the GitHub Release.
- Default `./start.sh` / `.env.example` / `compose.release.yaml` pins GHCR
  `*:0.3.3-beta` (tag publish does not rewrite these files).
- Highlights: global Token analytics; Agent workspace VCS + file history;
  SSH credentials in Agent meta with pre-clone inject; Plan multi-diagram tabs
  and mermaid validate; composer drafts in IndexedDB; `skill_profile` →
  `agent_profile` with legacy compat.

## 0.3.2-beta — 2026-08-26

- Public beta follow-up on [`v0.3.2-beta`](https://github.com/cocofhu/approving/releases/tag/v0.3.2-beta)
  (relative to `v0.3.1-beta`: PRs #415–#439). Full notes on the GitHub Release.
- Default `./start.sh` / `.env.example` / `compose.release.yaml` pins GHCR
  `*:0.3.2-beta` (tag publish does not rewrite these files).
- Highlights: project shared Agent config (+ sandboxEnv migration); project external MCP;
  clarify queue cancel/reorder/edit; notification read prefs / per-run reads; mobile UX;
  Plan data_design hard gate; ACP timeline snapshot; home composer draft.

## 0.3.1-beta — 2026-08-23

- Public beta follow-up on [`v0.3.1-beta`](https://github.com/cocofhu/approving/releases/tag/v0.3.1-beta)
  (relative to `v0.3.0-beta`: CI flake fix #414). Full notes on the GitHub Release.
- Default `./start.sh` / `.env.example` / `compose.release.yaml` pins GHCR
  `*:0.3.1-beta` (tag publish does not rewrite these files).

## 0.3.0-beta — 2026-08-23

- Public beta follow-up on [`v0.3.0-beta`](https://github.com/cocofhu/approving/releases/tag/v0.3.0-beta)
  (relative to `v0.2.2-beta`: PRs #385–#412). Full notes on the GitHub Release.
- Default `./start.sh` / `.env.example` / `compose.release.yaml` pins GHCR
  `*:0.3.0-beta` (tag publish does not rewrite these files).

## 0.2.2-beta — 2026-08-21

- Public beta follow-up on [`v0.2.2-beta`](https://github.com/cocofhu/approving/releases/tag/v0.2.2-beta)
  (relative to `v0.2.1-beta`: PRs #327–#384). Full notes on the GitHub Release.
- Pin `./start.sh`, `.env.example`, and `compose.release.yaml` defaults to GHCR
  `*:0.2.2-beta` (tag publish does not rewrite these files).

## 0.2.1-beta — 2026-08-14

- Public beta follow-up on [`v0.2.1-beta`](https://github.com/cocofhu/approving/releases/tag/v0.2.1-beta)
  (relative to `v0.2.0-beta`: PRs #311–#325). Full notes on the GitHub Release.
- Pin `./start.sh`, `.env.example`, and `compose.release.yaml` defaults to GHCR
  `*:0.2.1-beta` (tag publish does not rewrite these files).
- Highlights: 反馈账本与 Agent 总结；GateShare 权限预设；应用预览 IP 直连/取点；
  公开审批滚动与确认态；MCP CAPA 误杀修复；若干 Web UX 修复。

## 0.2.0-beta — 2026-08-13

- Public beta follow-up on [`v0.2.0-beta`](https://github.com/cocofhu/approving/releases/tag/v0.2.0-beta)
  (relative to `v0.1.2-beta`: ~385 commits, PRs through #309). Full notes on the GitHub Release.
- Pin `./start.sh`, `.env.example`, and `compose.release.yaml` defaults to GHCR
  `*:0.2.0-beta` (tag publish does not rewrite these files).
- Highlights: QQ/企微/飞书/钉钉等多渠道与 live 接话；公开审批与分享链路；需求草稿/PRD；
  通知中心与顶栏指标；应用预览/noVNC；工作流收藏与团队模板；大量 Web Loading/布局打磨。

## 0.1.2-beta — 2026-07-31

- Public beta follow-up on [`v0.1.2-beta`](https://github.com/cocofhu/approving/releases/tag/v0.1.2-beta)
  (relative to `v0.1.1-beta`: PRs #143–#151). Full notes on the GitHub Release.
- Pin `./start.sh`, `.env.example`, and `compose.release.yaml` defaults to GHCR
  `*:0.1.2-beta` (tag publish does not rewrite these files).
- Highlights: Gates Inbox 纳入 app_preview 待审批；Inbox 布局/预览预算修复；inspect 可取消；
  revise 失败不再显示 Done；PreviewIssues 冷会话退回；sandbox `/tmp` PVC；Token 实心饼图；
  ArtifactPreview 图片 padding；输出节点 goto 邻接选项。

## 0.1.1-beta — 2026-07-29

- Public beta follow-up on [`v0.1.1-beta`](https://github.com/cocofhu/approving/releases/tag/v0.1.1-beta)
  (relative to `v0.1.0-beta`: PRs #136–#141). Full notes on the GitHub Release.
- Pin `./start.sh`, `.env.example`, and `compose.release.yaml` defaults to GHCR
  `*:0.1.1-beta` (tag publish does not rewrite these files).
- Highlights: Token 按模型统计；审计分页升级；HTML 主产物放大；沙箱环境变量单行启用开关；
  human_gate 冷会话静默；Run 筛选触发文案左对齐。

## 0.1.0-beta — 2026-07-29

- Public beta on [`v0.1.0-beta`](https://github.com/cocofhu/approving/releases/tag/v0.1.0-beta)
  (relative to `v0.0.4-beta`: PRs #113–#134). Full notes on the GitHub Release.
- Pin `./start.sh`, `.env.example`, and `compose.release.yaml` defaults to GHCR
  `*:0.1.0-beta` (tag publish does not rewrite these files).
- Highlights: 评审统一「确认并流转」；澄清/预览流式与刷新续传；桌面 HTML 预览 fillParent；
  项目管理文案；app_preview 纯 ReAct；同项目 agent_profile 约束。

## 0.0.4-beta — 2026-07-28

- Public beta follow-up on [`v0.0.4-beta`](https://github.com/cocofhu/approving/releases/tag/v0.0.4-beta)
  (relative to `v0.0.3-beta`: onboarding bootstrap #109 + pin). Full notes on the GitHub Release.
- Pin `./start.sh`, `.env.example`, and `compose.release.yaml` defaults to GHCR
  `*:0.0.4-beta` (tag publish does not rewrite these files).
- Highlights: 空项目首次上手引导（五步向导 + bootstrap 五 Agent +「快速上手·轻量」；默认 well-known Heroku git）。

## 0.0.3-beta — 2026-07-28

- Public beta follow-up on [`v0.0.3-beta`](https://github.com/cocofhu/approving/releases/tag/v0.0.3-beta)
  (relative to `v0.0.2-beta`: ~43 commits, PRs #62–#106). Full notes on the GitHub Release.
- Pin `./start.sh`, `.env.example`, and `compose.release.yaml` defaults to GHCR
  `*:0.0.3-beta` (tag publish does not rewrite these files).
- Highlights: PM MCP artifact/react/fs；QQ 通知与模板；审计轨迹；TagFilter；澄清/审批流式续传；
  release 持久化 `/app/data` + 按 acpBackend 多镜像；安全/CI（CodeQL、x/net、gateway Dockerfile）。

## 0.0.2-beta — 2026-07-26

- Public beta follow-up on [`v0.0.2-beta`](https://github.com/cocofhu/approving/releases/tag/v0.0.2-beta)
  (relative to `v0.0.1-beta`: ~161 commits, PRs #1–#61). Full notes on the GitHub Release.
- Pin `./start.sh`, `.env.example`, and `compose.release.yaml` defaults to GHCR
  `*:0.0.2-beta` (tag publish does not rewrite these files).
- Highlights: Run delete/cancel/sort + Token KPIs; Board/project Token stats;
  Agent 5-step wizard; PM IM QQ egress + cron UTC fix; Docker/K8s sandbox logs;
  docs `/en` + marketing site; CI coverage gates, e2e, CodeQL/security cleanup.

## 0.0.1-beta — 2026-07-25

- Initial public beta of Approving (MIT, Copyright 2026 cocofhu).
- Strip private registry, Apollo, and k3s preview hosts; default sandbox images
  to `universal-sandbox-*:local` (keep public `github.com/cocofhu` / `ghcr.io/cocofhu`).
- Adopt `APPROVING_*` environment variables, `.approving/` workspace paths, and
  matching binary/image names.
- Remove repository GitLab CI, VitePress docs site, showcases, deploy previews,
  and root selftest scripts. Keep sandbox Git host authorization for GitLab and
  GitHub (`GITLAB_*` / `GITHUB_*` / SSH on Agent meta env).
- Vendor `sandbox-gateway` (control plane + universal sandbox image) into
  `sandbox-gateway/` so a single clone runs via `./start.sh` / `docker compose up`.
- Add GitHub Actions CI, release-smoke, and GHCR publish workflows; root
  community policy files; `GATEWAY.md` and generated `server/CONFIGURATION.md`.
- Chinese README (`README.zh-CN.md`).
