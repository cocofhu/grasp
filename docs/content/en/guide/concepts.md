---
title: Core concepts
description: FSM orchestration, human gates, sandboxed execution, and artifact contracts.
---

## FSM orchestration

Grasp turns coding agents into steps in a workflow. You orchestrate on a finite state machine:

- **Nodes** are states (agent / react / gate / …)
- **Edges** are transitions, with configurable success, failure, and rollback paths
- Use `when` guards and checkpoints to make risky steps explicit

This is not a one-shot, irreversible agent run: design the path first, then gate the critical steps.

## Human gates

When a step needs a human decision, the run stops at a **gate** until someone approves or rejects.

Approval moments are first-class — not an afterthought. Grasp bets that agents can be fast while people still own the critical decisions.

### Temporary approval links (human_gate)

In the pending-gates inbox, only **human_gate** cards (and the visual preview toolbar when a page artifact exists) offer **Copy temp link**. A signed-in operator can mint a one-shot URL so an unauthenticated person can approve or reject once.

- Default TTL is 24 hours (1h / 8h / 24h / 72h / 7d). At most one active link per instance.
- When minting, operators pick a permission preset: **Full access** (default — hot session reply / cancel / confirm+reject) or **ReAct chat only** (reply + cancel; every public confirm/reject decide is denied). The preset is stored on the link row and enforced on Preview.actions and public decide/reply/cancel together — hiding buttons alone is not enough. Legacy rows with an empty preset are treated as full access.
- The management panel masks the URL by default; Copy writes the full fragment URL. Refreshing the same browser tab still lets you copy the same active URL. **Regen (inherit preset)** immediately revokes the old link and reuses the same TTL tier and permission preset; changing permission requires creating a new link (which revokes the prior active one). Revoke now disables the link. While the gate is still pending, revoked/expired links can be replaced; after the gate is decided the entry is read-only. `proposal_select` and pending clarify have no share entry (app preview uses the review-share path below).
- The external page needs no login. It shows the title, description, redacted artifacts, a preset chip, and actions allowed by the preset. It does not expose project, run, members, or internal URLs. A cold **ReAct-only** link shows a dead-end message and never falls back to decide.
- The token is bound to that one approval. Expiry, revoke, a **successful** decide, a login-side decision, or run completion invalidate unused links immediately. Denied decide calls on ReAct-only links do not mark the link used.
- **Each visitor has their own conversation.** When one link is shared with several people, every browser (identified by an anonymous visitor id kept in local storage, so refreshes and new tabs keep it) gets its own dialogue and its own Agent context; visitors never see each other's messages. Before speaking, a visitor sees the node's dialogue so far. The Agent runs each visitor in a separate sandbox chat, but the **workspace and artifacts stay shared**: product edits one visitor asks for are visible to the others and in-product. Up to 6 visitors per link can talk at once; past that the page asks them to try again later. A visitor idle for 30 minutes has their sandbox chat closed, and their next message resumes with the existing conversation.
- **First decision wins.** Once any visitor confirms or rejects successfully, the link stops working for everyone and all visitor chats end.

### Temporary review links (Inbox kind=review / app_preview)

Inbox **pending review** and **app preview** cards reuse the same management panel and token rules (`ShareLinkKindReview`, including TTL and permission presets), but authenticated APIs live under `/api/runs/:id/reviews/:nodeId/share-link*` — not `/gates/...`, and no fake Gate row is created. In-product entries: card **Copy temp link** and the mobile detail top bar button with the same label. The public page is labeled **External review**; hot sessions support multi-turn ReAct. For `productKind=app_preview` the public page defaults to remote desktop and picking via a short-lived ticket channel (desensitized ports; API ports use a same-origin iframe); mobile shows a degrade hint only. The only footer action is **Confirm and advance**. Per-visitor conversations, the shared workspace, and first-decision-wins apply as above. Run-detail review tabs and the logged-in review composer do not add a temp-link entry; `proposal_select` and pending clarify stay out of scope.

### App preview: noVNC and direct IP

Sandbox ports registered with `set_preview` always preview over noVNC in the panel, which shows `http://127.0.0.1:port/` inside the sandbox. The preview page in the sandbox stays open: after you leave and come back, switch tabs, or reload the platform page, you see the same page with its login and form state; logins also survive a Chromium restart. A sandbox shows one screen at a time, and switching ports navigates that same page.

The preview is watch-only by default. Select Take over in the toolbar to use mouse and keyboard, and Return control when you are done; Pick annotation allows clicks while it is armed. This only prevents stray clicks; it is not access control.

When the node has direct IP preview (`direct_preview`) on, the noVNC toolbar adds an Open directly in new tab button for `http://IP:port/`, which carries the pick bar and the Chat drawer at the bottom right.

### Live variants (Grasp / app preview + direct IP)

**Grasp** (`grasp`, historical `approve`) and **app preview** (`app_preview`) nodes with direct IP preview (`direct_preview`) get **Live variants** by default (`live_variants`). After the agent registers an in-sandbox app port with `set_preview`, open its direct preview and click the **Page collaboration** icon at the bottom of the chat input, next to the attachment button. Choose **Page candidates** from the menu. Existing direct-preview nodes that omit the node setting have this capability without migration. Explicitly disabled Live, noVNC, external URL-only previews and links without Live permission do not provide candidate generation.

**Page collaboration** is collapsed by default. Its icon shares the attachment toolbar; hover to see its full name. Click the icon to expand the menu and set **Page control** and **Page candidates** independently. You can collapse the menu when finished; both switch settings are retained. The icon shows how many features are enabled; hover or open the menu to see which ones.

**Page candidates** in **Page collaboration** is off by default. Turn it on, describe the change and send: the agent renders **3 candidates** on the current page and waits for you to compare and choose. You do not need to pick an element or open the page toolbar first. Images and picked annotations travel with your request. Turning the switch off returns to ordinary chat and keeps existing candidates; use **Discard** to undo them. The node setting controls availability, while the chat switch chooses candidate generation for your request.

**Let the agent operate this page**, in the same menu, is also off by default and separately controls whether the agent can read, click and fill in the preview page you are viewing. The switches are independent; generating candidates does not require page control. While page control is on, page content the agent reads appears in this conversation and other participants can see it, so use a test account. The menu retains the privacy reminder and shows whether page control is connected, paused, disconnected or assigned to another tab.

To use Live:

1. Click the **Page collaboration** icon at the bottom of the chat input, next to the attachment button, and turn on **Page candidates** in the menu. Describe the change and send; you can collapse the menu before typing. For example: "Make this setup dialog clearer while keeping its purple style." The agent uses the request, attachments, annotations and current page route to locate the relevant components.
2. The agent writes the variants **straight into the sandbox source**, wrapped in temporary `data-grasp-live` / `data-grasp-variant` markers, and the dev server's hot reload shows them on the page you are looking at.
3. **In place**: a `‹ 2/3 ›  ✕  Accept` switcher sits under the element; use ← → and the parameter knobs. Candidate switches fade in unless reduced motion is enabled. **Side by side**: the original and every candidate are laid out at once; "Use this" accepts one, "Keep original" discards.
4. **Accept** keeps only the chosen variant in source and removes every marker; **Discard** restores the original. With candidates open, continue refining the current candidate in chat or say "Use this one" to accept it. If a set is still generating, recovering or not displayed, wait or resolve that session before starting another set.

The page toolbar's **Pick** opens an action card on the element you click: **Quote** hands it to the agent as an annotation; **Edit** expands the design form, where you choose a design action (bolder / quieter / polish / typeset / colorize / layout / distill / adapt / animate / delight / overdrive), optionally open **Marks** to add notes, draw or pin notes, choose 2–4 variants and press **Generate**. With Live on, turning on **Pick** also shows a bar above the toolbar: switch between **Select** (pick an element) and **Insert block** (pick an anchor to insert a new block before or after it), or type into **Adjust the whole page…** to change the whole page in one sentence; 👁 appears while the page has candidates — hold it to see the original, click it to hide the candidate controls. None of these are required to generate candidates from chat. The toolbar and these panels follow the language set in Grasp, including a switch made after the preview is open.

Each Live action shows up in the chat drawer as a card you can drive (switch, compare, accept, discard). While a set is open, "make 2's title bigger" edits variant 2 only. Reloads, new tabs and route changes keep the state: markers live in source, session state on the server, and the variant you are viewing in this tab. Confirm & continue is blocked until every set is accepted or discarded (the drawer offers "Discard all"), and a local pre-commit hook in the sandbox rejects commits that still contain markers.

The page and chat card share the current candidate. "Use this one" keeps the version and tuned parameters you were viewing when you sent the message; switch candidates first to accept another number. If a whole-page adjustment fails or is interrupted, retry the same request or dismiss it. Dismissal preserves edits already made and does not roll back the page. "Discard all" also dismisses failed whole-page requests; review the retained result before confirming. If adoption is interrupted, "Retry accept" resumes the saved candidate and parameter values. Confirmation pauses when the workspace cannot be scanned and can be retried after access is restored.

## Real Docker sandboxes

Agents are not black-box prompts on a laptop. They execute in Docker containers through the in-repo [sandbox-gateway](https://github.com/cocofhu/approving/tree/main/sandbox-gateway), talking over ACP.

Supported backends: **Cursor**, **Claude Code**, **CodeBuddy**, **Trae**, and **OpenCode**. Configure `acpBackend` per agent; save backend keys, sites, and other runtime credentials in the project's credential UI first. Compatible project or Agent meta environment variables are a fallback only when no project credential is configured (OpenCode also takes vendor, optional API Base, and model).

## Artifact contract and MCP

Each run has an isolated artifact MCP. Agents call tools such as:

- `write_artifact`
- `set_*`
- `node_complete`

Isolation is by run token, leaving an inspectable paper trail.

## PM: `pm-agent-fs` (org + Agent workspace)

A project-bound PM Leader can enable the dedicated MCP `pm-agent-fs` (on by default for new projects; older projects with an explicit EnabledMcps list must opt in under PM settings):

- `pm_get_org`: read virtual groups and flat same-project members (self / direct / other relative to the PM)
- `pm_fs_*`: list/read/write/delete/mkdir/rename the **host-side** `workspace/` of any same-project agent (**not** Run sandbox FS)

Writes land on the same disk tree as Agent Studio「Agent workspace」and are visible after **refresh or reopen** (no live hot-reload). If Studio still has an unsaved dirty draft for the same Agent, a later Save may overwrite MCP writes — refresh and avoid concurrent dirty edits during demos.

## Single-repo self-hosting

`sandbox-gateway` and the generic sandbox image sources live in this repository. One clone is enough to self-host.
