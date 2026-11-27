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

## Artifacts and tools {#artifacts}

Artifacts are task data read and written by workflow nodes, such as requirements, plans, test reports, and attachments. Downstream steps can reference upstream artifacts. Grasp provides applicable workflow, conversation, and artifact operations through built-in agent tools. Open Settings → Integrations to browse the catalog.
