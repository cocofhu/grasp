# Grasp project site

Static HTML homepage + Markdown help, built to `public/` and published by
`ci-docs` to [`cocofhu/approving-pages`](https://github.com/cocofhu/approving-pages)
(`https://www.approving-ai.com/`).

## Layout

| Path | Role |
|------|------|
| `site/` | Static assets and homepage (`index.html`, CSS, JS). Copied into `public/` |
| `content/` | Help / guide Markdown (`*.md` with YAML front matter). Rendered into `public/` |
| `scripts/build.mjs` | Copy `site/` → `public/`, render Markdown → HTML. Does not read `agent/` or `dev/` |
| `public/` | Build output (gitignored) |
| `agent/` | In-repo agent handbook ([`agent/README.md`](agent/README.md)). Not published to `public/` |
| `dev/` | In-repo notes and the development log ([`dev/DEVLOG.md`](dev/DEVLOG.md)). Not published to `public/` |

## Local commands

```bash
cd docs
npm ci --no-audit --no-fund
npm run build                 # BASE_PATH=/ (custom domain root)
BASE_PATH=/ npm run server    # local preview at http://localhost:4000
```

## Deploy prerequisites

See [Contributing → Project site](../CONTRIBUTING.md#project-site-docs): create
`cocofhu/approving-pages`, enable GitHub Pages on `main`/root, add a write
Deploy key on `approving-pages`, and store the private key as Secret
`PAGES_DEPLOY_KEY` on `cocofhu/approving`.
