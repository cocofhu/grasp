---
title: Configuration
description: Configuration highlights; full details live in source CONFIGURATION.md.
---

Grasp platform service configuration is primarily YAML / environment variables (local examples: `server/config.example.yaml` and the root `.env.example`). Runtime credentials such as ACP and Git credentials are managed only in the project's credential UI.

## Full documentation

Treat the in-repo docs as authoritative (avoids drift between this site and source):

- [server/CONFIGURATION.md](https://github.com/cocofhu/approving/blob/main/server/CONFIGURATION.md)

That document is generated/checked by `go run ./cmd/gen-configdoc`; CI runs `-check`.

## Local quick path

```bash
./start.sh -d          # published image stack
./start.sh dev -d      # source + HMR
```

Image tags / digests, gateway, and sandbox-related variables are in `.env.example`. Agent API keys, GitHub / GitLab tokens, SSH keys, and similar credentials can only be saved in the project's credential UI; putting them in project-shared, Agent meta, or platform `sandbox.env` is rejected. Non-secret options such as sites and models may still go in Agent meta env (values may reference `${vars.<name>}`).

## Database and attachment lifecycle

In the release stack, SQLite (`./.localdata/db`) and app-data / default blobs (`./.localdata/app-data`, or a custom `GRASP_BLOBS_ROOT`) must be **backed up and cleaned as a pair**; do not migrate only the database. Otherwise composite images can keep orphan `blob:` refs (GET `/api/blobs/:id` → 404). Historical orphans only get a permanent UI placeholder; this delivery does not add an inspection console. See [Quick start](../guide/quick-start.md#database-and-attachments-share-one-lifecycle-backup--cleanup).

## Related

- [Gateway](../gateway/)
- [Contributing](../contributing/)
