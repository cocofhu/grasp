---
title: Work in Solo
navTitle: Solo sessions
description: Open a session, switch models, inspect a preview, and resume a stopped sandbox.
section: use
order: 10
keywords: [Solo, chat, terminal, IDE, preview, switch model, restore, attachments]
---

## Create a session {#create}

Choose Solo from the home page. Pick an agent, a model, and any repositories you need, then send your first message. A conversation can start without a repository. For a new installation, [create a model group](/en/guide/first-run/#model) first.

A session uses a persistent sandbox workspace. Use the run information and tool activity beside a reply to see what the agent actually used. Open the terminal, browser IDE, or preview from the session to inspect the changes. An app preview requires a running service and a registered preview in the sandbox. The deployment controls how it opens.

## Switch models and tools {#switch-model}

The picker lists the agent's bound model groups and their main and auxiliary models. The main model in the default group is selected initially. The chosen group determines the tool, credential, and reasoning configuration for the turn.

A turn in progress finishes with its existing choice. Your new selection applies on the next turn. When changing tools, Grasp keeps the working directory and uncommitted changes and passes its saved conversation context to the new tool. The agent can read earlier history using Grasp tools. Some environment or credential changes prompt you to reopen the session.

## Open a terminal, preview, or IDE {#tools}

- Use the **terminal** to run commands and inspect sandbox files and processes.
- Use the **preview** after the agent registers an app. Proxy and subdomain availability depend on [gateway and preview settings](/en/help/gateway/#preview).
- Use a **local IDE** after an admin enables the SSH gateway and you add a public key. Follow [Connect a local IDE](/en/help/local-ide/).
- Drag or paste **attachments** into the composer. Each file can be up to 50 MiB. Workflow chat and agent tests use the same attachment controls.

## Stop, restore, or close {#lifecycle}

Open the sandbox info in the session header to see the environment and its state. Choose **Stop** to save the environment and free its slot, then **Resume** when you are ready. File archives and E2B snapshots preserve different state. See [Sandbox lifecycle](/en/help/sandbox-lifecycle/).

Closing a session cleans up its environment and restore points. Commit or export code you want to keep first. When capacity is full, check which sandboxes are stopped and which still occupy slots before restoring a session.
