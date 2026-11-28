---
title: Complete your first run
navTitle: First run
description: Add credentials, create a model group, and run Solo or the default workflow.
section: start
order: 20
keywords: [onboarding, setup, model group, API account, run]
---

Start Grasp with [Quick start](/en/guide/quick-start/). Have an account for a supported coding tool or a model service with a compatible API.

## Set up a credential and model group {#model}

1. In the setup prompt, choose **Coding tool authorization** or **Model API**. You can also add an account under Settings → Credentials.
2. For a model API, save the key and endpoint with the provider's protocol. For tool authorization, sign in or save the credential requested by that tool.
3. Create a **model group** with a coding tool, the saved credential or API account, and a main model. Use the provider's exact model ID, including any slash.
4. Select the group for the agents created during setup. You can choose groups per role. One model you can call successfully is enough to check the connection.

## Prepare a repository {#repository}

Setup can register one Git repository. For a public repository, enter its URL. For a private repository, save a token or SSH key, select it on the repository record, and check the host, protocol, and commit identity. You can skip the repository for a conversation-only test. See [Repositories and Git access](/en/help/repositories/).

Setup creates the selected role agents, a Solo agent, and a published default workflow. Start a task to verify them.

## Verify Solo {#solo}

On the home page, choose **Solo** and an agent that is bound to your model group. Send a short request such as “List the files you can access in this directory and tell me whether a repository was cloned.”

When you get a reply, open its run information to check the tool and model. If you selected a repository, use `git status` in the terminal to confirm the directory and branch. Continue with [Solo sessions](/en/guide/solo/).

## Verify a workflow {#workflow}

Open Workflows → Default workflow and choose **Run**. Enter a request and confirm the repositories and branches. Use a small task with a clear result, such as documenting a README step and checking its links. Review any clarification, review item, or human gate in the inbox. Open the run detail to inspect the outputs and result. Continue with [Run and review workflows](/en/guide/workflows/).

For an unavailable model, failed clone, or a sandbox that stays in preparation, check [Troubleshooting](/en/help/troubleshooting/) at the stage that failed.
