---
title: Contributing
description: How to contribute to Grasp; points to the repository contributing guide.
section: support
order: 20
---

Contributions are welcome. Please read:

- [CODE_OF_CONDUCT.md](https://github.com/cocofhu/approving/blob/main/CODE_OF_CONDUCT.md)
- [SECURITY.md](https://github.com/cocofhu/approving/blob/main/SECURITY.md)
- [CONTRIBUTING.md](https://github.com/cocofhu/approving/blob/main/CONTRIBUTING.md)

Short rules for agents / contributors (path→commands, gates, do-not-touch lists):

- [AGENTS.md](https://github.com/cocofhu/approving/blob/main/AGENTS.md)

## Local build for this site (`docs/`)

```bash
cd docs
npm ci --no-audit --no-fund
npm run build
```

Preview:

```bash
BASE_PATH=/ npm run server
```

Output lands in `docs/public/`. When changes under `docs/**` land on `main`, `ci-docs` builds and deploys to the separate Pages repo `cocofhu/approving-pages` (requires Secret `PAGES_DEPLOY_KEY`).

## Related module gates

When you change `server/`, `web/`, or `sandbox-gateway/`, run the matching local suites from `AGENTS.md` — do not rely only on the always-on `ci` gate.
