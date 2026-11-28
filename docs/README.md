# Grasp project site

The bilingual Grasp homepage and help center build to static files in `public/` and are published by
`ci-docs` to [`cocofhu/approving-pages`](https://github.com/cocofhu/approving-pages)
(`https://www.approving-ai.com/`).

## Layout

| Path | Role |
|------|------|
| `site/` | Styles, browser scripts, images, and the Pages domain binding. Copied into `public/` |
| `scripts/home.mjs` | Shared homepage markup and localized copy |
| `scripts/layout.mjs` | Header, page layouts, navigation, outline, and search dialog |
| `content/` | Help / guide Markdown (`*.md` with YAML front matter). Rendered into `public/` |
| `scripts/build.mjs` | Build HTML and search indexes, then verify local routes, anchors, and assets |
| `reference/` | Localized descriptions for configuration tables. Keys and defaults come from the checked-in configuration references |
| `public/` | Build output (gitignored) |
| `agent/` | In-repo agent handbook ([`agent/README.md`](agent/README.md)). Not published to `public/` |
| `dev/` | In-repo notes and the development log ([`dev/devlog/`](dev/devlog/README.md), one file per entry). Not published to `public/` |

## Local commands

```bash
cd docs
npm ci --no-audit --no-fund
npm run build                 # BASE_PATH=/ (custom domain root)
BASE_PATH=/ npm run server    # local preview at http://localhost:4000
```

## Edit the help center

Each path under `content/` builds to a directory URL. For example,
`content/help/settings.md` becomes `/help/settings/` and its English counterpart
is `content/en/help/settings.md`. Add or update both languages together.

Pages use `title`, `description`, `section` and `order` in YAML front matter.
Use `navTitle` to shorten a navigation label and `keywords` for search aliases.
For links shared across languages, put an explicit ID after the heading
such as `## Sandbox memory {#sandbox-memory}`. Use root-relative site URLs
for cross-page links. The build also resolves relative links to Markdown
source files.

The configuration indexes import keys and defaults from the repository's
`CONFIGURATION.md` and `server/CONFIGURATION.md`. When adding an option or changing
its effect, update the corresponding translated description in `reference/`.
The build checks that each imported option has a translation.

## Deploy prerequisites

See [Contributing → Project site](../CONTRIBUTING.md#project-site-docs): create
`cocofhu/approving-pages`, enable GitHub Pages on `main`/root, add a write
Deploy key on `approving-pages`, and store the private key as Secret
`PAGES_DEPLOY_KEY` on `cocofhu/approving`.
