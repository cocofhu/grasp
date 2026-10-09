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

## Optional web search with Parallel (OpenCode)

[Parallel Search MCP](https://docs.parallel.ai/integrations/mcp/search-mcp) provides `web_search` and `web_fetch` over Streamable HTTP without a Parallel API key. The anonymous tier is free for light use and has rate limits.

In Agent Studio, select an agent using the **OpenCode** backend and open its **MCP** panel. Add a custom server named `parallel-search`, select **HTTP (url)**, set the URL to `https://search.parallel.ai/mcp`, and add the header `User-Agent: grasp/parallel-search-example`. Save the agent, then start a fresh sandbox session so it loads the updated configuration. Keep the agent's existing MCP entries, including any platform tools it needs.

The equivalent entry to append to the MCP panel's **Raw JSON** array is:

```json
{
  "name": "parallel-search",
  "url": "https://search.parallel.ai/mcp",
  "headers": {
    "User-Agent": "grasp/parallel-search-example"
  }
}
```

The panel expects an array of entries, so append this object to the existing array rather than replacing it with a `mcpServers` wrapper. Grasp translates the entry into OpenCode's remote MCP configuration; no additional MCP package or Parallel credential is needed in the sandbox. The agent still needs its usual model configuration.

Try a task such as: “Use parallel-search to find the Go release notes at go.dev, then fetch https://go.dev/doc/devel/release and summarize the supported releases with source URLs.” The tool names exposed by OpenCode have the server prefix (`parallel-search_web_search` and `parallel-search_web_fetch`). Search accepts `objective` and `search_queries`; fetch accepts `urls` and an optional `objective`. Ask for excerpts first to keep the output small.

This example is opt-in and does not change the backend or enable search for other agents. To remove it, delete only the `parallel-search` entry and start a fresh sandbox session. If a tool call is rate limited, wait for the server's retry interval before trying again.

## Related

- [Gateway](../gateway/)
- [Contributing](../contributing/)
