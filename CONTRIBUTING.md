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

**Go** — shared root config [`.golangci.yml`](.golangci.yml) (golangci-lint v2,
`staticcheck` SA*/S1*, `_test.go` excluded for the first pass). Install a v2.x
binary (`go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.12.0`
or the [official releases](https://golangci-lint.run/docs/welcome/install/)),
then from each module directory:

```bash
# from repo root
ROOT="$PWD"
(cd server && golangci-lint run --config "$ROOT/.golangci.yml" ./...)
(cd sandbox-gateway/gateway && golangci-lint run --config "$ROOT/.golangci.yml" ./...)
(cd sandbox-gateway/sandbox && golangci-lint run --config "$ROOT/.golangci.yml" ./...)
```

Matching workflows: `ci-server`, `ci-gateway`, `ci-sandbox` (sandbox-go job).

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

Later tightening (more Go linters, stricter ESLint rules, optional type-aware
ESLint) can ratchet without changing this layout; not required for this pass.

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

- `ghcr.io/cocofhu/grasp:1.1.0`
- `ghcr.io/cocofhu/sandbox-gateway:1.1.0`
- `ghcr.io/cocofhu/universal-sandbox:1.1.0`
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

## Security scans (CodeQL and friends)

Workflow: `.github/workflows/security.yml` (push to `main`, every PR, weekly
schedule). Jobs: CodeQL (go + javascript-typescript), `npm audit` (web, high+),
gitleaks.

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
