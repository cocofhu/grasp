---
title: Configure credentials, model groups, and agents
navTitle: Model groups and agents
description: Understand how accounts, tools, model groups, and agents fit together and verify the model a turn used.
section: use
order: 30
keywords: [model group, inheritance, credential, API Base URL, API key, Codex, primary, auxiliary, agent]
---

## Add an account {#credentials}

Open Settings → Credentials and add a tool authorization or API account. Some tools use their own login. Tools with a compatible protocol can also use a model service. The form shows the options for the tool you choose.

| Field | What to enter | Effect |
| --- | --- | --- |
| API account name | A name that identifies the service or environment | Identifies the account in pickers. |
| API key or token | A credential issued by the provider | Authenticates upstream requests. |
| API Base URL | The provider's API base | Sets the destination for model requests. |
| Protocol and authentication | The provider's supported protocol and way of sending a key | Changes tool compatibility and request format. Enter a custom header only when the provider requires it. |
| Model ID | The provider's complete model identifier | Selects the model to call. Retry catalog loading or enter the ID manually. |

Save secrets in the credentials form. Agent profiles keep references to credentials or groups. When an authorization expires, update the existing account so its valid bindings can continue to use it.

## Save a model group {#model-groups}

Open Settings → Credentials → Model groups. A model group is a reusable combination of **coding tool, account, models, and reasoning settings**.

| Field | Effect |
| --- | --- |
| Model group name | Identifies the group in pickers and run information. Models with the same name can be distinguished by group and tool. |
| Coding tool | Determines which agent CLI starts in the sandbox. Create another group when changing tools. |
| Model source | Chooses a tool authorization or a compatible API account and the connection used for calls. |
| Main model | The model used by default. Supported tools can select their own default. |
| Auxiliary models | Configure roles exposed by the tool or make additional models available. Roles depend on the coding tool. |
| Reasoning options | Set supported parameters for the tool and model. The options follow the selected group. |

## Bind the group to an agent {#binding}

Open Settings → Agents, then the agent's model settings. The shared defaults are pinned at the top of the agent list.

- **Inherit defaults** to use the default configuration's groups and default group.
- **Choose groups** to bind a set of groups or all active groups. Select a default when several groups are available. Automated workflow runs use the main model in the default group.
- **Disable or delete a group** to make its references unavailable. Pick an available group on the affected agent before running again.

After a group is saved, other tabs in the same browser refresh their choices. A selection made in a session applies to the next turn. To verify a turn, open the run information beside the reply for its tool, model, reasoning effort, and usage.
