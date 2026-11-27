---
title: Grasp core concepts
navTitle: Core concepts
description: Understand agents, model groups, runs, human gates, sandboxes, and artifacts.
section: start
order: 30
keywords: [FSM, agent, Solo, gate, sandbox, artifact, MCP]
---

## Agents and model groups {#agents}

An agent defines the instructions, capabilities, and tools for a task. A model group selects the coding tool, authorization, and model that execute it. Agents can bind several groups or inherit defaults. Repository records select independent Git access. See [Model groups and agents](/en/help/agents-models/).

## Workflows, runs, and human gates {#workflows}

A workflow is a graph of steps and transitions. You can start a published version. Each run records its inputs, active nodes, conversations, and artifacts. A human gate pauses the run for a decision, then follows the applicable transition. The run page, inbox, and artifacts help you follow that work.

## Solo and sandboxes {#sandboxes}

Solo is a task session where you can continue a conversation. Both Solo and workflows execute in sandboxes managed by the gateway. A sandbox provides a workspace, terminal, and optional IDE and app preview. The gateway selects an image or template. Grasp requests environments for sessions and runs as needed. See [Solo sessions](/en/guide/solo/) and [Sandbox lifecycle](/en/help/sandbox-lifecycle/).

## Review workspace changes {#workspace-changes}

Open **Changes** in the toolbar of a Solo session or workflow run to see the branch and line counts for each repository. Select a repository to open its file diffs. The cumulative view compares against the initial checkout and includes changes after the agent commits them. The last archived view remains available after the workflow releases its sandbox. When multiple workspaces are available, use **Workspace** next to the file counts to switch which files and line totals are shown.

Open **Turn changes** above the composer to see what changed between the start and end of the last turn. Diffs show syntax highlighting and line markers for recognized code files. The viewer offers read-only source and rendered Markdown. For deleted files, it shows the version before deletion.

## @ references in Solo {#references}

Type `@` to search in the Solo composer, including when starting a session from the home page. You can also choose a reference type from the **+** menu.

- **Your other Solo sessions**, searched by title; closed sessions work too. The agent first sees a summary of the user's requests in that session and reads the full conversation page by page with the grasp-tool `read_reference` tool. It sees the session as it was when the turn began; later messages there are not included.
- **GitHub issues and pull requests** in the repositories mounted in the current session, or the repositories picked on the home page when starting one. You can also type `#number` or paste an issue or PR link. Grasp checks access with the same Git credential the sandbox uses, and the agent reads the latest text, comments and diff with `gh` inside the sandbox.
- **GitHub discussions** can be searched by keyword or number using a repository credential with read access to discussions. You can also paste a discussion link directly. The agent can read the body, accepted answer, and visible replies from a public page while signed out. For private discussions, each read uses the target repository’s credential and follows separate pages for comments and their replies.
- **Workspace artifacts** can be searched by filename. Sending a message pins the file version. Opening the file from the message or using `read_reference` to read a text excerpt or download the original file retrieves that version.

Use `↑` `↓` to choose, `Enter` to confirm, `Tab` to switch types, and `Esc` to close. On a phone, tap **Discussion** or **Artifacts** and keep typing to narrow the results. `Enter` with nothing highlighted sends the message as typed. A message can carry up to 5 references. The selected references appear above the composer. Delete the `@title` text or click × to remove one. The agent uses referenced content only as source material.

Workflows support the same references:

- In the **home page Workflow composer**, pick a workflow, then type `@` or use the **+** menu. GitHub search uses the repositories bound to the workflow. You can search sessions and artifacts with or without a repository. A message with only references can start a run, which is then titled after the first reference. Switching workflows or back to Solo clears the picked references and keeps the text.
- In **clarification and review chats and gate hot revisions**, GitHub search uses the repositories the run cloned. The current node's agent uses `read_reference` for sessions and artifacts. It reads GitHub references in the sandbox and selects the target repository’s credential again when reopening a discussion. References are unavailable when the node is finished, inactive or its composer is locked, and the anonymous shared approval page does not offer them.

## Artifacts and tools {#artifacts}

Artifacts are task data read and written by workflow nodes, such as requirements, plans, test reports, and attachments. Downstream steps can reference upstream artifacts. Grasp provides applicable workflow, conversation, and artifact operations through built-in agent tools. Open Settings → Integrations to browse the catalog.
