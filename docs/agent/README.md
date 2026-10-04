# Agent documentation boundaries

This handbook tells a coding agent **which file to read or edit**. It does not
replace the short command list. Commands, coverage thresholds, known pitfalls,
and the do-not-touch list live only in the repository root
[`AGENTS.md`](../../AGENTS.md). Do not copy those blocks into this file.

`docs/scripts/build.mjs` copies `docs/site/` and renders `docs/content/` into
`public/`. This directory and `docs/dev/` are **not** inputs to that build and
are **not** published to the public help site.

## Which file is which

| Path | What it is | Do not use it for |
|------|------------|-------------------|
| [`AGENTS.md`](../../AGENTS.md) (repository root) | Short rules for humans and coding agents: directory roles, path→local commands, gates, pitfalls, do-not-touch | Role-pack missions, full environment setup, or a running journal |
| [`docs/agent/README.md`](README.md) (this file) | Boundaries between those documents, and when to write the internal log | A second copy of commands or thresholds |
| [`CONTRIBUTING.md`](../../CONTRIBUTING.md) | Environment, layout, and the full contribution flow | The hard command and gate checklist (that stays in root `AGENTS.md`) |
| `agents/<Role>/workspace/AGENTS.md` | Platform role-pack mission and the single delivery contract for that role | Repository contribution rules. Do not replace or rewrite the root `AGENTS.md` with a role pack. Embedded copies under `server/internal/services/team_embed` and `first_install_embed` are the same texts — leave them alone unless the task is the role pack itself |
| [`docs/dev/DEVLOG.md`](../dev/DEVLOG.md) | Internal development log (Chinese). Newest entry on top | Public release notes |

Same filename, different jobs: root `AGENTS.md` is the repo rule. A role-pack
`AGENTS.md` is not.

## Development log

After a change worth a later reader, append one entry at the top of
[`docs/dev/DEVLOG.md`](../dev/DEVLOG.md). Use the template in that file. Each
entry needs:

- **日期** — the day the note was written, not a schedule
- **范围** — paths touched
- **做了什么** — what changed
- **为什么** — why
- **如何验证** — how it was checked

Do not paste that journal into root `AGENTS.md`. Keep the root file a short
entry point.

## What does not go in CHANGELOG

[`CHANGELOG.md`](../../CHANGELOG.md) records **public-release** behavior only.
A docs-only or internal change still gets a DEVLOG line and does **not** get a
`CHANGELOG.md` line.

If a change also changes behavior users see, keep the DEVLOG entry and, under
the existing changelog practice, add a separate `Unreleased` bullet in
`CHANGELOG.md`. This handbook does not change that practice.
