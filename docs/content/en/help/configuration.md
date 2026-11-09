---
title: Configuration
description: Configuration highlights; full details live in source CONFIGURATION.md.
---

Grasp platform service configuration is primarily YAML / environment variables (local examples: `server/config.example.yaml` and the root `.env.example`). Runtime credentials such as ACP and Git credentials are managed only in Settings → Credentials.

## Full documentation

Treat the in-repo docs as authoritative (avoids drift between this site and source):

- [server/CONFIGURATION.md](https://github.com/cocofhu/approving/blob/main/server/CONFIGURATION.md)

That document is generated/checked by `go run ./cmd/gen-configdoc`; CI runs `-check`.

## Local quick path

```bash
./start.sh -d          # published image stack
./start.sh dev -d      # source + HMR
```

Image tags / digests, gateway, and sandbox-related variables are in `.env.example`. Agent API keys, GitHub / GitLab tokens, SSH keys, and similar credentials can only be saved in Settings → Credentials; putting them in the shared Agent config, Agent meta, or platform `sandbox.env` is rejected. Save a preset or custom API base and key in Credentials, then combine a coding tool, credential and selected model in the Models tab. Agents reference that named model configuration through `modelBinding.configurationId`. Model routing fields come from the selected configuration; Agent env cannot override them. Other non-secret options may go in Agent meta env (values may reference `${vars.<name>}`).

An Agent can bind multiple saved GitHub, GitLab or SSH credentials. All selected credentials are placed in the sandbox and matched by site or repository; equally specific scopes use the earlier selected credential. Optional scope settings distinguish accounts on the same platform. Credential selection is separate from the repositories to clone. A repository record keeps only its name, URL, branch and directory; it no longer stores an access method, credential bindings or environment variables. Workflow nodes use their selected Agent's bindings, and shared sandboxes clear the previous node's credentials before applying the next set. An empty list injects none of these default Git credentials. Upgrade removes historical repository and Solo snapshot permissions and environment variables without copying them onto any Agent; configure them again on the current Agent when a private repository or variable is still needed. Unavailable references stop execution without switching accounts.

GitHub and GitLab credentials configure Host and Token together. Host defaults to the official service and supports self-hosted sites and ports; there is no separate GitLab URL credential. Agent defaults can select multiple model groups and Git authorizations. Each Agent independently opts into model or Git inheritance; turning a switch off uses its local selection, and unavailable defaults never fall back to another account or model.

### Sign in with GitHub (device code)

When configured, adding a github.com GitHub credential offers "Sign in with GitHub": the page shows an 8-character code, the user enters it at `https://github.com/login/device` and authorizes, and the platform saves the token. No personal access token has to be created. The result is a regular GitHub credential (`GITHUB_TOKEN`, Host `https://github.com`) that binds and injects like a pasted token. The credential row shows the GitHub account and offers "Sign in again" to replace the token while keeping the credential ID, alias and existing bindings. Without configuration only token pasting is shown.

To enable it:

1. In GitHub, open **Settings → Developer settings → OAuth Apps** and create an OAuth App (personal or organization). Use the platform URL as Homepage URL; the device flow does not use the callback, so the Authorization callback URL can be the same address.
2. Check **Enable Device Flow** in the App settings. Without it, sign-in reports that Device Flow is disabled.
3. Give the App's **Client ID** to the server through the `GRASP_GITHUB_OAUTH_CLIENT_ID` environment variable or YAML `github.oauth_client_id` (the environment variable wins), then restart. No client secret is needed; do not configure one.

Notes and limits:

- The requested scopes are fixed to `repo read:org workflow`. The token has `repo` access to every repository the user can reach; for per-repository permissions, paste a fine-grained PAT instead. If the grant lacks `repo`, the credential is still saved with a warning that private repositories may not clone.
- Organizations with OAuth App access restrictions must approve the OAuth App, otherwise the token cannot reach their repositories.
- Only github.com is supported. GitHub Enterprise Server keeps using pasted tokens.
- Sign-in sessions live in server memory for the lifetime GitHub returns (usually 15 minutes). A restart drops sessions in progress; get a new code. Multi-instance deployments need sticky sessions.
- GitHub revokes OAuth App tokens unused for a year; use "Sign in again" on the credential row.

### Multiple model groups and conversation continuity

Bind several groups, including groups for different coding tools, and choose an explicit default. Home, new Solo sessions and active Solo conversations offer primary and auxiliary models from every bound group. Identical models from different groups retain their group and tool labels. An unspecified choice and automated workflow execution use the default group's primary model.

A session choice selects the group's tool, account and reasoning settings together. An active turn finishes with its original settings; the new choice applies before the next turn. Tool changes retain the current workspace and uncommitted changes, and hand early requirements, recent dialogue, attachment references and tool results to the new tool. Agents can retrieve older original messages through artifact-store `get_history`. Native sessions are not interchangeable between tools; continuity uses persisted task records and the current workspace.

Successful group or binding saves refresh selectors in the current page and other tabs in the same browser. Other users see changes after refreshing. Valid selections stay selected; disabled, deleted or unbound choices require an explicit replacement. A temporary read failure is not treated as deletion. Different dedicated sandbox images or changed Git authorization require reopening the session.

### Per-reply time and execution details

Solo, workflows, Agent tests and PM share the same chat presentation. Hover a message to see its time. The reply information icon supports hover, keyboard focus and touch, showing the turn's Agent, coding tool, model, reasoning, duration and token usage. Input, output, caches and individual models remain separate, with upstream or estimated attribution preserved.

Each reply keeps its execution snapshot; later model changes do not relabel old replies. Missing historical fields remain unavailable rather than being inferred from the current selection. An unfinished reply shows only information already received.

## Local object storage and COS / S3

The Solo-continuation migration release and source stack can store message bodies, attachments and workspace archives in private object storage. The published 3.2.0 images currently shown in `.env.example` predate this capability; use the source stack to evaluate this branch and the migration image when upgrading.

Without COS, add this to the root `.env`:

```dotenv
GRASP_STORAGE_DRIVER=s3
GRASP_S3_BUCKET=grasp
GRASP_S3_PREFIX=application
```

Leave `GRASP_S3_ENDPOINT`, access credentials and session token empty, then run `./start.sh dev -d` (source) or `./start.sh -d` (migration image). The script starts SeaweedFS, creates a private bucket and generates an application identity scoped to that bucket before starting Grasp. S3 binds only to `http://127.0.0.1:8333`; change its port with `GRASP_LOCAL_OBJECTSTORE_PORT`. Admin, Filer and Master ports are not published to the host.

Data and credentials live in `.localdata/objectstore`, or `.devdata/objectstore` for the source stack. `client.env` and `iam.json` have mode 600. Stops, restarts and container recreation retain the same data and identity. Include the entire directory in backups; do not delete or regenerate credentials separately. Changing an existing bucket requires a storage migration.

For an existing COS / S3 service, configure `GRASP_S3_ENDPOINT`, bucket, region, prefix and credentials. Use `GRASP_STORAGE_DRIVER=cos` for COS and `s3` for other S3 services; `GRASP_S3_PATH_STYLE` controls addressing. An external endpoint disables bundled storage, and an outage never switches to an empty local store. Credentials without an endpoint are rejected.

After a storage location is registered, endpoint, bucket or prefix changes require an offline copy, verification and explicit switch. Retain legacy local files until archival and integrity checks complete; changing environment variables alone does not migrate data. Use `./start.sh logs objectstore` for component logs and `./start.sh down` to stop it while retaining data.

## Database and attachment lifecycle

In the release stack, SQLite (`./.localdata/db`) and app-data / default blobs (`./.localdata/app-data`, or a custom `GRASP_BLOBS_ROOT`) must be **backed up and cleaned as a pair**; do not migrate only the database. Otherwise composite images can keep orphan `blob:` refs (GET `/api/blobs/:id` → 404). Historical orphans only get a permanent UI placeholder; this delivery does not add an inspection console. See [Quick start](../guide/quick-start.md#database-and-attachments-share-one-lifecycle-backup--cleanup).

With object storage, the backup set must also include the corresponding bucket / prefix and bundled storage credentials. The database, retained legacy files and objects must form one reference-verified backup set; backing up only SeaweedFS or only SQLite is incomplete.

## Related

- [Gateway](../gateway/)
- [Contributing](../contributing/)
