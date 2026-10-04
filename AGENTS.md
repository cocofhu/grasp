# AGENTS.md — repo contribution rules

**This file is the short, hard contribution / change-code rules for this
repository.** It is for humans and coding agents working on Grasp.

It is **not** `agents/*/workspace/AGENTS.md` (platform role-pack mission and
delivery contracts). Do not mix them; nested role-pack files must not replace
these repo rules.

This file does **not** replace [`CONTRIBUTING.md`](CONTRIBUTING.md),
[`README.md`](README.md), or other encyclopedic docs. Prefer this page for
path→commands, gates, pitfalls, and do-not-touch; use CONTRIBUTING for full
setup and layout.

## Read which doc

| Need | File |
|------|------|
| Commands, gates, pitfalls, do-not-touch | This file |
| Which document to edit, and when to log | [`docs/agent/README.md`](docs/agent/README.md) |
| Internal development log | [`docs/dev/DEVLOG.md`](docs/dev/DEVLOG.md) |
| Environment and full contribution flow | [`CONTRIBUTING.md`](CONTRIBUTING.md) |

Boundaries only — do not copy command blocks into the handbook. See
[`docs/agent/README.md`](docs/agent/README.md).

## Development log (hard rule)

When a change is worth a later reader, append one entry at the **top** of
[`docs/dev/DEVLOG.md`](docs/dev/DEVLOG.md): date, scope (paths), what, why, how
verified. Do not paste that journal into this file.

If the change has **no** user-visible behavior, do **not** edit
[`CHANGELOG.md`](CHANGELOG.md). If it does, still write the DEVLOG entry and
add a separate `Unreleased` bullet in `CHANGELOG.md`.

---

## Directory roles

| Path | Role |
|------|------|
| `server/` | Go backend (FSM, sandbox client, artifact MCP, APIs) |
| `web/` | Vue 3 + Vue Flow UI |
| `sandbox-gateway/gateway/` | Gateway control plane |
| `sandbox-gateway/sandbox/` | Sandbox image (environment + `grasp-bootstrap.sh` only) and the runtime bundle source (`scripts/`, `cmd/`, `internal/`, `web/`) |
| `sandbox-gateway/scripts/` | Sandbox smoke / E2E scripts, cover helpers |
| `scripts/build-sandbox-runtime.sh` | Builds the sandbox runtime bundle (also run by `server/Dockerfile`) |
| `docs/` | Project site (static HTML) + help (Markdown → HTML); CI publishes to `cocofhu/approving-pages` |

Always-on branch-protection job: `.github/workflows/ci.yml` (`gate`). It runs
the brand-text assert, pinned `actionlint` v1.7.12, and error-level
`shellcheck`. It does **not** run module lint, tests, or coverage. Module
suites are path-filtered.

---

## Change path → local commands

Copy from CI. Cross-tree changes: run each matching suite. `ROOT` = repo root.

### Every pull request → `ci` (`gate`)

No path filter. From the repo root:

```bash
./.github/scripts/actionlint.sh          # actionlint v1.7.12, all .github/workflows
./.github/scripts/shellcheck-error.sh    # shellcheck v0.11.0, --severity=error
```

`shellcheck-error.sh` covers repo-root `*.sh`, `server/scripts`, `scripts`,
`sandbox-gateway`, and `.github/scripts`. Warnings do not fail. This job is
not module proof.

### `server/**` or root `.golangci.yml` → `ci-server`

Working directory: `server/` (golangci-lint v2.12, config `$ROOT/.golangci.yml`).

```bash
ROOT="$PWD"   # from repo root
cd server
golangci-lint run --config "$ROOT/.golangci.yml" ./...
go vet ./...
go run ./cmd/gen-configdoc -out CONFIGURATION.md -check
go test ./...
./scripts/cover-check-server.sh 90
go test ./internal/runtime/ -count=1 -run 'TestRunAgent|TestReact'
```

### `web/**` → `ci-web`

Working directory: `web/` (Node 24).

```bash
cd web
npm ci --no-audit --no-fund
npm run lint
npx vue-tsc --noEmit
npm test -- --coverage
npm run build
```

Parallel job `web-e2e` runs `npm run test:e2e:ci` (critical-path Playwright
subset; install Chromium first). Fan-in job `web-gate` (`needs: [web, web-e2e]`,
`if: always()`) fails the workflow if either job failed or was cancelled.

**When this workflow runs, treat these three check names as merge-required:**
`web`, `web-e2e`, `web-gate`. Path filters mean non-web PRs skip `ci-web`; the
always-on `ci` / `gate` job does **not** replace `web-e2e`.

### `sandbox-gateway/gateway/**` → `ci-gateway`

```bash
ROOT="$PWD"
cd sandbox-gateway/gateway
golangci-lint run --config "$ROOT/.golangci.yml" ./...
go vet ./...
cd .. && ./scripts/cover-check.sh gateway 90
```

### `docs/**` → `ci-docs`

Working directory: `docs/` (Node 24). Homepage is static HTML under `site/`;
help pages are Markdown under `content/` (built to `public/`).

```bash
cd docs
npm ci --no-audit --no-fund
npm run build
npm_config_registry=https://registry.npmjs.org npm run audit:check
# optional local preview (root-relative assets):
# BASE_PATH=/ npm run server
```

`audit:check` is the high/critical npm audit (`docs/scripts/audit-check.mjs`,
`docs/audit-allowlist.json`). The same command is the `npm audit (docs)` job
in `security.yml`. An expired allowlist entry fails the command.

On push to `main`, `ci-docs` also runs `.github/scripts/publish-pages.sh` when
Secret `PAGES_DEPLOY_KEY` is set (SSH deploy key with write access on
`cocofhu/approving-pages`).

### `sandbox-gateway/sandbox/**`, `sandbox-gateway/scripts/**`, `scripts/build-sandbox-runtime.sh` or `server/Dockerfile` → `ci-sandbox`

From `sandbox-gateway/`:

```bash
# Shell syntax + smoke (cwd: sandbox-gateway)
bash -n sandbox/scripts/startup.sh
bash -n sandbox/scripts/install-agent.sh
bash -n sandbox/scripts/claude-env.sh
bash -n sandbox/scripts/vnc-preview.sh
bash -n sandbox/scripts/grasp-bootstrap.sh
bash -n sandbox/scripts/services.sh
bash -n scripts/test-runtime-bootstrap.sh
bash -n scripts/test-runtime-e2e.sh
bash -n scripts/test-inject.sh
bash -n scripts/test-git-auth.sh
bash -n scripts/test-agent-connect.sh
bash -n scripts/cover-check-sandbox.sh
node --check scripts/agent-ws-check.mjs
node --check scripts/mock-chat-model.mjs
./scripts/test-inject.sh
./scripts/test-git-auth.sh
./scripts/test-runtime-bootstrap.sh
node --test scripts/mock-chat-model.test.mjs

# Sandbox Go (golangci and go vet from sandbox/; cover from sandbox-gateway/)
ROOT="$PWD/.."   # if cwd is sandbox-gateway; else set to repo root
(cd sandbox && golangci-lint run --config "$ROOT/.golangci.yml" ./...)
(cd sandbox && go vet ./...)
./scripts/cover-check-sandbox.sh 90

# Docker cli-tools stage (glab/gh) — as in ci-sandbox
# docker build --target cli-tools -t universal-sandbox-cli-tools:ci \
#   -f sandbox/Dockerfile sandbox/

# Full image + runtime bundle (slow; job sandbox-images, not the ci.yml gate).
# opencode mock chat always runs and does not need a vendor key.
# CURSOR_API_KEY, when set, still adds the real cursor chat; without it only
# that real chat is skipped. Trae handshake still needs TRAECLI_PERSONAL_ACCESS_TOKEN.
# ../scripts/build-sandbox-runtime.sh /tmp/rt
# docker build -t universal-sandbox:local sandbox/
# ./scripts/test-runtime-e2e.sh universal-sandbox:local /tmp/rt/sandbox-runtime.tgz
# RUNTIME_BUNDLE=/tmp/rt/sandbox-runtime.tgz ./scripts/test-agent-connect.sh universal-sandbox:local
```

---

## Sandbox image and runtime bundle (沙箱镜像与运行时包)

The sandbox image is environment only; Grasp's in-sandbox logic ships as a
runtime bundle served by the server (`GRASP_RUNTIME_URL` at create, SSH push to
running sandboxes). Details: `sandbox-gateway/sandbox/README.md`.

1. Logic changes (sandbox `scripts/`, `cmd/`, `internal/`, `web/`, page scripts)
   go into the runtime bundle: release the server. Do not rebuild the image for them.
2. Only system packages, Agent CLIs, toolchains, code-server, sshd and
   `grasp-bootstrap.sh` need a new sandbox image.
3. When the bundle starts depending on something new in the image, bump
   `sandbox-gateway/sandbox/IMAGE_LEVEL` in the same PR and add it to the image.
4. Do not change `grasp-bootstrap.sh` subcommands or exit codes; if you must,
   bump `IMAGE_LEVEL` too.
5. Release order: server first, then the sandbox image (a new image cannot
   start without a server that sets `GRASP_RUNTIME_URL`).

---

## Quality gates (hard numbers)

| Gate | Threshold / rule | Source |
|------|------------------|--------|
| Server Go coverage | ≥ **90** via `./scripts/cover-check-server.sh 90` | **Core unit-testable package subset**, not full `go test ./...` coverpkg |
| Gateway Go coverage | ≥ **90** via `./scripts/cover-check.sh gateway 90` | `ci-gateway` |
| Sandbox Go coverage | ≥ **90** via `./scripts/cover-check-sandbox.sh 90` | `ci-sandbox` |
| Web Lines coverage | ≥ **85** | `web/vite.config.ts` → `thresholds.lines` |
| golangci-lint | v2.12, root `.golangci.yml`: staticcheck SA*/S1*, errcheck, unused; `_test.go` excluded | ci-server / ci-gateway / ci-sandbox |
| go vet | `go vet ./...` in server, gateway, and `sandbox-gateway/sandbox` | ci-server / ci-gateway / ci-sandbox `sandbox-go` |
| actionlint | v1.7.12 via `./.github/scripts/actionlint.sh` | always-on `ci` |
| shellcheck | v0.11.0, `--severity=error`, via `./.github/scripts/shellcheck-error.sh` | always-on `ci` |
| govulncheck | v1.1.4 via `./.github/scripts/govulncheck-check.sh` on Go 1.25.x (latest patch) | `security.yml` |
| docs npm audit | `npm run audit:check` in `docs/`, high/critical, official registry | `security.yml` |
| ESLint | `npm run lint` — **errors** fail; warnings allowed | `ci-web` |
| vue-tsc | `npx vue-tsc --noEmit` | `ci-web` |
| gen-configdoc | `go run ./cmd/gen-configdoc -out CONFIGURATION.md -check` | `ci-server` |

Do not invent new thresholds. Badge color bands on orphan `coverage-badges` are
not a substitute for these gates.

---

## Before opening a PR

- [ ] Run the local gates for every tree you touched (cross-tree → run each suite).
- [ ] Do not commit secrets, local config, generated artifacts, or org-private URLs.
- [ ] Land via PR into `main`; do not push protected `main` directly.
- [ ] Commands and thresholds were not invented — verified against `ci-*.yml`,
      cover scripts, and `web/vite.config.ts`.

---

## Known pitfalls (short)

1. **Pending gates UI after approve** — Optimistic remove + invalidate generation +
   drop stale peek, or ghost rows / badge bounce. (PR #20 · `usePendingGates` /
   `GatesInboxView`)
2. **cron `NextScheduleTime` → UTC** — After IANA wall-clock math, persist with
   `.UTC()`; SQLite compares `next_run_at` as UTC text. (PR #19 · `schedule.go`)
3. **`submit_mr` list-first idempotency** — List open → create → parse
   already-exists / no-commits-between; non-zero create must not mean hard fail;
   idempotent success may leave empty `mr_url`. (PR #13 · git SKILL)
4. **Path-filtered CI blind spot** — Docs-only / root-only / cross-module PRs may
   skip module jobs. The always-on `ci` gate runs actionlint and shellcheck,
   and is still not module proof. Still run local suites for trees you actually touched.
5. **Runtime hot update skips one-time startup** — Pushing a bundle restarts
   only `backend` / `preview-inject` (`services.sh`); one-time `startup.sh`
   steps (credentials, clone, dockerd, code-server config) apply to new sandboxes only.
6. **Page script copies** — After editing `live-overlay.js` (and other page
   scripts) run `npm run build:live-overlay` in `web/`; it syncs the three copies
   and `assertCopiesInSync` checks them.

---

## Do not touch

1. Secrets, local config, generated artifacts, org-private URLs.
2. `.github/workflows` and publish/image contracts unless the task explicitly requires it.
3. Orphan `coverage-badges` branch payload.
4. Direct push to protected `main`.
5. Treat or rewrite `agents/*/workspace/AGENTS.md` as repo contribution rules.
