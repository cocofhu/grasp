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

## Artifacts and tools {#artifacts}

Artifacts are task data read and written by workflow nodes, such as requirements, plans, test reports, and attachments. Downstream steps can reference upstream artifacts. Grasp provides applicable workflow, conversation, and artifact operations through built-in agent tools. Open Settings → Integrations to browse the catalog.
