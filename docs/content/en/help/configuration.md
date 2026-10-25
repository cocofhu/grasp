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

An Agent can bind multiple saved GitHub, GitLab or SSH credentials. All selected credentials are placed in the sandbox and matched by site or repository; equally specific scopes use the earlier selected credential. Optional scope settings distinguish accounts on the same platform. Credential selection is separate from the repositories to clone. Workflow nodes use their selected Agent's bindings, and shared sandboxes clear the previous node's credentials before applying the next set. An empty list injects none of these default Git credentials. Separately configured repository permissions remain independent; new Agents do not automatically inherit shared repository grants. Unavailable references stop execution without switching accounts.

GitHub and GitLab credentials configure Host and Token together. Host defaults to the official service and supports self-hosted sites and ports; there is no separate GitLab URL credential. Agent defaults can select a named model and multiple Git authorizations. Each Agent independently opts into model or Git inheritance; turning a switch off uses its local selection, and unavailable defaults never fall back to another account or model. Selecting a model also determines its CLI.

## Database and attachment lifecycle

In the release stack, SQLite (`./.localdata/db`) and app-data / default blobs (`./.localdata/app-data`, or a custom `GRASP_BLOBS_ROOT`) must be **backed up and cleaned as a pair**; do not migrate only the database. Otherwise composite images can keep orphan `blob:` refs (GET `/api/blobs/:id` → 404). Historical orphans only get a permanent UI placeholder; this delivery does not add an inspection console. See [Quick start](../guide/quick-start.md#database-and-attachments-share-one-lifecycle-backup--cleanup).

## Related

- [Gateway](../gateway/)
- [Contributing](../contributing/)
