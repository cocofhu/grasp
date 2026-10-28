# Changelog

All notable public-release changes are documented here.

## Unreleased

- **Accepted chat messages:** Workflow, Solo and gate chats show each accepted send once while waiting for live frames and saved history. Repeated text and image-only sends retain separate turns and queue order; rejected, edited or cancelled pending messages are removed correctly. Queuing another message preserves the current answer's streamed segments. Gate replies stay attached to their own request without duplicating saved preview feedback, and the LLM transcript receives live frames and reconnect recovery.

- **Worktree Git authentication:** Agents now receive their selected Git credentials for commands after checkout, including `git push` and GitHub/GitLab CLI operations. Each backend uses private credential files and Git configuration, so concurrent nodes keep their own authorization and an empty selection cannot inherit the server's credentials.

- **Reliable live message delivery:** repeated identical clarify replies stay in separate turns, including when the new reply text matches the previous one and the saved snapshot has not arrived yet. Completion refreshes are no longer lost, and stale detail or preview-feedback responses cannot overwrite newer context. PM retries a failed cold-start history read on the next idle frame of the same connection and still isolates back-to-back turns. Agent Studio reconnects without deleting an older same-text turn or duplicating the turn that started while the socket was down. When a reconnect flush delivers new output before `turn_begin`, the restored open reply keeps both the event-log prefix and that output. Slow chat subscribers close the WebSocket even when a data write is blocked, so Solo, PM, and Agent Studio can reconnect. A Solo reconnect snapshot restores session status and model state that is not kept in the replay buffer, and a slower session read started before that snapshot cannot overwrite it.

- **Repository settings:** a repository now stores only its name, description, URL, default branch and directory. Access methods, credential bindings and environment variables are removed from the repository form, API and Solo snapshots. New sessions and restarts use the current Agent's environment, credential bindings and Git authorization, including its existing default inheritance. Upgrade deletes those historical fields and does not copy them onto any Agent; a private repository that relied on them needs the Agent configured again. Public anonymous clones, repository selection, branch and directory checks, and platform or system variables stay as they are. Running sandboxes are not interrupted; the next restart follows the new rules.

- **DeepSeek Harness token usage:** workflow, React and Solo turns now record per-turn input, output and cache tokens from DSH's session projection, so usage analytics, cost estimates and the token ledger include DeepSeek Harness. Cached input is counted once in its own bucket; turns without a model call, unavailable caches and subagent sessions keep the previous omission behavior.

- **Clarify timeline alignment:** the in-progress 「思考中…」 indicator and label now line up with thought and tool summaries in current turns, follow-up nodes and run history. First-token placeholders, 「输出中」, other busy indicators and reduced-motion behavior are unchanged.

- **Agent chat failures:** OpenCode passes reasoning variants separately from model IDs. Gemini chats now display native streamed text, tools and failure details, with cached input counted once. Solo and Agent test chats show provider failures and refusals, including errors recovered from older empty replies, while retaining partial output and usage.

- **API model image input:** OpenCode, CodeBuddy and DeepSeek Harness model groups can declare text-only or text-and-image input for primary and auxiliary models. Saving fills missing model-directory metadata without overwriting explicit choices. Native adapters consistently honor those declarations, preventing image-capable custom models from being treated as text-only when their API directory omits capabilities. Claude Code and Codex retain their native image handling; platform-authorized tools retain their own model catalogs.

- **CLI chat activity:** Solo and Agent test chats keep an animated processing indicator visible between tool calls and reply text until the turn ends, including OpenCode, Claude Code and DeepSeek Harness. Completed tools retain their completed status, and stopped or failed replies clear the activity indicator.

- **Home scrolling:** hide the scrollbar beside the home composer while keeping overflow content accessible by mouse wheel and touch in Workflow and Solo modes.

- **Sandbox Agent recovery:** a broken ACP output stream now closes the failed connection, releases pending calls and marks the Agent disconnected. Reconnecting creates a usable session instead of leaving later messages busy forever; delayed exit handling cannot disconnect a replacement session.

- **Solo app previews:** preview registration now resolves the current Solo sandbox and its Agent's preview permissions. Existing Solo sessions also recover their MCP authorization after a server restart, while closed sessions and replaced sandbox generations remain rejected.

- **Solo message delivery:** fixed subscription races that could discard a sent message's opening frame when the queue snapshot shared its sequence number. Reconnects now resume after the last processed real frame rather than the latest snapshot, without mistaking legal replay gaps for lost frames; identical replays remain deduplicated, and the live transcript no longer requires a refresh.

- **Coding-tool credentials:** OpenCode CLI and DeepSeek Harness now save their own vendor keys as coding-tool authorizations, alongside Cursor and CodeBuddy. General model service keys remain in API accounts. Both tools can use their vendor authorization or an explicitly selected API account when configuring a model.

- **API account model selection:** model configuration reads the selected API account's model directory for searchable primary and auxiliary choices. Saved models stay available, failed reads can be retried, and original model IDs can still be entered manually. Only selected models are added to the account when saving.

- **Model brand icons:** API model options and Solo selectors show their own brand marks, including GPT, Kimi, GLM and Hunyuan, even when accessed through one shared API account. Unknown models retain the account provider mark, and color logos remain readable in dark mode.

- **Default Solo Agent:** first-time setup also creates an empty `Solo` Agent using the selected model and Git settings. Solo conversations select it by default when no valid saved choice exists; existing choices and an already customized Solo Agent are preserved.

- **First-time setup:** the existing setup window guides users through credentials, coding-tool configuration and Git. Empty steps open their setup forms directly; saving credentials or a model advances to the next step without a completed-items list. Forms stay inside the current window, and API address settings use the shared switch component. Existing credentials and models can be reused, and edits stay protected while saving.

- **Solo model controls:** startup selectors have a border, while active conversations use a borderless model control. Both show the actual model name and open a compact name-only model list with lighter shadows, small brand marks and a clear selected check. Light mode uses a white panel. Supported models offer their available reasoning levels in a small slider panel; a single-model configuration opens that panel directly. Session changes apply to the next turn, with rollback if the coding tool rejects the change.

- **Model groups and model names:** workspace model management now uses “Model groups” and “New model group” (「模型组」/「新建模型组」), including the edit dialog and group-name field. Solo selectors show each concrete model's catalog name or upstream ID instead of a configuration or API service name such as TokenHub; automatic tool selection keeps its existing label.

## 3.0.0 — 2026-10-26

- **Clarify conversation recovery:** delayed queue snapshots and old temporary replies no longer leave later questions read-only after submitting choices. Reconnecting after a completed turn restores the saved questions and releases the stale busy state without reloading the page.

- **Clearer token analytics:** usage structure now uses readable ranked rows with drill-down details. Model and workflow breakdowns share a full-width tabbed table with usage bars, and the busiest runs show eight rows by default with an option to expand; CSV exports still include the complete list.

- **Home default subtitle:** when the home subtitle is left blank, the line under the mascot and the settings placeholder now read 「我们应该做些什么？」 in Chinese and “What should we do?” in English. Saved subtitles stay as written.

- **Workflow shared sandbox switch:** enable Shared sandbox in the workflow editor’s default settings and save. New runs use that selection directly; the separate platform-wide enable flag has been removed. Workflow settings now use consistent groups and switches, with searchable variable-type and Agent selectors.

- **Simpler account and inheritance settings:** Credentials separates coding-tool authorizations under Agent CLI from model service API accounts, both with provider logos. Removed the separate connections list, add-connection entry and model catalog. New Agents inherit by default; Agent metadata collapses inherited settings into one card; switching inheritance off restores local choices. The card keeps its size, and save feedback transitions smoothly. Removed automatic credential slots and old-format inference; only explicitly created current credentials are listed and used.

- **Credential hosts and Agent defaults:** GitHub and GitLab credentials now store their own Host, defaulting to the official service, instead of a separate GitLab URL credential. Coding-tool and Git credentials appear in their own sections. Agent defaults can select a named model and several Git authorizations; each Agent can independently inherit either selection. Selecting a model determines the coding CLI without a duplicate tool picker.

- **Unified model bindings (breaking configuration change):** Agent creation, editing, onboarding, team creation and execution now use only a saved model configuration: coding tool + credential + upstream model. Removed legacy Agent credential slots, direct bindings and automatic migrations. Existing Agents must select a model configuration or explicitly inherit the configured default before execution. Missing or inactive references fail explicitly; changing a shared model configuration affects all referencing Agents.

- **Sandbox binding validation:** long-lived sandboxes validate applied model and Git bindings before reuse or another chat turn; changed authorization requires reopening. Messages adapters preserve gateway prefixes and avoid duplicate API-version paths. CodeBuddy model connections support Chat Completions only.

- **Conversation header alignment:** Solo sessions now use the same workspace frame as workflow conversations, aligning header spacing and resource/artifact controls. Mobile approval chats place their back and share buttons in the shared header, removing the repeated title and separate share row.

- **Multiple Git credentials per Agent:** Agents can select several saved GitHub, GitLab and SSH credentials, mark a preferred identity and optionally scope accounts to a site or repository. Selected credentials are staged in the sandbox; workflow nodes apply their Agent's bindings and shared sandboxes replace credentials when nodes change. Missing or revoked selections stop execution without trying another account. Empty custom bindings grant no Git access when inheritance is disabled; inherited Agents use the selected default authorizations. Old single-slot bindings are removed.

- **Explicit Agent model selection:** creating an Agent requires a valid selected or inherited model configuration. Turn inheritance off to choose a custom configuration; missing or unavailable configurations block progression and creation. Both Credentials and Models tabs now share one add menu for credentials and models.

- **Agent configuration controls:** the create wizard combines coding tools and models on one page, followed by Git and review. Coding tools use a compact logo list with an integrated service-region selector across create and edit. Model, credential, connection, auxiliary-model and MCP dropdowns use the shared searchable picker, with explicit unavailable-reference states and keyboard navigation.

- **Model setup wizard:** coding tools use logo cards, and one searchable credential picker automatically selects the matching authorization or service. OpenCode CLI and DeepSeek Harness shortcuts create reusable model API credentials. Creating a missing credential returns to the current model draft; back navigation preserves selections. Advanced API settings are collapsed, and dialogs support keyboard focus and narrow screens.

- **Credentials and models UI:** simplified rows show a logo, name and short subtitle, with icon actions and model enable switches. Service details stay in the editor. Provider selection now supports search, logos and 15 presets, alongside custom services.

- **Reusable model configurations:** create credentials first, then save a named combination of coding tool, credential and selected model in the Models tab. Agents bind these configurations by reference. API accounts store service addresses and keys; Added protocol adapters for Claude Code, CodeBuddy Chat Completions, OpenCode, Codex Responses and DeepSeek Harness; Cursor and Trae retain platform authorization. Missing or revoked references fail explicitly without falling back to another key.

- **Workspace credentials:** credentials now appear in compact lists grouped by coding tools, model API keys, repositories and SSH. Status, masked values and actions stay together; adding a credential uses a three-step wizard with a visible step indicator. Configure provider presets or custom API bases and keys here, then combine a coding tool, credential and model in the separate Models tab.

- **Icon motion and shapes:** functional icons across the app, sandbox chat and preview tools now share single-play hover and keyboard feedback, with reduced-motion support. Labels, logout, workflow, copy, delete and terminal glyphs are corrected; completed checks draw left to right without a dot. Only active loading states keep moving.

- **Coding tool picker:** product icons now appear before tool names in Agent, team and first-time setup. The picker hides internal configuration paths.

- **DeepSeek Harness backend:** choose DeepSeek Harness alongside the other coding tools, select Flash or Pro, and run agents with their instructions, skills and MCP tools through native dsh ACP. Authenticate with a dedicated DeepSeek API key. Requires the updated sandbox image with dsh installed.

- **Settings navigation:** clicking the selected settings icon again returns to the previous workspace page, preserving its filters and URL fragment; a direct settings visit returns to Home.

- **Sidebar consistency:** settings and workspace pages now use the same inset, rounded sidebar frame, keeping the left navigation icons and account avatar in the same positions when switching pages.

- **Segmented tabs:** run status, usage filters, editor modes and other segmented controls now share the Home switch's smooth moving highlight. Run counts travel with the highlight and reuse the matching filter's count instead of briefly showing the previous tab's number.

- **Home empty state:** removed the explanatory paragraph and view-all shortcut when no workflows are available on Home, keeping the create-workflow card without an empty spacer row.

- **Home mode highlight:** Workflow and Solo now share one background highlight that slides and resizes with a soft spring easing. The previous option stays transparent, including during rapid switching; reduced-motion preferences are respected.

- **Home mode switching:** switching between Workflow and Solo keeps the hero and mode switch anchored, and fades and slides the mode-specific controls and panels into place instead of shifting abruptly.

- **Home workflow selector:** the inline workflow picker now uses the same flat, transparent treatment as the Solo selectors instead of appearing as a raised card.

- **Workflow and Solo conversation headers:** Solo sessions now use the shared workflow conversation header sizing and compact drawer controls so both startup views have the same top bar.

- **Solo home selectors:** the Agent and repository selectors now use a flat transparent treatment in the composer toolbar, with a subtle hover/open tint instead of a raised gradient and glow.

- **Agent files editor:** fewer columns. Markdown files open in a single editor with an Edit / Split / Preview switch in the toolbar (no minimap), and version history opens from a toolbar button instead of taking a column on the right.

- **Shared run sandbox (opt-in, off by default):** a workflow can run every node of a run in one sandbox, so the workspace, installed dependencies and uncommitted work carry over between nodes. Enable Shared sandbox in the workflow editor’s default settings (`defaults.sandbox.mode = shared`). When the next node uses a different agent or different credentials, the sandbox's env and credentials are re-initialized before that node starts.

- **Loading skeletons:** the shimmer is now a soft neutral sheen with a pause between sweeps instead of a fast purple wave, blocks have rounded corners, and the skeleton fades in after a short delay so quick loads no longer flash it.

- **Conversation navigation:** hovering the message rail now opens a compact outline of every question, highlights the current one, previews the hovered reply and can load earlier messages; touch screens jump straight from the marks.

- **Artifact drawer preview:** artifacts opened in the drawer no longer repeat the name and file-name row above the content; the tab already names them.

- **Artifact preview:** the file header no longer repeats the type (e.g. “JSON”) next to a filename that already shows its extension.

- **Artifact drawer workflow:** the workflow list now matches the resource and artifact cards — node icons form the timeline, the duplicate status dots are gone, and the current step is a soft highlight with the same badge style.

- **Sidebar history:** “历史执行” rows drop the repeated clock icon and use the same text-only rhythm as “正在执行”: titles align with the section heading, a failed run shows a small red ⓘ-style mark at the row end and a cancelled run a grey dot. The “正在执行” and “历史执行” headings now carry a small run / history icon that wakes up on hover, and their rows hang off the heading with a thin tree rail to show the hierarchy.

- **Resource/artifact drawer:** the `+` button now opens a closable “New tab” next to the existing tabs instead of hiding the tab strip, and open tabs stay side by side; the redundant “Resources / Artifacts ×” heading row is gone on desktop (the toolbar toggles open and close the drawer).

- **Solo resources:** Solo now uses the same conversation shell and artifact stage as workflow runs, including shared drawer state, card sizing, previews and a new-tab action; resources include IDE, Terminal, browser and log cards.

- **Execution navigation:** active Solo sandboxes now appear with workflow runs in the shared “正在执行” sidebar, including shared row context actions. Solo conversations use the workflow conversation shell, toolbar, resource/artifact switcher and full-screen layout instead of a separate session list; their sandbox MCP now exposes the selected Agent’s artifact read/write capabilities.

- **Clarify chat:** parent classes, styles, accessibility attributes and native
  events now reach the chat column, resolving the Vue attribute warning in the
  review composer.

- **Conversation actions:** uses one readable purple for submissions and flow
  confirmation, neutral secondary/disabled buttons and quieter recommendation
  labels, including compact approval layouts.

- **Home background:** removes the separate gray-purple backdrop and aligns
  the home page, workflow edge fades and controls with the app theme.

- **Chat design:** unifies readable message typography, neutral user messages,
  composer actions, line icons, tool/thought states and artifact controls across
  chats. Motion is restrained and honors reduced-motion preferences; icon
  changes now update their shapes and sizes correctly during live replies.

- **Inbox conversation:** removes the redundant run/node heading and run-detail
  shortcut above the chat, including the desktop loading heading.

- **Conversation navigation:** previews only the last nonempty assistant reply
  for each question, preserving its paragraphs and Markdown text while leaving
  earlier progress messages out of the summary.

- **Notifications:** uses a clean inbox with small unread indicators, quiet
  status labels and a separate filter/action toolbar. Mobile rows show time and
  their destination, while long titles and run identifiers stay within the list.

- **Audit workspace:** separates view controls from filters, uses quiet inline
  counts and compact groups on the page surface, and removes repeated visible
  table headings. Event details and filter menus support keyboard navigation,
  with theme-aware mobile records and bounded dropdown positioning.

- **Usage statistics:** detail tables expand with their rows, leaving one main
  vertical scroll area. Wide tables scroll horizontally within their own bounds
  on narrow screens without stretching the page. Long model names retain a
  readable prefix and show their full name on hover.

- **Workflow notification selector:** replaces the native dropdown with a
  compact themed menu, a checkmark and soft accent selection. Keyboard focus,
  dismissal and viewport placement work across desktop and mobile layouts.

- **Run board:** status columns have subtle outlines and tinted headings, with
  inset task content and fine row dividers for clearer grouping in both themes.
  Long unbroken titles wrap without overlapping status or priority badges.

- **Inbox conversation:** detail views use a flat surface without an outer
  rounded border or card shadow on desktop and mobile, including initial loading.

- **Conversation navigation:** hover summaries display readable Markdown text
  across chats, preserving filenames and code symbols while removing formatting
  markers. Summaries are shortened after parsing; full replies keep their layout.

- **Chat theme:** conversations share the app's light and dark palette, with
  lavender user messages, clearer prose and compact metadata. Composer focus,
  artifact references and message navigation use the existing brand accent.
  Code blocks and wide tables scroll within their messages on narrow screens.

- **Conversation workspace:** chats use a shared reading column with question
  navigation, hover previews and stable jumps through loaded history. Artifact
  references open in a resizable right pane; narrow canvases use a returnable
  preview while preserving drafts and reading position. Version menus dismiss
  with their pane, filenames retain explicit run provenance, and public approval
  links keep their scoped APIs and reset on credential changes.

- **Application background:** all app routes use a clean, solid canvas in light
  and dark themes, including trailing-slash and case-insensitive workflow links.
  Removes the route-specific background whitelist and decorative shell dot grid.

- **Artifact library and settings tools:** artifacts use a compact file directory
  and wide preview, with searchable source filtering on demand and a list/preview
  switch on smaller canvases. Structured reports use document sections. Settings
  lists and Agent editors remove repeated card frames; narrow Agent layouts retain
  their configuration tabs and file history. Failed source changes never display
  another source's cached files, and deleting a final paged file returns to the list.

- **Browser icon:** uses the Grasp character in a transparent multi-resolution
  favicon, served through the existing ICO endpoint with an updated cache key.

- **Workflow, drafts and assistant layout:** workflows use a flat directory with
  compact notification controls and responsive rows. Requirement drafts use a
  document workspace with an on-demand planning inspector and distinct empty,
  filtered and error states. Assistant setup and conversations use an open canvas
  with a single composer and lightweight suggestions instead of large panels.
  Sidebar directories now use neutral surfaces, and narrower editors adapt to
  their actual available width; mobile chat controls stay clear of floating navigation.
  Failed history loading keeps its retry state until a deliberate retry.

- **Settings layout:** the narrow navigation rail stays beside a 248px category
  directory, with workspace tools first, then execution and platform settings.
  Centered forms use flat groups and fine separators, aligned labels and controls,
  and natural page scrolling. General settings scroll correctly; assistant,
  notification and external MCP forms share the layout without sidebar search.

- **Workspace tools:** board focuses on current runs and human attention;
  Token, model and cost analysis lives in Usage, with clearer filters and
  chart sections. Assistant setup and first-chat screens replace the empty
  split view and repeated settings buttons; paused conversations remain
  readable. Agent lists show full titles and group secondary actions under More.
  The duplicate board usage endpoint and unused charts are removed.

- **Settings navigation:** switching to board, workflows, drafts, or assistant
  keeps the settings directory open. The navigation rail and mobile drawer
  share this context; returning home or selecting a main section exits it.

- **Single workspace:** removes project selection and project management. The
  compact sidebar lists every workflow without search; workflow search stays on
  the management page. Board, drafts, assistant, and shared configuration have
  dedicated pages and Settings entries. Project APIs, `projectId` parameters
  and project MCP addresses are removed in favour of workspace endpoints
  (`/api/workspace`, `/mcp/workspace`); the PM tool is now
  `pm_list_workspace_agents` and Agent bundles use `workspace.json`.
  Requires a fresh install with an empty database and an empty data directory.

- **HTML preview toolbar:** removes the repeated file-information row and puts
  copy and download immediately before the enlarge control. Version selection
  and historical read-only controls remain available, including on narrow panels.
- **Unified chat actions:** Confirm & continue now sits inside the input panel
  alongside send and attachments, with wrapping for narrow panels. Draft text,
  attachments, annotations and pending sends hide confirmation with a smooth
  collapse and reveal. Sending shows progress inside its button, and reduced
  motion switches immediately. Unsent content remains protected from confirmation.
- **Yume motion:** the home mascot's tail stays attached throughout its wag,
  press and jump. Home and sidebar characters gain gentle breathing, double
  blinks and hover greetings; reduced-motion mode keeps both still.
- **Two-level navigation:** a compact desktop icon rail sits beside the
  workflow directory and recent runs. New request, pinning, contextual actions,
  account controls, and a single-column mobile drawer remain available.
- **Priority and chat controls:** the home priority chooser gains a compact
  lightning icon and a layered three-level panel; keyboard opening and short
  windows scroll the active option into view. Chat avatars, bubbles, input and actions
  share a softer purple palette with a thinner pane separator.
- **Resource card icons:** IDE, terminal, browser and logs share upright SVG
  windows with consistent size, strokes and neutral surfaces, replacing the
  mixed paper, dark terminal and planet illustrations.
- **Home and artifact visuals:** the home page uses a quieter deep-purple
  palette, layered workflow illustrations and subtle hover feedback while
  keeping its mascot, layout and controls. Resource and artifact cards gain
  softer surfaces and animated illustrations, with static reduced-motion states.
- **Agent wizard service address:** the OpenCode API Key step now hints the
  picked vendor's real default base URL (from the model catalog) as the service
  address placeholder, instead of a fixed `https://api.example.com/v1`. A vendor
  with no published address, or a self-hosted gateway, falls back to neutral
  copy. The field stays optional and blank by default.
- **Control-plane restarts no longer wait for agents:** on SIGTERM Grasp now
  stops scheduling, finishes in-flight HTTP requests (at most 10s) and exits,
  instead of draining agent runs for up to 10 minutes (which outlasted
  `kubectl rollout status` timeouts). Sandboxes keep running. On startup, an
  autonomous agent node whose sandbox is still alive is reattached and its
  current visit continues: a turn still running is awaited, a finished one is
  read back from the sandbox event log, and the main prompt is not resent.
  Queued runs stay queued. Other in-flight nodes (clarify, human-facing,
  non-agent) re-enter at the same node. If reattach fails, the node retries in
  a fresh sandbox as before.
- **Live request card while generating:** in the preview chat drawer, a Live
  request (for example「趣味细节」from the element card) now shows its card
  right away with a loading animation (skeleton rows and a sweep; static
  under reduced motion) instead of an empty「我」bubble. The full card with
  switch / adopt controls still appears under the agent reply once the
  candidates are ready (#813).
- **Candidate switcher clicks:** the in-page switcher no longer rebuilds its
  buttons when nothing changed, so ‹ / › and「原版」clicks are no longer lost
  on pages that keep changing (animated candidates, HMR), and switching no
  longer cuts off the candidate fade (#813).
- **Preview pick on any port:** with IP-direct preview, `set_preview(port=X)`
  now gives the reviewer's new-tab link (`IP:$PREVIEW_PORT`) the page on port X
  with pick injected, even when the app does not listen on `$PREVIEW_PORT`
  (e.g. Vite on 5173). Only the last registered port per sandbox has pick;
  if `$PREVIEW_PORT` already serves another app it keeps the entry. Sandbox
  service ports (SSH, VNC, IDE, CDP, sandbox backend) are rejected. Agent
  prompts now say not to register backends and to self-test on
  `127.0.0.1:<app port>`. Sandboxes on an older runtime bundle keep the old
  behavior until the bundle updates.
- Default `./start.sh` / `.env.example` / `compose.release.yaml` pins GHCR
  images to `*:3.0.0`.

## 2.2.0 — 2026-10-15

<img src="media/yume-mark.png" alt="Black-and-white ghost cat, the Grasp mascot" width="120" />

- **Home mascot:** the home page, the desktop sidebar, and the login page now
  use this black-and-white ghost cat (yume). White body, ink outline, grey
  cheeks, a small tail. The home hero is the cropped cat (240×200) with no
  wordmark and no logo-mark. It floats, blinks, and wags its tail; hovering
  makes the eyes follow the pointer; pressing squishes it and it springs
  back. Reduced motion keeps a still cat. Light theme drops the sparkles and
  uses a light shadow. The sidebar shows it at 28px and the login page at
  48px (#794, #798).
- **Home subtitle:** the sentence under the home mascot now shows the brand
  setting「首页副标题」(`home_subtitle`) and falls back to the locale default
  (zh「先说清，再动手」/ en「Clarify first, then act」) when it is blank. The copy
  is one uniform color and no longer highlights only its first half.
- **Run canvas handoff:** a node with post-run review (e.g. the 实现 node) no
  longer loses its handoff when the reviewer clicks「确认并流转」. The handoff
  and its source are preserved through review confirm, revise refresh, and
  clarify/react resume, and the platform fallback still writes one when a node
  with a downstream left none. The「实现 → 测试评审」edge shows the handoff chip
  again and the downstream prompt gets the upstream handoff, including after a
  test-fail loop-back (#796 follow-up).
- **App preview:** the "may be stuck" warning now appears after 60 seconds of
  connecting instead of 20, matching the advertised 30–60 second first launch.
  The "please wait" hint is hidden once the warning shows (#802).
- **Run canvas handoff:** traversed-edge handoffs are now a compact "⇄ 交接"
  icon chip instead of a truncated slice of the text. Opening it shows a card
  above the nodes with the "source → target" route, the handoff rendered as
  Markdown (headings, lists, inline code; sanitized), and a source footer.
  Platform-written handoffs are dashed with a warning icon and a "平台代写 ·
  上游未提供交接" tag. Empty handoffs show no chip.
- **Home attachments:** adding an image on the home composer at a window height
  that just fits the page no longer brings up a scrollbar. The 120px top gap
  shrinks first (down to 48px) and the column only scrolls after that (#800).
- **Codex login failures:** when Codex rejects or cannot refresh the ChatGPT
  login (CLI `401 Unauthorized` from `api.openai.com`), the Agent node now fails
  on the first failed turn with "Codex 登录已失效或无法刷新，请重新粘贴登录文件"
  instead of re-prompting, reporting "MCP 工具面为空", and auto-retrying. The
  run failure banner links to the project credentials tab.
- **CLI models:** Agent settings, the project shared Agent config, the create
  wizard, and the workflow node inspector pick a model from a dropdown
  (vendor marks, recommended cards, search). Codex also picks a reasoning
  effort. The lists are the CLI's real model IDs. A shared default applies
  only to Agents on the same CLI; an Agent that has not picked its own model
  says it follows the project model (#789, #790).
- **Sandbox lifetime:** a run sandbox stays up by lease (debug after the node
  ends, activity while the terminal, IDE, chat, or preview is in use, preview
  after `set_preview`, and a manual extension) instead of a fixed 30 minutes.
  The sandbox list and console show time left and offer +30 minutes, +2 hours,
  and keep until the run ends. The cap is **Settings → Sandbox recycle →
  Run sandbox max retention** (default 24 hours, 1–168) (#796).
- **Test node:** destroying one node's sandbox no longer revokes MCP for the
  rest of the run, and the test node follows the implement branch instead of
  falling back to `main` (#796).
- **Page Harness images:** an image opened from the Page Harness chat is a
  floating window next to the artifact window, not inside the chat card.
  Opening the artifact after the image offsets it by 36px so both title bars
  stay visible (#795).
- **Project agents:** a workflow created from the baseline binds Agents in
  this project (reuse one with the same name, or create it from the shared
  Agent config). Saving or starting a run rejects an Agent that belongs to
  another project. If the clarify node's sandbox fails to start, the run is
  marked failed. A sandbox config or credential error is not retried. The
  start-run dialog accepts an optional first message (#787).
- Default `./start.sh` / `.env.example` / `compose.release.yaml` pins GHCR
  images to `*:2.2.0`.

## 2.1.0 — 2026-10-12

- **MCP tool-list refresh on confirmation:** run-scoped HTTP MCP sessions now
  receive `notifications/tools/list_changed` over SSE when clarification
  confirmation exposes or hides `node_complete`. Reconnecting refreshes the
  current tool list; deleting a session or unregistering a run closes its streams
  (#781).
- **Run canvas:** the run page and version preview always auto-layout and ignore
  saved coordinates. Crowded forward edges stay on a smooth step; only a
  topological back edge whose target sits to the left uses the lower lane, and
  that lane drops below every card it passes. Passed and failed edges keep
  green and red (#782).
- **Project credentials:** a saved row collapses to status, edit, and clear; an
  empty row shows fill. Edit opens the input, save, and cancel on one line, and
  an empty value cannot be saved (#783).
- **App preview:** clicking the noVNC picture takes over. That first gesture,
  including right click, middle click, and touch, is not sent to the remote
  page (#784).
- **Artifacts:** the workflow artifact grid badges fixed sandbox tools apart
  from this run's artifacts, and explains the grid when there are no artifacts
  yet (#785).
- Default `./start.sh` / `.env.example` / `compose.release.yaml` pins GHCR
  images to `*:2.1.0`.

## 2.0.0 — 2026-10-09

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
