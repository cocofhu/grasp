# Contributing to Grasp

Short, hard Agent/contribution rules (path→commands, gates, pitfalls, do-not-touch):
see [`AGENTS.md`](AGENTS.md).

Before contributing, read [`CODE_OF_CONDUCT.md`](CODE_OF_CONDUCT.md) and
[`SECURITY.md`](SECURITY.md).

## Development setup

- Backend: `cd server && go test ./...`
- Web: `cd web && npm ci && npm test && npx vue-tsc --noEmit`
- Web critical e2e (same subset as CI `web-e2e`): see [Critical-path Playwright e2e](#critical-path-playwright-e2e)
- Docs site: `cd docs && npm ci && npm run build` (preview: `BASE_PATH=/ npm run server`)
- Configuration doc: `cd server && go run ./cmd/gen-configdoc -check`
- Full local development stack: `./start.sh dev -d` (source/HMR; gateway
  sources live in `sandbox-gateway/`). Default `./start.sh -d` pulls published
  GHCR images via `compose.release.yaml`.

### Static checks (lint)

CI appends these gates alongside existing vet/tests/`vue-tsc` (nothing is
replaced). Use the same commands locally before opening a PR.

**Go** — shared root config [`.golangci.yml`](.golangci.yml) (golangci-lint v2.12).
Enabled on non-test code: `staticcheck` SA*/S1*, `errcheck`, and `unused`.
`_test.go` stays excluded. Install the v2.12 binary
(`go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.12.0`
or the [official releases](https://golangci-lint.run/docs/welcome/install/)),
then from each module directory:

```bash
# from repo root
ROOT="$PWD"
(cd server && golangci-lint run --config "$ROOT/.golangci.yml" ./...)
(cd sandbox-gateway/gateway && golangci-lint run --config "$ROOT/.golangci.yml" ./...)
(cd sandbox-gateway/sandbox && golangci-lint run --config "$ROOT/.golangci.yml" ./...)
```

`go vet ./...` runs in `ci-server`, `ci-gateway`, and the `sandbox-go` job of
`ci-sandbox` (from `sandbox-gateway/sandbox`).

Matching workflows: `ci-server`, `ci-gateway`, `ci-sandbox` (sandbox-go job).

**Workflows and shell** — always-on [`.github/workflows/ci.yml`](.github/workflows/ci.yml)
(every pull request and push to `main`, no path filter). These do not replace
module tests or coverage.

```bash
# from repo root; downloads pinned actionlint v1.7.12 and shellcheck v0.11.0
./.github/scripts/actionlint.sh
./.github/scripts/shellcheck-error.sh
```

`actionlint` checks every file under `.github/workflows`. `shellcheck-error.sh`
runs shellcheck `--severity=error` on repo-root `*.sh`, `server/scripts`,
`scripts`, `sandbox-gateway`, and `.github/scripts`. Warnings do not fail.
An exemption is a `# shellcheck` directive with a reason comment, and it must
not change the script's behavior. A workflow syntax or expression error, or a
shell error, fails the job. There is no `continue-on-error` on these steps.

**Web** — ESLint (flat config in `web/`) coexists with `vue-tsc` (types stay on
`vue-tsc`; ESLint is not type-aware in this first pass). CI fails on ESLint
**errors** only; warnings are allowed:

```bash
cd web && npm ci && npm run lint && npx vue-tsc --noEmit
```

Matching workflow: `ci-web` (`web` job).

### Critical-path Playwright e2e

When `web/**` (or `ci-web.yml` / coverage badge scripts) change, `ci-web` runs
three checks in parallel / fan-in:

| Check (job name) | Role |
|------------------|------|
| `web` | lint, vue-tsc, unit+coverage, build |
| `web-e2e` | critical-path Playwright subset (mock vite.e2e harness; no real backend) |
| `web-gate` | always-on fan-in of `web` + `web-e2e`; fails if either failed/cancelled |

**Required for merge when this workflow runs:** `web`, `web-e2e`, and `web-gate`.
Non-web path changes do not trigger `ci-web`, so these three are not in play;
the always-on `ci` / `gate` job is **not** a substitute for `web-e2e`.

`test:e2e:ci` specs (target wall time usually under 5–8 minutes with
`workers: 1`; not the full suite):

- `gate-mobile-fill.spec.ts` — gate mobile approve / reject
- `clarify-inbox-product.spec.ts` — clarify inbox product surface
- `delete-run-list.spec.ts` — run list delete / cancel / placeholders
- `cancel-run.spec.ts` — cancel run across statuses
- `agent-create-wizard.spec.ts` — agent create wizard happy path
- `board-token-stats.spec.ts` — board / token stats incl. narrow viewport
- `run-detail-mobile.spec.ts` — run detail mobile layout (assertions aligned to
  5 KPI cards: wall / node-sum / gap / total-tokens / token-rate; trim further
  if CI flake appears)

Reproduce locally (same entry as CI):

```bash
cd web && npm ci && npx playwright install chromium && npm run test:e2e:ci
```

Full suite remains `npm run test:e2e` (not run in PR CI). On `web-e2e`
failure, Actions uploads `playwright-report/` and `test-results/` artifacts.
Do **not** put the full suite or whole `project-detail.spec.ts` into
`test:e2e:ci`.

**Branch protection (maintainers):** mark `web`, `web-e2e`, and `web-gate` as
required status checks for PRs into `main` (rulesets / classic protection).
Code-side `web-gate` still fails the workflow when e2e fails even if protection
is not updated yet.

Further tightening (more Go linters, stricter ESLint rules, optional type-aware
ESLint) can ratchet without changing this layout. Do not raise the coverage
numbers below, and do not put the full Playwright suite or `release-smoke` on
every pull request.

Never commit credentials, local configuration, generated databases, or
organization-only URLs to public examples.

## Repository layout

```
approving/
├── server/                 Go backend (FSM + sandbox client + artifact MCP)
├── web/                    Vue3 + Vue Flow UI
├── sandbox-gateway/        Vendored gateway + universal sandbox image
├── docs/                   Project site (static HTML) + help (Markdown)
├── docker-compose.yml      Dev/source stack (./start.sh dev)
├── start.sh                Default: pull GHCR + up; dev/source via ./start.sh dev
├── compose.release.yaml    Published-image stack (./start.sh default)
├── release-smoke.sh        Clean-Linux release smoke
├── GATEWAY.md              Gateway contract
└── .github/                Issues, PRs, Actions
```

### Project site (`docs/`)

Static HTML homepage in [`docs/site/`](docs/site/); help/guide pages are Markdown
in [`docs/content/`](docs/content/). `npm run build` writes `docs/public/`.

Matching workflow: `ci-docs`. On push to `main`, the job publishes `public/` to
the standalone Pages repo [`cocofhu/approving-pages`](https://github.com/cocofhu/approving-pages)
when Secret `PAGES_DEPLOY_KEY` is configured.

**One-time GitHub setup (maintainers):**

1. Create public empty repo `cocofhu/approving-pages`.
2. Settings → Pages → Deploy from branch `main` / root (or `/ (root)`).
3. Generate an ed25519 keypair for publish only, e.g.
   `ssh-keygen -t ed25519 -C approving-pages-deploy -f approving-pages-deploy -N ""`.
4. On `cocofhu/approving-pages`, add the **public** key as a Deploy key with
   **Allow write access**.
5. On `cocofhu/approving`, add the **private** key as Secret `PAGES_DEPLOY_KEY`.
6. Optional: set the Grasp repo Homepage to
   `https://www.approving-ai.com/`.

## Release images and smoke

Pushing a `v*` tag runs:

- `publish-image` → `ghcr.io/cocofhu/grasp`
- `publish-gateway` → `ghcr.io/cocofhu/sandbox-gateway`
- `publish-sandbox` → `ghcr.io/cocofhu/universal-sandbox`

A release is complete only when all three workflows succeed. If any job fails,
re-run the failed workflow via **Actions → workflow_dispatch** (e.g. re-run
`publish-sandbox` for an existing `v*` tag).

Sandbox builds are large (often 30–90+ minutes). Packages may start private;
set them Public under GitHub → Packages if anonymous pulls are required.

Default tags used by `./start.sh` (overridable in `.env`):

- `ghcr.io/cocofhu/grasp:1.2.1`
- `ghcr.io/cocofhu/sandbox-gateway:1.2.1`
- `ghcr.io/cocofhu/universal-sandbox:1.2.1`
  (one image for every `acpBackend`; `SANDBOX_IMAGE` / `GRASP_SANDBOX_IMAGE` pin or override it — used by release-smoke).

### release-smoke (manual; not a PR required check)

Workflow: `.github/workflows/release-smoke.yml`.

| Trigger | Behavior |
|---------|----------|
| `workflow_dispatch` | Intended entry: run after digest-pinned GHCR images exist |
| `v*` / other tags | **Not** wired on purpose — beta tags without release secrets would fail red noise |
| Pull requests | **Not** triggered — do not add as a per-PR required check |

The job needs repository secrets `GRASP_IMAGE`, `SANDBOX_GATEWAY_IMAGE`,
and `SANDBOX_IMAGE` (each a digest-pinned reference such as
`ghcr.io/...@sha256:...`). It pulls multi-GB images, runs `./release-smoke.sh`,
and uploads `release-evidence/`. That cost is why smoke stays manual: do **not**
run full image pulls on every PR unless a future lightweight mode exists.

Local equivalent after images are available:

```bash
export GRASP_IMAGE='ghcr.io/cocofhu/grasp@sha256:...'
export SANDBOX_GATEWAY_IMAGE='ghcr.io/cocofhu/sandbox-gateway@sha256:...'
export SANDBOX_IMAGE='ghcr.io/cocofhu/universal-sandbox@sha256:...'
# release-smoke.sh exports GRASP_SANDBOX_IMAGE=$SANDBOX_IMAGE.
./release-smoke.sh
```

Optional future: a weekly/nightly `schedule` for maintainers — not required for
merge quality gates today.

Dev-only local sandbox image:

```bash
./start.sh sandbox       # build universal-sandbox:local
```

### Sandbox agent chat check

`ci-sandbox` job `sandbox-images` runs `sandbox-gateway/scripts/test-agent-connect.sh`.
That script always runs one opencode chat against a host-side mock chat model
(fixture `ci-e2e`, assistant text `GRASP_AGENT_E2E_OK`, `finish_reason=stop`).
The container reaches the mock through `host.docker.internal`. No
`CURSOR_API_KEY` is required for this path. When that key is set, the script
still runs the original real cursor chat as well; without the key only the
real cursor chat is skipped. Trae still skips its handshake unless
`TRAECLI_PERSONAL_ACCESS_TOKEN` is set. This chat check is not part of the
always-on `.github/workflows/ci.yml` gate.

The mock contract test does not build `universal-sandbox`. From
`sandbox-gateway/`:

```bash
node --test scripts/mock-chat-model.test.mjs
```

Same mock chat as CI, after the image and runtime bundle exist:

```bash
cd sandbox-gateway
../scripts/build-sandbox-runtime.sh /tmp/rt
docker build -t universal-sandbox:local sandbox/
RUNTIME_BUNDLE=/tmp/rt/sandbox-runtime.tgz ./scripts/test-agent-connect.sh universal-sandbox:local
```

## Security scans (CodeQL and friends)

Workflow: `.github/workflows/security.yml` (push to `main`, every PR, weekly
schedule). Jobs: CodeQL (go + javascript-typescript), `npm audit` (web and
docs, high+), `govulncheck` (server, gateway, sandbox), gitleaks. New jobs do
not read repository secrets. A failed audit or vuln scan fails the job; none
of them set `continue-on-error`.

- `npm audit` runs as `npm run audit:check` in `web/` and in `docs/` (official
  registry `https://registry.npmjs.org`). A high/critical advisory with **no
  patched release** that only reaches dev/build tooling may be added to
  `web/audit-allowlist.json` or `docs/audit-allowlist.json` with `id`,
  `reason`, and `expires` (a few months out). Expired entries fail the check
  even after npm stops reporting them. Remove the entry once a fix ships.

```bash
cd docs && npm_config_registry=https://registry.npmjs.org npm run audit:check
```

- `govulncheck` v1.1.4 scans `server`, `sandbox-gateway/gateway`, and
  `sandbox-gateway/sandbox` for called-symbol vulnerabilities. Use Go 1.26.x
  with a patch of at least 1.26.9 and below 1.27 — the same line that compiles
  the binaries. `actions/setup-go` `go-version: "1.26.x"` still resolves to
  1.26.8 from the actions/go-versions manifest, so CI pins `1.26.9` (an exact
  version missing from that manifest is downloaded from go.dev/dl). An older
  1.26 patch still reports the 2026-10-08 standard-library findings. Called
  findings fail unless listed in `govulncheck-allowlist.json` with `id`,
  `module`, `reason`, and `expires`. Expired entries fail the check. The
  allowlist is empty; do not exempt findings fixed by this toolchain or by
  `golang.org/x/net` v0.60.0 and `golang.org/x/crypto` v0.57.0.

```bash
# from repo root, with Go 1.26.9 or a newer 1.26 patch on PATH
./.github/scripts/govulncheck-check.sh
```

- A failing CodeQL **analyze** job turns the corresponding PR check red.
- **Job green ≠ default branch has zero open alerts.** Historical / residual
  findings can remain under Security → Code scanning after analyze succeeds.
  After merging security-sensitive changes, spot-check that UI (acceptance item
  for maintainers).
- Easy residual patterns: incomplete multi-character sanitization (e.g. strip
  tags with `/<[^>]+>/g` then re-interpret HTML — see
  `web/src/lib/highlightJson.test.ts`); boolean / flag “sanitizers” that do not
  break taint for CodeQL; DOM reinterpret after encode. Prefer sink hardening
  and fixture tests over dismissing alerts.
- CodeQL is **not** currently a branch-protection required check; treat
  post-merge scanning review as complementary to CI green.

## Coverage badges

README shows `coverage-web` / `coverage-sandbox` / `coverage-server` /
`coverage-gateway` via shields.io Endpoint Badges. Endpoint JSON lives on the
orphan `coverage-badges` branch and is updated only when the corresponding
workflow succeeds on the default branch (`ci-web` / `ci-sandbox` / `ci-server` /
`ci-gateway`). Failed or skipped coverage runs do not overwrite the last
successful value. Color bands: ≥85% green, 70–84% yellow, below 70% orange;
cold start is `n/a` / lightgrey.

Because those workflows are path-filtered, a module badge stays at its last
successful percent until that module’s paths change again — expected lag, not a
badge outage. There is no coverage SaaS.

## Changes and pull requests

1. Create a focused branch from the current default branch.
2. Add tests for behavior changes and update `README.md` /
   `server/README.md` / `GATEWAY.md` / `server/CONFIGURATION.md` when public
   behavior, commands, configuration, or links change.
3. Run the relevant checks above. Generated configuration docs must be current
   (`go run ./cmd/gen-configdoc`).
4. Use a concise commit subject describing why the change is needed.
5. Open a pull request with the problem, solution, risk, and test evidence.

Keep pull requests reviewable. Avoid unrelated formatting or refactors.
Maintainers may ask for a smaller follow-up when a proposal crosses security,
compatibility, or release-contract boundaries.

## Reporting problems

Use the issue templates for reproducible defects and feature proposals. Do not
include secrets or vulnerability details. Security reports follow
[`SECURITY.md`](SECURITY.md).
