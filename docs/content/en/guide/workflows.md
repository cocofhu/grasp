---
title: Build, run, and review a workflow
navTitle: Workflows
description: Connect agent nodes and human gates on the canvas, publish a version, and review its runs.
section: use
order: 20
keywords: [workflow, canvas, node, publish, gate, approval, artifact, run]
---

## Define delivery steps on the canvas {#canvas}

Open an existing workflow or create one for your team's delivery steps. Use input nodes for requirements and run parameters, agent nodes for roles and objectives, conditions for test outcomes, and output nodes for deliverables.

Add a human gate at a decision that needs review. Decide what reviewers will inspect and what they can approve, then reference upstream inputs or artifacts in node instructions. Workflow variables can be used in supported nodes. Save your changes, validate the canvas, and publish the version you want your team to run.

## Start a run {#run}

Choose **Run**, enter the request, and check which repositories and branches to clone. The selection, order, and branches are fixed for the run at launch. To add a repository, register and bind it in [Repository settings](/en/help/repositories/).

Open run details to inspect each node and its inputs and artifacts. A run can queue while waiting for sandbox capacity. If a node reaches its total time limit, is cancelled, or is taken over, the run shows that reason.

## Review a human gate {#review}

Open the pending item and inspect its conversation, preview, and artifacts. Use the actions available to submit a decision or continue the conversation so the run can advance or return for changes. Check the recipient and expiry before sharing a temporary approval link.

A run remains in the waiting state until someone responds. An eligible E2B sandbox can save and reclaim its resources while the run waits, then resume on response. Docker and Kubernetes environments remain until an explicit stop or cleanup. See [Sandbox lifecycle](/en/help/sandbox-lifecycle/#active-work).

## Verify the outputs {#artifacts}

Once the run finishes, inspect its output node and artifacts. Check that the test report belongs to this run, then verify the final changes and branch. To change how subsequent runs work, edit, save, and publish a new workflow version.
