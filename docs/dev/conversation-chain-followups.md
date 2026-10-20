# Conversation chain (#850) follow-ups

Items intentionally left out of `GET /api/runs/:id/conversation-chain`.

## Pagination
Not implemented. A page cannot be computed in isolation:
- each `task` needs the outputs of every earlier execution;
- gate turns are attributed across segments (producer conversation → later
  gate visit);
- `until` already cuts the chain.

A `limit` / cursor would therefore still read the whole run and only trim the
response. After the count-only queries, 300 segments take about 65 ms with
about 63 KB of output (see the PR). Revisit if real runs grow past about 1000
executions, e.g. `?limit=N` returning the last N segments plus a `truncated`
flag.

## Task previews use current artifact content
`artifact()` / `json()` / `exists()` in a node prompt read the artifact's
**current** content: the store keeps no per-visit snapshot. An older
execution's `task` can therefore show a newer artifact. Each artifact is read
at most once per request. Fixing this needs a per-visit artifact revision
lookup (`ArtifactBaseRev` + `ArtifactVersion`).

## Secrets outside task previews
`task` masks variables flagged `secret` as `•••`. `RunVariable.secret` is
carried over from secret workspace variables. These surfaces are already
plaintext and were not changed:
- `GET /runs/:id/variables`;
- `prompt` events;
- `varsBefore` / `varsSnapshot`.

Masking them is a separate, cross-cutting change.

## Turn → gate attribution for legacy rows
Gate hot-revise turns written since this change carry `gateNodeId`. Older rows
are attributed by time window (a later second than the gate visit's start and,
for a finished gate, not after its end).
