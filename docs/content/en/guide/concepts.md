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

### Temporary review links (Inbox kind=review / app_preview)

Inbox **pending review** and **app preview** cards reuse the same management panel and token rules (`ShareLinkKindReview`, including TTL and permission presets), but authenticated APIs live under `/api/runs/:id/reviews/:nodeId/share-link*` — not `/gates/...`, and no fake Gate row is created. In-product entries: card **Copy temp link** and the mobile detail top bar button with the same label. The public page is labeled **External review**; hot sessions support multi-turn ReAct. For `productKind=app_preview` the public page defaults to remote desktop and picking via a short-lived ticket channel (desensitized ports; API ports use a same-origin iframe); mobile shows a degrade hint only. The only footer action is **Confirm and advance**. Run-detail review tabs and the logged-in review composer do not add a temp-link entry; `proposal_select` and pending clarify stay out of scope.

### Live variants (Grasp / app preview + direct IP)

**Grasp** (`grasp`, historical `approve`) and **app preview** (`app_preview`) nodes with direct IP preview (`direct_preview`) get **Live variants** by default (`live_variants`). After the agent registers an in-sandbox app port with `set_preview`, open its direct preview to find "Live" on the bottom bar. Existing direct-preview nodes that omit this switch default to enabled without migration. Explicitly disabled Live, noVNC and external URL-only previews do not provide this entry.

The page’s **Live · Off / On** button toggles the Live controls immediately. Closing them keeps existing candidates; use **Discard** to undo candidates. The node setting controls whether this entry is available.

To use Live:

1. **Pick an element**, choose a design action (bolder / quieter / polish / typeset / colorize / layout / distill / adapt / animate / delight / overdrive) or describe the change. Draw on the element or pin a comment, with undo and clear controls; positions, circled controls and notes reach the agent together. Choose 2–4 variants and a display mode, then press **Go**.
2. The agent writes the variants **straight into the sandbox source**, wrapped in temporary `data-grasp-live` / `data-grasp-variant` markers, and the dev server's hot reload shows them on the page you are looking at.
3. **In place**: a `‹ 2/3 ›  ✕  Accept` switcher sits under the element; use ← → and the parameter knobs. Candidate switches fade in unless reduced motion is enabled. **Side by side**: the original and every candidate are laid out at once; "Use this" accepts one, "Keep original" discards.
4. **Accept** keeps only the chosen variant in source and removes every marker; **Discard** restores the original. `+` inserts a new block before or after an anchor; the Steer box tweaks the whole page without picking.

Each Live action shows up in the chat drawer as a card you can drive (switch, compare, accept, discard). While a set is open, "make 2's title bigger" edits variant 2 only. Reloads, new tabs and route changes keep the state: markers live in source, session state on the server, and the variant you are viewing in this tab. Confirm & continue is blocked until every set is accepted or discarded (the drawer offers "Discard all"), and a local pre-commit hook in the sandbox rejects commits that still contain markers.

The page and chat card share the current candidate. "Use this one" keeps the version and tuned parameters you were viewing when you sent the message; switch candidates first to accept another number. If a whole-page adjustment fails or is interrupted, retry the same request or dismiss it. Dismissal preserves edits already made and does not roll back the page. "Discard all" also dismisses failed whole-page requests; review the retained result before confirming. If adoption is interrupted, "Retry accept" resumes the saved candidate and parameter values. Confirmation pauses when the workspace cannot be scanned and can be retried after access is restored.

## Real Docker sandboxes

Agents are not black-box prompts on a laptop. They execute in Docker containers through the in-repo [sandbox-gateway](https://github.com/cocofhu/approving/tree/main/sandbox-gateway), talking over ACP.

Supported backends: **Cursor**, **Claude Code**, **CodeBuddy**, **Trae**, and **OpenCode**. Configure `acpBackend` per agent; keep secrets in agent meta env (OpenCode also takes vendor, optional API Base, and model).

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
