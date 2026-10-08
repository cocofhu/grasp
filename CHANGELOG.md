# Changelog

All notable public-release changes are documented here.

## Unreleased

- **Cursor turns no longer hang on background tasks:** `cursor-agent -p`
  waits for every background shell it started, so a preview server launched
  as a background task kept the turn busy forever. Once the Agent has been
  quiet for `SANDBOX_BG_TASK_GRACE` (default `2m`, `0` disables) with only
  background shells left, the sandbox ends those shells' process groups (only
  ones it can prove belong to the turn's CLI) and the CLI wraps up. A CLI that
  reported its result but has not exited 5s later is killed and the turn still
  succeeds. The liveness watchdog ignores the CLI's CPU/IO while it only waits
  on background tasks. Claude Code, which waits up to 10 minutes on its own,
  gets the same limit through `CLAUDE_CODE_PRINT_BG_WAIT_CEILING_MS` (an
  explicit value wins). The base Agent rule now says to start long-running
  services with `setsid nohup … &`.
- **One VNC desktop per sandbox — breaking route change:** the per-port
  `/preview-vnc/:runId/:nodeId/:port/ws` WebSocket is removed; every viewer
  uses `/sandbox-vnc/:sandboxId/ws`, and the external share page uses one
  desktop ticket per share (`preview-ticket` for `vnc` no longer needs a port).
  The desktop is the Agent's own screen: the platform takes over the browser
  page chrome-devtools MCP drives instead of opening its own tab, and
  connecting never navigates it. `set_preview` switches the desktop to the
  registered port, and port tabs navigate the same desktop. The run page shows
  the desktop as soon as the node has a sandbox, before any port is
  registered. Agents that can `set_preview` now default to `VNC_PREVIEW=1` and
  `BROWSER_MCP=1` (an explicit value wins). Direct-IP preview, the `/preview/`
  HTTP proxy and `preview-api` are unchanged.

- **Agent time limits — breaking config change:** an Agent turn no longer has
  a wall-clock limit. Two limits remain: the node's total time (the canvas
  node **Timeout** field; empty means the platform cap,
  `sandbox.agent_node_hard_cap_hours` / `GRASP_AGENT_NODE_HARD_CAP_HOURS`,
  24 h by default) and the no-activity limit per turn (20 minutes by default,
  **Settings → Sandbox resources**, pinned by `GRASP_CHAT_IDLE_SEC`).
  `sandbox.agent_chat_timeout_seconds` / `GRASP_AGENT_TIMEOUT_SEC` is ignored
  with a startup warning. A running command counts as activity while its
  processes use CPU or do IO, so long builds and test runs are not cut off. An
  Agent that goes quiet or repeats the same tool call 8 times is resumed once
  with a hint, then stopped and retried once in a fresh sandbox.
- **Test review gate:** a test result passes only when every plan coverage
  item cites a case that actually passed; skipped cases need a reason.
  Re-prompts ("nudges") ask the Agent to record what it did not run instead
  of writing placeholder results. The number of nudges is configurable per
  node (**Nudge retries**, default 3).
- **Sandbox exit reason:** when a sandbox dies mid-turn, the node error and
  the retry notice say why, e.g. `沙箱 OOM 被杀(8192MiB)`, instead of only
  `acp connection closed`. Needs the updated sandbox gateway
  (`GET /api/v1/sandboxes/:id/exit`).
- **Work branch names:** implement Agents name branches
  `<type>/<topic>-<run short ID>`, e.g. `fix/agent-liveness-224eb8c7`.

- **Sandbox memory limit setting:** every sandbox the platform creates now
  requests a memory limit from the gateway, 8192 MiB by default (previously
  the gateway default, usually 4096 MiB). Change it under **Settings →
  Sandbox resources** or pin it with `GRASP_SANDBOX_MEMORY_MB` /
  `sandbox.sandbox_memory_mb`; it applies to newly created sandboxes and must
  not exceed the gateway `maxMemoryMB`. Agent node prompts now state the limit
  and ask the agent to run heavy build/test/lint commands one at a time, since
  running them in parallel used to OOM-kill the sandbox and restart the node
  from scratch (seen as `acp connection closed: not connected`).
- **Breaking — one Agent node:** workflows now have seven node types:
  input, output, set variable, branch, Agent, human gate and proposal select.
  The old Agent-like types (Grasp, approve, react, plan, preflight, research,
  proposal, test, review, submit MR, visual, app preview, conditional prompt)
  are removed. An Agent node only picks an Agent, a goal and a timeout; what
  the Agent may do is declared once in its `capabilities` (interaction, review,
  tools, readable artifacts, written products) and edited on the new
  **Capabilities** tab in Agent Studio. Agents that write a test result or a
  review get **Pass** / **Fail** outlets. Workflows that still contain a
  removed type fail validation with "未知节点类型 X", and Agents without
  capabilities fail with "Agent X 未声明能力"; there is no migration, so
  delete and recreate them.
- **Breaking — platform rule overrides removed:** the platform rules settings
  page, the per-Agent platform rules tab and the `/api/platform-rules` APIs are
  gone. Behaviour now lives in each Agent's `AGENTS.md` and skills; the
  platform keeps only its fixed common protocol.
- **Built-in templates:** Clarify, Implement and Test & review replace the
  previous engineer templates. All three can start a preview with
  `set_preview`. Onboarding has four steps (connect, team, workflow preview,
  done) and generates input → Clarify → Implement → Test & review, with Fail
  looping back to Implement.
- **Workflow canvas:** redesigned with autosave, a collapsible palette, a
  slide-out inspector, labelled outlets, inline edge conditions, quick add
  (`/`, Ctrl/Cmd+K, dragging an outlet to empty space, or the + on an edge),
  undo/redo, keyboard shortcuts, 8px grid snapping with alignment guides and
  automatic layout. Run details use the same canvas read-only, follow the
  running node and lay out workflows that have no saved positions.
- **Wording:** "流水线" / "pipeline" is now "工作流" / "workflow" everywhere in
  the product.
- **Direct preview toolbar:** refresh the Pick / Artifact / Chat controls with consistent icons, spacing, states, responsive wrapping, and an accessible custom hint for the original-page preview action.
- **Project credentials UI:** reorganize credentials into API key, Git, SSH, and other sections with responsive cards, configuration summary, and a structured add-credential form.
- **Project credentials:** add a model-provider API Key card with provider/model/API Base/vision settings; keys remain write-only and masked, while routing metadata is injected into OpenCode Runs.
- **Project credentials:** ACP and Git credentials can be managed in project
  credential settings, which take precedence at runtime; compatible
  project/Agent environment variables remain a fallback. Platform service
  configuration keeps its explicit environment > file > defaults precedence.
- **Gate approval chat:** a message that starts while an earlier queued item
  was already trimmed no longer removes the wrong waiting message from the
  queue panel.
- **App preview:** noVNC now keeps one persistent page per sandbox. Reopening
  the preview resumes the same screen without reloading, switching ports
  navigates within that page, and browser logins survive a Chromium restart.
  The viewer starts watch-only, with **Take over** / **Return control**. With
  IP direct preview on, the panel still uses noVNC and adds an "open directly in
  new tab" button. Public share pages behave the same way. The new
  `desktop_idle_ttl_seconds` setting (default 0) can close idle pages.
- **App preview:** the noVNC window shows the browser tab strip and address bar
  again and no longer clips the top of the page. **Cancel annotation** now
  leaves pick mode, and the panel shows a tip if the page did not leave it. The
  watch-only hint is easier to read.
- **App preview:** a watched preview no longer drops after five minutes without
  input. If the connection is closed for inactivity or lost, the panel shows a
  readable reason and reconnects on its own with backoff, waiting until the tab
  is visible again. It does not reconnect when the preview was opened in another
  window.
- **App preview:** `set_preview` now waits briefly for a port that is still
  starting. When the port is unreachable, the error tells the agent what to
  fix, including the address the app actually listens on (for example
  `127.0.0.1` instead of `0.0.0.0`), instead of `Process exited with status 1`.
- **Runs:** a paused node's agent session no longer floods the server log with
  `acp event channel full` warnings, and the platform stops reconnecting to the
  sandbox every two seconds while no turn is running.
- **PM chat:** a reply no longer fails with "connection lost" when the socket
  drops, the tab is refreshed, or a turn runs longer than 90 seconds. The turn
  keeps running on the server; reopening the thread replays it and follows it
  live. Messages sent while PM is busy wait in line, and **Stop** also clears
  the line. A turn cut short by a server restart is marked **Interrupted by
  restart** and can be retried.
- **Agent Studio:** the chat tester and the sandbox console's chat tab keep
  their queue and running reply when the page is refreshed or the connection
  drops. The panel reconnects on its own and picks the reply up where it is,
  and destroying the sandbox stops its queued messages.

## 1.2.1 — 2026-09-28

- **Preview drawer:** compact Page Harness CoCo title bar (grip + PH badge +
  ellipsis name, 48px toolbar) (#672).
- **Direct preview:** keep the drawer ticket across a full-page redirect
  (#670).
- Default `./start.sh` / `.env.example` / `compose.release.yaml` pins GHCR
  images to `*:1.2.1`.

## 1.2.0 — 2026-09-28

- **Agent page control:** server bridge, drawer relay, and page executor so
  the agent can drive the preview (#656–#658); prompt and docs (#659);
  page-agent pointer (#660); Pick and Chat stay grey without a live drawer
  ticket (#661); custom no-ticket tooltip (#662).
- **Direct preview:** in-page Pick bar (#646); drawer tickets and chat tokens
  (#649); chat-only drawer (#650); chat drawer opened in a new tab (#651);
  floating chat (#654).
- **Requirement leftovers:** auto-write test/review leftovers to requirement
  drafts (#633); root-cause JSON (#634); leftover drafts are self-contained
  specs (#635).
- **Preview:** noVNC and direct iframes go through the pick proxy (#640);
  share polling no longer trips the public rate limit (#655); app preview
  refits when the viewport grows (#666).
- **Chat:** clear stale busy state so the last turn is not replayed into a
  new bubble (#652); ignore the previous-turn ACP snapshot in the next live
  bubble (#653); confirm-and-advance no longer hides behind a hung sandbox
  turn (#663).
- **Sandbox:** resume a silent turn once before timing out (#667).
- **Inbox:** stop the false pending-update banner when the list is already
  current (#632).
- **Run detail:** stage and Agent sidebar lay flush instead of floating
  cards; artifact tiles fill the stage without rounded corners (#668).
- Floating nav on all routes (#641).
- Default `./start.sh` / `.env.example` / `compose.release.yaml` pins GHCR
  images to `*:1.2.0`.

## 1.1.0 — 2026-09-20

- **Preflight node:** env-confirmation Agent node with `ask_form` and vars
  export (#604); create-wizard template dropdown plus PreflightAgent pack
  (#611).
- **Approve identity:** rename the Approve node type to `grasp` so confirm-flow
  uses the canonical type (#607). Grasp/approve Phase1 hides `node_complete`
  until the user confirms (#628).
- **Artifacts:** auto-pin visible products to preview tabs on create/update
  (#602); show agent products, sniff image uploads, and version overwrites
  (#603).
- **Confirm flow:** success overlay with a single-path check draw (#613);
  page-level host and click-overlay fixes (#614, #616).
- **Fix:** one-shot turns end on process exit, not pipe EOF — leftover child
  processes no longer hang the session (#619).
- **Fix:** Plan mermaid diagrams render serially (#622).
- Home pipeline rail rise-on-settle (#621); StatusMetrics run zone routes to
  `/runs` (#612).
- Web polish: artifact version menu, clarify labels, inbox deep-links,
  skip-round placeholder, new-workflow plus centering, brand-purple button
  hover (#608–#610, #615, #617, #618).
- README brand banner / Trendshift badge / QQ community QR (#623, #625);
  ignore the whole `data/` runtime root (#620).
- Default `./start.sh` / `.env.example` / `compose.release.yaml` pins GHCR
  images to `*:1.1.0`.

## 1.0.1 — 2026-09-14

- **Fix:** ReAct empty-fail Retry was a dead click — wrappers never forwarded
  `retry-last`; also stop duplicating the optimistic retry slot (#600).
- **Fix:** Persist ACP `error_text` / `prompt_done{failed}` on clarify, open,
  and revise turns so the failure card shows provider errors (quota / 4xx)
  instead of a blank “no output” bubble (#600).
- **Fix:** Keep AppButton slotted inline icons on one row (#599).
- Default `./start.sh` / `.env.example` / `compose.release.yaml` pins GHCR
  images to `*:1.0.1`.

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
