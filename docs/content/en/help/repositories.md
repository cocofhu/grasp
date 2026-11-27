---
title: Repositories and Git access
description: Register repositories, configure private clone and push access, and verify branches and commit identity.
section: use
order: 40
keywords: [Git, GitHub, GitLab, SSH, token, PAT, clone, repository, OAuth, device flow]
---

## Register a repository {#register}

1. Save a GitHub or GitLab token or an SSH key in Settings → Credentials. For GitHub and GitLab, choose the official host or a self-hosted domain. For SSH, configure the trusted host too.
2. Add a record in Settings → Repositories. Enter the clone URL and select a credential for its protocol and host. A public repository can be registered without a credential.
3. Enter the default branch and commit identity you need. Bind the repository in workflow settings or choose it when creating a Solo session.

| Repository field | Effect |
| --- | --- |
| Name | Labels the repository in workflow and Solo pickers. |
| URL | Determines the clone destination and which credential hosts and protocols are allowed. |
| Default branch | Sets the initial branch choice at launch. You can choose another branch before starting a run. |
| Credential | Determines the account used for access. Revoking or deleting it can block new starts. |
| Commit name and email | Set commit attribution within the repository. |

## What happens at runtime {#at-runtime}

Launching a workflow fixes the selected repositories, their order, URLs, and branches. When a sandbox starts or restores, it uses the current credential and commit identity for each registered repository. Agent model settings select the coding tool and model. Repository records select Git access.

To check the branch and author in a sandbox terminal, run:

```bash
git status --short --branch
git config --get user.name
git config --get user.email
```

For a failed clone, check the clone URL, credential host and protocol, and repository permissions. After updating a credential, follow the session prompt to reopen or retry initialization.

## Sign in with GitHub {#github-login}

An admin can enable **Device Flow** on their GitHub OAuth App, provide its Client ID through `github.oauth_client_id` or `GRASP_GITHUB_OAUTH_CLIENT_ID`, and restart Grasp. See [Deployment configuration](/en/help/configuration/#secrets).

When adding a github.com credential, choose **Sign in with GitHub** and enter the displayed code on GitHub. The resulting credential is available in the repository picker. Use **Sign in again** on the credential to refresh the token and keep existing references. For self-hosted GitHub, paste a token. Organizations that restrict OAuth apps may need to approve yours. The scopes are `repo`, `read:org`, and `workflow`. Use a fine-grained token when you want to scope access to a specific repository.
